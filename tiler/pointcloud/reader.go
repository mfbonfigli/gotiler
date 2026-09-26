package pointcloud

import (
	"github.com/mfbonfigli/gotiler/v3/tiler/geom"
	"github.com/mfbonfigli/gotiler/v3/tiler/model"
)

// CRSLocal is the sentinel CRS marking ungeoreferenced input: point
// coordinates are treated as a local Z-up cartesian system in meters and
// placed on the globe through a Placement transform instead of a CRS
// conversion. Readers receive it as an ordinary CRS string; converters in
// placement mode ignore it.
const CRSLocal = "local"

// Reader reads point-cloud points and source CRS metadata.
type Reader interface {
	NumberOfPoints() int
	GetNext() (geom.Point64, error)
	GetCRS() string
	Reset() error
	Close()
	// AttributeSchema describes the per-point attributes this reader emits and
	// the packed layout of Point64.Attributes: values are stored contiguously,
	// little-endian, in schema order (see model.AttributeSchemaLayout). The
	// schema is fixed for the reader's lifetime; nil means no attributes are
	// emitted. Fields a reader cannot provide for some points must be written
	// as zero values.
	AttributeSchema() []model.AttributeDescriptor
}
