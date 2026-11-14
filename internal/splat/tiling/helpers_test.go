package tiling

import (
	"context"
	"io"
	"math"
	"math/rand/v2"
	"path/filepath"
	"testing"

	"github.com/mfbonfigli/gotiler/v3/internal/geom"
	"github.com/mfbonfigli/gotiler/v3/internal/splat"
)

const tolerance = 1e-9

func closeTo(got, want float64) bool {
	return math.Abs(got-want) <= tolerance*math.Max(1, math.Max(math.Abs(got), math.Abs(want)))
}

func assertVec3(t *testing.T, name string, got, want geom.Vec3) {
	t.Helper()
	if !closeTo(got.X, want.X) || !closeTo(got.Y, want.Y) || !closeTo(got.Z, want.Z) {
		t.Errorf("%s = %+v, want %+v", name, got, want)
	}
}

// sliceSource is a Source of the splats of a slice.
type sliceSource struct {
	degree int
	splats []splat.Splat
	err    error // returned after the splats instead of io.EOF, if set
}

func (s *sliceSource) SHDegree() int { return s.degree }

func (s *sliceSource) NumberOfSplats() int { return len(s.splats) }

func (s *sliceSource) Next() (splat.Splat, error) {
	if len(s.splats) == 0 {
		if s.err != nil {
			return splat.Splat{}, s.err
		}
		return splat.Splat{}, io.EOF
	}
	next := s.splats[0]
	s.splats = s.splats[1:]
	return next, nil
}

// shCount returns the number of higher-degree coefficients of degree.
func shCount(degree int) int { return (degree+1)*(degree+1) - 1 }

// randomSplats returns n splats in a box of the given size around center,
// with spherical harmonics of the given degree. Each SourceIndex is the
// splat's position.
func randomSplats(seed uint64, n, degree int, center, size geom.Vec3) []splat.Splat {
	rng := rand.New(rand.NewPCG(seed, 3))
	splats := make([]splat.Splat, n)
	for i := range splats {
		s := splat.Splat{
			Center: geom.Vec3{
				X: center.X + (rng.Float64()-0.5)*size.X,
				Y: center.Y + (rng.Float64()-0.5)*size.Y,
				Z: center.Z + (rng.Float64()-0.5)*size.Z,
			},
			Rotation:    geom.Quat{X: rng.NormFloat64(), Y: rng.NormFloat64(), Z: rng.NormFloat64(), W: rng.NormFloat64()}.Normalize(),
			Scale:       geom.Vec3{X: 0.01 + 0.05*rng.Float64(), Y: 0.01 + 0.05*rng.Float64(), Z: 0.01 + 0.05*rng.Float64()},
			Opacity:     rng.Float64(),
			SH0:         geom.Vec3{X: rng.NormFloat64(), Y: rng.NormFloat64(), Z: rng.NormFloat64()},
			SourceIndex: i,
		}
		if degree > 0 {
			s.SHRest = make([]geom.Vec3, shCount(degree))
			for j := range s.SHRest {
				s.SHRest[j] = geom.Vec3{X: rng.NormFloat64(), Y: rng.NormFloat64(), Z: rng.NormFloat64()}
			}
		}
		splats[i] = s
	}
	return splats
}

// stored returns s as a store gives it back: every value but the center
// rounded to float32.
func stored(s splat.Splat) splat.Splat {
	r := func(x float64) float64 { return float64(float32(x)) }
	v := func(x geom.Vec3) geom.Vec3 { return geom.Vec3{X: r(x.X), Y: r(x.Y), Z: r(x.Z)} }
	out := s
	out.Rotation = geom.Quat{X: r(s.Rotation.X), Y: r(s.Rotation.Y), Z: r(s.Rotation.Z), W: r(s.Rotation.W)}
	out.Scale, out.SH0, out.Opacity = v(s.Scale), v(s.SH0), r(s.Opacity)
	if len(s.SHRest) > 0 {
		out.SHRest = make([]geom.Vec3, len(s.SHRest))
		for i, c := range s.SHRest {
			out.SHRest[i] = v(c)
		}
	}
	return out
}

// mustReadInput reads splats of the given degree into a store in a temporary
// directory.
func mustReadInput(t testing.TB, splats []splat.Splat, degree int, opts inputOptions) *input {
	t.Helper()
	in, err := readInput(context.Background(), &sliceSource{degree: degree, splats: splats}, filepath.Join(t.TempDir(), "input.bin"), opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	return in
}

// keepAll are input options that keep every splat as it is.
var keepAll = inputOptions{maxSHDegree: -1}
