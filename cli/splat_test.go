package cli

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mfbonfigli/gotiler/v3/internal/splat/tiling"
	"github.com/mfbonfigli/gotiler/v3/internal/viewer"
	"github.com/mfbonfigli/gotiler/v3/tiler"
	"github.com/mfbonfigli/gotiler/v3/tiler/tree"
	"github.com/rivo/uniseg"
)

// writeSplatPLY writes a binary PLY of n splats of the given spherical
// harmonic degree, spread over a 20 x 20 x 2 box.
func writeSplatPLY(t *testing.T, path string, n, degree int) {
	t.Helper()
	writeSplatPLYWith(t, path, n, degree, nil)
}

// writeSplatPLYWith writes splats like writeSplatPLY, letting edit change
// the values of splat i, in the order of the PLY properties, before writing.
func writeSplatPLYWith(t *testing.T, path string, n, degree int, edit func(i int, values []float32)) {
	t.Helper()
	rest := 3 * ((degree+1)*(degree+1) - 1)
	var header strings.Builder
	fmt.Fprintf(&header, "ply\nformat binary_little_endian 1.0\nelement vertex %d\n", n)
	names := []string{"x", "y", "z", "f_dc_0", "f_dc_1", "f_dc_2"}
	for i := range rest {
		names = append(names, fmt.Sprintf("f_rest_%d", i))
	}
	names = append(names, "opacity", "scale_0", "scale_1", "scale_2", "rot_0", "rot_1", "rot_2", "rot_3")
	for _, name := range names {
		fmt.Fprintf(&header, "property float %s\n", name)
	}
	header.WriteString("end_header\n")
	var body bytes.Buffer
	body.WriteString(header.String())
	rng := rand.New(rand.NewPCG(1, 2))
	for i := range n {
		values := []float32{float32(rng.Float64() * 20), float32(rng.Float64() * 20), float32(rng.Float64() * 2)}
		for range 3 + rest {
			values = append(values, float32(rng.NormFloat64()))
		}
		// opacity logit, log scales and an identity rotation in W, X, Y, Z order
		values = append(values, 2, -3, -3, -4, 1, 0, 0, 0)
		if edit != nil {
			edit(i, values)
		}
		if err := binary.Write(&body, binary.LittleEndian, values); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(path, body.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// splatRun returns a function that runs the app with the given arguments.
func splatRun() func(args ...string) error {
	app := NewApp(Options{})
	return func(args ...string) error {
		return app.Run(context.Background(), append([]string{"gotiler"}, args...))
	}
}

// fakeInteractive makes the command see a terminal, or not, for the test.
func fakeInteractive(t *testing.T, value bool) {
	t.Helper()
	original := interactive
	interactive = func() bool { return value }
	t.Cleanup(func() { interactive = original })
}

type tileCall struct {
	dir  string
	opts tiling.Options
}

// fakeTile replaces the tiling for the test with one that records its
// options, and runs the real tiling if real is set.
func fakeTile(t *testing.T, real bool) *[]tileCall {
	t.Helper()
	var calls []tileCall
	original := tileSplats
	tileSplats = func(ctx context.Context, source tiling.Source, dir string, opts tiling.Options) (tiling.Result, error) {
		calls = append(calls, tileCall{dir: dir, opts: opts})
		if real {
			return original(ctx, source, dir, opts)
		}
		return tiling.Result{}, nil
	}
	t.Cleanup(func() { tileSplats = original })
	return &calls
}

// captureStderr returns what f writes to standard error.
func captureStderr(t *testing.T, f func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stderr
	os.Stderr = w
	done := make(chan string)
	go func() {
		data, _ := io.ReadAll(r)
		done <- string(data)
	}()
	defer func() { os.Stderr = original }()
	f()
	w.Close() //nolint:errcheck
	return <-done
}

func placementTransform(t *testing.T, p tiler.Placement) [16]float64 {
	t.Helper()
	transform, err := p.Transform()
	if err != nil {
		t.Fatal(err)
	}
	return transform.ForwardColumnMajor()
}

func TestSplatCommandTilesAPLY(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "scene.ply")
	writeSplatPLY(t, input, 3000, 1)
	out := filepath.Join(dir, "out")
	fakeInteractive(t, false)
	var err error
	stderr := captureStderr(t, func() {
		err = splatRun()("splat", "-o", out, "--splats-per-tile", "1000", "--plain", input)
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(out, "tileset.json"))
	if err != nil {
		t.Fatal(err)
	}
	var ts struct {
		Root struct {
			Transform [16]float64 `json:"transform"`
			Children  []any       `json:"children"`
		} `json:"root"`
	}
	if err := json.Unmarshal(data, &ts); err != nil {
		t.Fatal(err)
	}
	// Without placement flags, the tileset sits at longitude 0, latitude 0.
	if want := placementTransform(t, tiler.Placement{}); ts.Root.Transform != want {
		t.Errorf("root transform %v, want %v", ts.Root.Transform, want)
	}
	tiles, _ := os.ReadDir(filepath.Join(out, "data"))
	if len(ts.Root.Children) != 2 || len(tiles) < 3 {
		t.Errorf("the root has %d children and there are %d tile files, want a tree", len(ts.Root.Children), len(tiles))
	}
	for _, want := range []string{"Splats:        3,000", "SH Degree:     1, capped to 0", "Min Opacity:   0.05", "[1/3] Reading", "[3/3] Building", "Tiled 3,000 splats into", "spherical harmonics of degree 0."} {
		if !strings.Contains(stderr, want) {
			t.Errorf("the output does not mention %q:\n%s", want, stderr)
		}
	}
}

func TestSplatCommandPassesTheFlags(t *testing.T) {
	input := filepath.Join(t.TempDir(), "scene.ply")
	writeSplatPLY(t, input, 10, 2)
	calls := fakeTile(t, false)
	captureStderr(t, func() {
		err := splatRun()("splat", "-o", "out", "--no-preview",
			"--splats-per-tile", "5000", "--max-sh-degree", "1", "--min-opacity", "0.2", "--ge-correction", "1.5",
			"--input-up-axis", "-y", "--longitude", "12.5", "--latitude", "41.9", "--height", "30",
			"--heading", "45", "--pitch", "1", "--roll", "2", "--scale", "0.5", input)
		if err != nil {
			t.Fatal(err)
		}
	})
	if len(*calls) != 1 {
		t.Fatalf("%d tilings, want 1", len(*calls))
	}
	got := (*calls)[0]
	want := tiling.DefaultOptions()
	want.SplatsPerTile, want.MaxSHDegree, want.MinOpacity, want.GECorrection, want.UpAxis = 5000, 1, 0.2, 1.5, "-y"
	transform := placementTransform(t, tiler.Placement{Longitude: 12.5, Latitude: 41.9, Height: 30, Heading: 45, Pitch: 1, Roll: 2, Scale: 0.5})
	want.RootTransform = &transform
	want.Progress = got.opts.Progress
	if got.dir != "out" || !reflect.DeepEqual(got.opts, want) || got.opts.Progress == nil {
		t.Errorf("tiled to %s with %+v, want out with %+v", got.dir, got.opts, want)
	}
}

func TestSplatCommandDefaults(t *testing.T) {
	input := filepath.Join(t.TempDir(), "scene.ply")
	writeSplatPLY(t, input, 10, 0)
	calls := fakeTile(t, false)
	captureStderr(t, func() {
		if err := splatRun()("splat", "-o", "out", "--no-preview", input); err != nil {
			t.Fatal(err)
		}
	})
	got := (*calls)[0].opts
	want := tiling.DefaultOptions()
	transform := placementTransform(t, tiler.Placement{})
	want.RootTransform = &transform
	// Unlike the tiling, the command drops the view-dependent colors and the
	// almost invisible splats by default.
	want.MaxSHDegree, want.MinOpacity = 0, 0.05
	want.UpAxis = "z"
	want.Progress = got.Progress
	if !reflect.DeepEqual(got, want) {
		t.Errorf("options %+v, want %+v", got, want)
	}
}

func TestSplatSummaryBoxLinesUp(t *testing.T) {
	input := filepath.Join(t.TempDir(), "scene.ply")
	writeSplatPLY(t, input, 10, 0)
	fakeTile(t, false)
	out := strings.Repeat("long-folder-name/", 8) + "out"
	stderr := captureStderr(t, func() {
		if err := splatRun()("splat", "-o", out, "--no-preview", input); err != nil {
			t.Fatal(err)
		}
	})
	start, end := strings.Index(stderr, "┌"), strings.Index(stderr, "┘")
	if start < 0 || end < start {
		t.Fatalf("no summary box in the output:\n%s", stderr)
	}
	// uniseg measures the columns a terminal draws, emoji included.
	for _, line := range strings.Split(stderr[start:end+len("┘")], "\n") {
		if got := uniseg.StringWidth(line); got != boxTotalWidth {
			t.Errorf("%q is %d columns wide, want %d", line, got, boxTotalWidth)
		}
	}
}

func TestSplatCommandOpensTheViewer(t *testing.T) {
	t.Setenv(ionTokenEnvVar, "ion-token")
	input := filepath.Join(t.TempDir(), "scene.ply")
	writeSplatPLY(t, input, 10, 0)
	fakeTile(t, false)
	fakeInteractive(t, true)
	views := fakeViewer(t, nil)
	captureStderr(t, func() {
		if err := splatRun()("splat", "-o", "out", "--port", "9001", input); err != nil {
			t.Fatal(err)
		}
	})
	want := viewerCall{filepath.Join("out", "tileset.json"), viewer.Options{Port: 9001, IonToken: "ion-token", StartEditing: true}}
	if len(*views) != 1 || (*views)[0] != want {
		t.Errorf("viewer calls %+v, want [%+v]", *views, want)
	}
}

func TestSplatCommandSkipsTheViewer(t *testing.T) {
	input := filepath.Join(t.TempDir(), "scene.ply")
	writeSplatPLY(t, input, 10, 0)
	fakeTile(t, false)
	views := fakeViewer(t, nil)

	fakeInteractive(t, true)
	captureStderr(t, func() {
		if err := splatRun()("splat", "-o", "out", "--no-preview", input); err != nil {
			t.Fatal(err)
		}
	})
	fakeInteractive(t, false)
	stderr := captureStderr(t, func() {
		if err := splatRun()("splat", "-o", "out", input); err != nil {
			t.Fatal(err)
		}
	})
	if len(*views) != 0 {
		t.Errorf("the viewer opened %d times", len(*views))
	}
	if !strings.Contains(stderr, "not a terminal") {
		t.Errorf("the output does not say why the viewer did not open:\n%s", stderr)
	}
}

func TestSplatCommandReturnsViewerErrors(t *testing.T) {
	input := filepath.Join(t.TempDir(), "scene.ply")
	writeSplatPLY(t, input, 10, 0)
	fakeTile(t, false)
	fakeInteractive(t, true)
	viewErr := errors.New("port taken")
	fakeViewer(t, viewErr)
	var err error
	captureStderr(t, func() { err = splatRun()("splat", "-o", "out", input) })
	if !errors.Is(err, viewErr) {
		t.Errorf("error %v, want the viewer's", err)
	}
}

func TestSplatCommandRejectsBadInput(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "scene.ply")
	writeSplatPLY(t, input, 10, 0)
	points := filepath.Join(dir, "points.ply")
	writeFile(t, points, "ply\nformat ascii 1.0\nelement vertex 1\nproperty float x\nproperty float y\nproperty float z\nend_header\n1 2 3\n")
	las := filepath.Join(dir, "cloud.las")
	writeFile(t, las, "")
	calls := fakeTile(t, false)
	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{"no input", []string{"-o", "out"}, "one splat .ply file"},
		{"two inputs", []string{"-o", "out", input, input}, "one splat .ply file"},
		{"no output", []string{input}, "--out"},
		{"folder", []string{"-o", "out", dir}, "is a folder"},
		{"missing file", []string{"-o", "out", filepath.Join(dir, "missing.ply")}, "not accessible"},
		{"not a PLY", []string{"-o", "out", las}, "not a .ply file"},
		{"point cloud PLY", []string{"-o", "out", points}, "not a Gaussian splat PLY"},
		{"tiny tiles", []string{"-o", "out", "--splats-per-tile", "10", input}, "--splats-per-tile"},
		{"SH degree", []string{"-o", "out", "--max-sh-degree", "4", input}, "spherical-harmonic degree"},
		{"opacity", []string{"-o", "out", "--min-opacity", "2", input}, "opacity"},
		{"error correction", []string{"-o", "out", "--ge-correction", "0", input}, "geometric error correction"},
		{"up axis", []string{"-o", "out", "--input-up-axis", "w", input}, "up axis"},
		{"latitude", []string{"-o", "out", "--latitude", "95", input}, "latitude"},
		{"scale", []string{"-o", "out", "--scale", "0", input}, "--scale"},
	} {
		var err error
		captureStderr(t, func() {
			original := os.Stdout
			os.Stdout, _ = os.Open(os.DevNull) // the help that missing inputs print
			defer func() { os.Stdout = original }()
			err = splatRun()(append([]string{"splat", "--no-preview"}, test.args...)...)
		})
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%s: error %v, want one mentioning %q", test.name, err, test.want)
		}
	}
	if len(*calls) != 0 {
		t.Errorf("bad input started %d tilings", len(*calls))
	}
}

func TestSplatProgressMapsThePhases(t *testing.T) {
	var events []tiler.ProgressEvent
	p := newSplatProgress(true, "scene.ply")
	p.listener = func(e tiler.ProgressEvent) { events = append(events, e) }
	p.Report(tree.ProgressUpdate{Phase: "reading", Percent: 0, Message: "start", IsMilestone: true, ItemTotal: 10})
	p.Report(tree.ProgressUpdate{Phase: "splitting", Percent: 50, ItemCount: 5, ItemTotal: 10})
	p.Report(tree.ProgressUpdate{Phase: "building", Percent: 100, Message: "done", IsMilestone: true, ItemCount: 3, ItemTotal: 3})
	p.Report(tree.ProgressUpdate{Phase: "unknown"})
	if len(events) != 3 {
		t.Fatalf("%d events, want 3", len(events))
	}
	want := []tiler.ProgressEvent{
		{Phase: tiler.Phase{Index: 1, Total: 3, Name: "Reading", Unit: "splats"}, Percent: 0, Message: "start", Level: tiler.ProgressMilestone, InputDesc: "scene.ply", ItemTotal: 10},
		{Phase: tiler.Phase{Index: 2, Total: 3, Name: "Splitting", Unit: "splats"}, Percent: 50, Level: tiler.ProgressDetail, InputDesc: "scene.ply", ItemCount: 5, ItemTotal: 10},
		{Phase: tiler.Phase{Index: 3, Total: 3, Name: "Building", Unit: "tiles"}, Percent: 100, Message: "done", Level: tiler.ProgressMilestone, InputDesc: "scene.ply", ItemCount: 3, ItemTotal: 3},
	}
	for i := range events {
		events[i].ElapsedMs = 0
		if events[i] != want[i] {
			t.Errorf("event %d = %+v, want %+v", i, events[i], want[i])
		}
	}
}

func TestSplatCommandHelpListsTheFlags(t *testing.T) {
	app := NewApp(Options{})
	var out bytes.Buffer
	app.Writer = &out
	if err := app.Run(context.Background(), []string{"gotiler", "splat", "--help"}); err != nil {
		t.Fatal(err)
	}
	for _, flag := range []string{"--out", "--splats-per-tile", "--max-sh-degree", "--min-opacity", "--ge-correction", "--input-up-axis", "--longitude", "--scale", "--no-preview", "--port", "--plain"} {
		if !strings.Contains(out.String(), flag) {
			t.Errorf("the help does not list %s", flag)
		}
	}
}

func TestSplatCommandReportsDroppedSplats(t *testing.T) {
	input := filepath.Join(t.TempDir(), "scene.ply")
	writeSplatPLYWith(t, input, 1000, 0, func(i int, values []float32) {
		opacity := len(values) - 8
		switch {
		case i%2 == 1:
			values[opacity] = -2 // An opacity of 0.12.
		case i%100 == 0:
			values[0] = float32(math.NaN())
		}
	})
	fakeInteractive(t, false)
	var err error
	stderr := captureStderr(t, func() {
		err = splatRun()("splat", "-o", filepath.Join(t.TempDir(), "out"), "--min-opacity", "0.5", "--plain", input)
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Tiled 490 splats", "Dropped 500 splats below the minimum opacity", "Skipped 10 vertices with invalid values"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("the output does not say %q:\n%s", want, stderr)
		}
	}
}

func TestSplatCommandReturnsTilingErrors(t *testing.T) {
	input := filepath.Join(t.TempDir(), "scene.ply")
	writeSplatPLY(t, input, 10, 0)
	original := tileSplats
	tileSplats = func(context.Context, tiling.Source, string, tiling.Options) (tiling.Result, error) {
		return tiling.Result{}, errors.New("disk full")
	}
	t.Cleanup(func() { tileSplats = original })
	var err error
	captureStderr(t, func() { err = splatRun()("splat", "-o", "out", "--no-preview", input) })
	if err == nil || err.Error() != "disk full" {
		t.Errorf("error %v, want the tiling's", err)
	}
}
