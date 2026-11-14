// Package geom holds small 3D geometry types: vectors, 3x3 matrices,
// quaternions and axis-aligned boxes.
package geom

import "math"

// Vec3 is a three-dimensional vector.
type Vec3 struct {
	X, Y, Z float64
}

// Add returns a+b.
func (a Vec3) Add(b Vec3) Vec3 { return Vec3{a.X + b.X, a.Y + b.Y, a.Z + b.Z} }

// Sub returns a-b.
func (a Vec3) Sub(b Vec3) Vec3 { return Vec3{a.X - b.X, a.Y - b.Y, a.Z - b.Z} }

// MulScalar returns a scaled by s.
func (a Vec3) MulScalar(s float64) Vec3 { return Vec3{a.X * s, a.Y * s, a.Z * s} }

// Dot returns the dot product of a and b.
func (a Vec3) Dot(b Vec3) float64 { return a.X*b.X + a.Y*b.Y + a.Z*b.Z }

// Cross returns the cross product a×b.
func (a Vec3) Cross(b Vec3) Vec3 {
	return Vec3{
		X: a.Y*b.Z - a.Z*b.Y,
		Y: a.Z*b.X - a.X*b.Z,
		Z: a.X*b.Y - a.Y*b.X,
	}
}

// LengthSquared returns the squared Euclidean length of a.
func (a Vec3) LengthSquared() float64 { return a.Dot(a) }

// Length returns the Euclidean length of a.
func (a Vec3) Length() float64 { return math.Sqrt(a.LengthSquared()) }

// MaxComponent returns the largest of the components of a.
func (a Vec3) MaxComponent() float64 { return math.Max(a.X, math.Max(a.Y, a.Z)) }

// IsFinite reports whether no component of a is NaN or infinite.
func (a Vec3) IsFinite() bool { return isFinite(a.X) && isFinite(a.Y) && isFinite(a.Z) }

func isFinite(x float64) bool { return !math.IsNaN(x) && !math.IsInf(x, 0) }
