package coord

import "github.com/mfbonfigli/gotiler/v3/tiler/model"

// Converter transforms coordinates between coordinate reference systems.
type Converter interface {
	Transform(sourceCRS string, targetCRS string, coord model.Vector) (model.Vector, error)
	// TransformFlat transforms a flat X,Y,Z slice in place between arbitrary CRSs.
	TransformFlat(sourceCRS string, targetCRS string, flatCoords []float64) error
	ToWGS84Cartesian(sourceCRS string, coord model.Vector) (model.Vector, error)
	// ToWGS84CartesianFlat transforms a flat X,Y,Z slice in place.
	ToWGS84CartesianFlat(sourceCRS string, flatCoords []float64) error
	Cleanup()
}

// ConverterFactory returns a converter scoped to the caller goroutine.
type ConverterFactory func() (Converter, error)
