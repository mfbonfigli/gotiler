package geom

import (
	"math"

	"github.com/mfbonfigli/gotiler/v3/tiler/model"
)

// LocalToGlobalTransformFromPoint returns a transform from a local Z-up CRS
// centered on an EPSG:4978 point back to global EPSG:4978 coordinates.
func LocalToGlobalTransformFromPoint(x, y, z float64) model.Transform {
	zAxis := normalToWGS84FromPoint(x, y, z)
	xAxis, yAxis := normals(zAxis)

	toGlobal := [4][4]float64{
		{xAxis.X, yAxis.X, zAxis.X, x},
		{xAxis.Y, yAxis.Y, zAxis.Y, y},
		{xAxis.Z, yAxis.Z, zAxis.Z, z},
		{0, 0, 0, 1},
	}

	return model.NewTransform(toGlobal)
}

func normals(v model.Vector) (model.Vector, model.Vector) {
	arbitraryVector := model.Vector{X: 0, Y: 1, Z: 0}
	if v.Cross(arbitraryVector).Norm() < 0.05 {
		arbitraryVector = model.Vector{X: 1, Y: 0, Z: 0}
	}
	xAxis := arbitraryVector.Cross(v).Unit()
	yAxis := v.Cross(xAxis).Unit()
	return xAxis, yAxis
}

func normalToWGS84FromPoint(x, y, z float64) model.Vector {
	a := 6378137.0
	b := 6356752.31424518
	if x == 0 && y == 0 && z == 0 {
		return model.Vector{X: 0, Y: 0, Z: 1}
	}

	return model.Vector{
		X: 2 * x / math.Pow(a, 2),
		Y: 2 * y / math.Pow(a, 2),
		Z: 2 * z / math.Pow(b, 2),
	}.Unit()
}
