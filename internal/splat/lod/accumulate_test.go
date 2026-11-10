package lod

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/mfbonfigli/gotiler/v3/internal/geom"
	"github.com/mfbonfigli/gotiler/v3/internal/splat"
)

func TestWeight(t *testing.T) {
	s := roundSplat(0, geom.Vec3{})
	s.Scale = geom.Vec3{X: 1, Y: 2, Z: 3}
	s.Opacity = 0.5
	assertClose(t, "weight", Weight(s), 0.5*(2+3+6)/3.0)

	s.Opacity = 0
	assertClose(t, "transparent weight", Weight(s), minWeight)
	s.Opacity, s.Scale = 1, geom.Vec3{}
	assertClose(t, "pointlike weight", Weight(s), minWeight)
}

// TestAccumulatorMatchesMoments checks that the merged Gaussian has the
// weighted mean and covariance of the splats it merges.
func TestAccumulatorMatchesMoments(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for _, n := range []int{2, 3, 7} {
		splats := make([]splat.Splat, n)
		for i := range splats {
			splats[i] = randomSplat(rng, i, 0)
			// Round enough that no splat is thin, so the surface guard stays off.
			splats[i].Scale = geom.Vec3{X: 0.5 + 0.5*rng.Float64(), Y: 0.5 + 0.5*rng.Float64(), Z: 0.5 + 0.5*rng.Float64()}
		}

		var total float64
		var mean geom.Vec3
		for _, s := range splats {
			total += Weight(s)
			mean = mean.Add(s.Center.MulScalar(Weight(s)))
		}
		mean = mean.MulScalar(1 / total)
		var want geom.Mat3
		for _, s := range splats {
			d := s.Center.Sub(mean)
			want = want.Add(covariance(s).Add(geom.Outer(d, d)).MulScalar(Weight(s) / total))
		}

		merged := merge(t, splats...)
		assertVec3(t, "center", merged.Center, mean)
		assertMat3(t, "covariance", covariance(merged), want)
	}
}

// TestAccumulatorMatchesPairMoments checks a pair by hand: two equal round
// splats one unit apart merge into a Gaussian with a quarter of a unit of
// extra variance along the line between them.
func TestAccumulatorMatchesPairMoments(t *testing.T) {
	merged := merge(t, roundSplat(0, geom.Vec3{}), roundSplat(1, geom.Vec3{X: 1}))
	assertVec3(t, "center", merged.Center, geom.Vec3{X: 0.5})
	assertMat3(t, "covariance", covariance(merged), geom.Mat3{{0.0004 + 0.25, 0, 0}, {0, 0.0004, 0}, {0, 0, 0.0004}})
}

func TestAccumulatorCombinesAppearance(t *testing.T) {
	weak := roundSplat(3, geom.Vec3{})
	weak.Opacity = 0.2
	weak.SH0 = geom.Vec3{X: 1}
	weak.SHRest = []geom.Vec3{{X: 1}, {Y: 1}, {Z: 1}}
	strong := roundSplat(7, geom.Vec3{X: 0.01})
	strong.Opacity = 0.6
	strong.SH0 = geom.Vec3{Y: 1}
	strong.SHRest = []geom.Vec3{{X: 9}, {Y: 9}, {Z: 9}}

	for _, order := range [][]splat.Splat{{weak, strong}, {strong, weak}} {
		merged := merge(t, order...)
		// Coverage combines: light passes through both splats in turn.
		assertClose(t, "opacity", merged.Opacity, 1-0.8*0.4)
		assertVec3(t, "SH0", merged.SH0, geom.Vec3{X: 0.25, Y: 0.75})
		if &merged.SHRest[0] != &strong.SHRest[0] || len(merged.SHRest) != 3 {
			t.Errorf("SHRest = %v, want the coefficients of the heavier splat %v", merged.SHRest, strong.SHRest)
		}
		if merged.SourceIndex != 3 {
			t.Errorf("SourceIndex = %d, want the lowest, 3", merged.SourceIndex)
		}
	}
}

func TestAccumulatorKeepsNilSHRest(t *testing.T) {
	if merged := merge(t, roundSplat(0, geom.Vec3{}), roundSplat(1, geom.Vec3{X: 0.1})); merged.SHRest != nil {
		t.Errorf("SHRest = %v, want nil", merged.SHRest)
	}
}

// thinSplat returns a splat flat in the XY plane, so with its normal along Z,
// turned by rotation.
func thinSplat(index int, center geom.Vec3, rotation geom.Quat) splat.Splat {
	s := roundSplat(index, center)
	s.Rotation = rotation
	s.Scale = geom.Vec3{X: 1, Y: 1, Z: 0.01}
	return s
}

// bumpySurface returns thin splats along the XY plane, at heights that vary
// by more than their thickness.
func bumpySurface(rotation geom.Quat) []splat.Splat {
	return []splat.Splat{
		thinSplat(0, geom.Vec3{X: -1, Y: -1, Z: 0.05}, rotation),
		thinSplat(1, geom.Vec3{X: 1, Y: -1, Z: -0.05}, rotation),
		thinSplat(2, geom.Vec3{X: -1, Y: 1, Z: -0.05}, rotation),
		thinSplat(3, geom.Vec3{X: 1, Y: 1, Z: 0.05}, rotation),
	}
}

func TestAccumulatorGuardsThinSurfaces(t *testing.T) {
	flipped := geom.Quat{X: 1} // Turns the normal from +Z to -Z.
	for name, splats := range map[string][]splat.Splat{
		"aligned normals":  bumpySurface(geom.IdentityQuat()),
		"opposite normals": append(bumpySurface(geom.IdentityQuat())[:2], bumpySurface(flipped)[2:]...),
	} {
		t.Run(name, func(t *testing.T) {
			merged := merge(t, splats...)
			// Matching moments would give 0.0001 + 0.0025 along the normal.
			assertClose(t, "normal variance", variance(merged, geom.Vec3{Z: 1}), thicknessGrowth*0.0001)
			assertClose(t, "tangent variance", variance(merged, geom.Vec3{X: 1}), 1+1)
			assertClose(t, "tangent variance", variance(merged, geom.Vec3{Y: 1}), 1+1)
		})
	}
}

// TestAccumulatorGuardKeepsTangents checks the guard on splats of a tilted
// surface whose centers drift off it along one tangent: it caps the variance
// along the normal and drops its correlation with the tangent, but keeps the
// spread along the surface.
func TestAccumulatorGuardKeepsTangents(t *testing.T) {
	tilt := geom.Quat{Y: math.Sin(0.3 / 2), W: math.Cos(0.3 / 2)}
	normal := tilt.RotationMatrix().MulVec(geom.Vec3{Z: 1})
	tangent := tilt.RotationMatrix().MulVec(geom.Vec3{X: 1})
	var splats []splat.Splat
	for _, offset := range []float64{-1, 1} {
		for _, y := range []float64{-1, 1} {
			center := tangent.MulScalar(offset).Add(geom.Vec3{Y: y}).Add(normal.MulScalar(0.05 * offset))
			splats = append(splats, thinSplat(len(splats), center, tilt))
		}
	}
	merged := merge(t, splats...)
	assertClose(t, "normal variance", variance(merged, normal), thicknessGrowth*0.0001)
	assertClose(t, "tangent variance", variance(merged, tangent), 2)
	assertClose(t, "Y variance", variance(merged, geom.Vec3{Y: 1}), 2)
	if c := normal.Dot(covariance(merged).MulVec(tangent)); math.Abs(c) > tolerance {
		t.Errorf("normal-tangent covariance = %v, want 0", c)
	}
}

func TestAccumulatorGuardStaysOff(t *testing.T) {
	quarterTurn := geom.Quat{X: math.Sqrt2 / 2, W: math.Sqrt2 / 2} // Turns the normal from Z to Y.
	thin := bumpySurface(geom.IdentityQuat())
	round := make([]splat.Splat, len(thin))
	for i, s := range thin {
		round[i] = s
		round[i].Scale = geom.Vec3{X: 1, Y: 1, Z: 0.5}
	}
	// Make the round splats outweigh the thin ones.
	heavyRound := append([]splat.Splat{}, thin[:2]...)
	for _, s := range round[2:] {
		s.Scale = geom.Vec3{X: 3, Y: 3, Z: 1.5}
		heavyRound = append(heavyRound, s)
	}

	for name, splats := range map[string][]splat.Splat{
		"round splats":        round,
		"crossed normals":     append(append([]splat.Splat{}, thin[:2]...), bumpySurface(quarterTurn)[2:]...),
		"mostly round":        heavyRound,
		"already thin enough": {thinSplat(0, geom.Vec3{}, geom.IdentityQuat()), thinSplat(1, geom.Vec3{X: 1, Z: 0.01}, geom.IdentityQuat())},
	} {
		t.Run(name, func(t *testing.T) {
			var total float64
			var mean geom.Vec3
			for _, s := range splats {
				total += Weight(s)
				mean = mean.Add(s.Center.MulScalar(Weight(s)))
			}
			mean = mean.MulScalar(1 / total)
			var want geom.Mat3
			for _, s := range splats {
				d := s.Center.Sub(mean)
				want = want.Add(covariance(s).Add(geom.Outer(d, d)).MulScalar(Weight(s) / total))
			}
			assertMat3(t, "covariance", covariance(merge(t, splats...)), want)
		})
	}
}

func TestThinNormal(t *testing.T) {
	rotation := geom.Quat{X: 0.2, Y: -0.4, Z: 0.1, W: 0.9}.RotationMatrix()
	normal, v, ok := thinNormal(rotation, geom.Vec3{X: 1, Y: 0.2, Z: 0.5})
	if !ok {
		t.Fatal("a splat with scales 1, 0.2 and 0.5 is thin")
	}
	assertVec3(t, "normal", normal, geom.Vec3{X: rotation[0][1], Y: rotation[1][1], Z: rotation[2][1]})
	assertClose(t, "variance", v, 0.04)

	for _, scale := range []geom.Vec3{{X: 1, Y: 0.36, Z: 1}, {X: 1, Y: 0, Z: 1}, {}} {
		if _, _, ok := thinNormal(rotation, scale); ok {
			t.Errorf("a splat with scale %+v is not thin", scale)
		}
	}
}

func TestTangents(t *testing.T) {
	for _, normal := range []geom.Vec3{{X: 1}, {Y: 1}, {Z: 1}, {Z: -1}, geom.Vec3{X: 1, Y: 2, Z: 3}.MulScalar(1 / math.Sqrt(14))} {
		a, b := tangents(normal)
		assertClose(t, "|a|", a.Length(), 1)
		assertClose(t, "|b|", b.Length(), 1)
		assertClose(t, "a·b", a.Dot(b), 0)
		assertClose(t, "a·n", a.Dot(normal), 0)
		assertClose(t, "b·n", b.Dot(normal), 0)
	}
}
