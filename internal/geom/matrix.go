package geom

import (
	"errors"
	"math"
)

// Mat3 is a 3x3 matrix, indexed by row and then column.
type Mat3 [3][3]float64

// IdentityMat3 returns the 3x3 identity matrix.
func IdentityMat3() Mat3 {
	return Mat3{{1, 0, 0}, {0, 1, 0}, {0, 0, 1}}
}

// Outer returns the outer product a·bᵀ.
func Outer(a, b Vec3) Mat3 {
	return Mat3{
		{a.X * b.X, a.X * b.Y, a.X * b.Z},
		{a.Y * b.X, a.Y * b.Y, a.Y * b.Z},
		{a.Z * b.X, a.Z * b.Y, a.Z * b.Z},
	}
}

// Transpose returns mᵀ.
func (m Mat3) Transpose() Mat3 {
	return Mat3{
		{m[0][0], m[1][0], m[2][0]},
		{m[0][1], m[1][1], m[2][1]},
		{m[0][2], m[1][2], m[2][2]},
	}
}

// Mul returns the matrix product m·n.
func (m Mat3) Mul(n Mat3) Mat3 {
	var out Mat3
	for r := range 3 {
		for c := range 3 {
			out[r][c] = m[r][0]*n[0][c] + m[r][1]*n[1][c] + m[r][2]*n[2][c]
		}
	}
	return out
}

// MulVec returns the matrix-vector product m·v.
func (m Mat3) MulVec(v Vec3) Vec3 {
	return Vec3{
		X: m[0][0]*v.X + m[0][1]*v.Y + m[0][2]*v.Z,
		Y: m[1][0]*v.X + m[1][1]*v.Y + m[1][2]*v.Z,
		Z: m[2][0]*v.X + m[2][1]*v.Y + m[2][2]*v.Z,
	}
}

// Add returns m+n.
func (m Mat3) Add(n Mat3) Mat3 {
	var out Mat3
	for r := range 3 {
		for c := range 3 {
			out[r][c] = m[r][c] + n[r][c]
		}
	}
	return out
}

// MulScalar returns m with every element multiplied by s.
func (m Mat3) MulScalar(s float64) Mat3 {
	var out Mat3
	for r := range 3 {
		for c := range 3 {
			out[r][c] = m[r][c] * s
		}
	}
	return out
}

// Determinant returns the determinant of m.
func (m Mat3) Determinant() float64 {
	return m[0][0]*(m[1][1]*m[2][2]-m[1][2]*m[2][1]) -
		m[0][1]*(m[1][0]*m[2][2]-m[1][2]*m[2][0]) +
		m[0][2]*(m[1][0]*m[2][1]-m[1][1]*m[2][0])
}

// IsFinite reports whether no element of m is NaN or infinite.
func (m Mat3) IsFinite() bool {
	for r := range 3 {
		for c := range 3 {
			if !isFinite(m[r][c]) {
				return false
			}
		}
	}
	return true
}

const (
	maxJacobiSweeps   = 32
	jacobiOffDiagonal = 1e-12
)

// SymmetricEigen returns the eigenvalues of the symmetric matrix m, largest
// first, and its eigenvectors as the columns of a rotation matrix, in the same
// order.
//
// It uses the cyclic Jacobi method: each step zeroes the largest off-diagonal
// element with a plane rotation, accumulating the rotations into the
// eigenvector matrix, until the off-diagonal elements are negligible.
func (m Mat3) SymmetricEigen() (Vec3, Mat3, error) {
	if !m.IsFinite() {
		return Vec3{}, Mat3{}, errors.New("the matrix has non-finite elements")
	}
	a := m
	v := IdentityMat3()
	for range maxJacobiSweeps {
		p, q := 0, 1
		largest := math.Abs(a[0][1])
		if x := math.Abs(a[0][2]); x > largest {
			p, q, largest = 0, 2, x
		}
		if x := math.Abs(a[1][2]); x > largest {
			p, q, largest = 1, 2, x
		}
		if largest < jacobiOffDiagonal {
			values, vectors := sortEigenpairs(Vec3{a[0][0], a[1][1], a[2][2]}, v)
			return values, vectors, nil
		}
		if !isFinite(largest) {
			return Vec3{}, Mat3{}, errors.New("the matrix has non-finite elements")
		}

		tau := (a[q][q] - a[p][p]) / (2 * a[p][q])
		t := math.Copysign(1/(math.Abs(tau)+math.Sqrt(1+tau*tau)), tau)
		c := 1 / math.Sqrt(1+t*t)
		s := t * c

		apq := a[p][q]
		a[p][p] -= t * apq
		a[q][q] += t * apq
		a[p][q], a[q][p] = 0, 0
		for k := range 3 {
			if k == p || k == q {
				continue
			}
			akp, akq := a[k][p], a[k][q]
			a[k][p] = c*akp - s*akq
			a[p][k] = a[k][p]
			a[k][q] = s*akp + c*akq
			a[q][k] = a[k][q]
		}
		for k := range 3 {
			vkp, vkq := v[k][p], v[k][q]
			v[k][p] = c*vkp - s*vkq
			v[k][q] = s*vkp + c*vkq
		}
	}
	return Vec3{}, Mat3{}, errors.New("the eigendecomposition did not converge")
}

// sortEigenpairs orders the eigenvalues from largest to smallest, moving the
// eigenvector columns along, and flips the last eigenvector if needed so that
// the eigenvectors form a right-handed basis.
func sortEigenpairs(values Vec3, vectors Mat3) (Vec3, Mat3) {
	pairs := [3]struct {
		value  float64
		column int
	}{{values.X, 0}, {values.Y, 1}, {values.Z, 2}}
	for i := range len(pairs) - 1 {
		for j := i + 1; j < len(pairs); j++ {
			if pairs[j].value > pairs[i].value {
				pairs[i], pairs[j] = pairs[j], pairs[i]
			}
		}
	}
	var sorted Mat3
	for column, pair := range pairs {
		for r := range 3 {
			sorted[r][column] = vectors[r][pair.column]
		}
	}
	if sorted.Determinant() < 0 {
		for r := range 3 {
			sorted[r][2] = -sorted[r][2]
		}
	}
	return Vec3{pairs[0].value, pairs[1].value, pairs[2].value}, sorted
}
