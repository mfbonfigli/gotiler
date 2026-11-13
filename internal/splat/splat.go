// Package splat holds Gaussian splats: their shape, their appearance and the
// rotations that turn them.
package splat

import (
	"cmp"
	"errors"
	"math"
	"slices"

	"github.com/mfbonfigli/gotiler/v3/internal/geom"
	"github.com/mfbonfigli/gotiler/v3/internal/radix"
)

// shBasisDegree0 is the value of the degree-0 spherical harmonic basis
// function, 1/(2·√π).
const shBasisDegree0 = 0.28209479177387814

// minVariance floors the variances of a covariance matrix converted back to a
// splat, so that flat or degenerate shapes keep a small positive scale.
const minVariance = 1e-18

// Splat is one 3D Gaussian with its appearance.
//
// Scale holds the standard deviations along the local axes of the Gaussian,
// in world units, and Opacity is in [0, 1]: both are activated values, not the
// log scale and logit that trainers store. SH0 is the degree-0 spherical
// harmonic coefficient of each color channel, and SHRest the coefficients of
// the higher degrees in band order, one RGB triplet per coefficient: 3, 8 or
// 15 of them for degrees 1, 2 or 3.
type Splat struct {
	Center   geom.Vec3
	Rotation geom.Quat
	Scale    geom.Vec3
	Opacity  float64
	SH0      geom.Vec3
	SHRest   []geom.Vec3
	// SourceIndex is the position of the splat in its input file, used to
	// order splats deterministically.
	SourceIndex int
}

// Validate checks that every value of the splat is finite and in range.
func (s Splat) Validate() error {
	switch {
	case !s.Center.IsFinite():
		return errors.New("the splat center is not finite")
	case !s.Rotation.IsFinite():
		return errors.New("the splat rotation is not finite")
	case !s.Scale.IsFinite() || s.Scale.X < 0 || s.Scale.Y < 0 || s.Scale.Z < 0:
		return errors.New("the splat scale must be finite and not negative")
	case math.IsNaN(s.Opacity) || s.Opacity < 0 || s.Opacity > 1:
		return errors.New("the splat opacity must be between 0 and 1")
	case !s.SH0.IsFinite():
		return errors.New("the splat color is not finite")
	case !holdsCompleteSHDegrees(len(s.SHRest)):
		return errors.New("the splat spherical harmonics must hold complete degrees, up to degree 3")
	}
	for _, coefficient := range s.SHRest {
		if !coefficient.IsFinite() {
			return errors.New("the splat spherical harmonics are not finite")
		}
	}
	return nil
}

// holdsCompleteSHDegrees reports whether count higher-degree coefficients
// fill degrees 1 to n exactly, for some n up to the maximum degree.
func holdsCompleteSHDegrees(count int) bool {
	filled := 0
	for degree := 1; filled < count && degree <= maxSHDegree; degree++ {
		filled += 2*degree + 1
	}
	return filled == count
}

// CovarianceFromRotationScale returns the covariance matrix R·diag(scale²)·Rᵀ
// of a Gaussian with the given rotation matrix and standard deviations.
func CovarianceFromRotationScale(rotation geom.Mat3, scale geom.Vec3) geom.Mat3 {
	variances := [3]float64{scale.X * scale.X, scale.Y * scale.Y, scale.Z * scale.Z}
	var covariance geom.Mat3
	for r := range 3 {
		for c := range 3 {
			covariance[r][c] = rotation[r][0]*rotation[c][0]*variances[0] +
				rotation[r][1]*rotation[c][1]*variances[1] +
				rotation[r][2]*rotation[c][2]*variances[2]
		}
	}
	return covariance
}

// WithCovariance returns the splat reshaped to the Gaussian with the given
// covariance matrix: its eigenvectors become the rotation and the square roots
// of its eigenvalues the scale, largest first.
func (s Splat) WithCovariance(covariance geom.Mat3) (Splat, error) {
	variances, axes, err := covariance.SymmetricEigen()
	if err != nil {
		return Splat{}, err
	}
	s.Rotation = geom.QuatFromRotationMatrix(axes)
	s.Scale = geom.Vec3{
		X: math.Sqrt(math.Max(variances.X, minVariance)),
		Y: math.Sqrt(math.Max(variances.Y, minVariance)),
		Z: math.Sqrt(math.Max(variances.Z, minVariance)),
	}
	return s, s.Validate()
}

// Bounds returns a cube centered on the splat that contains it out to sigmas
// standard deviations along its largest axis.
func (s Splat) Bounds(sigmas float64) geom.Bounds {
	radius := sigmas * s.Scale.MaxComponent()
	var b geom.Bounds
	b.IncludePoint(geom.Vec3{X: s.Center.X - radius, Y: s.Center.Y - radius, Z: s.Center.Z - radius})
	b.IncludePoint(geom.Vec3{X: s.Center.X + radius, Y: s.Center.Y + radius, Z: s.Center.Z + radius})
	return b
}

// BaseColor returns the view-independent RGB color of the splat, each channel
// in [0, 1].
func (s Splat) BaseColor() geom.Vec3 {
	return geom.Vec3{
		X: clamp01(s.SH0.X*shBasisDegree0 + 0.5),
		Y: clamp01(s.SH0.Y*shBasisDegree0 + 0.5),
		Z: clamp01(s.SH0.Z*shBasisDegree0 + 0.5),
	}
}

func clamp01(x float64) float64 { return math.Min(math.Max(x, 0), 1) }

// SortBySourceIndex orders splats by SourceIndex, keeping splats with the same
// index in their order.
//
// It sorts the indices alone and then moves each splat once, to its place,
// which costs less than moving the splats at every step of a sort.
func SortBySourceIndex(splats []Splat) {
	if slices.IsSortedFunc(splats, func(a, b Splat) int { return cmp.Compare(a.SourceIndex, b.SourceIndex) }) {
		return
	}
	order := make([]radix.Item, len(splats))
	for i, s := range splats {
		// Flipping the sign bit orders the keys like the signed indices.
		order[i] = radix.Item{Key: uint64(s.SourceIndex) ^ 1<<63, Value: uint64(i)}
	}
	radix.Sort(order, nil)
	// The splat at order[i].Value goes to i. Following those moves from one
	// place to the next, around each cycle they form, moves every splat once.
	for start := range order {
		if int(order[start].Value) == start {
			continue
		}
		held := splats[start]
		for i := start; ; {
			from := int(order[i].Value)
			order[i].Value = uint64(i)
			if from == start {
				splats[i] = held
				break
			}
			splats[i] = splats[from]
			i = from
		}
	}
}
