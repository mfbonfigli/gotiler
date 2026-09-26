// Package threetz writes OGC 3D Tiles Archive (.3tz) output.
package threetz

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"crypto/md5"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mfbonfigli/gotiler/v3/tiler/plugin"
)

const (
	// IndexFilename is the required 3tz binary index entry name.
	IndexFilename = "@3dtilesIndex1@"

	// MethodStore writes files without compression.
	MethodStore = zip.Store

	// MethodDeflate writes files with standard ZIP deflate compression.
	MethodDeflate = zip.Deflate

	maxZIP32Size = uint64(1<<32 - 1)
)

type options struct {
	method       uint16
	autoFinalize bool
	modified     time.Time
	mode         os.FileMode
}

// Option configures an Archive.
type Option func(*options)

// WithCompressionMethod sets the ZIP compression method for tile payload files.
// Supported values are MethodStore and MethodDeflate. The 3tz index is always
// stored uncompressed as required by the format.
func WithCompressionMethod(method uint16) Option {
	return func(opts *options) {
		opts.method = method
	}
}

// WithAutoFinalize controls whether closing the root tileset.json also finalizes
// the archive. It defaults to true for compatibility with writer providers used
// without the tiler's finalizer hook.
func WithAutoFinalize(enabled bool) Option {
	return func(opts *options) {
		opts.autoFinalize = enabled
	}
}

// WithModifiedTime sets the ZIP modification time used for every entry.
func WithModifiedTime(t time.Time) Option {
	return func(opts *options) {
		opts.modified = t
	}
}

// WithFileMode sets the ZIP file mode used for every entry.
func WithFileMode(mode os.FileMode) Option {
	return func(opts *options) {
		opts.mode = mode
	}
}

// Archive implements a gotiler writer provider plus finalizer for one .3tz file.
type Archive struct {
	mu        sync.Mutex
	file      *os.File
	writer    *zip.Writer
	counter   *countingWriter
	opts      options
	entries   []indexEntry
	names     map[string]struct{}
	sawRoot   bool
	finalized bool
	finalErr  error
}

type indexEntry struct {
	hash   [16]byte
	offset uint64
}

type countingWriter struct {
	w io.Writer
	n uint64
}

type archiveFile struct {
	archive *Archive
	name    string
	buf     bytes.Buffer
	closed  bool
}

// New creates a .3tz archive writer. Call WriterProvider when wiring gotiler
// output, and call Finalize after all tile files and tileset.json are written.
func New(filename string, optFns ...Option) (*Archive, error) {
	if !strings.EqualFold(filepath.Ext(filename), ".3tz") {
		return nil, fmt.Errorf("3tz archive filename must use .3tz extension: %s", filename)
	}
	opts := options{
		method:       MethodStore,
		autoFinalize: true,
		mode:         0644,
	}
	for _, optFn := range optFns {
		optFn(&opts)
	}
	if opts.method != MethodStore && opts.method != MethodDeflate {
		return nil, fmt.Errorf("unsupported 3tz compression method %d", opts.method)
	}
	if dir := filepath.Dir(filename); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0777); err != nil {
			return nil, err
		}
	}
	file, err := os.Create(filename)
	if err != nil {
		return nil, err
	}
	counter := &countingWriter{w: file}
	return &Archive{
		file:    file,
		writer:  zip.NewWriter(counter),
		counter: counter,
		opts:    opts,
		names:   map[string]struct{}{},
	}, nil
}

// WriterProvider returns the gotiler writer provider for this archive.
func (a *Archive) WriterProvider() plugin.WriterProvider {
	return a.Open
}

// Open creates an in-archive file writer for a tileset-relative filename.
func (a *Archive) Open(filename string) (io.WriteCloser, error) {
	name, err := normalizeArchiveName(filename)
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.finalized {
		return nil, errors.New("3tz archive is already finalized")
	}
	if _, exists := a.names[name]; exists {
		return nil, fmt.Errorf("duplicate 3tz archive entry %q", name)
	}
	return &archiveFile{archive: a, name: name}, nil
}

// Finalize appends the required 3tz index as the last ZIP entry and closes the
// archive. It is safe to call more than once.
func (a *Archive) Finalize() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.finalized {
		return a.finalErr
	}
	if !a.sawRoot {
		a.finalized = true
		a.finalErr = a.closeWithError(errors.New("3tz archive is missing root tileset.json"))
		return a.finalErr
	}

	indexData := buildIndex(a.entries)
	if err := a.writeRaw(IndexFilename, indexData, MethodStore, false); err != nil {
		a.finalized = true
		a.finalErr = a.closeWithError(err)
		return a.finalErr
	}

	a.finalized = true
	if err := a.writer.Close(); err != nil {
		a.finalErr = a.closeWithError(err)
		return a.finalErr
	}
	a.finalErr = a.file.Close()
	return a.finalErr
}

func (a *Archive) writeFile(name string, data []byte) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.finalized {
		return errors.New("3tz archive is already finalized")
	}
	if _, exists := a.names[name]; exists {
		return fmt.Errorf("duplicate 3tz archive entry %q", name)
	}
	if err := a.writeRaw(name, data, a.opts.method, true); err != nil {
		return err
	}
	a.names[name] = struct{}{}
	if name == "tileset.json" {
		a.sawRoot = true
	}
	return nil
}

func (a *Archive) writeRaw(name string, data []byte, method uint16, addToIndex bool) error {
	payload, err := payloadForMethod(data, method)
	if err != nil {
		return err
	}
	if uint64(len(data)) > maxZIP32Size || uint64(len(payload)) > maxZIP32Size {
		return fmt.Errorf("3tz entry %q exceeds 4 GB ZIP32 size limit", name)
	}

	if err := a.writer.Flush(); err != nil {
		return err
	}
	offset := a.counter.n
	header := &zip.FileHeader{
		Name:               name,
		Method:             method,
		CRC32:              crc32.ChecksumIEEE(data),
		CompressedSize64:   uint64(len(payload)),
		UncompressedSize64: uint64(len(data)),
	}
	if !a.opts.modified.IsZero() {
		header.Modified = a.opts.modified
	}
	header.SetMode(a.opts.mode)

	out, err := a.writer.CreateRaw(header)
	if err != nil {
		return err
	}
	if _, err := out.Write(payload); err != nil {
		return err
	}
	if addToIndex {
		a.entries = append(a.entries, indexEntry{
			hash:   md5.Sum([]byte(name)),
			offset: offset,
		})
	}
	return nil
}

func (a *Archive) closeWithError(err error) error {
	if closeErr := a.file.Close(); closeErr != nil {
		return errors.Join(err, closeErr)
	}
	return err
}

func (w *countingWriter) Write(p []byte) (int, error) {
	n, err := w.w.Write(p)
	w.n += uint64(n)
	return n, err
}

func (f *archiveFile) Write(p []byte) (int, error) {
	if f.closed {
		return 0, errors.New("3tz archive entry is already closed")
	}
	return f.buf.Write(p)
}

func (f *archiveFile) Close() error {
	if f.closed {
		return nil
	}
	f.closed = true
	if err := f.archive.writeFile(f.name, f.buf.Bytes()); err != nil {
		return err
	}
	if f.name == "tileset.json" && f.archive.opts.autoFinalize {
		return f.archive.Finalize()
	}
	return nil
}

func normalizeArchiveName(filename string) (string, error) {
	name := strings.TrimSpace(strings.ReplaceAll(filename, "\\", "/"))
	for strings.HasPrefix(name, "/") {
		name = strings.TrimPrefix(name, "/")
	}
	if name == "" || name == "." || strings.HasSuffix(name, "/") {
		return "", fmt.Errorf("invalid 3tz archive filename %q", filename)
	}
	if name == IndexFilename {
		return "", fmt.Errorf("3tz archive index filename %q is reserved", IndexFilename)
	}
	if strings.Contains(strings.ToLower(name), ".3tz") {
		return "", fmt.Errorf("3tz archive filenames must not contain .3tz: %q", name)
	}
	return name, nil
}

func payloadForMethod(data []byte, method uint16) ([]byte, error) {
	switch method {
	case MethodStore:
		return data, nil
	case MethodDeflate:
		var buf bytes.Buffer
		w, err := flate.NewWriter(&buf, flate.DefaultCompression)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(data); err != nil {
			_ = w.Close()
			return nil, err
		}
		if err := w.Close(); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	default:
		return nil, fmt.Errorf("unsupported 3tz compression method %d", method)
	}
}

func buildIndex(entries []indexEntry) []byte {
	sortedEntries := append([]indexEntry(nil), entries...)
	sort.Slice(sortedEntries, func(i, j int) bool {
		leftHigh := binary.LittleEndian.Uint64(sortedEntries[i].hash[0:8])
		rightHigh := binary.LittleEndian.Uint64(sortedEntries[j].hash[0:8])
		if leftHigh == rightHigh {
			leftLow := binary.LittleEndian.Uint64(sortedEntries[i].hash[8:16])
			rightLow := binary.LittleEndian.Uint64(sortedEntries[j].hash[8:16])
			return leftLow < rightLow
		}
		return leftHigh < rightHigh
	})

	data := make([]byte, 0, len(sortedEntries)*24)
	for _, entry := range sortedEntries {
		data = append(data, entry.hash[:]...)
		var offset [8]byte
		binary.LittleEndian.PutUint64(offset[:], entry.offset)
		data = append(data, offset[:]...)
	}
	return data
}
