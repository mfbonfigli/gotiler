// Package store keeps splats in temporary files of fixed-size records, so
// that tiling can stream inputs larger than memory.
//
// Centers are stored as float64, so that inputs far from their origin keep
// their precision, and every other value as float32.
package store

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"

	"github.com/mfbonfigli/gotiler/v3/internal/geom"
	"github.com/mfbonfigli/gotiler/v3/internal/splat"
	"github.com/mfbonfigli/gotiler/v3/internal/utils"
)

// BatchSize is the most splats Reader.Next returns at a time.
const BatchSize = 4096

const (
	// baseRecordSize holds a float64 center, a float32 rotation, scale,
	// opacity and SH0, and an int64 source index. Each higher-degree
	// spherical-harmonic coefficient adds three float32 values.
	baseRecordSize = 3*8 + (4+3+1+3)*4 + 8

	// Tiling keeps a writer open for each of many tiles at once, so writers
	// use small buffers.
	writerBufferSize = 64 << 10
)

// batchBytes holds the bytes of one batch of records being decoded. Its
// capacity fits a batch of degree-3 splats.
var batchBytes = utils.NewSlicePool[byte](BatchSize * RecordSize(15))

// RecordSize returns the size in bytes of a stored splat with shCount
// higher-degree spherical-harmonic coefficients.
func RecordSize(shCount int) int {
	return baseRecordSize + shCount*3*4
}

// Writer appends splats to a store. It is not safe for concurrent use.
type Writer struct {
	file    *os.File
	buffer  *bufio.Writer
	record  []byte
	shCount int
	count   int
}

// Create creates or truncates the store at path, for splats with shCount
// higher-degree spherical-harmonic coefficients.
func Create(path string, shCount int) (*Writer, error) {
	return openWriter(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, shCount)
}

// Append opens the store at path to add splats after the ones it holds,
// creating it if needed.
func Append(path string, shCount int) (*Writer, error) {
	return openWriter(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, shCount)
}

func openWriter(path string, flag, shCount int) (*Writer, error) {
	file, err := os.OpenFile(path, flag, 0o644)
	if err != nil {
		return nil, err
	}
	return &Writer{
		file:    file,
		buffer:  bufio.NewWriterSize(file, writerBufferSize),
		record:  make([]byte, RecordSize(shCount)),
		shCount: shCount,
	}, nil
}

// Write appends splats to the store. Each splat must have the number of
// coefficients the store was opened for.
func (w *Writer) Write(splats []splat.Splat) error {
	for i, s := range splats {
		if len(s.SHRest) != w.shCount {
			return fmt.Errorf("splat %d has %d spherical-harmonic coefficients, but the store holds %d", i, len(s.SHRest), w.shCount)
		}
		encodeRecord(w.record, s)
		if _, err := w.buffer.Write(w.record); err != nil {
			return err
		}
		w.count++
	}
	return nil
}

// Count returns how many splats this writer has written.
func (w *Writer) Count() int {
	return w.count
}

// Close flushes the buffered splats and closes the file. Closing a closed
// writer does nothing.
func (w *Writer) Close() error {
	if w.file == nil {
		return nil
	}
	flushErr := w.buffer.Flush()
	closeErr := w.file.Close()
	w.file = nil
	if flushErr != nil {
		return flushErr
	}
	return closeErr
}

// Reader reads the splats of a store in batches. It is not safe for
// concurrent use, but several readers can read the same store at once.
type Reader struct {
	file       *os.File
	buffer     *bufio.Reader
	shCount    int
	count      int
	remaining  int
	recordSize int
	shRest     []geom.Vec3
}

// Open opens the store at path to read all of its splats.
func Open(path string, shCount int) (*Reader, error) {
	return OpenRange(path, shCount, 0, -1)
}

// OpenRange opens the store at path to read count splats from the one at
// index start. A negative count reads to the end of the store.
func OpenRange(path string, shCount, start, count int) (*Reader, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	r, err := newReader(file, shCount, start, count)
	if err != nil {
		file.Close() //nolint:errcheck
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return r, nil
}

func newReader(file *os.File, shCount, start, count int) (*Reader, error) {
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	recordSize := RecordSize(shCount)
	if info.Size()%int64(recordSize) != 0 {
		return nil, fmt.Errorf("the store holds %d bytes, which is not a whole number of %d-byte splat records", info.Size(), recordSize)
	}
	total := int(info.Size() / int64(recordSize))
	if count < 0 {
		count = total - start
	}
	if start < 0 || count < 0 || start+count > total {
		return nil, fmt.Errorf("cannot read %d splats from index %d: the store holds %d", count, start, total)
	}
	if _, err := file.Seek(int64(start)*int64(recordSize), io.SeekStart); err != nil {
		return nil, err
	}
	return &Reader{
		file:       file,
		buffer:     bufio.NewReaderSize(file, 256<<10),
		shCount:    shCount,
		count:      count,
		remaining:  count,
		recordSize: recordSize,
	}, nil
}

// Count returns how many splats the reader reads in total.
func (r *Reader) Count() int {
	return r.count
}

// Next returns the next batch of up to BatchSize splats, reusing the memory
// of dest. It returns no splats and io.EOF once all of them are read.
//
// The SHRest slices of a batch share memory that the next call overwrites:
// copy them to keep the splats past that call.
func (r *Reader) Next(dest []splat.Splat) ([]splat.Splat, error) {
	n := min(r.remaining, BatchSize)
	if n == 0 {
		return dest[:0], io.EOF
	}
	if r.shRest == nil {
		r.shRest = make([]geom.Vec3, BatchSize*r.shCount)
	}
	dest = resize(dest, n)
	if err := r.decode(dest, r.shRest); err != nil {
		return dest[:0], err
	}
	r.remaining -= n
	return dest, nil
}

// decode fills splats from the next len(splats) records, slicing their SHRest
// from shRest. Splats without higher-degree coefficients get a nil SHRest,
// as the PLY reader gives them.
func (r *Reader) decode(splats []splat.Splat, shRest []geom.Vec3) error {
	bytesPtr := batchBytes.GetWithMinCapacity(len(splats) * r.recordSize)
	defer batchBytes.Put(bytesPtr)
	data := *bytesPtr
	if _, err := io.ReadFull(r.buffer, data); err != nil {
		return err
	}
	for i := range splats {
		var coefficients []geom.Vec3
		if r.shCount > 0 {
			coefficients = shRest[i*r.shCount : (i+1)*r.shCount : (i+1)*r.shCount]
		}
		splats[i] = decodeRecord(data[i*r.recordSize:], coefficients)
	}
	return nil
}

// Close closes the file.
func (r *Reader) Close() error {
	return r.file.Close()
}

// ReadAll returns all the splats of the store at path, each with SHRest
// memory of its own.
func ReadAll(path string, shCount int) ([]splat.Splat, error) {
	r, err := Open(path, shCount)
	if err != nil {
		return nil, err
	}
	defer r.Close() //nolint:errcheck
	splats := make([]splat.Splat, r.count)
	shRest := make([]geom.Vec3, r.count*shCount)
	for start := 0; start < r.count; start += BatchSize {
		end := min(start+BatchSize, r.count)
		if err := r.decode(splats[start:end], shRest[start*shCount:end*shCount]); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
	}
	return splats, nil
}

// resize returns s with length n, reallocated only if it is too small.
func resize(s []splat.Splat, n int) []splat.Splat {
	if cap(s) < n {
		return make([]splat.Splat, n)
	}
	return s[:n]
}

func encodeRecord(b []byte, s splat.Splat) {
	putFloat64s(b[0:], s.Center.X, s.Center.Y, s.Center.Z)
	offset := putFloat32s(b[24:],
		s.Rotation.X, s.Rotation.Y, s.Rotation.Z, s.Rotation.W,
		s.Scale.X, s.Scale.Y, s.Scale.Z,
		s.Opacity,
		s.SH0.X, s.SH0.Y, s.SH0.Z,
	) + 24
	for _, c := range s.SHRest {
		offset += putFloat32s(b[offset:], c.X, c.Y, c.Z)
	}
	binary.LittleEndian.PutUint64(b[offset:], uint64(s.SourceIndex))
}

func decodeRecord(b []byte, shRest []geom.Vec3) splat.Splat {
	f64 := func(i int) float64 { return math.Float64frombits(binary.LittleEndian.Uint64(b[8*i:])) }
	f32 := func(i int) float64 { return float64(math.Float32frombits(binary.LittleEndian.Uint32(b[24+4*i:]))) }
	for j := range shRest {
		shRest[j] = geom.Vec3{X: f32(11 + 3*j), Y: f32(12 + 3*j), Z: f32(13 + 3*j)}
	}
	return splat.Splat{
		Center:      geom.Vec3{X: f64(0), Y: f64(1), Z: f64(2)},
		Rotation:    geom.Quat{X: f32(0), Y: f32(1), Z: f32(2), W: f32(3)},
		Scale:       geom.Vec3{X: f32(4), Y: f32(5), Z: f32(6)},
		Opacity:     f32(7),
		SH0:         geom.Vec3{X: f32(8), Y: f32(9), Z: f32(10)},
		SHRest:      shRest,
		SourceIndex: int(int64(binary.LittleEndian.Uint64(b[24+4*(11+3*len(shRest)):]))),
	}
}

func putFloat64s(b []byte, values ...float64) {
	for i, v := range values {
		binary.LittleEndian.PutUint64(b[8*i:], math.Float64bits(v))
	}
}

// putFloat32s writes values as float32 and returns the bytes written.
func putFloat32s(b []byte, values ...float64) int {
	for i, v := range values {
		binary.LittleEndian.PutUint32(b[4*i:], math.Float32bits(float32(v)))
	}
	return 4 * len(values)
}
