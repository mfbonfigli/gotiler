package tiling

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/qmuntal/gltf"

	"github.com/mfbonfigli/gotiler/v3/internal/geom"
	"github.com/mfbonfigli/gotiler/v3/internal/splat"
	"github.com/mfbonfigli/gotiler/v3/internal/splat/glb"
	"github.com/mfbonfigli/gotiler/v3/internal/splat/spz"
	"github.com/mfbonfigli/gotiler/v3/tiler/tree"
)

func testOptions(perTile int) Options {
	opts := DefaultOptions()
	opts.SplatsPerTile = perTile
	opts.Workers = 4
	return opts
}

func mustTile(t *testing.T, splats []splat.Splat, degree int, opts Options) (string, Result) {
	t.Helper()
	dir := t.TempDir()
	res, err := Tile(context.Background(), &sliceSource{degree: degree, splats: splats}, dir, opts)
	if err != nil {
		t.Fatal(err)
	}
	return dir, res
}

func readTileset(t *testing.T, dir string) tilesetJSON {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "tileset.json"))
	if err != nil {
		t.Fatal(err)
	}
	var ts tilesetJSON
	if err := json.Unmarshal(data, &ts); err != nil {
		t.Fatal(err)
	}
	return ts
}

// content is what a tile's GLB holds.
type content struct {
	count      int
	attributes []string
	spzVersion uint32
}

func readContent(t *testing.T, dir, uri string) content {
	t.Helper()
	doc, err := gltf.Open(filepath.Join(dir, uri))
	if err != nil {
		t.Fatalf("%s: %v", uri, err)
	}
	attributes := doc.Meshes[0].Primitives[0].Attributes
	c := content{count: doc.Accessors[attributes[gltf.POSITION]].Count}
	for name := range attributes {
		c.attributes = append(c.attributes, name)
	}
	slices.Sort(c.attributes)
	payload, err := gzip.NewReader(bytes.NewReader(doc.Buffers[0].Data))
	if err != nil {
		t.Fatal(err)
	}
	header := make([]byte, 8)
	if _, err := io.ReadFull(payload, header); err != nil {
		t.Fatal(err)
	}
	c.spzVersion = binary.LittleEndian.Uint32(header[4:])
	return c
}

// walk calls f for every tile under root, with its parent, nil for the root.
func walk(root *tileJSON, f func(tile, parent *tileJSON)) {
	var visit func(tile, parent *tileJSON)
	visit = func(tile, parent *tileJSON) {
		f(tile, parent)
		for i := range tile.Children {
			visit(&tile.Children[i], tile)
		}
	}
	visit(root, nil)
}

func boxContains(outer, inner [12]float64) bool {
	for axis := range 3 {
		half := [2]float64{outer[3+4*axis], inner[3+4*axis]}
		if inner[axis]-half[1] < outer[axis]-half[0]-1e-9 || inner[axis]+half[1] > outer[axis]+half[0]+1e-9 {
			return false
		}
	}
	return true
}

func TestTileWritesATileset(t *testing.T) {
	splats := randomSplats(1, 30_000, 1, geom.Vec3{X: 4.5e6, Y: 1e5, Z: 50}, geom.Vec3{X: 80, Y: 40, Z: 10})
	opts := testOptions(2000)
	transform := [16]float64{2, 0, 0, 0, 0, 2, 0, 0, 0, 0, 2, 0, 100, 200, 300, 1}
	opts.RootTransform = &transform
	dir, res := mustTile(t, splats, 1, opts)
	ts := readTileset(t, dir)

	if ts.Asset.Version != "1.1" {
		t.Errorf("asset version %q, want 1.1", ts.Asset.Version)
	}
	splatExtensions := []string{glb.ExtensionGaussianSplatting, glb.ExtensionSPZ}
	if !reflect.DeepEqual(ts.ExtensionsUsed, []string{contentGLTF}) || !reflect.DeepEqual(ts.ExtensionsRequired, []string{contentGLTF}) ||
		!reflect.DeepEqual(ts.Extensions, map[string]gltfJSON{contentGLTF: {ExtensionsUsed: splatExtensions, ExtensionsRequired: splatExtensions}}) {
		t.Errorf("extensions %v, %v, %v: want %s requiring the splat extensions", ts.ExtensionsUsed, ts.ExtensionsRequired, ts.Extensions, contentGLTF)
	}
	if ts.Root.Transform == nil || *ts.Root.Transform != transform {
		t.Errorf("root transform %v, want %v", ts.Root.Transform, transform)
	}
	if ts.GeometricError != ts.Root.GeometricError || ts.GeometricError != res.GeometricError || res.GeometricError <= 0 {
		t.Errorf("geometric errors: tileset %v, root %v, result %v", ts.GeometricError, ts.Root.GeometricError, res.GeometricError)
	}

	var tiles, leafSplats, depth int
	depths := map[*tileJSON]int{}
	walk(&ts.Root, func(tile, parent *tileJSON) {
		tiles++
		if parent != nil {
			depths[tile] = depths[parent] + 1
			depth = max(depth, depths[tile])
			if tile.Transform != nil {
				t.Errorf("tile %s has a transform", tile.Content.URI)
			}
			if !(parent.GeometricError >= tile.GeometricError*minErrorGrowth) {
				t.Errorf("tile %s has a geometric error of %v, its parent %v", tile.Content.URI, tile.GeometricError, parent.GeometricError)
			}
			if !boxContains(parent.BoundingVolume.Box, tile.BoundingVolume.Box) {
				t.Errorf("tile %s reaches out of its parent", tile.Content.URI)
			}
		}
		if tile.Refine != "REPLACE" || !strings.HasPrefix(tile.Content.URI, "data/tile_0") {
			t.Errorf("tile %s refines by %q", tile.Content.URI, tile.Refine)
		}
		c := readContent(t, dir, tile.Content.URI)
		if c.spzVersion != 2 || len(c.attributes) != 4+3 {
			t.Errorf("tile %s has SPZ version %d and attributes %v", tile.Content.URI, c.spzVersion, c.attributes)
		}
		if len(tile.Children) == 0 {
			leafSplats += c.count
			if tile.GeometricError != 0 {
				t.Errorf("leaf %s has a geometric error of %v", tile.Content.URI, tile.GeometricError)
			}
			if c.count > 2000*1.1 {
				t.Errorf("leaf %s holds %d splats", tile.Content.URI, c.count)
			}
			return
		}
		var children int
		for _, child := range tile.Children {
			children += readContent(t, dir, child.Content.URI).count
		}
		// Each level keeps about half the splats of the one below.
		if c.count > children/2+1 || c.count < children/3 {
			t.Errorf("tile %s holds %d splats, its children %d", tile.Content.URI, c.count, children)
		}
	})
	if leafSplats != len(splats) {
		t.Errorf("the leaves hold %d splats, want %d", leafSplats, len(splats))
	}
	if res.Read != len(splats) || res.Splats != len(splats) || res.SHDegree != 1 || res.Tiles != tiles || res.Depth != depth || depth < 3 {
		t.Errorf("result %+v, want %d splats in %d tiles %d levels deep", res, len(splats), tiles, depth)
	}
	// Only the tileset is left: no temporary files.
	entries, _ := os.ReadDir(dir)
	data, _ := os.ReadDir(filepath.Join(dir, dataDirName))
	if len(entries) != 2 || len(data) != tiles {
		t.Errorf("the output holds %v and %d tile files, want data and tileset.json, and %d tiles", entries, len(data), tiles)
	}
}

// readOutput returns the files of a tileset, by path.
func readOutput(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		rel, _ := filepath.Rel(dir, path)
		files[rel] = data
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestTileIsDeterministic(t *testing.T) {
	splats := randomSplats(2, 20_000, 2, geom.Vec3{}, geom.Vec3{X: 30, Y: 30, Z: 5})
	var outputs []map[string][]byte
	for _, workers := range []int{1, 8, 8} {
		opts := testOptions(1500)
		opts.Workers = workers
		dir, _ := mustTile(t, splats, 2, opts)
		outputs = append(outputs, readOutput(t, dir))
	}
	for i, output := range outputs[1:] {
		if !reflect.DeepEqual(output, outputs[0]) {
			t.Errorf("run %d wrote other files than the first", i+2)
		}
	}
}

func TestTileFitsSmallInputsInOneTile(t *testing.T) {
	splats := randomSplats(3, 500, 0, geom.Vec3{}, geom.Vec3{X: 10, Y: 10, Z: 10})
	dir, res := mustTile(t, splats, 0, DefaultOptions())
	ts := readTileset(t, dir)
	if len(ts.Root.Children) != 0 || ts.Root.GeometricError != 0 || ts.GeometricError != 0 {
		t.Errorf("root %+v, want one tile without error", ts.Root)
	}
	if c := readContent(t, dir, ts.Root.Content.URI); c.count != 500 {
		t.Errorf("the tile holds %d splats, want 500", c.count)
	}
	if res.Tiles != 1 || res.Depth != 0 || res.SHDegree != 0 {
		t.Errorf("result %+v", res)
	}
}

// TestTileKeepsOverfullLeaves tiles splats that pile up on one point, where
// the partition cannot split them: their leaf holds them all, and its parent
// merges fewer.
func TestTileKeepsOverfullLeaves(t *testing.T) {
	splats := randomSplats(4, 6000, 0, geom.Vec3{}, geom.Vec3{X: 20, Y: 20, Z: 2})
	for i := range 3000 {
		splats[i].Center = geom.Vec3{X: 5, Y: 5, Z: 0}
	}
	dir, _ := mustTile(t, splats, 0, testOptions(1000))
	ts := readTileset(t, dir)
	var overfull, leafSplats int
	walk(&ts.Root, func(tile, _ *tileJSON) {
		if len(tile.Children) == 0 {
			c := readContent(t, dir, tile.Content.URI).count
			leafSplats += c
			if c >= 3000 {
				overfull++
			}
		}
	})
	if overfull != 1 || leafSplats != 6000 {
		t.Errorf("%d overfull leaves and %d splats in the leaves, want 1 and 6000", overfull, leafSplats)
	}
}

func TestTileOptions(t *testing.T) {
	splats := randomSplats(5, 8000, 3, geom.Vec3{Y: -10}, geom.Vec3{X: 20, Y: 4, Z: 20})
	base := testOptions(1000)
	baseDir, baseRes := mustTile(t, splats, 3, base)
	baseTileset := readTileset(t, baseDir)
	if c := readContent(t, baseDir, baseTileset.Root.Content.URI); len(c.attributes) != 4+15 {
		t.Errorf("degree 3 content has attributes %v", c.attributes)
	}

	t.Run("max SH degree", func(t *testing.T) {
		opts := base
		opts.MaxSHDegree = 0
		dir, res := mustTile(t, splats, 3, opts)
		if c := readContent(t, dir, readTileset(t, dir).Root.Content.URI); len(c.attributes) != 4 || res.SHDegree != 0 {
			t.Errorf("degree %d, attributes %v", res.SHDegree, c.attributes)
		}
	})
	t.Run("min opacity", func(t *testing.T) {
		opts := base
		opts.MinOpacity = 0.5
		dir, res := mustTile(t, splats, 3, opts)
		var leafSplats int
		ts := readTileset(t, dir)
		walk(&ts.Root, func(tile, _ *tileJSON) {
			if len(tile.Children) == 0 {
				leafSplats += readContent(t, dir, tile.Content.URI).count
			}
		})
		if res.Read != 8000 || res.Splats > 4400 || res.Splats < 3600 || leafSplats != res.Splats {
			t.Errorf("read %d, kept %d, the leaves hold %d", res.Read, res.Splats, leafSplats)
		}
	})
	t.Run("up axis", func(t *testing.T) {
		opts := base
		opts.UpAxis = "-y"
		_, res := mustTile(t, splats, 3, opts)
		// The splats lie 8 to 12 below the origin along -Y, so 8 to 12 up.
		if res.Origin.Z < 7.5 || res.Origin.Z > 8 || baseRes.Origin.Z > -9 {
			t.Errorf("origins %+v and %+v, want the turned one about 8 up", res.Origin, baseRes.Origin)
		}
	})
	t.Run("geometric error correction", func(t *testing.T) {
		opts := base
		opts.GECorrection = 2.5
		dir, res := mustTile(t, splats, 3, opts)
		ts := readTileset(t, dir)
		var want, got []float64
		walk(&baseTileset.Root, func(tile, _ *tileJSON) { want = append(want, tile.GeometricError*2.5) })
		walk(&ts.Root, func(tile, _ *tileJSON) { got = append(got, tile.GeometricError) })
		for i := range want {
			if !closeTo(got[i], want[i]) {
				t.Fatalf("tile %d has a geometric error of %v, want %v", i, got[i], want[i])
			}
		}
		if !closeTo(res.GeometricError, baseRes.GeometricError*2.5) || !closeTo(ts.GeometricError, res.GeometricError) {
			t.Errorf("root error %v, want %v", res.GeometricError, baseRes.GeometricError*2.5)
		}
	})
	t.Run("SPZ version", func(t *testing.T) {
		opts := base
		opts.SPZVersion = spz.Version3
		dir, _ := mustTile(t, splats, 3, opts)
		if c := readContent(t, dir, readTileset(t, dir).Root.Content.URI); c.spzVersion != 3 {
			t.Errorf("SPZ version %d, want 3", c.spzVersion)
		}
	})
	t.Run("no root transform", func(t *testing.T) {
		if baseTileset.Root.Transform != nil {
			t.Errorf("root transform %v, want none", baseTileset.Root.Transform)
		}
	})
}

func TestTileValidatesOptions(t *testing.T) {
	nan := [16]float64{math.NaN()}
	for name, change := range map[string]func(*Options){
		"no splats per tile":    func(o *Options) { o.SplatsPerTile = 0 },
		"SH degree 4":           func(o *Options) { o.MaxSHDegree = 4 },
		"negative SH degree":    func(o *Options) { o.MaxSHDegree = -1 },
		"negative opacity":      func(o *Options) { o.MinOpacity = -0.1 },
		"opacity above 1":       func(o *Options) { o.MinOpacity = 1.5 },
		"NaN opacity":           func(o *Options) { o.MinOpacity = math.NaN() },
		"zero error correction": func(o *Options) { o.GECorrection = 0 },
		"infinite correction":   func(o *Options) { o.GECorrection = math.Inf(1) },
		"SPZ version 4":         func(o *Options) { o.SPZVersion = 4 },
		"no workers":            func(o *Options) { o.Workers = 0 },
		"bad up axis":           func(o *Options) { o.UpAxis = "w" },
		"non-finite transform":  func(o *Options) { o.RootTransform = &nan },
	} {
		opts := DefaultOptions()
		change(&opts)
		dir := filepath.Join(t.TempDir(), "out")
		if _, err := Tile(context.Background(), &sliceSource{}, dir, opts); err == nil {
			t.Errorf("%s: no error", name)
		}
		if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s: the output folder was created", name)
		}
	}
}

// cancelAt is a progress reporter that cancels a tiling when its phase
// starts.
type cancelAt struct {
	phase  string
	cancel context.CancelFunc
}

func (c cancelAt) Report(u tree.ProgressUpdate) {
	if u.Phase == c.phase {
		c.cancel()
	}
}

func TestTileCleansUpAfterErrors(t *testing.T) {
	splats := randomSplats(6, 5000, 1, geom.Vec3{}, geom.Vec3{X: 10, Y: 10, Z: 10})
	for _, phase := range []string{"reading", "splitting", "building"} {
		t.Run("canceled while "+phase, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			opts := testOptions(500)
			opts.Progress = cancelAt{phase: phase, cancel: cancel}
			dir := t.TempDir()
			_, err := Tile(ctx, &sliceSource{degree: 1, splats: splats}, dir, opts)
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("error %v, want it canceled", err)
			}
			assertOnlyData(t, dir)
		})
	}
	t.Run("source error", func(t *testing.T) {
		dir := t.TempDir()
		_, err := Tile(context.Background(), &sliceSource{degree: 1, splats: splats, err: errors.New("disk on fire")}, dir, testOptions(500))
		if err == nil || !strings.Contains(err.Error(), "disk on fire") {
			t.Fatalf("error %v", err)
		}
		assertOnlyData(t, dir)
	})
	t.Run("output is a file", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "file")
		if err := os.WriteFile(file, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Tile(context.Background(), &sliceSource{degree: 1, splats: splats}, file, testOptions(500)); err == nil {
			t.Error("tiling into a file succeeded")
		}
	})
}

// assertOnlyData checks that a failed tiling left neither temporary files
// nor a tileset.json in dir.
func assertOnlyData(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != dataDirName {
		names := []string{}
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("the output holds %v, want only the data folder", names)
	}
}

// recorder records progress updates.
type recorder struct {
	mu      sync.Mutex
	updates []tree.ProgressUpdate
}

func (r *recorder) Report(u tree.ProgressUpdate) {
	r.mu.Lock()
	r.updates = append(r.updates, u)
	r.mu.Unlock()
}

func TestTileReportsProgress(t *testing.T) {
	splats := randomSplats(7, 250_000, 0, geom.Vec3{}, geom.Vec3{X: 100, Y: 100, Z: 10})
	for i := range splats {
		splats[i].Opacity = 1
		if i < 50_000 {
			splats[i].Opacity = 0
		}
	}
	opts := testOptions(10_000)
	opts.MinOpacity = 0.01
	var r recorder
	opts.Progress = &r
	_, res := mustTile(t, splats, 0, opts)

	totals := map[string]int64{"reading": 250_000, "splitting": 200_000, "building": int64(res.Tiles)}
	phase := 0
	var last tree.ProgressUpdate
	for i, u := range r.updates {
		if u.Phase != Phases[phase].Name {
			// A phase ends with a milestone that counts all its items.
			if !last.IsMilestone || last.ItemCount != totals[last.Phase] || last.Percent != 100 {
				t.Errorf("phase %s ended with %+v", last.Phase, last)
			}
			phase++
			if phase == len(Phases) || u.Phase != Phases[phase].Name {
				t.Fatalf("update %d is of phase %q after %q", i, u.Phase, last.Phase)
			}
		}
		if u.ItemTotal != totals[u.Phase] && u.Phase != "reading" {
			t.Errorf("update %+v, want a total of %d", u, totals[u.Phase])
		}
		if u.Phase == last.Phase && u.ItemCount < last.ItemCount {
			t.Errorf("update %+v counts less than %+v", u, last)
		}
		last = u
	}
	if phase != len(Phases)-1 || !last.IsMilestone || last.ItemCount != totals["building"] {
		t.Errorf("the updates end with %+v", last)
	}
	// Reading and splitting report every 100,000 splats.
	var details int
	for _, u := range r.updates {
		if !u.IsMilestone && u.Phase != "building" {
			details++
		}
	}
	if details != 2+2 {
		t.Errorf("%d detail updates of reading and splitting, want 4", details)
	}
}

func TestGeometricError(t *testing.T) {
	size := geom.Vec3{X: 2, Y: 3, Z: 4}
	// The surface is 2·3 + 2·4 + 3·4 = 26.
	want := math.Sqrt(26.0/100) * 5 * 1.5 * math.Pow(1.1, 3)
	if got := geometricError(size, 100, 3, 1.5); !closeTo(got, want) {
		t.Errorf("geometric error %v, want %v", got, want)
	}
	if geometricError(size, 0, 1, 1) != 0 || geometricError(geom.Vec3{}, 10, 1, 1) != 0 {
		t.Error("no splats or no area should have no error")
	}
}

// gridSplats returns the splats of a grid of nx by ny by nz centers, one
// unit apart from the origin.
func gridSplats(nx, ny, nz int) []splat.Splat {
	var splats []splat.Splat
	for x := range nx {
		for y := range ny {
			for z := range nz {
				splats = append(splats, splat.Splat{
					Center:   geom.Vec3{X: float64(x), Y: float64(y), Z: float64(z)},
					Rotation: geom.IdentityQuat(), Scale: geom.Vec3{X: 0.1, Y: 0.1, Z: 0.1}, Opacity: 1,
					SourceIndex: len(splats),
				})
			}
		}
	}
	return splats
}

func TestSpread(t *testing.T) {
	// The centers left out are made up for as if all were evenly spread, which
	// overshoots by up to 2% on the planes of a grid.
	assertAbout := func(name string, got, want geom.Vec3) {
		t.Helper()
		for axis := range uint8(3) {
			if g, w := coordinate(got, axis), coordinate(want, axis); g < w || g > w*1.03 {
				t.Errorf("%s = %+v, want about %+v", name, got, want)
				return
			}
		}
	}
	grid := gridSplats(41, 21, 11)
	assertAbout("spread of a grid", spread(grid), geom.Vec3{X: 40, Y: 20, Z: 10})

	// A few splats far away, or very large, do not grow the box: 50 of 9471
	// splats are fewer than the 1% left out at each end.
	strays := slices.Clone(grid)
	for i := range 50 {
		strays[i*100].Center = geom.Vec3{X: 5000, Y: -3000, Z: 800}
		strays[i*100+1].Scale = geom.Vec3{X: 900, Y: 900, Z: 900}
	}
	assertAbout("spread with strays", spread(strays), geom.Vec3{X: 40, Y: 20, Z: 10})

	// Too few splats to leave any out fill the box of their centers.
	few := []splat.Splat{{Center: geom.Vec3{X: 1, Y: 2, Z: 3}}, {Center: geom.Vec3{X: 4, Y: 0, Z: 3}}, {Center: geom.Vec3{X: 2, Y: 8, Z: 5}}}
	assertVec3(t, "spread of three splats", spread(few), geom.Vec3{X: 3, Y: 8, Z: 2})
	if spread(few[:1]) != (geom.Vec3{}) || spread(nil) != (geom.Vec3{}) {
		t.Error("one splat or none should have no spread")
	}
}

func TestLODTarget(t *testing.T) {
	for count, want := range map[int]int{1: 1, 2: 1, 3: 2, 20_000: 10_000, 20_001: 10_001} {
		if got := lodTarget(count); got != want {
			t.Errorf("lodTarget(%d) = %d, want %d", count, got, want)
		}
	}
}

func TestBox(t *testing.T) {
	var b geom.Bounds
	b.IncludePoint(geom.Vec3{X: -1, Y: 2, Z: 3})
	b.IncludePoint(geom.Vec3{X: 3, Y: 4, Z: 9})
	if got, want := box(b), [12]float64{1, 3, 6, 2, 0, 0, 0, 1, 0, 0, 0, 3}; got != want {
		t.Errorf("box %v, want %v", got, want)
	}
}

// routedPlan returns a plan of splats routed to leaf stores in a temporary
// directory, and the input.
func routedPlan(t *testing.T, splats []splat.Splat, perTile int) (*node, *input, string) {
	t.Helper()
	in := mustReadInput(t, splats, 0, keepAll)
	plan := buildPlan(in.samples, in.count, perTile)
	dir := t.TempDir()
	if err := route(context.Background(), in, plan, dir, 2, nil); err != nil {
		t.Fatal(err)
	}
	return plan, in, dir
}

func TestBuilderErrors(t *testing.T) {
	splats := randomSplats(8, 3000, 0, geom.Vec3{}, geom.Vec3{X: 10, Y: 10, Z: 10})
	missing := filepath.Join(t.TempDir(), "missing")
	for _, test := range []struct {
		name         string
		perTile      int
		data, temp   func(routed string) string
		removeStores bool
	}{
		// Leaves cannot write their content.
		{"no data folder", 500, func(string) string { return missing }, func(r string) string { return r }, false},
		// Leaves cannot read their splats.
		{"no leaf stores", 500, func(string) string { return t.TempDir() }, func(r string) string { return r }, true},
		// Inner tiles cannot write the splats they hand up.
		{"no temporary folder", 500, func(string) string { return t.TempDir() }, func(string) string { return missing }, false},
		// An overfull leaf cannot write the splats it hands up.
		{"overfull leaf", 1, func(string) string { return t.TempDir() }, func(string) string { return missing }, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			plan, in, routed := routedPlan(t, splats, test.perTile)
			if test.removeStores {
				for _, leaf := range plan.leaves(nil) {
					os.Remove(leaf.path) //nolint:errcheck
				}
			}
			b := &builder{tempDir: test.temp(routed), dataDir: test.data(routed), shCount: in.shCount, perTile: test.perTile, geFactor: 1, version: spz.DefaultVersion}
			if _, err := b.build(context.Background(), plan, 3); err == nil {
				t.Error("building succeeded")
			}
		})
	}
}
