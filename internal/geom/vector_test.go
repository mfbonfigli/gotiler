package geom

import (
	"math"
	"testing"
)

func TestVec3Arithmetic(t *testing.T) {
	a := Vec3{1, 2, 3}
	b := Vec3{-4, 5, 0.5}

	assertVec3(t, "a+b", a.Add(b), Vec3{-3, 7, 3.5})
	assertVec3(t, "a-b", a.Sub(b), Vec3{5, -3, 2.5})
	assertVec3(t, "a·2", a.MulScalar(2), Vec3{2, 4, 6})
	assertClose(t, "a·b", a.Dot(b), 7.5)
	assertClose(t, "|(3,4,12)|²", Vec3{3, 4, 12}.LengthSquared(), 169)
	assertClose(t, "|(3,4,12)|", Vec3{3, 4, 12}.Length(), 13)
	assertClose(t, "max component", Vec3{-1, -7, -3}.MaxComponent(), -1)
}

func TestVec3CrossIsRightHanded(t *testing.T) {
	x, y, z := Vec3{X: 1}, Vec3{Y: 1}, Vec3{Z: 1}
	assertVec3(t, "x×y", x.Cross(y), z)
	assertVec3(t, "y×z", y.Cross(z), x)
	assertVec3(t, "z×x", z.Cross(x), y)

	a, b := Vec3{1, 2, 3}, Vec3{-4, 5, 0.5}
	assertVec3(t, "a×b", a.Cross(b), b.Cross(a).MulScalar(-1))
	assertClose(t, "(a×b)·a", a.Cross(b).Dot(a), 0)
}

func TestVec3IsFinite(t *testing.T) {
	if !(Vec3{1, -2, 0}).IsFinite() {
		t.Error("a finite vector is reported as not finite")
	}
	for _, bad := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		for _, v := range []Vec3{{X: bad}, {Y: bad}, {Z: bad}} {
			if v.IsFinite() {
				t.Errorf("%+v is reported as finite", v)
			}
		}
	}
}
