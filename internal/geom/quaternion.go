package geom

import "math"

// Quat is a rotation quaternion, with its components in the glTF order X, Y,
// Z, W.
type Quat struct {
	X, Y, Z, W float64
}

// IdentityQuat returns the quaternion of no rotation.
func IdentityQuat() Quat { return Quat{W: 1} }

// Normalize returns q scaled to unit length. A zero or non-finite quaternion
// becomes the identity.
func (q Quat) Normalize() Quat {
	n := math.Sqrt(q.X*q.X + q.Y*q.Y + q.Z*q.Z + q.W*q.W)
	if n == 0 || !isFinite(n) {
		return IdentityQuat()
	}
	return Quat{X: q.X / n, Y: q.Y / n, Z: q.Z / n, W: q.W / n}
}

// IsFinite reports whether no component of q is NaN or infinite.
func (q Quat) IsFinite() bool {
	return isFinite(q.X) && isFinite(q.Y) && isFinite(q.Z) && isFinite(q.W)
}

// RotationMatrix returns the rotation matrix of q, normalized first.
func (q Quat) RotationMatrix() Mat3 {
	q = q.Normalize()
	x, y, z, w := q.X, q.Y, q.Z, q.W
	xx, yy, zz := x*x, y*y, z*z
	xy, xz, yz := x*y, x*z, y*z
	wx, wy, wz := w*x, w*y, w*z
	return Mat3{
		{1 - 2*(yy+zz), 2 * (xy - wz), 2 * (xz + wy)},
		{2 * (xy + wz), 1 - 2*(xx+zz), 2 * (yz - wx)},
		{2 * (xz - wy), 2 * (yz + wx), 1 - 2*(xx+yy)},
	}
}

// QuatFromRotationMatrix returns the unit quaternion of the rotation matrix m.
//
// It derives the component with the largest magnitude from the trace or the
// largest diagonal element, and the other components from it, which keeps the
// division well conditioned for every rotation angle.
func QuatFromRotationMatrix(m Mat3) Quat {
	trace := m[0][0] + m[1][1] + m[2][2]
	switch {
	case trace > 0:
		s := math.Sqrt(trace+1) * 2
		return Quat{
			W: 0.25 * s,
			X: (m[2][1] - m[1][2]) / s,
			Y: (m[0][2] - m[2][0]) / s,
			Z: (m[1][0] - m[0][1]) / s,
		}.Normalize()
	case m[0][0] > m[1][1] && m[0][0] > m[2][2]:
		s := math.Sqrt(1+m[0][0]-m[1][1]-m[2][2]) * 2
		return Quat{
			W: (m[2][1] - m[1][2]) / s,
			X: 0.25 * s,
			Y: (m[0][1] + m[1][0]) / s,
			Z: (m[0][2] + m[2][0]) / s,
		}.Normalize()
	case m[1][1] > m[2][2]:
		s := math.Sqrt(1+m[1][1]-m[0][0]-m[2][2]) * 2
		return Quat{
			W: (m[0][2] - m[2][0]) / s,
			X: (m[0][1] + m[1][0]) / s,
			Y: 0.25 * s,
			Z: (m[1][2] + m[2][1]) / s,
		}.Normalize()
	default:
		s := math.Sqrt(1+m[2][2]-m[0][0]-m[1][1]) * 2
		return Quat{
			W: (m[1][0] - m[0][1]) / s,
			X: (m[0][2] + m[2][0]) / s,
			Y: (m[1][2] + m[2][1]) / s,
			Z: 0.25 * s,
		}.Normalize()
	}
}
