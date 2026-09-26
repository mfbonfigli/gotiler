package proj

import (
	internalproj "github.com/mfbonfigli/gotiler/v3/internal/conv/coor/proj"
	"github.com/mfbonfigli/gotiler/v3/tiler/coord"
)

// NewConverter returns the default PROJ-backed coordinate converter.
func NewConverter() (coord.Converter, error) {
	return internalproj.NewProjCoordinateConverter()
}

// NewConverterFactory returns a factory that creates default PROJ-backed
// converters scoped to the caller goroutine.
func NewConverterFactory() coord.ConverterFactory {
	return NewConverter
}
