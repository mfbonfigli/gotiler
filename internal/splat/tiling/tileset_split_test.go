package tiling

import (
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mfbonfigli/gotiler/v3/internal/geom"
)

func TestTileSplitsLargeTilesetsIntoExternalTilesets(t *testing.T) {
	splats := randomSplats(1, 30_000, 1, geom.Vec3{X: 4.5e6, Y: 1e5, Z: 50}, geom.Vec3{X: 80, Y: 40, Z: 10})
	opts := testOptions(1000)
	opts.MaxTilesPerTileset = 6
	transform := [16]float64{2, 0, 0, 0, 0, 2, 0, 0, 0, 0, 2, 0, 100, 200, 300, 1}
	opts.RootTransform = &transform
	dir, res := mustTile(t, splats, 1, opts)

	contents := map[string]int{}
	files := 0
	var read func(name string)
	read = func(name string) {
		files++
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		var ts tilesetJSON
		if err := json.Unmarshal(data, &ts); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		// CesiumJS reads the splat extensions from tileset.json only, but
		// every file declares them, as 3D Tiles asks.
		if len(ts.Extensions[contentGLTF].ExtensionsRequired) != 2 || ts.Asset.Version != "1.1" {
			t.Errorf("%s declares %+v", name, ts)
		}
		if (ts.Root.Transform != nil) != (name == "tileset.json") {
			t.Errorf("%s has the root transform %v", name, ts.Root.Transform)
		}
		dir := path.Dir(name)
		walk(&ts.Root, func(tile, _ *tileJSON) {
			if tile.Refine != "REPLACE" {
				t.Errorf("a tile of %s refines by %q", name, tile.Refine)
			}
			uri := path.Join(dir, tile.Content.URI)
			if strings.HasSuffix(uri, ".json") {
				read(uri)
				return
			}
			contents[uri]++
		})
	}
	read("tileset.json")

	if files < 3 {
		t.Errorf("%d tileset files for %d tiles", files, res.Tiles)
	}
	entries, err := os.ReadDir(filepath.Join(dir, dataDirName))
	if err != nil {
		t.Fatal(err)
	}
	tiles := 0
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".glb") {
			continue
		}
		tiles++
		if n := contents[path.Join(dataDirName, e.Name())]; n != 1 {
			t.Errorf("%s is referenced %d times", e.Name(), n)
		}
	}
	if tiles != res.Tiles || len(contents) != res.Tiles {
		t.Errorf("%d tile files, %d referenced, %d in the result", tiles, len(contents), res.Tiles)
	}
}
