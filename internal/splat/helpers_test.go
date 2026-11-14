package splat

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/mfbonfigli/gotiler/v3/internal/geom"
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

func assertVec3(t *testing.T, name string, got, want geom.Vec3) {
	t.Helper()
	if !closeTo(got.X, want.X) || !closeTo(got.Y, want.Y) || !closeTo(got.Z, want.Z) {
		t.Errorf("%s = %+v, want %+v", name, got, want)
	}
}

func assertMat3(t *testing.T, name string, got, want geom.Mat3) {
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

func diagonal(x, y, z float64) geom.Mat3 {
	return geom.Mat3{{x, 0, 0}, {0, y, 0}, {0, 0, z}}
}

func randomRotation(rng *rand.Rand) geom.Quat {
	return geom.Quat{X: rng.NormFloat64(), Y: rng.NormFloat64(), Z: rng.NormFloat64(), W: rng.NormFloat64()}.Normalize()
}

func randomScale(rng *rand.Rand) geom.Vec3 {
	return geom.Vec3{X: 0.01 + rng.Float64(), Y: 0.01 + rng.Float64(), Z: 0.01 + rng.Float64()}
}
