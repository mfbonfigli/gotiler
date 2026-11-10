package tiling

import (
	"testing"

	"github.com/mfbonfigli/gotiler/v3/internal/geom"
)

func TestUpAxisRotation(t *testing.T) {
	for _, test := range []struct {
		names       []string
		up, forward geom.Vec3 // input axes
		wantForward geom.Vec3 // where forward ends up
	}{
		{[]string{"", "z", "+z", " Z "}, geom.Vec3{Z: 1}, geom.Vec3{Y: 1}, geom.Vec3{Y: 1}},
		{[]string{"-z", "-Z"}, geom.Vec3{Z: -1}, geom.Vec3{Y: 1}, geom.Vec3{Y: -1}},
		// As in the point cloud command and glTF: +Z, toward the viewer, ends up south.
		{[]string{"y", "+y", "Y"}, geom.Vec3{Y: 1}, geom.Vec3{Z: 1}, geom.Vec3{Y: -1}},
		// As in OpenCV and COLMAP: +Z, the camera's forward, ends up north.
		{[]string{"-y", " -Y"}, geom.Vec3{Y: -1}, geom.Vec3{Z: 1}, geom.Vec3{Y: 1}},
		{[]string{"x", "+x"}, geom.Vec3{X: 1}, geom.Vec3{Y: 1}, geom.Vec3{Y: 1}},
		{[]string{"-x"}, geom.Vec3{X: -1}, geom.Vec3{Y: 1}, geom.Vec3{Y: 1}},
	} {
		for _, name := range test.names {
			m, err := upAxisRotation(name)
			if err != nil {
				t.Fatalf("%q: %v", name, err)
			}
			assertVec3(t, name+" up", m.MulVec(test.up), geom.Vec3{Z: 1})
			assertVec3(t, name+" forward", m.MulVec(test.forward), test.wantForward)
			// A rotation, not a reflection: orthonormal with determinant 1.
			if m.Mul(m.Transpose()) != geom.IdentityMat3() || m.Determinant() != 1 {
				t.Errorf("%q: %v is not a rotation", name, m)
			}
		}
	}
	for _, name := range []string{"w", "--y", "xy", "up"} {
		if _, err := upAxisRotation(name); err == nil {
			t.Errorf("%q is accepted as an axis", name)
		}
	}
}
