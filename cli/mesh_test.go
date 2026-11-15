package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	meshtiling "github.com/mfbonfigli/gotiler/v3/internal/mesh/tiling"
	"github.com/mfbonfigli/gotiler/v3/tiler"
)

// writeMeshOBJ writes a square of side by side cells, two triangles each,
// without materials, as an OBJ file at path.
func writeMeshOBJ(t *testing.T, path string, side int) {
	t.Helper()
	var b strings.Builder
	for y := 0; y <= side; y++ {
		for x := 0; x <= side; x++ {
			b.WriteString("v " + itoa(x) + " " + itoa(y) + " 0\n")
		}
	}
	for y := range side {
		for x := range side {
			v := y*(side+1) + x + 1
			b.WriteString("f " + itoa(v) + " " + itoa(v+1) + " " + itoa(v+side+2) + "\n")
			b.WriteString("f " + itoa(v) + " " + itoa(v+side+2) + " " + itoa(v+side+1) + "\n")
		}
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

func itoa(n int) string { return formatComma(n) }

type meshCall struct {
	source, dir string
	opts        meshtiling.Options
}

// fakeTileMesh replaces the mesh tiling for the test with one that records
// its arguments.
func fakeTileMesh(t *testing.T) *[]meshCall {
	t.Helper()
	var calls []meshCall
	original := tileMesh
	tileMesh = func(ctx context.Context, source, dir string, opts meshtiling.Options) (meshtiling.Result, error) {
		calls = append(calls, meshCall{source, dir, opts})
		return meshtiling.Result{}, nil
	}
	t.Cleanup(func() { tileMesh = original })
	return &calls
}

func TestMeshCommandTilesAnOBJ(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "square.obj")
	writeMeshOBJ(t, input, 30)
	out := filepath.Join(dir, "out")
	fakeInteractive(t, false)
	var err error
	stderr := captureStderr(t, func() {
		err = splatRun()("mesh", "-o", out, "--triangles-per-tile", "1000", "--plain", input)
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
	if want := placementTransform(t, tiler.Placement{}); ts.Root.Transform != want {
		t.Errorf("root transform %v, want %v", ts.Root.Transform, want)
	}
	tiles, _ := os.ReadDir(filepath.Join(out, "data"))
	if len(ts.Root.Children) != 2 || len(tiles) < 3 {
		t.Errorf("the root has %d children and there are %d tile files, want a tree", len(ts.Root.Children), len(tiles))
	}
	for _, want := range []string{"Tris/Tile:  1,000", "Compress:   meshopt", "[1/4] Reading", "[4/4] Building", "Tiled 1,800 triangles into"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("the output does not mention %q:\n%s", want, stderr)
		}
	}
}

func TestMeshCommandPassesTheFlags(t *testing.T) {
	input := filepath.Join(t.TempDir(), "square.obj")
	writeMeshOBJ(t, input, 2)
	calls := fakeTileMesh(t)
	captureStderr(t, func() {
		err := splatRun()("mesh", "-o", "out", "--no-preview",
			"--triangles-per-tile", "20000", "--max-texture-size", "2048", "--texture-quality", "70",
			"--compression", "none", "--ge-correction", "2", "--max-tiles-per-tileset", "50",
			"--input-up-axis", "y", "--longitude", "24.9", "--latitude", "60.2", "--height", "12",
			"--heading", "10", "--scale", "0.2", input)
		if err != nil {
			t.Fatal(err)
		}
	})
	if len(*calls) != 1 {
		t.Fatalf("%d tilings, want 1", len(*calls))
	}
	got := (*calls)[0]
	want := meshtiling.DefaultOptions()
	want.TrianglesPerTile, want.MaxTextureSize, want.TextureQuality, want.Compress = 20000, 2048, 70, false
	want.GECorrection, want.MaxTilesPerTileset, want.UpAxis = 2, 50, "y"
	transform := placementTransform(t, tiler.Placement{Longitude: 24.9, Latitude: 60.2, Height: 12, Heading: 10, Scale: 0.2})
	want.RootTransform = &transform
	want.Progress = got.opts.Progress
	if got.source != input || got.dir != "out" || !reflect.DeepEqual(got.opts, want) || got.opts.Progress == nil {
		t.Errorf("tiled %s to %s with %+v, want %s to out with %+v", got.source, got.dir, got.opts, input, want)
	}
}

func TestMeshCommandRejectsBadInput(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "square.obj")
	writeMeshOBJ(t, input, 2)
	other := filepath.Join(dir, "square.ply")
	writeMeshOBJ(t, other, 2)
	calls := fakeTileMesh(t)
	for name, args := range map[string][]string{
		"no output":            {input},
		"no input":             {"-o", "out"},
		"missing input":        {"-o", "out", filepath.Join(dir, "none.obj")},
		"a folder":             {"-o", "out", dir},
		"not an OBJ":           {"-o", "out", other},
		"too few triangles":    {"-o", "out", "--triangles-per-tile", "10", input},
		"too large a texture":  {"-o", "out", "--max-texture-size", "100000", input},
		"a bad quality":        {"-o", "out", "--texture-quality", "0", input},
		"an unknown codec":     {"-o", "out", "--compression", "draco", input},
		"a bad up axis":        {"-o", "out", "--input-up-axis", "w", input},
		"a non-positive scale": {"-o", "out", "--scale", "0", input},
	} {
		var err error
		captureStderr(t, func() { err = splatRun()(append([]string{"mesh", "--no-preview"}, args...)...) })
		if err == nil {
			t.Errorf("%s: the command accepted %v", name, args)
		}
	}
	if len(*calls) != 0 {
		t.Errorf("%d tilings started with bad input", len(*calls))
	}
}
