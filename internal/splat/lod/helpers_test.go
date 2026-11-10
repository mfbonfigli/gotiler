package lod

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/mfbonfigli/gotiler/v3/internal/geom"
	"github.com/mfbonfigli/gotiler/v3/internal/splat"
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

// covariance returns the covariance matrix of s.
func covariance(s splat.Splat) geom.Mat3 {
	return splat.CovarianceFromRotationScale(s.Rotation.RotationMatrix(), s.Scale)
}

// variance returns the variance of s along the unit vector direction.
func variance(s splat.Splat, direction geom.Vec3) float64 {
	return direction.Dot(covariance(s).MulVec(direction))
}

// roundSplat returns a small round splat at center.
func roundSplat(index int, center geom.Vec3) splat.Splat {
	return splat.Splat{
		Center:      center,
		Rotation:    geom.IdentityQuat(),
		Scale:       geom.Vec3{X: 0.02, Y: 0.02, Z: 0.02},
		Opacity:     0.8,
		SH0:         geom.Vec3{X: 0.1, Y: 0.2, Z: 0.3},
		SourceIndex: index,
	}
}

// colorSH0 returns the degree-0 coefficients of the color r, g, b.
func colorSH0(r, g, b float64) geom.Vec3 {
	const basis = 0.28209479177387814
	return geom.Vec3{X: (r - 0.5) / basis, Y: (g - 0.5) / basis, Z: (b - 0.5) / basis}
}

// randomSplat returns a splat near the origin with a random shape and
// appearance and shCount higher-degree coefficients.
func randomSplat(rng *rand.Rand, index, shCount int) splat.Splat {
	s := splat.Splat{
		Center:      geom.Vec3{X: rng.Float64() * 10, Y: rng.Float64() * 10, Z: rng.Float64()},
		Rotation:    geom.Quat{X: rng.NormFloat64(), Y: rng.NormFloat64(), Z: rng.NormFloat64(), W: rng.NormFloat64()}.Normalize(),
		Scale:       geom.Vec3{X: 0.005 + 0.05*rng.Float64(), Y: 0.005 + 0.05*rng.Float64(), Z: 0.005 + 0.05*rng.Float64()},
		Opacity:     rng.Float64(),
		SH0:         geom.Vec3{X: rng.NormFloat64(), Y: rng.NormFloat64(), Z: rng.NormFloat64()},
		SourceIndex: index,
	}
	if shCount > 0 {
		s.SHRest = make([]geom.Vec3, shCount)
		for j := range s.SHRest {
			s.SHRest[j] = geom.Vec3{X: rng.NormFloat64() * 0.1, Y: rng.NormFloat64() * 0.1, Z: rng.NormFloat64() * 0.1}
		}
	}
	return s
}

func randomSplats(seed uint64, n, shCount int) []splat.Splat {
	rng := rand.New(rand.NewPCG(seed, 1))
	splats := make([]splat.Splat, n)
	for i := range splats {
		splats[i] = randomSplat(rng, i, shCount)
	}
	return splats
}

// gridSplats returns round splats on a side × side grid of unit spacing in
// the XY plane.
func gridSplats(side int) []splat.Splat {
	splats := make([]splat.Splat, 0, side*side)
	for y := range side {
		for x := range side {
			splats = append(splats, roundSplat(len(splats), geom.Vec3{X: float64(x), Y: float64(y)}))
		}
	}
	return splats
}

// merge returns the splat an accumulator makes of splats.
func merge(t *testing.T, splats ...splat.Splat) splat.Splat {
	t.Helper()
	var acc accumulator
	for _, s := range splats {
		acc.add(s)
	}
	merged, err := acc.finish()
	if err != nil {
		t.Fatal(err)
	}
	return merged
}
