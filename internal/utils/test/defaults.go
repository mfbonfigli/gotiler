package test

import (
	"github.com/mfbonfigli/gotiler/v3/internal/conv/coor/proj"
	coor "github.com/mfbonfigli/gotiler/v3/tiler/coord"
)

// GetTestCoordinateConverter returns the function to use to convert coordinates in tests
func GetTestCoordinateConverterFactory() coor.ConverterFactory {
	return func() (coor.Converter, error) {
		return proj.NewProjCoordinateConverter()
	}
}
