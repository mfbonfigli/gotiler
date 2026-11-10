package tiling

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mfbonfigli/gotiler/v3/internal/geom"
	"github.com/mfbonfigli/gotiler/v3/internal/splat"
	"github.com/mfbonfigli/gotiler/v3/internal/splat/lod"
	"github.com/mfbonfigli/gotiler/v3/internal/splat/store"
)

func readStore(t *testing.T, in *input) []splat.Splat {
	t.Helper()
	splats, err := store.ReadAll(in.path, in.shCount)
	if err != nil {
		t.Fatal(err)
	}
	return splats
}

func TestReadInputStoresTheSplats(t *testing.T) {
	for degree := range 4 {
		splats := randomSplats(1, 10_000, degree, geom.Vec3{X: 4.5e6, Y: 1e5, Z: 30}, geom.Vec3{X: 50, Y: 40, Z: 10})
		in := mustReadInput(t, splats, degree, keepAll)
		if in.count != len(splats) || in.read != len(splats) || in.shCount != shCount(degree) {
			t.Fatalf("degree %d: count %d, read %d, shCount %d", degree, in.count, in.read, in.shCount)
		}
		got := readStore(t, in)
		for i := range splats {
			if want := stored(splats[i]); !reflect.DeepEqual(got[i], want) {
				t.Fatalf("degree %d: splat %d = %+v, want %+v", degree, i, got[i], want)
			}
		}
	}
}

// TestReadInputPlacesTheOrigin checks that the origin is the bottom center of
// the bounds of the splats out to three standard deviations, and that the
// bounds and samples are relative to it.
func TestReadInputPlacesTheOrigin(t *testing.T) {
	splats := randomSplats(2, 1000, 0, geom.Vec3{X: 4.5e6, Y: -2e5, Z: 100}, geom.Vec3{X: 50, Y: 40, Z: 10})
	in := mustReadInput(t, splats, 0, keepAll)

	var bounds geom.Bounds
	for _, s := range splats {
		bounds.Include(s.Bounds(3))
	}
	origin := geom.Vec3{X: bounds.Center().X, Y: bounds.Center().Y, Z: bounds.Min.Z}
	if in.origin != origin {
		t.Errorf("origin = %+v, want %+v", in.origin, origin)
	}
	assertVec3(t, "bounds min", in.bounds.Min, bounds.Min.Sub(origin))
	assertVec3(t, "bounds max", in.bounds.Max, bounds.Max.Sub(origin))
	if in.bounds.Min.Z != 0 || !closeTo(in.bounds.Min.X, -in.bounds.Max.X) {
		t.Errorf("bounds = %+v, want them centered above the origin", in.bounds)
	}
	// Fewer splats than samples: each splat is a sample, in order.
	if len(in.samples) != len(splats) {
		t.Fatalf("%d samples, want %d", len(in.samples), len(splats))
	}
	for i, s := range in.samples {
		want := sample{center: splats[i].Center.Sub(origin), weight: lod.Weight(splats[i])}
		if s != want {
			t.Fatalf("sample %d = %+v, want %+v", i, s, want)
		}
	}
}

func TestReadInputFiltersByOpacity(t *testing.T) {
	splats := randomSplats(3, 2000, 1, geom.Vec3{}, geom.Vec3{X: 10, Y: 10, Z: 10})
	var kept []splat.Splat
	for _, s := range splats {
		if s.Opacity >= 0.3 {
			kept = append(kept, stored(s))
		}
	}
	in := mustReadInput(t, splats, 1, inputOptions{minOpacity: 0.3, maxSHDegree: -1})
	if in.read != len(splats) || in.count != len(kept) || len(in.samples) != len(kept) {
		t.Fatalf("read %d, count %d, %d samples; want %d, %d, %d", in.read, in.count, len(in.samples), len(splats), len(kept), len(kept))
	}
	if got := readStore(t, in); !reflect.DeepEqual(got, kept) {
		t.Error("the store does not hold the splats of opacity 0.3 or more")
	}
}

func TestReadInputCapsTheSHDegree(t *testing.T) {
	splats := randomSplats(4, 100, 3, geom.Vec3{}, geom.Vec3{X: 10, Y: 10, Z: 10})
	for _, test := range []struct{ max, want int }{{-1, 3}, {0, 0}, {1, 1}, {2, 2}, {3, 3}, {5, 3}} {
		in := mustReadInput(t, splats, 3, inputOptions{maxSHDegree: test.max})
		if in.shCount != shCount(test.want) {
			t.Fatalf("max degree %d: %d coefficients, want %d", test.max, in.shCount, shCount(test.want))
		}
		for i, got := range readStore(t, in) {
			want := stored(splats[i])
			want.SHRest = want.SHRest[:in.shCount]
			if in.shCount == 0 {
				want.SHRest = nil
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("max degree %d: splat %d = %+v, want %+v", test.max, i, got, want)
			}
		}
	}
	// The source's splats are left whole.
	if len(splats[0].SHRest) != 15 {
		t.Errorf("the source splat has %d coefficients left", len(splats[0].SHRest))
	}
}

func TestReadInputTurnsTheUpAxisUp(t *testing.T) {
	splats := randomSplats(5, 500, 2, geom.Vec3{Y: -20}, geom.Vec3{X: 10, Y: 4, Z: 10})
	in := mustReadInput(t, splats, 2, inputOptions{maxSHDegree: -1, upAxis: "-y"})
	m, _ := upAxisRotation("-y")
	rotation := splat.NewRotation(m)
	for i, got := range readStore(t, in) {
		if want := stored(rotation.Rotate(splats[i])); !reflect.DeepEqual(got, want) {
			t.Fatalf("splat %d = %+v, want %+v", i, got, want)
		}
	}
	// The splats lie 18 to 22 below the origin along -Y: they end up above it.
	if in.origin.Z < 17 || in.origin.Z > 18 {
		t.Errorf("origin = %+v, want it about 17.9 up, the bottom of the turned splats", in.origin)
	}
}

func TestReadInputSamplesLargeInputsUniformly(t *testing.T) {
	const n = maxSamples + maxSamples/2
	splats := make([]splat.Splat, n)
	for i := range splats {
		// The X coordinate tells the index of the splat.
		splats[i] = splat.Splat{Center: geom.Vec3{X: float64(i)}, Rotation: geom.IdentityQuat(), Scale: geom.Vec3{X: 1, Y: 1, Z: 1}, Opacity: 1, SourceIndex: i}
	}
	in := mustReadInput(t, splats, 0, keepAll)
	if len(in.samples) != maxSamples {
		t.Fatalf("%d samples, want %d", len(in.samples), maxSamples)
	}
	seen := map[int]bool{}
	firstThird := 0
	for _, s := range in.samples {
		i := int(s.center.X + in.origin.X)
		if seen[i] || i < 0 || i >= n || float64(i) != s.center.X+in.origin.X {
			t.Fatalf("sample %+v is not a distinct splat", s)
		}
		seen[i] = true
		if i < n/3 {
			firstThird++
		}
	}
	// Each splat is kept with chance 2/3: the first third of the splats holds
	// a third of the samples, give or take a few standard deviations (0.1%).
	if share := float64(firstThird) / maxSamples; share < 0.33 || share > 0.337 {
		t.Errorf("the first third of the splats holds %.4f of the samples", share)
	}
	if again := mustReadInput(t, splats, 0, keepAll); !reflect.DeepEqual(again.samples, in.samples) {
		t.Error("sampling the same splats again picked other samples")
	}
}

func TestReadInputErrors(t *testing.T) {
	splats := randomSplats(6, 10, 0, geom.Vec3{}, geom.Vec3{X: 1, Y: 1, Z: 1})
	dir := t.TempDir()
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	sourceErr := errors.New("disk on fire")
	for _, test := range []struct {
		name   string
		ctx    context.Context
		source Source
		path   string
		opts   inputOptions
		want   string
	}{
		{"no splats", context.Background(), &sliceSource{}, "a.bin", keepAll, "no valid splats"},
		{"all transparent", context.Background(), &sliceSource{splats: splats}, "b.bin", inputOptions{minOpacity: 1.1}, "none of the 10 splats has an opacity of at least 1.1"},
		{"bad axis", context.Background(), &sliceSource{splats: splats}, "c.bin", inputOptions{upAxis: "w"}, "up axis"},
		{"source error", context.Background(), &sliceSource{splats: splats, err: sourceErr}, "d.bin", keepAll, "disk on fire"},
		{"canceled", canceled, &sliceSource{splats: splats}, "e.bin", keepAll, "canceled"},
		{"missing directory", context.Background(), &sliceSource{splats: splats}, "missing/f.bin", keepAll, "missing"},
		// Splats of a lower degree than the source declares, in a full batch
		// and in the last one.
		{"short batch", context.Background(), &sliceSource{degree: 1, splats: splats}, "g.bin", keepAll, "spherical-harmonic coefficients"},
		{"full batch", context.Background(), &sliceSource{degree: 1, splats: randomSplats(7, 5000, 0, geom.Vec3{}, geom.Vec3{X: 1, Y: 1, Z: 1})}, "h.bin", keepAll, "spherical-harmonic coefficients"},
	} {
		_, err := readInput(test.ctx, test.source, filepath.Join(dir, test.path), test.opts)
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%s: error %v, want one mentioning %q", test.name, err, test.want)
		}
	}
}
