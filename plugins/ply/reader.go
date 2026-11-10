package ply

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/mfbonfigli/gotiler/v3/tiler/geom"
	"github.com/mfbonfigli/gotiler/v3/tiler/model"
	"github.com/mfbonfigli/gotiler/v3/tiler/plugin"
	"github.com/mfbonfigli/gotiler/v3/tiler/pointcloud"
)

const readBufferSize = 64 * 1024

func init() {
	plugin.RegisterPointCloudReader(".ply", func(filename, crs string, opts plugin.ReaderOptions) (pointcloud.Reader, error) {
		return NewReader(filename, crs, opts.RequestedAttributes)
	})
}

// Reader reads the vertices of a PLY file as points. It implements
// pointcloud.Reader and is safe for concurrent use.
type Reader struct {
	filename string
	crs      string
	file     *os.File
	buffer   *bufio.Reader
	decoder  *vertexDecoder
	mapper   *pointMapper
	arena    model.AttributeValuesArena
	mu       sync.Mutex
}

var _ pointcloud.Reader = (*Reader)(nil)

// NewReader opens a PLY file. PLY files carry no CRS, so crs is required: use
// pointcloud.CRSLocal for ungeoreferenced data placed on the globe. attrs lists
// the optional per-point attributes to emit; nil means none.
func NewReader(filename, crs string, attrs model.Attributes) (*Reader, error) {
	if crs == "" {
		return nil, fmt.Errorf("%s: PLY files carry no CRS: provide one with --crs, or use --crs local for ungeoreferenced data", filename)
	}
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	r := &Reader{
		filename: filename,
		crs:      crs,
		file:     file,
		buffer:   bufio.NewReaderSize(file, readBufferSize),
	}
	if err := r.open(attrs); err != nil {
		file.Close() //nolint:errcheck
		return nil, fmt.Errorf("%s: %w", filename, err)
	}
	return r, nil
}

func (r *Reader) open(attrs model.Attributes) error {
	h, err := readHeader(r.buffer)
	if err != nil {
		return err
	}
	if r.decoder, err = newVertexDecoder(r.buffer, h); err != nil {
		return err
	}
	if r.mapper, err = newPointMapper(r.decoder.scalars, attrs); err != nil {
		return err
	}
	return checkBinaryDataIsComplete(r.file, r.decoder)
}

// checkBinaryDataIsComplete fails fast on truncated binary files, which would
// otherwise only be detected after reading all the vertices before the cut.
func checkBinaryDataIsComplete(file *os.File, decoder *vertexDecoder) error {
	want, ok := decoder.binaryDataSize()
	if !ok {
		return nil
	}
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if available := info.Size() - decoder.vertexDataOffset; available < want {
		return fmt.Errorf("the file is truncated: the header declares %d vertices, needing %d bytes, but only %d bytes follow", decoder.count, want, available)
	}
	return nil
}

// NumberOfPoints returns the vertex count declared in the header. Vertices
// with non-finite coordinates are skipped while reading, so fewer points may
// be returned.
func (r *Reader) NumberOfPoints() int {
	return r.decoder.count
}

func (r *Reader) GetCRS() string {
	return r.crs
}

func (r *Reader) AttributeSchema() []model.AttributeDescriptor {
	return r.mapper.schema
}

// GetNext returns the next point, or io.EOF once all the vertices are read.
func (r *Reader) GetNext() (geom.Point64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for {
		values, err := r.decoder.next()
		if errors.Is(err, io.EOF) {
			return geom.Point64{}, io.EOF
		}
		if err != nil {
			return geom.Point64{}, fmt.Errorf("%s: %w", r.filename, err)
		}
		if pt, ok := r.mapper.point(values, &r.arena); ok {
			return pt, nil
		}
	}
}

func (r *Reader) Reset() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, err := r.file.Seek(r.decoder.vertexDataOffset, io.SeekStart); err != nil {
		return fmt.Errorf("%s: %w", r.filename, err)
	}
	r.buffer.Reset(r.file)
	r.decoder.restart(r.buffer)
	return nil
}

func (r *Reader) Close() {
	r.file.Close() //nolint:errcheck
}
