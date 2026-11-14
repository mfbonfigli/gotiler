package splat

import "github.com/mfbonfigli/gotiler/v3/internal/geom"

// Rotation turns splats about the origin, together with the view-dependent
// colors of their spherical harmonics. Building one computes how the rotation
// mixes spherical-harmonic coefficients, so reuse it for every splat it turns.
type Rotation struct {
	matrix  geom.Mat3
	shBands [maxSHDegree]shBandMatrix
}

// NewRotation returns the Rotation of the rotation matrix m, which must be
// orthonormal with determinant +1.
func NewRotation(m geom.Mat3) Rotation {
	r := Rotation{matrix: m}
	for degree := 1; degree <= maxSHDegree; degree++ {
		r.shBands[degree-1] = shRotationMatrix(degree, m)
	}
	return r
}

// Rotate returns s turned by r. The spherical harmonics of the result are a
// new slice: s is left unchanged.
func (r Rotation) Rotate(s Splat) Splat {
	s.Center = r.matrix.MulVec(s.Center)
	s.Rotation = geom.QuatFromRotationMatrix(r.matrix.Mul(s.Rotation.RotationMatrix()))
	s.SHRest = r.rotateSH(s.SHRest)
	return s
}

// rotateSH rotates spherical-harmonic coefficients holding complete degrees,
// one band at a time. Each color channel is rotated independently.
func (r Rotation) rotateSH(coefficients []geom.Vec3) []geom.Vec3 {
	if len(coefficients) == 0 {
		return coefficients
	}
	rotated := make([]geom.Vec3, len(coefficients))
	first := 0
	for degree := 1; first < len(coefficients); degree++ {
		n := 2*degree + 1
		band := &r.shBands[degree-1]
		for i := range n {
			var sum geom.Vec3
			for j := range n {
				sum = sum.Add(coefficients[first+j].MulScalar(band[i][j]))
			}
			rotated[first+i] = sum
		}
		first += n
	}
	return rotated
}
