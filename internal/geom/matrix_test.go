package geom

import (
	"math"
	"math/rand/v2"
	"testing"
)

func TestMat3Arithmetic(t *testing.T) {
	m := Mat3{{1, 2, 3}, {4, 5, 6}, {7, 8, 10}}
	n := Mat3{{0, 1, 0}, {-1, 0, 0}, {0, 0, 2}}

	assertMat3(t, "m·I", m.Mul(IdentityMat3()), m)
	assertMat3(t, "m·n", m.Mul(n), Mat3{{-2, 1, 6}, {-5, 4, 12}, {-8, 7, 20}})
	assertMat3(t, "mᵀ", m.Transpose(), Mat3{{1, 4, 7}, {2, 5, 8}, {3, 6, 10}})
	assertMat3(t, "m+n", m.Add(n), Mat3{{1, 3, 3}, {3, 5, 6}, {7, 8, 12}})
	assertMat3(t, "m·0.5", m.MulScalar(0.5), Mat3{{0.5, 1, 1.5}, {2, 2.5, 3}, {3.5, 4, 5}})
	assertVec3(t, "m·v", m.MulVec(Vec3{1, 0, -1}), Vec3{-2, -2, -3})
	assertClose(t, "det m", m.Determinant(), -3)
	assertClose(t, "det I", IdentityMat3().Determinant(), 1)
	assertClose(t, "det singular", Mat3{{1, 2, 3}, {2, 4, 6}, {0, 1, 1}}.Determinant(), 0)
	assertMat3(t, "outer", Outer(Vec3{1, 2, 3}, Vec3{4, 5, 6}), Mat3{{4, 5, 6}, {8, 10, 12}, {12, 15, 18}})
}

func TestMat3IsFinite(t *testing.T) {
	if !IdentityMat3().IsFinite() {
		t.Error("the identity is reported as not finite")
	}
	for _, bad := range []float64{math.NaN(), math.Inf(1)} {
		m := IdentityMat3()
		m[2][1] = bad
		if m.IsFinite() {
			t.Errorf("a matrix holding %v is reported as finite", bad)
		}
	}
}

// assertEigendecomposition checks that values and vectors decompose m: the
// values are sorted from largest to smallest, the vectors form a rotation
// matrix and V·diag(values)·Vᵀ rebuilds m.
func assertEigendecomposition(t *testing.T, m Mat3, values Vec3, vectors Mat3) {
	t.Helper()
	if values.X < values.Y || values.Y < values.Z {
		t.Errorf("eigenvalues %+v are not sorted from largest to smallest", values)
	}
	assertRotationMatrix(t, "eigenvectors", vectors)
	rebuilt := vectors.Mul(diagonal(values.X, values.Y, values.Z)).Mul(vectors.Transpose())
	assertMat3(t, "V·diag(values)·Vᵀ", rebuilt, m)
}

func TestSymmetricEigenDecomposesCovariances(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for range 200 {
		r := randomRotation(rng).RotationMatrix()
		variances := diagonal(0.0001+rng.Float64(), 0.0001+rng.Float64(), 0.0001+rng.Float64())
		m := r.Mul(variances).Mul(r.Transpose())
		values, vectors, err := m.SymmetricEigen()
		if err != nil {
			t.Fatalf("decomposing %v: %v", m, err)
		}
		assertEigendecomposition(t, m, values, vectors)
	}
}

func TestSymmetricEigenOfDiagonalMatrices(t *testing.T) {
	tests := []struct {
		name       string
		m          Mat3
		wantValues Vec3
	}{
		{"distinct values", diagonal(1, 3, 2), Vec3{3, 2, 1}},
		{"repeated values", diagonal(2, 2, 2), Vec3{2, 2, 2}},
		{"a zero value", diagonal(0, 5, 1), Vec3{5, 1, 0}},
		{"negative values", diagonal(-1, 4, -6), Vec3{4, -1, -6}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			values, vectors, err := tt.m.SymmetricEigen()
			if err != nil {
				t.Fatal(err)
			}
			assertVec3(t, "eigenvalues", values, tt.wantValues)
			assertEigendecomposition(t, tt.m, values, vectors)
		})
	}
}

func TestSymmetricEigenRejectsNonFiniteMatrices(t *testing.T) {
	for _, bad := range []float64{math.NaN(), math.Inf(-1)} {
		m := diagonal(1, 2, 3)
		m[0][1], m[1][0] = bad, bad
		if _, _, err := m.SymmetricEigen(); err == nil {
			t.Errorf("decomposing a matrix holding %v did not fail", bad)
		}
	}
}
