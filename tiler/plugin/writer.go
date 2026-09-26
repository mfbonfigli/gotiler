package plugin

import (
	"io"

	"github.com/mfbonfigli/gotiler/v3/tiler/model"
	"github.com/mfbonfigli/gotiler/v3/tiler/tree"
	"github.com/mfbonfigli/gotiler/v3/version"
)

const (
	// EncoderPNTS writes 3D Tiles 1.0 PNTS content.
	EncoderPNTS = "pnts"

	// EncoderGLB writes uncompressed 3D Tiles 1.1 GLB content.
	EncoderGLB = "glb"
)

// WriterProvider creates a writable destination for a tileset-relative output
// filename, such as "data/d.glb" or "tileset.json".
type WriterProvider func(filename string) (io.WriteCloser, error)

// WriterMiddleware wraps a WriterProvider. Use it for concerns that should sit
// between the core writer and its destination, such as client-side encryption.
type WriterMiddleware func(WriterProvider) WriterProvider

// WriterFinalizer performs work that must happen after all tile content and
// tileset.json have been written, such as closing an archive index.
type WriterFinalizer interface {
	Finalize() error
}

// GeometryEncoder encodes a tree node into tile content.
type GeometryEncoder interface {
	Write(n tree.Node, wp WriterProvider, prefix string) error
	TilesetVersion() version.TilesetVersion
	ContentFilename() string
}

// GeometryEncoderFactory creates a geometry encoder for an attribute set. The
// encoder determines its own tileset version and output filename.
type GeometryEncoderFactory func(attrs model.Attributes) GeometryEncoder

// ChainWriterMiddleware applies middlewares to a writer provider in order.
func ChainWriterMiddleware(base WriterProvider, middlewares ...WriterMiddleware) WriterProvider {
	next := base
	for _, middleware := range middlewares {
		if middleware != nil {
			next = middleware(next)
		}
	}
	return next
}
