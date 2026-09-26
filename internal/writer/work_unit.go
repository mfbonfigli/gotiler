package writer

import (
	"github.com/mfbonfigli/gotiler/v3/tiler/plugin"
	"github.com/mfbonfigli/gotiler/v3/tiler/tree"
)

// WorkUnit contains the minimal data needed to produce a single 3d tile, i.e.
// a binary content.pnts file, a tileset.json file
type WorkUnit struct {
	// Node contains the data for the current tile
	Node tree.Node
	// WriterProvider creates the destination for the encoded tile content.
	WriterProvider plugin.WriterProvider
	// Prefix is the content file prefix
	Prefix string
	// OnDone, when non-nil, is called by the consumer after the tile is successfully written.
	OnDone func()
}
