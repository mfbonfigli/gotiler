package tileset

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"path"
	"strings"
	"testing"
)

// files is an in-memory tileset folder, recording the order files are
// written in.
type files struct {
	data  map[string][]byte
	order []string
}

func newFiles() *files { return &files{data: map[string][]byte{}} }

type memoryFile struct {
	bytes.Buffer
	files *files
	name  string
}

func (f *memoryFile) Close() error {
	f.files.data[f.name] = f.Bytes()
	f.files.order = append(f.files.order, f.name)
	return nil
}

func (f *files) create(name string) (io.WriteCloser, error) {
	if _, ok := f.data[name]; ok {
		return nil, fmt.Errorf("%s written twice", name)
	}
	return &memoryFile{files: f, name: name}, nil
}

// binaryTree returns a full binary tree of the given depth, whose tiles hold
// "data/<id>d.glb", with the root's id empty, as the point cloud tiler names
// them.
func binaryTree(depth int, id string) *Tile {
	t := &Tile{
		Box:            [12]float64{float64(len(id)), 0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 1},
		GeometricError: float64(depth),
		Refine:         "ADD",
		Content:        "data/" + id + "d.glb",
	}
	if depth > 0 {
		t.Children = []*Tile{binaryTree(depth-1, id+"0"), binaryTree(depth-1, id+"1")}
	}
	return t
}

type tileFile struct {
	Asset              map[string]string `json:"asset"`
	ExtensionsUsed     []string          `json:"extensionsUsed"`
	ExtensionsRequired []string          `json:"extensionsRequired"`
	Extensions         map[string]any    `json:"extensions"`
	Properties         any               `json:"properties"`
	Schema             any               `json:"schema"`
	Metadata           any               `json:"metadata"`
	GeometricError     float64           `json:"geometricError"`
	Root               *readTile         `json:"root"`
}

type readTile struct {
	BoundingVolume struct {
		Box [12]float64 `json:"box"`
	} `json:"boundingVolume"`
	GeometricError float64      `json:"geometricError"`
	Refine         string       `json:"refine"`
	Transform      *[16]float64 `json:"transform"`
	Content        *struct {
		URI string `json:"uri"`
	} `json:"content"`
	Children []*readTile `json:"children"`
}

// tilesetReport is what reading a written tileset back finds.
type tilesetReport struct {
	// contents counts each content, by its path relative to the tileset root.
	contents map[string]int
	// tilesPerFile counts the tiles of each file.
	tilesPerFile map[string]int
	parsed       map[string]tileFile
}

// readTileset reads the tileset written to f from tileset.json, following
// its external tilesets, and checks the rules every file must follow.
func readTileset(t *testing.T, f *files) tilesetReport {
	t.Helper()
	report := tilesetReport{contents: map[string]int{}, tilesPerFile: map[string]int{}, parsed: map[string]tileFile{}}
	var readFile func(name string, parentBox *[12]float64, parentError float64)
	readFile = func(name string, parentBox *[12]float64, parentError float64) {
		data, ok := f.data[name]
		if !ok {
			t.Fatalf("%s is referenced but was not written", name)
		}
		var file tileFile
		if err := json.Unmarshal(data, &file); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		report.parsed[name] = file
		if file.Root == nil || file.Root.Refine == "" {
			t.Errorf("%s has a root tile without refine", name)
		}
		if file.GeometricError != file.Root.GeometricError {
			t.Errorf("%s has a geometric error of %v, its root %v", name, file.GeometricError, file.Root.GeometricError)
		}
		if parentBox != nil && (file.Root.BoundingVolume.Box != *parentBox || file.Root.GeometricError != parentError) {
			t.Errorf("the root of %s does not match the tile that refers to it", name)
		}
		dir := path.Dir(name)
		var walk func(tile *readTile)
		walk = func(tile *readTile) {
			report.tilesPerFile[name]++
			if tile.Content != nil {
				target := path.Join(dir, tile.Content.URI)
				if strings.HasSuffix(target, ".json") {
					if len(tile.Children) > 0 {
						t.Errorf("the tile of %s in %s has children", target, name)
					}
					if tile.Transform != nil {
						t.Errorf("the tile of %s in %s has a transform", target, name)
					}
					readFile(target, &tile.BoundingVolume.Box, tile.GeometricError)
					return
				}
				report.contents[target]++
			}
			for _, child := range tile.Children {
				walk(child)
			}
		}
		walk(file.Root)
	}
	readFile(RootFilename, nil, 0)
	if len(report.parsed) != len(f.data) {
		t.Errorf("%d files were written, %d are referenced", len(f.data), len(report.parsed))
	}
	if last := f.order[len(f.order)-1]; last != RootFilename {
		t.Errorf("the last file written is %s, want %s", last, RootFilename)
	}
	return report
}

func checkContents(t *testing.T, report tilesetReport, root *Tile) {
	t.Helper()
	var walk func(*Tile)
	want := 0
	walk = func(tile *Tile) {
		want++
		if n := report.contents[tile.Content]; n != 1 {
			t.Errorf("%s appears %d times", tile.Content, n)
		}
		for _, c := range tile.Children {
			walk(c)
		}
	}
	walk(root)
	if len(report.contents) != want {
		t.Errorf("the tileset holds %d contents, want %d", len(report.contents), want)
	}
}

func TestWriteKeepsSmallTilesetsInOneFile(t *testing.T) {
	root := binaryTree(5, "") // 63 tiles
	transform := [16]float64{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 10, 20, 30, 1}
	root.Transform = &transform
	for _, maxTiles := range []int{0, 63, 1000} {
		f := newFiles()
		result, err := Write(f.create, root, Header{Version: "1.1"}, RootOnly{}, Options{MaxTiles: maxTiles})
		if err != nil {
			t.Fatal(err)
		}
		if result.Files != 1 || len(f.data) != 1 {
			t.Fatalf("MaxTiles %d: %d files written, want 1", maxTiles, len(f.data))
		}
		report := readTileset(t, f)
		checkContents(t, report, root)
		file := report.parsed[RootFilename]
		if file.Asset["version"] != "1.1" || file.Root.Transform == nil || *file.Root.Transform != transform {
			t.Errorf("MaxTiles %d: root file %+v", maxTiles, file)
		}
		if uri := file.Root.Children[0].Content.URI; uri != "data/0d.glb" {
			t.Errorf("MaxTiles %d: child content %q", maxTiles, uri)
		}
	}
}

func TestWriteSplitsLargeTilesets(t *testing.T) {
	root := binaryTree(12, "") // 8191 tiles
	transform := [16]float64{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 10, 20, 30, 1}
	root.Transform = &transform
	header := Header{
		Version:            "1.1",
		ExtensionsUsed:     []string{"3DTILES_content_gltf"},
		ExtensionsRequired: []string{"3DTILES_content_gltf"},
		Extensions:         map[string]any{"3DTILES_content_gltf": map[string]any{"extensionsRequired": []string{"KHR_gaussian_splatting"}}},
	}
	rootOnly := RootOnly{Schema: map[string]any{"id": "s"}, Metadata: map[string]any{"class": "c"}}
	f := newFiles()
	result, err := Write(f.create, root, header, rootOnly, Options{MaxTiles: 500})
	if err != nil {
		t.Fatal(err)
	}
	if result.Files != len(f.data) || result.Files < 16 {
		t.Fatalf("%d files written, result says %d", len(f.data), result.Files)
	}
	report := readTileset(t, f)
	checkContents(t, report, root)

	for name, file := range report.parsed {
		if n := report.tilesPerFile[name]; n > 2*500 {
			t.Errorf("%s holds %d tiles", name, n)
		}
		if file.Asset["version"] != "1.1" || len(file.ExtensionsRequired) != 1 || file.Extensions["3DTILES_content_gltf"] == nil {
			t.Errorf("%s does not declare the header: %+v", name, file)
		}
		isRoot := name == RootFilename
		if (file.Schema != nil) != isRoot || (file.Metadata != nil) != isRoot || (file.Root.Transform != nil) != isRoot {
			t.Errorf("%s: schema %v, metadata %v, transform %v", name, file.Schema, file.Metadata, file.Root.Transform)
		}
		if !isRoot && !strings.HasPrefix(name, "data/") {
			t.Errorf("external tileset %s is not next to the contents", name)
		}
	}
	// The root file holds the top of the tree, which viewers read first: the
	// largest subtrees that fit in 500 tiles hold 255 and start 5 levels
	// down, under 31 tiles, plus the 32 tiles that refer to them.
	if n := report.tilesPerFile[RootFilename]; n != 63 {
		t.Errorf("the root file holds %d tiles, want 63", n)
	}
	// External tilesets refer to their contents from their own folder.
	if uri := report.parsed["data/00000d.json"].Root.Content.URI; uri != "00000d.glb" {
		t.Errorf("data/00000d.json refers to its content as %q", uri)
	}
}

func TestWriteSplitsTreesTooLargeForOneLevelOfFiles(t *testing.T) {
	root := binaryTree(14, "") // 32767 tiles: 64 files of at most 500 under a skeleton of 127
	f := newFiles()
	if _, err := Write(f.create, root, Header{Version: "1.1"}, RootOnly{}, Options{MaxTiles: 40}); err != nil {
		t.Fatal(err)
	}
	report := readTileset(t, f)
	checkContents(t, report, root)
	for name, n := range report.tilesPerFile {
		if n > 3*40 {
			t.Errorf("%s holds %d tiles", name, n)
		}
	}
}

func TestWriteKeepsSmallSubtreesInTheirParentFile(t *testing.T) {
	// A root over one large subtree and one single tile: the single tile
	// stays in the root file rather than in a file of its own.
	root := &Tile{Refine: "REPLACE", Content: "data/tile_r.glb", Children: []*Tile{
		binaryTree(9, "a"), // 1023 tiles
		{Refine: "REPLACE", Content: "data/tile_b.glb"},
	}}
	f := newFiles()
	if _, err := Write(f.create, root, Header{Version: "1.1"}, RootOnly{}, Options{MaxTiles: 800}); err != nil {
		t.Fatal(err)
	}
	report := readTileset(t, f)
	checkContents(t, report, root)
	children := report.parsed[RootFilename].Root.Children
	if len(children) != 2 || children[1].Content.URI != "data/tile_b.glb" {
		t.Errorf("the single tile is not in the root file: %+v", children)
	}
	if _, ok := f.data["data/tile_b.json"]; ok {
		t.Error("the single tile has a file of its own")
	}
}

func TestWriteNamesExternalTilesetsApart(t *testing.T) {
	// Tiles without content, or whose contents share a name, still get
	// files of their own, and none replaces tileset.json.
	leaf := func(content string) *Tile { return &Tile{Refine: "ADD", Content: content} }
	branch := func(children ...*Tile) *Tile { return &Tile{Refine: "ADD", Children: children} }
	root := branch(
		branch(leaf("data/a.glb"), leaf("data/b.glb")),
		branch(leaf("data/c.glb"), leaf("data/d.glb")),
	)
	f := newFiles()
	if _, err := Write(f.create, root, Header{Version: "1.0"}, RootOnly{}, Options{MaxTiles: 1}); err != nil {
		t.Fatal(err)
	}
	report := readTileset(t, f)
	if len(report.contents) != 4 {
		t.Errorf("the tileset holds contents %v", report.contents)
	}
	if _, ok := f.data["tileset_2.json"]; !ok {
		t.Errorf("files %v", f.order)
	}
}

func TestWriteReportsErrors(t *testing.T) {
	if _, err := Write(newFiles().create, nil, Header{}, RootOnly{}, Options{}); err == nil {
		t.Error("no error without a root tile")
	}
	if _, err := Write(newFiles().create, binaryTree(1, ""), Header{}, RootOnly{}, Options{MaxTiles: -1}); err == nil {
		t.Error("no error with a negative MaxTiles")
	}
	failure := errors.New("disk full")
	failing := func(name string) (io.WriteCloser, error) { return nil, failure }
	if _, err := Write(failing, binaryTree(3, ""), Header{}, RootOnly{}, Options{MaxTiles: 4}); !errors.Is(err, failure) {
		t.Errorf("error %v, want %v", err, failure)
	}
}

func TestRelative(t *testing.T) {
	for _, c := range []struct{ dir, target, want string }{
		{".", "data/0d.glb", "data/0d.glb"},
		{"data", "data/0d.glb", "0d.glb"},
		{"data", "data/sub/0d.glb", "sub/0d.glb"},
		{"a/b", "a/c/x.glb", "../c/x.glb"},
		{"a/b", "c/d.glb", "../../c/d.glb"},
		{"data", "https://example.com/x.glb", "https://example.com/x.glb"},
	} {
		if got := relative(c.dir, c.target); got != c.want {
			t.Errorf("relative(%q, %q) = %q, want %q", c.dir, c.target, got, c.want)
		}
	}
}

func TestRoundBoxGrowsOutwardAndKeepsChildrenInside(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	scale := gridScale([12]float64{0, 0, 0, 150, 0, 0, 0, 120, 0, 0, 0, 40}) // a 300 m root
	if scale != 1e4 {
		t.Fatalf("grid scale %v, want 1e4", scale)
	}
	for range 10_000 {
		var parent, child [12]float64
		for axis, half := range [3]int{3, 7, 11} {
			parent[axis] = rng.Float64()*300 - 150
			parent[half] = rng.Float64() * 100
			// a child inside the parent, sometimes sharing a face with it
			lo, hi := parent[axis]-parent[half], parent[axis]+parent[half]
			a, b := lo+rng.Float64()*(hi-lo), lo+rng.Float64()*(hi-lo)
			if rng.IntN(4) == 0 {
				a = lo
			}
			child[axis], child[half] = (a+b)/2, math.Abs(b-a)/2
		}
		rp, rc := roundBox(parent, scale), roundBox(child, scale)
		// faces are compared as viewers compute them, center minus or plus
		// half, which may differ in the last bit when they are shared
		const epsilon = 1e-9
		for axis, half := range [3]int{3, 7, 11} {
			if rp[axis]-rp[half] > parent[axis]-parent[half]+epsilon || rp[axis]+rp[half] < parent[axis]+parent[half]-epsilon {
				t.Fatalf("rounded box %v does not hold %v", rp, parent)
			}
			if rc[axis]-rc[half] < rp[axis]-rp[half]-epsilon || rc[axis]+rc[half] > rp[axis]+rp[half]+epsilon {
				t.Fatalf("rounded child %v reaches out of rounded parent %v", rc, rp)
			}
			if growth := rp[half] - parent[half]; growth > 1/scale {
				t.Fatalf("box grew by %v", growth)
			}
		}
	}
	// Rounded values print short: faces on the grid, centers and halves on
	// the grid or halfway between two steps.
	data, _ := json.Marshal(roundBox([12]float64{89.40999984741211, 120.57673835754395, 70.00825905799866, 136.75468826293945, 0, 0, 0, 163.34071159362793, 0, 0, 0, 74.20632529258728}, scale))
	if string(data) != "[89.41,120.57675,70.00825,136.7547,0,0,0,163.34075,0,0,0,74.20635]" {
		t.Errorf("rounded box %s", data)
	}
}

func TestGridScaleFollowsTheDatasetSize(t *testing.T) {
	box := func(half float64) [12]float64 {
		return [12]float64{0, 0, 0, half, 0, 0, 0, half / 2, 0, 0, 0, half / 4}
	}
	for _, c := range []struct {
		box  [12]float64
		want float64
	}{
		{box(150), 1e4},   // 300 m: a tenth of a millimeter
		{box(5000), 1e2},  // 10 km: a centimeter
		{box(0.001), 1e9}, // 2 mm: at most a nanometer
		{box(1e9), 1},     // larger than the Earth: a meter
		{[12]float64{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}, 1e6}, // flat on X and Y, 2 m on Z
		{[12]float64{}, 0}, // no size: left as is
		{[12]float64{0, 0, 0, 1, 1, 0, 0, 1, 0, 0, 0, 1}, 0}, // not axis-aligned
	} {
		if got := gridScale(c.box); got != c.want {
			t.Errorf("gridScale(%v) = %v, want %v", c.box, got, c.want)
		}
	}
	rotated := [12]float64{1.23456789, 0, 0, 1, 1, 0, 0, 1, 0, 0, 0, 1}
	if roundBox(rotated, 1e4) != rotated {
		t.Error("a box that is not axis-aligned was rounded")
	}
}

func TestRoundErrorKeepsFiveSignificantDigits(t *testing.T) {
	for _, c := range []struct{ in, want float64 }{
		{127.72339392235129, 127.72},
		{0.012345678, 0.012346},
		{5584.594950530971, 5584.6},
		{0, 0},
	} {
		if got := roundError(c.in); got != c.want {
			t.Errorf("roundError(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestWriteStatesRefineOnlyWhereItChanges(t *testing.T) {
	root := &Tile{Refine: "ADD", Content: "data/r.glb", Children: []*Tile{
		{Refine: "ADD", Content: "data/a.glb"},
		{Refine: "REPLACE", Content: "data/b.glb", Children: []*Tile{{Refine: "REPLACE", Content: "data/c.glb"}}},
	}}
	f := newFiles()
	if _, err := Write(f.create, root, Header{Version: "1.1"}, RootOnly{}, Options{}); err != nil {
		t.Fatal(err)
	}
	file := readTileset(t, f).parsed[RootFilename]
	a, b := file.Root.Children[0], file.Root.Children[1]
	if file.Root.Refine != "ADD" || a.Refine != "" || b.Refine != "REPLACE" || b.Children[0].Refine != "" {
		t.Errorf("refines %q, %q, %q, %q", file.Root.Refine, a.Refine, b.Refine, b.Children[0].Refine)
	}
}
