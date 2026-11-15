package tiling

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"

	"github.com/mfbonfigli/gotiler/v3/internal/mesh"
)

// storeBuffer is the size of the buffer of a store being written or read.
const storeBuffer = 1 << 20

// storeWriter appends triangles to a temporary file.
type storeWriter struct {
	file   *os.File
	buffer *bufio.Writer
	record [mesh.TriangleSize]byte
	count  int
}

func createStore(path string) (*storeWriter, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	return &storeWriter{file: f, buffer: bufio.NewWriterSize(f, storeBuffer)}, nil
}

func (w *storeWriter) write(t *mesh.Triangle) error {
	t.Encode(w.record[:])
	w.count++
	_, err := w.buffer.Write(w.record[:])
	return err
}

func (w *storeWriter) close() error {
	return errors.Join(w.buffer.Flush(), w.file.Close())
}

// writeStore writes triangles to a new store at path.
func writeStore(path string, triangles []mesh.Triangle) error {
	w, err := createStore(path)
	if err != nil {
		return err
	}
	for i := range triangles {
		if err := w.write(&triangles[i]); err != nil {
			return errors.Join(err, w.close())
		}
	}
	return w.close()
}

// readStore returns all the triangles of the store at path.
func readStore(path string) ([]mesh.Triangle, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data)%mesh.TriangleSize != 0 {
		return nil, fmt.Errorf("%s holds %d bytes, not whole triangles", path, len(data))
	}
	triangles := make([]mesh.Triangle, len(data)/mesh.TriangleSize)
	for i := range triangles {
		triangles[i].Decode(data[i*mesh.TriangleSize:])
	}
	return triangles, nil
}

// scanStore passes the triangles of the store at path to visit, in batches
// that it reuses.
func scanStore(path string, batch int, visit func([]mesh.Triangle) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close() //nolint:errcheck
	r := bufio.NewReaderSize(f, storeBuffer)
	data := make([]byte, batch*mesh.TriangleSize)
	for {
		n, err := io.ReadFull(r, data)
		if n%mesh.TriangleSize != 0 {
			return fmt.Errorf("%s does not hold whole triangles", path)
		}
		if n > 0 {
			triangles := make([]mesh.Triangle, n/mesh.TriangleSize)
			for i := range triangles {
				triangles[i].Decode(data[i*mesh.TriangleSize:])
			}
			if err := visit(triangles); err != nil {
				return err
			}
		}
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// sortTriangles orders triangles by their content, so that what is built
// from them does not depend on the order they were stored in.
func sortTriangles(triangles []mesh.Triangle) {
	var a, b [mesh.TriangleSize]byte
	slices.SortFunc(triangles, func(x, y mesh.Triangle) int {
		for c := range 3 {
			for k := range 3 {
				if x.P[c][k] != y.P[c][k] {
					if x.P[c][k] < y.P[c][k] {
						return -1
					}
					return 1
				}
			}
		}
		x.Encode(a[:])
		y.Encode(b[:])
		return bytes.Compare(a[:], b[:])
	})
}
