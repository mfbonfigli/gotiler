// Package static provides a coordinate converter for ungeoreferenced input:
// instead of a CRS conversion, it places local cartesian coordinates on the
// globe by applying a fixed local-to-EPSG:4978 transform.
package static

import (
	"fmt"

	coor "github.com/mfbonfigli/gotiler/v3/tiler/coord"
	"github.com/mfbonfigli/gotiler/v3/tiler/model"
)

// Converter applies a fixed placement transform in place of CRS conversions.
// It implements coord.Converter; the source CRS parameters are ignored.
type Converter struct {
	t model.Transform
}

var _ coor.Converter = &Converter{}

// NewConverter returns a converter that maps local cartesian coordinates to
// EPSG:4978 through the given placement transform.
func NewConverter(t model.Transform) *Converter {
	return &Converter{t: t}
}

// Transform between arbitrary CRSs is not possible without a source CRS.
func (c *Converter) Transform(sourceCRS string, targetCRS string, coord model.Vector) (model.Vector, error) {
	return model.Vector{}, fmt.Errorf("CRS transformations are not available for ungeoreferenced input placed through a transform")
}

// TransformFlat between arbitrary CRSs is not possible without a source CRS.
func (c *Converter) TransformFlat(sourceCRS string, targetCRS string, flatCoords []float64) error {
	return fmt.Errorf("CRS transformations are not available for ungeoreferenced input placed through a transform")
}

// ToWGS84Cartesian places the local coordinate on the globe.
func (c *Converter) ToWGS84Cartesian(sourceCRS string, coord model.Vector) (model.Vector, error) {
	return c.t.Forward(coord), nil
}

// ToWGS84CartesianFlat places a flat X,Y,Z slice on the globe in place.
func (c *Converter) ToWGS84CartesianFlat(sourceCRS string, flatCoords []float64) error {
	if len(flatCoords)%3 != 0 {
		return fmt.Errorf("flat coordinate slice length %d is not a multiple of 3", len(flatCoords))
	}
	for i := 0; i < len(flatCoords); i += 3 {
		out := c.t.Forward(model.Vector{X: flatCoords[i], Y: flatCoords[i+1], Z: flatCoords[i+2]})
		flatCoords[i], flatCoords[i+1], flatCoords[i+2] = out.X, out.Y, out.Z
	}
	return nil
}

// Cleanup releases no resources.
func (c *Converter) Cleanup() {}
