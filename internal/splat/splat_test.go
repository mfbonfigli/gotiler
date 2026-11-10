package splat

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/mfbonfigli/gotiler/v3/internal/geom"
)

func validSplat() Splat {
	return Splat{
		Center:      geom.Vec3{X: 1, Y: 2, Z: 3},
		Rotation:    geom.IdentityQuat(),
		Scale:       geom.Vec3{X: 0.1, Y: 0.2, Z: 0.3},
		Opacity:     0.5,
		SH0:         geom.Vec3{X: 0.1, Y: -0.2, Z: 0.3},
		SHRest:      []geom.Vec3{{X: 0.01, Y: 0.02, Z: 0.03}, {X: -0.01, Y: 0, Z: 0.01}, {X: 0, Y: 0, Z: 0}},
		SourceIndex: 42,
	}
}

func TestValidateAcceptsValidSplats(t *testing.T) {
	edges := validSplat()
	edges.Opacity = 1
	edges.Scale = geom.Vec3{}
	transparent := validSplat()
	transparent.Opacity = 0
	for _, s := range []Splat{validSplat(), edges, transparent} {
		if err := s.Validate(); err != nil {
			t.Errorf("validating %+v: %v", s, err)
		}
	}
}

func TestValidateRejectsInvalidSplats(t *testing.T) {
	nan := math.NaN()
	tests := []struct {
		name   string
		modify func(*Splat)
	}{
		{"non-finite center", func(s *Splat) { s.Center.Y = nan }},
		{"non-finite rotation", func(s *Splat) { s.Rotation.W = math.Inf(1) }},
		{"negative scale", func(s *Splat) { s.Scale.Z = -0.1 }},
		{"non-finite scale", func(s *Splat) { s.Scale.X = math.Inf(1) }},
		{"opacity above 1", func(s *Splat) { s.Opacity = 1.01 }},
		{"negative opacity", func(s *Splat) { s.Opacity = -0.01 }},
		{"non-finite opacity", func(s *Splat) { s.Opacity = nan }},
		{"non-finite color", func(s *Splat) { s.SH0.Z = nan }},
		{"non-finite spherical harmonics", func(s *Splat) { s.SHRest[2].X = nan }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := validSplat()
			tt.modify(&s)
			if err := s.Validate(); err == nil {
				t.Error("validation did not fail")
			}
		})
	}
}

func TestCovarianceFromRotationScale(t *testing.T) {
	scale := geom.Vec3{X: 1, Y: 2, Z: 3}
	assertMat3(t, "axis-aligned covariance", CovarianceFromRotationScale(geom.IdentityMat3(), scale), diagonal(1, 4, 9))

	half := math.Pi / 4
	quarterTurnAboutZ := geom.Quat{Z: math.Sin(half), W: math.Cos(half)}.RotationMatrix()
	assertMat3(t, "covariance turned about Z", CovarianceFromRotationScale(quarterTurnAboutZ, scale), diagonal(4, 1, 9))

	rng := rand.New(rand.NewPCG(7, 8))
	for range 50 {
		r := randomRotation(rng).RotationMatrix()
		s := randomScale(rng)
		want := r.Mul(diagonal(s.X*s.X, s.Y*s.Y, s.Z*s.Z)).Mul(r.Transpose())
		assertMat3(t, "covariance", CovarianceFromRotationScale(r, s), want)
	}
}

func TestWithCovarianceRoundTrip(t *testing.T) {
	rng := rand.New(rand.NewPCG(9, 10))
	for range 200 {
		s := validSplat()
		s.Rotation = randomRotation(rng)
		s.Scale = randomScale(rng)
		covariance := CovarianceFromRotationScale(s.Rotation.RotationMatrix(), s.Scale)

		got, err := s.WithCovariance(covariance)
		if err != nil {
			t.Fatal(err)
		}
		assertMat3(t, "covariance of the reshaped splat", CovarianceFromRotationScale(got.Rotation.RotationMatrix(), got.Scale), covariance)
		if got.Scale.X < got.Scale.Y || got.Scale.Y < got.Scale.Z {
			t.Errorf("scale %+v is not sorted from largest to smallest", got.Scale)
		}
		assertClose(t, "rotation length", math.Sqrt(got.Rotation.X*got.Rotation.X+got.Rotation.Y*got.Rotation.Y+got.Rotation.Z*got.Rotation.Z+got.Rotation.W*got.Rotation.W), 1)
		if got.Center != s.Center || got.Opacity != s.Opacity || got.SH0 != s.SH0 || got.SourceIndex != s.SourceIndex || len(got.SHRest) != len(s.SHRest) {
			t.Errorf("reshaping changed more than the shape: got %+v from %+v", got, s)
		}
	}
}

func TestWithCovarianceFloorsDegenerateShapes(t *testing.T) {
	got, err := validSplat().WithCovariance(diagonal(4, 0, -1e-20))
	if err != nil {
		t.Fatal(err)
	}
	floor := math.Sqrt(minVariance)
	assertVec3(t, "scale", got.Scale, geom.Vec3{X: 2, Y: floor, Z: floor})
}

func TestWithCovarianceRejectsNonFiniteMatrices(t *testing.T) {
	if _, err := validSplat().WithCovariance(diagonal(1, math.NaN(), 1)); err == nil {
		t.Error("reshaping to a non-finite covariance did not fail")
	}
}

func TestSplatBounds(t *testing.T) {
	s := validSplat()
	s.Center = geom.Vec3{X: 1, Y: 2, Z: 3}
	s.Scale = geom.Vec3{X: 0.5, Y: 2, Z: 1}

	b := s.Bounds(3)
	assertVec3(t, "min", b.Min, geom.Vec3{X: -5, Y: -4, Z: -3})
	assertVec3(t, "max", b.Max, geom.Vec3{X: 7, Y: 8, Z: 9})
	if b.IsEmpty() {
		t.Error("the bounds of a splat are empty")
	}
}

func TestBaseColor(t *testing.T) {
	tests := []struct {
		name string
		sh0  geom.Vec3
		want geom.Vec3
	}{
		{"zero is mid gray", geom.Vec3{}, geom.Vec3{X: 0.5, Y: 0.5, Z: 0.5}},
		{"one coefficient unit", geom.Vec3{X: 1, Y: -1, Z: 0}, geom.Vec3{X: 0.5 + shBasisDegree0, Y: 0.5 - shBasisDegree0, Z: 0.5}},
		{"out of range values are clamped", geom.Vec3{X: 10, Y: -10, Z: 1.7724538509055159}, geom.Vec3{X: 1, Y: 0, Z: 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := validSplat()
			s.SH0 = tt.sh0
			assertVec3(t, "base color", s.BaseColor(), tt.want)
		})
	}
}
