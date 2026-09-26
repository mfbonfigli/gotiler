package model

// Point models a point cloud point expressed in local, single precision, coordinates
type Point struct {
	X float32
	Y float32
	Z float32
	R uint8
	G uint8
	B uint8
	// Attributes holds the point's optional generic attribute values packed
	// according to the tree's attribute summary layout. See AttributeValues.
	// All optional per-point data (intensity, classification, ...) travels here.
	Attributes AttributeValues
}

// Vector returns a Vector representation of the position of the point in the local coordinate space
func (p Point) Vector() Vector {
	return Vector{
		X: float64(p.X),
		Y: float64(p.Y),
		Z: float64(p.Z),
	}
}
