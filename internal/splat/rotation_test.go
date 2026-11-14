package splat

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/mfbonfigli/gotiler/v3/internal/geom"
)

// The constants of eval_sh in utils/sh_utils.py of the reference 3D Gaussian
// Splatting implementation, github.com/graphdeco-inria/gaussian-splatting.
var (
	referenceC1 = 0.4886025119029199
	referenceC2 = [5]float64{1.0925484305920792, -1.0925484305920792, 0.31539156525252005, -1.0925484305920792, 0.5462742152960396}
	referenceC3 = [7]float64{-0.5900435899266435, 2.890611442640554, -0.4570457994644658, 0.3731763325901154, -0.4570457994644658, 1.445305721320277, -0.5900435899266435}
)

// referenceViewColor evaluates the higher-degree spherical harmonics sh in the
// view direction d as eval_sh does, leaving out the degree-0 term.
func referenceViewColor(sh []geom.Vec3, d geom.Vec3) geom.Vec3 {
	x, y, z := d.X, d.Y, d.Z
	xx, yy, zz := x*x, y*y, z*z
	var color geom.Vec3
	add := func(basis float64, coefficient int) {
		color = color.Add(sh[coefficient].MulScalar(basis))
	}
	if len(sh) >= 3 {
		add(-referenceC1*y, 0)
		add(referenceC1*z, 1)
		add(-referenceC1*x, 2)
	}
	if len(sh) >= 8 {
		add(referenceC2[0]*x*y, 3)
		add(referenceC2[1]*y*z, 4)
		add(referenceC2[2]*(2*zz-xx-yy), 5)
		add(referenceC2[3]*x*z, 6)
		add(referenceC2[4]*(xx-yy), 7)
	}
	if len(sh) >= 15 {
		add(referenceC3[0]*y*(3*xx-yy), 8)
		add(referenceC3[1]*x*y*z, 9)
		add(referenceC3[2]*y*(4*zz-xx-yy), 10)
		add(referenceC3[3]*z*(2*zz-3*xx-3*yy), 11)
		add(referenceC3[4]*x*(4*zz-xx-yy), 12)
		add(referenceC3[5]*z*(xx-yy), 13)
		add(referenceC3[6]*x*(xx-3*yy), 14)
	}
	return color
}

func randomDirection(rng *rand.Rand) geom.Vec3 {
	d := geom.Vec3{X: rng.NormFloat64(), Y: rng.NormFloat64(), Z: rng.NormFloat64()}
	return d.MulScalar(1 / d.Length())
}

func randomSH(rng *rand.Rand, count int) []geom.Vec3 {
	sh := make([]geom.Vec3, count)
	for i := range sh {
		sh[i] = geom.Vec3{X: rng.NormFloat64(), Y: rng.NormFloat64(), Z: rng.NormFloat64()}
	}
	return sh
}

// testRotations returns the axis turns used to set a model upright, followed
// by random rotations.
func testRotations(rng *rand.Rand) []geom.Mat3 {
	rotations := []geom.Mat3{
		geom.IdentityMat3(),
		{{1, 0, 0}, {0, 0, -1}, {0, 1, 0}},
		{{1, 0, 0}, {0, 0, 1}, {0, -1, 0}},
		{{0, 0, -1}, {0, 1, 0}, {1, 0, 0}},
		{{1, 0, 0}, {0, -1, 0}, {0, 0, -1}},
	}
	for range 20 {
		rotations = append(rotations, randomRotation(rng).RotationMatrix())
	}
	return rotations
}

func TestRotateTurnsViewDependentColor(t *testing.T) {
	rng := rand.New(rand.NewPCG(11, 12))
	for _, count := range []int{3, 8, 15} {
		for _, m := range testRotations(rng) {
			rotation := NewRotation(m)
			s := validSplat()
			s.SHRest = randomSH(rng, count)
			turned := rotation.Rotate(s)
			for range 10 {
				d := randomDirection(rng)
				assertVec3(t, "color seen from the turned direction", referenceViewColor(turned.SHRest, m.MulVec(d)), referenceViewColor(s.SHRest, d))
			}
		}
	}
}

func TestRotateDegreeOneLikeAVector(t *testing.T) {
	// The degree-1 terms of eval_sh are C1·(d·v) with v = (-c3, -c1, c2):
	// this splat is brightest seen along +X, so turned a quarter about Z it is
	// brightest along +Y, which is v = (0, 1, 0) or c1 = -1.
	s := validSplat()
	s.SHRest = []geom.Vec3{{}, {}, {X: -1, Y: -1, Z: -1}}
	quarterTurnAboutZ := geom.Mat3{{0, -1, 0}, {1, 0, 0}, {0, 0, 1}}

	turned := NewRotation(quarterTurnAboutZ).Rotate(s)
	assertVec3(t, "c1", turned.SHRest[0], geom.Vec3{X: -1, Y: -1, Z: -1})
	assertVec3(t, "c2", turned.SHRest[1], geom.Vec3{})
	assertVec3(t, "c3", turned.SHRest[2], geom.Vec3{})
}

func TestRotateKeepsTheEnergyOfEachDegree(t *testing.T) {
	rng := rand.New(rand.NewPCG(13, 14))
	energy := func(sh []geom.Vec3) [3]float64 {
		var e [3]float64
		for i, c := range sh {
			degree := 0
			switch {
			case i >= 8:
				degree = 2
			case i >= 3:
				degree = 1
			}
			e[degree] += c.LengthSquared()
		}
		return e
	}
	for _, m := range testRotations(rng) {
		s := validSplat()
		s.SHRest = randomSH(rng, 15)
		before, after := energy(s.SHRest), energy(NewRotation(m).Rotate(s).SHRest)
		for degree := range 3 {
			assertClose(t, "energy of a degree", after[degree], before[degree])
		}
	}
}

func TestRotateTurnsPositionAndShape(t *testing.T) {
	rng := rand.New(rand.NewPCG(15, 16))
	for _, m := range testRotations(rng) {
		s := validSplat()
		s.Rotation = randomRotation(rng)
		s.Scale = randomScale(rng)
		s.SHRest = randomSH(rng, 8)
		original := append([]geom.Vec3(nil), s.SHRest...)

		turned := NewRotation(m).Rotate(s)

		assertVec3(t, "center", turned.Center, m.MulVec(s.Center))
		if turned.Scale != s.Scale {
			t.Errorf("scale = %+v, want %+v unchanged", turned.Scale, s.Scale)
		}
		covariance := CovarianceFromRotationScale(s.Rotation.RotationMatrix(), s.Scale)
		assertMat3(t, "covariance", CovarianceFromRotationScale(turned.Rotation.RotationMatrix(), turned.Scale), m.Mul(covariance).Mul(m.Transpose()))
		if turned.Opacity != s.Opacity || turned.SH0 != s.SH0 || turned.SourceIndex != s.SourceIndex {
			t.Errorf("rotating changed the appearance: got %+v from %+v", turned, s)
		}
		turned.SHRest[0] = geom.Vec3{X: 100, Y: 100, Z: 100}
		for i := range original {
			if s.SHRest[i] != original[i] {
				t.Fatal("rotating changed the spherical harmonics of the input splat")
			}
		}
	}
}

func TestRotateSplatsWithoutSH(t *testing.T) {
	s := validSplat()
	s.SHRest = nil
	half := math.Pi / 4
	turned := NewRotation(geom.Quat{X: math.Sin(half), W: math.Cos(half)}.RotationMatrix()).Rotate(s)
	if len(turned.SHRest) != 0 {
		t.Errorf("a splat without spherical harmonics got %d coefficients", len(turned.SHRest))
	}
	if err := turned.Validate(); err != nil {
		t.Error(err)
	}
}

func TestValidateRequiresCompleteSHDegrees(t *testing.T) {
	for count := range 25 {
		s := validSplat()
		s.SHRest = make([]geom.Vec3, count)
		complete := count == 0 || count == 3 || count == 8 || count == 15
		if err := s.Validate(); complete != (err == nil) {
			t.Errorf("validating %d coefficients: err = %v", count, err)
		}
	}
}
