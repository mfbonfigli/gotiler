package model

// IdentityTransform is the identity transformation object
var IdentityTransform Transform = Transform{
	forward: [4][4]float64{
		{1, 0, 0, 0},
		{0, 1, 0, 0},
		{0, 0, 1, 0},
		{0, 0, 0, 1},
	},
	inverse: [4][4]float64{
		{1, 0, 0, 0},
		{0, 1, 0, 0},
		{0, 0, 1, 0},
		{0, 0, 0, 1},
	},
}

// Transform represents a rigid roto-translation between cartesian reference systems
type Transform struct {
	forward [4][4]float64
	inverse [4][4]float64
}

// NewTransform returns a new transform object from the given forward transformation quaternion
func NewTransform(fwd [4][4]float64) Transform {
	inverse := [4][4]float64{
		{fwd[0][0], fwd[1][0], fwd[2][0], -fwd[0][0]*fwd[0][3] - fwd[1][0]*fwd[1][3] - fwd[2][0]*fwd[2][3]},
		{fwd[0][1], fwd[1][1], fwd[2][1], -fwd[0][1]*fwd[0][3] - fwd[1][1]*fwd[1][3] - fwd[2][1]*fwd[2][3]},
		{fwd[0][2], fwd[1][2], fwd[2][2], -fwd[0][2]*fwd[0][3] - fwd[1][2]*fwd[1][3] - fwd[2][2]*fwd[2][3]},
		{0, 0, 0, 1},
	}

	return Transform{
		forward: fwd,
		inverse: inverse,
	}
}

// NewTransformWithInverse returns a transform from the given forward matrix
// and its inverse. Use it when the forward matrix is not a pure
// roto-translation (e.g. it carries a uniform scale), where NewTransform's
// transpose-based inversion would be incorrect. The caller guarantees that
// inv is the inverse of fwd.
func NewTransformWithInverse(fwd, inv [4][4]float64) Transform {
	return Transform{
		forward: fwd,
		inverse: inv,
	}
}

// Forward transforms the given Vector from the source to the destination CRS
func (q Transform) Forward(v Vector) Vector {
	return q.transform(v, q.forward)
}

// Inverse transforms the given Vector from the destination to the source CRS
func (q Transform) Inverse(v Vector) Vector {
	return q.transform(v, q.inverse)
}

// ForwardColumnMajor returns the forward transformation quaternion in column-major order
func (q Transform) ForwardColumnMajor() [16]float64 {
	return q.columnMajor(q.forward)
}

// ForwardColumnMajor returns the inverse transformation quaternion in column-major order
func (q Transform) InverseColumnMajor() [16]float64 {
	return q.columnMajor(q.inverse)
}

func (q Transform) transform(v Vector, tr [4][4]float64) Vector {
	return Vector{
		X: tr[0][0]*v.X + tr[0][1]*v.Y + tr[0][2]*v.Z + tr[0][3],
		Y: tr[1][0]*v.X + tr[1][1]*v.Y + tr[1][2]*v.Z + tr[1][3],
		Z: tr[2][0]*v.X + tr[2][1]*v.Y + tr[2][2]*v.Z + tr[2][3],
	}
}

func (q Transform) columnMajor(tr [4][4]float64) [16]float64 {
	return [16]float64{
		tr[0][0], tr[1][0], tr[2][0], tr[3][0],
		tr[0][1], tr[1][1], tr[2][1], tr[3][1],
		tr[0][2], tr[1][2], tr[2][2], tr[3][2],
		tr[0][3], tr[1][3], tr[2][3], tr[3][3],
	}
}
