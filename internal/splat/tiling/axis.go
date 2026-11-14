package tiling

import (
	"fmt"
	"strings"

	"github.com/mfbonfigli/gotiler/v3/internal/geom"
)

// upAxisRotation returns the rotation that turns the input axis named by
// axis, such as "y", "+y" or "-y", to +Z, the up axis of the tiles. An empty
// name means +Z.
//
// The positive axes turn like in the point cloud command. Y up turns as glTF
// content does, its +Z becoming -Y, and -Y up, the convention of OpenCV and
// COLMAP, keeps its +Z forward as +Y.
func upAxisRotation(axis string) (geom.Mat3, error) {
	switch strings.ToLower(strings.TrimSpace(axis)) {
	case "", "z", "+z":
		return geom.IdentityMat3(), nil
	case "-z":
		// (x, y, z) -> (x, -y, -z)
		return geom.Mat3{{1, 0, 0}, {0, -1, 0}, {0, 0, -1}}, nil
	case "y", "+y":
		// (x, y, z) -> (x, -z, y)
		return geom.Mat3{{1, 0, 0}, {0, 0, -1}, {0, 1, 0}}, nil
	case "-y":
		// (x, y, z) -> (x, z, -y)
		return geom.Mat3{{1, 0, 0}, {0, 0, 1}, {0, -1, 0}}, nil
	case "x", "+x":
		// (x, y, z) -> (-z, y, x)
		return geom.Mat3{{0, 0, -1}, {0, 1, 0}, {1, 0, 0}}, nil
	case "-x":
		// (x, y, z) -> (z, y, -x)
		return geom.Mat3{{0, 0, 1}, {0, 1, 0}, {-1, 0, 0}}, nil
	default:
		return geom.Mat3{}, fmt.Errorf("the up axis %q must be one of x, y, z, -x, -y or -z", axis)
	}
}
