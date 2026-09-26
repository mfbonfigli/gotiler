package compression

import (
	"math"
)

// srgbToLinear converts an sRGB uint8 channel value to a linear-light uint8 value
// using the standard gamma 2.2 approximation. Computed once at init, used as a
// lookup table in the per-point loop to avoid calling math.Pow per point.
var srgbToLinear [256]uint8

func init() {
	for i := range 256 {
		srgbToLinear[i] = uint8(math.Pow(float64(i)/255.0, 2.2) * 255.0)
	}
}
