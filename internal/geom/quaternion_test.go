package geom

import (
	"math"
	"math/rand/v2"
	"testing"
)

func assertSameRotation(t *testing.T, got, want Quat) {
	t.Helper()
	// q and -q represent the same rotation.
	if want.X*got.X+want.Y*got.Y+want.Z*got.Z+want.W*got.W < 0 {
		want = Quat{-want.X, -want.Y, -want.Z, -want.W}
	}
	if !closeTo(got.X, want.X) || !closeTo(got.Y, want.Y) || !closeTo(got.Z, want.Z) || !closeTo(got.W, want.W) {
		t.Errorf("quaternion = %+v, want %+v", got, want)
	}
}

func TestQuatNormalize(t *testing.T) {
	assertSameRotation(t, Quat{X: 0, Y: 3, Z: 0, W: 4}.Normalize(), Quat{Y: 0.6, W: 0.8})
	for _, q := range []Quat{{}, {X: math.NaN(), W: 1}, {Y: math.Inf(1)}} {
		if got := q.Normalize(); got != IdentityQuat() {
			t.Errorf("normalizing %+v = %+v, want the identity", q, got)
		}
	}
}

func TestQuatIsFinite(t *testing.T) {
	if !IdentityQuat().IsFinite() {
		t.Error("the identity is reported as not finite")
	}
	for _, q := range []Quat{{X: math.NaN()}, {Y: math.Inf(1)}, {Z: math.Inf(-1)}, {W: math.NaN()}} {
		if q.IsFinite() {
			t.Errorf("%+v is reported as finite", q)
		}
	}
}

func TestQuatRotationMatrixUsesXYZWOrder(t *testing.T) {
	half := math.Pi / 4
	quarterTurnAboutZ := Quat{Z: math.Sin(half), W: math.Cos(half)}

	m := quarterTurnAboutZ.RotationMatrix()
	assertMat3(t, "rotation matrix", m, Mat3{{0, -1, 0}, {1, 0, 0}, {0, 0, 1}})
	assertVec3(t, "rotated x axis", m.MulVec(Vec3{X: 1}), Vec3{Y: 1})
}

func TestQuatRotationMatrixNormalizesFirst(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 4))
	for range 50 {
		q := randomRotation(rng)
		scaled := Quat{q.X * 7, q.Y * 7, q.Z * 7, q.W * 7}
		assertMat3(t, "rotation matrix of a scaled quaternion", scaled.RotationMatrix(), q.RotationMatrix())
		assertRotationMatrix(t, "rotation matrix", scaled.RotationMatrix())
	}
}

func TestQuatFromRotationMatrixRoundTrip(t *testing.T) {
	rng := rand.New(rand.NewPCG(5, 6))
	quats := []Quat{
		IdentityQuat(),
		// Half turns have a trace of -1, so each picks the conversion branch of
		// its largest diagonal element.
		{X: 1},
		{Y: 1},
		{Z: 1},
		Quat{X: 1, Y: 1, Z: 0.2, W: 0.1}.Normalize(),
		Quat{X: 0.1, Y: 1, Z: 1, W: 0.2}.Normalize(),
	}
	for range 200 {
		quats = append(quats, randomRotation(rng))
	}
	for _, q := range quats {
		assertSameRotation(t, QuatFromRotationMatrix(q.RotationMatrix()), q)
	}
}
