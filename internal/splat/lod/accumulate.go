package lod

import (
	"math"

	"github.com/mfbonfigli/gotiler/v3/internal/geom"
	"github.com/mfbonfigli/gotiler/v3/internal/splat"
)

const (
	// minWeight floors the weight of a splat, so that transparent or
	// degenerate splats still count a little.
	minWeight = 1e-12

	// thinRatio is the largest ratio of smallest to largest scale of a thin
	// splat, one flat enough to tell the normal of the surface it lies on.
	thinRatio = 0.35

	// The thickness of a merged splat is capped when thin splats hold at least
	// surfaceShare of its weight and their normals agree, the length of their
	// weighted mean normal being at least surfaceCoherence.
	surfaceShare     = 0.65
	surfaceCoherence = 0.85

	// thicknessGrowth is how many times the mean variance of the thin splats
	// along their normal the variance of a merged surface can reach.
	thicknessGrowth = 4

	// minNormalVariance floors the capped variance along a surface normal.
	minNormalVariance = 1e-10
)

// weight returns how much a splat contributes to the image: its opacity times
// its mean projected area.
func weight(s splat.Splat) float64 {
	area := (s.Scale.X*s.Scale.Y + s.Scale.X*s.Scale.Z + s.Scale.Y*s.Scale.Z) / 3
	return math.Max(minWeight, s.Opacity*math.Max(area, minWeight))
}

// accumulator merges splats into the one Gaussian that matches their combined
// mean and covariance, each splat weighted by its contribution.
//
// Opacities combine like layers of coverage, the base color is the weighted
// mean, and the higher-degree spherical harmonics are those of the heaviest
// splat, since averaging view-dependent detail of different splats blurs it.
// The merged splat keeps the lowest source index.
//
// When most of the weight comes from thin splats that agree on a surface
// normal, matching the covariance would thicken the surface into a blob: the
// accumulator caps the variance along that normal instead.
type accumulator struct {
	weight       float64
	center       geom.Vec3
	scatter      geom.Mat3
	transparency float64
	sh0          geom.Vec3
	dominant     float64
	shRest       []geom.Vec3
	sourceIndex  int

	surfaceWeight   float64
	normalRef       geom.Vec3
	normalSum       geom.Vec3
	thinVarianceSum float64
}

// add folds s into the accumulator, keeping a reference to its SHRest if it
// is the heaviest splat so far.
func (a *accumulator) add(s splat.Splat) {
	w := weight(s)
	rotation := s.Rotation.RotationMatrix()
	if a.weight == 0 {
		a.transparency = 1
		a.sourceIndex = s.SourceIndex
	}
	a.sourceIndex = min(a.sourceIndex, s.SourceIndex)

	// Weighted Welford update of the mean and of the scatter around it.
	total := a.weight + w
	delta := s.Center.Sub(a.center)
	a.center = a.center.Add(delta.MulScalar(w / total))
	a.scatter = a.scatter.
		Add(splat.CovarianceFromRotationScale(rotation, s.Scale).MulScalar(w)).
		Add(geom.Outer(delta, delta).MulScalar(a.weight * w / total))

	a.transparency *= 1 - math.Min(math.Max(s.Opacity, 0), 1)
	a.sh0 = a.sh0.Add(s.SH0.MulScalar(w))
	if w > a.dominant {
		a.dominant = w
		a.shRest = s.SHRest
	}
	if normal, variance, ok := thinNormal(rotation, s.Scale); ok {
		if a.surfaceWeight == 0 {
			a.normalRef = normal
		}
		if a.normalRef.Dot(normal) < 0 {
			normal = normal.MulScalar(-1)
		}
		a.surfaceWeight += w
		a.normalSum = a.normalSum.Add(normal.MulScalar(w))
		a.thinVarianceSum += variance * w
	}
	a.weight = total
}

// finish returns the merged splat. It must follow at least one add.
func (a *accumulator) finish() (splat.Splat, error) {
	merged := splat.Splat{
		Center:      a.center,
		Opacity:     1 - a.transparency,
		SH0:         a.sh0.MulScalar(1 / a.weight),
		SHRest:      a.shRest,
		SourceIndex: a.sourceIndex,
	}
	return merged.WithCovariance(a.guardSurface(a.scatter.MulScalar(1 / a.weight)))
}

// guardSurface caps the variance of covariance along the mean normal of the
// thin splats, when they hold most of the weight and agree on that normal. It
// keeps the spread along the surface.
func (a *accumulator) guardSurface(covariance geom.Mat3) geom.Mat3 {
	if a.surfaceWeight == 0 || a.surfaceWeight < surfaceShare*a.weight {
		return covariance
	}
	length := a.normalSum.Length()
	if length < surfaceCoherence*a.surfaceWeight {
		return covariance
	}
	normal := a.normalSum.MulScalar(1 / length)
	limit := math.Max(thicknessGrowth*a.thinVarianceSum/a.surfaceWeight, minNormalVariance)
	if normal.Dot(covariance.MulVec(normal)) <= limit {
		return covariance
	}
	// In the basis of two tangents and the normal, drop the correlation of the
	// normal with the tangents and cap its variance.
	tangentA, tangentB := tangents(normal)
	basis := geom.Mat3{
		{tangentA.X, tangentB.X, normal.X},
		{tangentA.Y, tangentB.Y, normal.Y},
		{tangentA.Z, tangentB.Z, normal.Z},
	}
	local := basis.Transpose().Mul(covariance).Mul(basis)
	local[0][2], local[2][0] = 0, 0
	local[1][2], local[2][1] = 0, 0
	local[2][2] = limit
	return basis.Mul(local).Mul(basis.Transpose())
}

// thinNormal returns the shortest axis of a thin splat with the given rotation
// matrix and scale, as a unit vector, and the variance along it. It reports
// false for splats too round to tell a normal.
func thinNormal(rotation geom.Mat3, scale geom.Vec3) (geom.Vec3, float64, bool) {
	scales := [3]float64{scale.X, scale.Y, scale.Z}
	shortest := 0
	for axis := 1; axis < 3; axis++ {
		if scales[axis] < scales[shortest] {
			shortest = axis
		}
	}
	smallest, largest := scales[shortest], scale.MaxComponent()
	if smallest <= 0 || largest <= 0 || smallest > thinRatio*largest {
		return geom.Vec3{}, 0, false
	}
	normal := geom.Vec3{X: rotation[0][shortest], Y: rotation[1][shortest], Z: rotation[2][shortest]}
	return normal, smallest * smallest, true
}

// tangents returns two unit vectors that form a right-handed orthonormal basis
// with the unit vector normal.
func tangents(normal geom.Vec3) (geom.Vec3, geom.Vec3) {
	helper := geom.Vec3{Z: 1}
	if math.Abs(normal.Z) > 0.9 {
		helper = geom.Vec3{Y: 1}
	}
	a := helper.Cross(normal)
	a = a.MulScalar(1 / a.Length())
	return a, normal.Cross(a)
}
