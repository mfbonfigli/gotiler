package tiling

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qmuntal/gltf"

	"github.com/mfbonfigli/gotiler/v3/internal/mesh"
	"github.com/mfbonfigli/gotiler/v3/internal/mesh/glb"
)

// terrainSize is the side of the test terrain, in cells and in units.
const terrainSize = 48

func terrainHeight(x, y float64) float64 { return 3 * math.Sin(x/7) * math.Cos(y/5) }

// writeTerrain writes a square terrain of two triangles a cell as an OBJ
// file in dir, and returns its path. Its west half uses the material and
// image "west" and its east half "east", each spanning its half, so that a
// texture seam runs down the middle. The red of the images grows with u and
// their green toward the bottom; their blue tells them apart.
func writeTerrain(t *testing.T, dir string) string {
	t.Helper()
	for name, blue := range map[string]uint8{"west": 40, "east": 220} {
		img := image.NewNRGBA(image.Rect(0, 0, 256, 256))
		for y := range 256 {
			for x := range 256 {
				img.Set(x, y, color.NRGBA{R: uint8(x), G: uint8(y), B: blue, A: 255})
			}
		}
		var encoded bytes.Buffer
		if err := png.Encode(&encoded, img); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".png"), encoded.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mtl := "newmtl west\nmap_Kd west.png\nnewmtl east\nmap_Kd east.png\n"
	if err := os.WriteFile(filepath.Join(dir, "terrain.mtl"), []byte(mtl), 0o644); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	b.WriteString("mtllib terrain.mtl\n")
	const half = terrainSize / 2
	for y := 0; y <= terrainSize; y++ {
		for x := 0; x <= terrainSize; x++ {
			fmt.Fprintf(&b, "v %d %d %.6f\n", 1000+x, 2000+y, terrainHeight(float64(x), float64(y)))
		}
	}
	// One set of texture coordinates for each half: columns 0 to half.
	for y := 0; y <= terrainSize; y++ {
		for x := 0; x <= half; x++ {
			fmt.Fprintf(&b, "vt %.6f %.6f\n", float64(x)/half, float64(y)/terrainSize)
		}
	}
	for i, name := range []string{"west", "east"} {
		fmt.Fprintf(&b, "usemtl %s\n", name)
		for y := range terrainSize {
			for x := range half {
				v := func(dx, dy int) string {
					position := (y+dy)*(terrainSize+1) + i*half + x + dx + 1
					texCoord := (y+dy)*(half+1) + x + dx + 1
					return fmt.Sprintf("%d/%d", position, texCoord)
				}
				fmt.Fprintf(&b, "f %s %s %s\nf %s %s %s\n", v(0, 0), v(1, 0), v(1, 1), v(0, 0), v(1, 1), v(0, 1))
			}
		}
	}
	path := filepath.Join(dir, "terrain.obj")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// content is a tile content read back: its triangles in the frame of the
// tileset, with texture coordinates in its image.
type content struct {
	triangles []mesh.Triangle
	image     image.Image
	doc       *gltf.Document
}

// readContent reads an uncompressed tile content.
func readContent(t *testing.T, path string) content {
	t.Helper()
	doc, err := gltf.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	data := doc.Buffers[0].Data
	view := func(accessor int) []byte {
		v := doc.BufferViews[*doc.Accessors[accessor].BufferView]
		return data[v.ByteOffset : v.ByteOffset+v.ByteLength]
	}
	primitive := doc.Meshes[0].Primitives[0]
	positions, texCoords := view(primitive.Attributes[gltf.POSITION]), view(primitive.Attributes[gltf.TEXCOORD_0])
	matrix := doc.Nodes[0].Matrix
	// The node scales the grid, moves it and turns Z-up into Y-up: undo the
	// turn, to get back the frame of the tileset.
	step := matrix[0]
	origin := [3]float64{matrix[12], -matrix[14], matrix[13]}
	indices := view(*primitive.Indices)
	indexSize := 2
	if doc.Accessors[*primitive.Indices].ComponentType == gltf.ComponentUint {
		indexSize = 4
	}
	c := content{doc: doc}
	for i := 0; i < len(indices)/indexSize; i += 3 {
		var tr mesh.Triangle
		for k := range 3 {
			var index int
			if indexSize == 2 {
				index = int(binary.LittleEndian.Uint16(indices[(i+k)*2:]))
			} else {
				index = int(binary.LittleEndian.Uint32(indices[(i+k)*4:]))
			}
			for a := range 3 {
				q := binary.LittleEndian.Uint16(positions[index*8+2*a:])
				tr.P[k][a] = float32(origin[a] + float64(q)*step)
			}
			for a := range 2 {
				tr.UV[k][a] = float32(binary.LittleEndian.Uint16(texCoords[index*4+2*a:])) / math.MaxUint16
			}
		}
		c.triangles = append(c.triangles, tr)
	}
	imageView := doc.BufferViews[*doc.Images[0].BufferView]
	if c.image, err = jpeg.Decode(bytes.NewReader(data[imageView.ByteOffset : imageView.ByteOffset+imageView.ByteLength])); err != nil {
		t.Fatal(err)
	}
	return c
}

type tilesetTile struct {
	BoundingVolume struct {
		Box [12]float64 `json:"box"`
	} `json:"boundingVolume"`
	GeometricError float64 `json:"geometricError"`
	Refine         string  `json:"refine"`
	Content        struct {
		URI string `json:"uri"`
	} `json:"content"`
	Children []*tilesetTile `json:"children"`
}

func readTileset(t *testing.T, dir string) *tilesetTile {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "tileset.json"))
	if err != nil {
		t.Fatal(err)
	}
	var ts struct {
		Root *tilesetTile `json:"root"`
	}
	if err := json.Unmarshal(data, &ts); err != nil {
		t.Fatal(err)
	}
	return ts.Root
}

func walk(tile *tilesetTile, visit func(*tilesetTile)) {
	visit(tile)
	for _, child := range tile.Children {
		walk(child, visit)
	}
}

func testOptions() Options {
	opts := DefaultOptions()
	opts.TrianglesPerTile = 600
	opts.Compress = false
	opts.TextureQuality = 95
	opts.Workers = 4
	return opts
}

func TestTileKeepsTheSurfaceAndItsTextureAtEveryLevel(t *testing.T) {
	source := writeTerrain(t, t.TempDir())
	dir := t.TempDir()

	result, err := Tile(context.Background(), source, dir, testOptions())

	if err != nil {
		t.Fatal(err)
	}
	if result.Read != 2*terrainSize*terrainSize || result.Tiles < 15 || result.Depth < 3 {
		t.Fatalf("result = %+v", result)
	}
	if result.Origin.X != 1000 || result.Origin.Y != 2000 {
		t.Errorf("origin = %v", result.Origin)
	}
	root := readTileset(t, dir)
	if root.GeometricError <= 0 || root.Refine != "REPLACE" {
		t.Errorf("root: error %v, refine %q", root.GeometricError, root.Refine)
	}
	tiles := 0
	walk(root, func(tile *tilesetTile) {
		tiles++
		c := readContent(t, filepath.Join(dir, filepath.FromSlash(tile.Content.URI)))
		leaf := len(tile.Children) == 0
		if leaf != (tile.GeometricError == 0) {
			t.Errorf("%s: geometric error %v with %d children", tile.Content.URI, tile.GeometricError, len(tile.Children))
		}
		for _, child := range tile.Children {
			if child.GeometricError >= tile.GeometricError {
				t.Errorf("%s: error %v not above that of its child, %v", tile.Content.URI, tile.GeometricError, child.GeometricError)
			}
		}
		// A leaf shows the input, and a tile above the same surface: both
		// cover their part of the square once, seen from above.
		box := tile.BoundingVolume.Box
		var covered float64
		for _, tr := range c.triangles {
			n := mesh.Normal(tr.P[0], tr.P[1], tr.P[2])
			// Slivers left where cuts meet have no area to speak of, and
			// no side once their positions are rounded.
			if n[2] < -1e-3 {
				t.Fatalf("%s: a triangle faces down", tile.Content.URI)
			}
			covered += n[2] / 2
			for _, p := range tr.P {
				for a := range 3 {
					if d := math.Abs(float64(p[a]) - box[a]); d > box[3+4*a]+1e-2 {
						t.Fatalf("%s: position %v outside the box %v", tile.Content.URI, p, box)
					}
				}
			}
			checkColor(t, tile.Content.URI, c, tr, leaf)
		}
		if want := 4 * box[3] * box[7]; math.Abs(covered-want) > 1e-2*want {
			t.Errorf("%s: covers %.2f of its box of %.2f", tile.Content.URI, covered, want)
		}
		if !leaf && len(c.triangles) > 2*testOptions().TrianglesPerTile {
			t.Errorf("%s: %d triangles", tile.Content.URI, len(c.triangles))
		}
	})
	if tiles != result.Tiles {
		t.Errorf("tileset.json lists %d tiles, the result %d", tiles, result.Tiles)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 2 {
		t.Errorf("the output holds %d entries, want tileset.json and data", len(entries))
	}
}

// checkColor checks that the middle of tr shows the color that the input
// has there.
func checkColor(t *testing.T, name string, c content, tr mesh.Triangle, exact bool) {
	t.Helper()
	var x, y, u, v float64
	for k := range 3 {
		x += float64(tr.P[k][0]) / 3
		y += float64(tr.P[k][1]) / 3
		u += float64(tr.UV[k][0]) / 3
		v += float64(tr.UV[k][1]) / 3
	}
	const half = terrainSize / 2
	// The colors jump where the two images meet. Coarser levels draw their
	// texture anew with larger pixels, which blend the two sides there.
	if !exact && math.Abs(x-half) < 2 {
		return
	}
	wantBlue := 40.0
	if x > half {
		x -= half
		wantBlue = 220
	}
	wantRed, wantGreen := 255*x/half, 255*(1-y/terrainSize)
	bounds := c.image.Bounds()
	px := min(int(u*float64(bounds.Dx())), bounds.Dx()-1)
	py := min(int(v*float64(bounds.Dy())), bounds.Dy()-1)
	r, g, b, _ := c.image.At(px, py).RGBA()
	tolerance := 6.0
	if !exact {
		// Coarser levels average more of the image into each pixel.
		tolerance = 16
	}
	if math.Abs(float64(r>>8)-wantRed) > tolerance || math.Abs(float64(g>>8)-wantGreen) > tolerance || math.Abs(float64(b>>8)-wantBlue) > 30 {
		t.Fatalf("%s: the triangle around %.2f, %.2f shows %d, %d, %d, want %.0f, %.0f, %.0f", name, x, y, r>>8, g>>8, b>>8, wantRed, wantGreen, wantBlue)
	}
}

func TestTileJoinsNeighborsWithoutCracks(t *testing.T) {
	source := writeTerrain(t, t.TempDir())
	dir := t.TempDir()
	if _, err := Tile(context.Background(), source, dir, testOptions()); err != nil {
		t.Fatal(err)
	}

	// The edges on the border of a tile are those of one triangle only. On
	// the outline of the terrain they have no neighbor; anywhere else, the
	// tiles shown next to this one, at any level of detail, must hold the
	// same positions, so that they meet.
	type point [3]float32
	type borderPoint struct {
		p    point
		tile int
	}
	levels := map[int][]borderPoint{}
	tiles := 0
	var collect func(tile *tilesetTile, level int)
	collect = func(tile *tilesetTile, level int) {
		index := tiles
		tiles++
		for _, child := range tile.Children {
			collect(child, level+1)
		}
		c := readContent(t, filepath.Join(dir, filepath.FromSlash(tile.Content.URI)))
		edges := map[[2]point]int{}
		for _, tr := range c.triangles {
			for k := range 3 {
				a, b := point(tr.P[k]), point(tr.P[(k+1)%3])
				if lower(b, a) {
					a, b = b, a
				}
				edges[[2]point{a, b}]++
			}
		}
		for e, uses := range edges {
			if uses == 1 {
				levels[level] = append(levels[level], borderPoint{e[0], index}, borderPoint{e[1], index})
			}
		}
	}
	collect(readTileset(t, dir), 0)
	// Tiles of one level may store positions on grids of different steps:
	// allow for the coarsest.
	const tolerance = 2e-3
	for level, points := range levels {
		for _, a := range points {
			outline := a.p[0] < tolerance || a.p[0] > terrainSize-tolerance || a.p[1] < tolerance || a.p[1] > terrainSize-tolerance
			if outline {
				continue
			}
			met := false
			for _, b := range points {
				if b.tile != a.tile && math.Abs(float64(a.p[0]-b.p[0])) < tolerance && math.Abs(float64(a.p[1]-b.p[1])) < tolerance && math.Abs(float64(a.p[2]-b.p[2])) < tolerance {
					met = true
					break
				}
			}
			if !met {
				t.Fatalf("level %d: no neighbor holds the border position %v", level, a.p)
			}
		}
	}
}

func TestTileCompressesAndIsDeterministic(t *testing.T) {
	source := writeTerrain(t, t.TempDir())
	opts := testOptions()
	opts.Compress = true
	var outputs [2]map[string][]byte
	for i := range outputs {
		dir := t.TempDir()
		if _, err := Tile(context.Background(), source, dir, opts); err != nil {
			t.Fatal(err)
		}
		outputs[i] = map[string][]byte{}
		err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}
			rel, _ := filepath.Rel(dir, path)
			outputs[i][rel], err = os.ReadFile(path)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(outputs[0]) != len(outputs[1]) {
		t.Fatalf("runs wrote %d and %d files", len(outputs[0]), len(outputs[1]))
	}
	for name, data := range outputs[0] {
		if !bytes.Equal(data, outputs[1][name]) {
			t.Errorf("%s differs between runs", name)
		}
	}
	checked := false
	for name, data := range outputs[0] {
		if !strings.HasSuffix(name, ".glb") {
			continue
		}
		checked = true
		for _, extension := range []string{glb.ExtensionMeshopt, glb.ExtensionQuantization, `"mode":"ATTRIBUTES"`, `"fallback":true`} {
			if !bytes.Contains(data, []byte(extension)) {
				t.Fatalf("%s does not mention %s", name, extension)
			}
		}
	}
	if !checked {
		t.Error("no tile content was written")
	}
}

func TestTilePlainMeshWithoutTexture(t *testing.T) {
	var b strings.Builder
	for y := 0; y <= 30; y++ {
		for x := 0; x <= 30; x++ {
			fmt.Fprintf(&b, "v %d %d 0\n", x, y)
		}
	}
	for y := range 30 {
		for x := range 30 {
			v := y*31 + x + 1
			fmt.Fprintf(&b, "f %d %d %d\nf %d %d %d\n", v, v+1, v+32, v, v+32, v+31)
		}
	}
	source := filepath.Join(t.TempDir(), "plain.obj")
	if err := os.WriteFile(source, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()

	result, err := Tile(context.Background(), source, dir, testOptions())

	if err != nil {
		t.Fatal(err)
	}
	if result.Tiles < 3 || result.GeometricError <= 0 {
		t.Fatalf("result = %+v", result)
	}
	walk(readTileset(t, dir), func(tile *tilesetTile) {
		c := readContent(t, filepath.Join(dir, filepath.FromSlash(tile.Content.URI)))
		r, g, b, _ := c.image.At(0, 0).RGBA()
		// Faces without a material show a light gray.
		if r>>8 < 190 || r>>8 > 210 || r != g || g != b {
			t.Errorf("%s: the texture shows %d, %d, %d", tile.Content.URI, r>>8, g>>8, b>>8)
		}
		if bounds := c.image.Bounds(); bounds.Dx() > 256 || bounds.Dy() > 256 {
			t.Errorf("%s: a texture of %dx%d for a plain color", tile.Content.URI, bounds.Dx(), bounds.Dy())
		}
	})
}

func TestTileRejectsBadOptionsAndEmptyMeshes(t *testing.T) {
	opts := testOptions()
	opts.UpAxis = "w"
	if _, err := Tile(context.Background(), "unused.obj", t.TempDir(), opts); err == nil {
		t.Error("an unknown up axis was accepted")
	}
	empty := filepath.Join(t.TempDir(), "empty.obj")
	if err := os.WriteFile(empty, []byte("v 0 0 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if _, err := Tile(context.Background(), empty, dir, testOptions()); err == nil {
		t.Error("a mesh without triangles was tiled")
	}
	if entries, _ := os.ReadDir(dir); len(entries) > 1 {
		t.Errorf("a failed tiling left %d entries behind", len(entries))
	}
}
