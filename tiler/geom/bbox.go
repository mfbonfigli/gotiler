package geom

type BoundingBox struct {
	Xmin, Xmax, Ymin, Ymax, Zmin, Zmax float64
}

// NewBoundingBox initializes a bounding box from min/max coordinates.
func NewBoundingBox(Xmin, Xmax, Ymin, Ymax, Zmin, Zmax float64) BoundingBox {
	return BoundingBox{
		Xmin: Xmin,
		Xmax: Xmax,
		Ymin: Ymin,
		Ymax: Ymax,
		Zmin: Zmin,
		Zmax: Zmax,
	}
}

// Center returns the bounding box center.
func (b BoundingBox) Center() (x, y, z float64) {
	return (b.Xmax + b.Xmin) / 2, (b.Ymax + b.Ymin) / 2, (b.Zmax + b.Zmin) / 2
}

// AsCesiumBox returns the bounding box in Cesium box format.
func (b BoundingBox) AsCesiumBox() [12]float64 {
	xMid, yMid, zMid := b.Center()
	return [12]float64{
		xMid, yMid, zMid,
		(b.Xmax - xMid), 0, 0,
		0, (b.Ymax - yMid), 0,
		0, 0, (b.Zmax - zMid),
	}
}
