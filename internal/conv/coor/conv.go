package coor

import (
	"github.com/mfbonfigli/gotiler/v3/tiler/model"
)

type Converter interface {
	Transform(sourceCRS string, targetCRS string, coord model.Vector) (model.Vector, error)
	// TransformFlat transforms a flat slice of X,Y,Z coordinates in-place between arbitrary CRSs.
	// flatCoords layout: [X0, Y0, Z0, X1, Y1, Z1, ...], len must be multiple of 3.
	TransformFlat(sourceCRS string, targetCRS string, flatCoords []float64) error
	ToWGS84Cartesian(sourceCRS string, coord model.Vector) (model.Vector, error)
	// ToWGS84CartesianFlat transforms a flat slice of X,Y,Z coordinates in-place.
	// flatCoords layout: [X0, Y0, Z0, X1, Y1, Z1, ...], len must be multiple of 3.
	ToWGS84CartesianFlat(sourceCRS string, flatCoords []float64) error
	Cleanup()
}

// ConverterFactory returns a new CoordinateConverter that should only be used in the same goroutine
// to avoid race conditions
type ConverterFactory func() (Converter, error)
