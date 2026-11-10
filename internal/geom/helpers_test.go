package geom

import (
	"math"
	"math/rand/v2"
	"testing"
)

const tolerance = 1e-9

func closeTo(got, want float64) bool {
	return math.Abs(got-want) <= tolerance*math.Max(1, math.Max(math.Abs(got), math.Abs(want)))
}

func assertClose(t *testing.T, name string, got, want float64) {
	t.Helper()
	if !closeTo(got, want) {
		t.Errorf("%s = %v, want %v", name, got, want)
	}
}

func assertVec3(t *testing.T, name string, got, want Vec3) {
	t.Helper()
	if !closeTo(got.X, want.X) || !closeTo(got.Y, want.Y) || !closeTo(got.Z, want.Z) {
		t.Errorf("%s = %+v, want %+v", name, got, want)
	}
}

func assertMat3(t *testing.T, name string, got, want Mat3) {
	t.Helper()
	for r := range 3 {
		for c := range 3 {
			if !closeTo(got[r][c], want[r][c]) {
				t.Errorf("%s = %v, want %v", name, got, want)
				return
			}
		}
	}
}

// assertRotationMatrix checks that m is orthonormal with determinant +1.
func assertRotationMatrix(t *testing.T, name string, m Mat3) {
	t.Helper()
	assertMat3(t, name+"ᵀ·"+name, m.Transpose().Mul(m), IdentityMat3())
	assertClose(t, "det "+name, m.Determinant(), 1)
}

func diagonal(x, y, z float64) Mat3 {
	return Mat3{{x, 0, 0}, {0, y, 0}, {0, 0, z}}
}

func randomRotation(rng *rand.Rand) Quat {
	return Quat{X: rng.NormFloat64(), Y: rng.NormFloat64(), Z: rng.NormFloat64(), W: rng.NormFloat64()}.Normalize()
}
