package model

import "math"

// Vector represents a Vector in a 3D space with double precision components
type Vector struct {
	X float64
	Y float64
	Z float64
}

// Unit returns the unit vector with same direction as the vector
func (v Vector) Unit() Vector {
	n := v.Norm()
	return Vector{
		X: v.X / n,
		Y: v.Y / n,
		Z: v.Z / n,
	}
}

// Norm return the euclidean norm of the vector
func (v Vector) Norm() float64 {
	return math.Sqrt(math.Pow(v.X, 2) + math.Pow(v.Y, 2) + math.Pow(v.Z, 2))
}

// Cross returns the result of the cross product with the vector passed as input
func (v Vector) Cross(w Vector) Vector {
	return Vector{
		X: v.Y*w.Z - v.Z*w.Y,
		Y: v.Z*w.X - v.X*w.Z,
		Z: v.X*w.Y - v.Y*w.X,
	}
}
