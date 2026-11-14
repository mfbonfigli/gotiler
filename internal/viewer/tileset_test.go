package viewer

import (
	"encoding/json"
	"errors"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/mfbonfigli/gotiler/v3/internal/geom"
)

func rotationZ(degrees float64) geom.Mat3 {
	s, c := math.Sincos(degrees * math.Pi / 180)
	return geom.Mat3{{c, -s, 0}, {s, c, 0}, {0, 0, 1}}
}

func rotationX(degrees float64) geom.Mat3 {
	s, c := math.Sincos(degrees * math.Pi / 180)
	return geom.Mat3{{1, 0, 0}, {0, c, -s}, {0, s, c}}
}

func scaled(m geom.Mat3, factor float64) geom.Mat3 {
	for row := range 3 {
		for col := range 3 {
			m[row][col] *= factor
		}
	}
	return m
}

// columnMajor builds a 4x4 column-major transform from its linear part and
// translation.
func columnMajor(linear geom.Mat3, tx, ty, tz float64) [16]float64 {
	return [16]float64{
		linear[0][0], linear[1][0], linear[2][0], 0,
		linear[0][1], linear[1][1], linear[2][1], 0,
		linear[0][2], linear[1][2], linear[2][2], 0,
		tx, ty, tz, 1,
	}
}

// placedTransform resembles the east-north-up root transform gotiler writes.
var placedTransform = columnMajor(rotationX(-48).Mul(rotationZ(102)), 4517590.9, 1096950.6, 4382030.8)

func transformJSON(transform [16]float64) string {
	data, _ := json.Marshal(transform)
	return string(data)
}

// sampleTileset is a compact tileset like the gotiler writer produces, with a
// content URI holding an ampersand and a property range beyond float64
// precision, which saving must preserve exactly.
func sampleTileset(rootTransform string) string {
	transformField := ""
	if rootTransform != "" {
		transformField = `,"transform":` + rootTransform
	}
	return `{"asset":{"version":"1.1"},"geometricError":12.5,` +
		`"root":{"children":[{"content":{"uri":"data/0.glb?a=1&b=2"},"boundingVolume":{"box":[0,0,0,5,0,0,0,5,0,0,0,5]},"geometricError":0,"refine":"ADD"}],` +
		`"boundingVolume":{"box":[0,0,0,10,0,0,0,10,0,0,0,10]},"geometricError":5,"refine":"ADD"` + transformField + `},` +
		`"properties":{"ID":{"minimum":0,"maximum":18446744073709551615}}}`
}

func writeTilesetFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tileset.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func mustOpenTileset(t *testing.T, path string) *tilesetFile {
	t.Helper()
	ts, err := openTileset(path)
	if err != nil {
		t.Fatalf("openTileset: %v", err)
	}
	return ts
}

func decodeJSONFile(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	if err := decoder.Decode(&doc); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return doc
}

func TestOpenTilesetAcceptsTheFileOrItsFolder(t *testing.T) {
	path := writeTilesetFile(t, sampleTileset(transformJSON(placedTransform)))
	for _, input := range []string{path, filepath.Dir(path)} {
		ts := mustOpenTileset(t, input)
		if ts.path != path {
			t.Errorf("openTileset(%q) opened %q, want %q", input, ts.path, path)
		}
	}
}

func TestOpenTilesetErrors(t *testing.T) {
	t.Run("missing file", func(t *testing.T) {
		_, err := openTileset(filepath.Join(t.TempDir(), "tileset.json"))
		if !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("error = %v, want fs.ErrNotExist", err)
		}
	})
	t.Run("folder without tileset.json", func(t *testing.T) {
		dir := t.TempDir()
		_, err := openTileset(dir)
		if err == nil || !strings.Contains(err.Error(), "contains no tileset.json") {
			t.Errorf("error = %v, want a missing tileset.json error", err)
		}
	})

	tests := []struct {
		name    string
		content string
		wantErr string
	}{
		{"not JSON", "not json", "is not a valid tileset"},
		{"not an object", "[1, 2]", "is not a valid tileset"},
		{"no root", `{"asset":{"version":"1.1"}}`, "it has no root tile"},
		{"root not an object", `{"root":[]}`, "invalid root tile"},
		{"transform too short", sampleTileset("[1,0,0,1]"), "the root transform is not an array of 16 numbers"},
		{"transform not numbers", sampleTileset(`["a","b","c","d","e","f","g","h","i","j","k","l","m","n","o","p"]`), "the root transform is not an array of 16 numbers"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := writeTilesetFile(t, tc.content)
			_, err := openTileset(path)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) || !strings.Contains(err.Error(), path) {
				t.Errorf("error = %v, want it to name the file and contain %q", err, tc.wantErr)
			}
		})
	}
}

func TestOpenTilesetReadsTheRootTransform(t *testing.T) {
	if got := mustOpenTileset(t, writeTilesetFile(t, sampleTileset(""))).rootTransform(); got != identityTransform {
		t.Errorf("rootTransform without a transform = %v, want the identity", got)
	}
	if got := mustOpenTileset(t, writeTilesetFile(t, sampleTileset(transformJSON(placedTransform)))).rootTransform(); got != placedTransform {
		t.Errorf("rootTransform = %v, want %v", got, placedTransform)
	}
}

// splatTileset is a tileset of Gaussian splats as gotiler writes it, with the
// given top-level extension fields.
func splatTileset(extensions string) string {
	return `{` + extensions + `,` + strings.TrimPrefix(sampleTileset(""), "{")
}

const splatExtensions = `"extensionsUsed":["3DTILES_content_gltf"],"extensionsRequired":["3DTILES_content_gltf"],` +
	`"extensions":{"3DTILES_content_gltf":{` +
	`"extensionsUsed":["KHR_gaussian_splatting","KHR_gaussian_splatting_compression_spz_2"],` +
	`"extensionsRequired":["KHR_gaussian_splatting","KHR_gaussian_splatting_compression_spz_2"]}}`

func TestOpenTilesetDetectsSplats(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{"splats as gotiler writes them", splatTileset(splatExtensions), true},
		{"points", sampleTileset(""), false},
		{"splats only used, drawn as points", splatTileset(`"extensions":{"3DTILES_content_gltf":{"extensionsUsed":["KHR_gaussian_splatting","KHR_gaussian_splatting_compression_spz_2"]}}`), false},
		{"uncompressed splats", splatTileset(`"extensions":{"3DTILES_content_gltf":{"extensionsRequired":["KHR_gaussian_splatting"]}}`), false},
		{"splat extensions at the top level", splatTileset(`"extensionsRequired":["KHR_gaussian_splatting","KHR_gaussian_splatting_compression_spz_2"]`), false},
		{"malformed extensions", splatTileset(`"extensions":"none"`), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := mustOpenTileset(t, writeTilesetFile(t, tc.content)).splats; got != tc.want {
				t.Errorf("splats = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestOpenTilesetMarksTilesetsThatCannotBeMoved(t *testing.T) {
	const region = `{"region":[0.1,0.2,0.3,0.4,0,10]}`
	const box = `{"box":[0,0,0,1,0,0,0,1,0,0,0,1]}`
	tests := []struct {
		name       string
		content    string
		wantReason string
	}{
		{"box volumes", sampleTileset(transformJSON(placedTransform)), ""},
		{"sphere volume", `{"root":{"boundingVolume":{"sphere":[0,0,0,10]},"geometricError":1}}`, ""},
		{"region on the root", `{"root":{"boundingVolume":` + region + `,"geometricError":1}}`, "region bounding volumes"},
		{"region on a child", `{"root":{"boundingVolume":` + box + `,"geometricError":1,"children":[{"boundingVolume":` + box + `,"children":[{"boundingVolume":` + region + `}]}]}}`, "region bounding volumes"},
		{"region on the content", `{"root":{"boundingVolume":` + box + `,"content":{"uri":"a.glb","boundingVolume":` + region + `}}}`, "region bounding volumes"},
		{"region on one of many contents", `{"root":{"boundingVolume":` + box + `,"contents":[{"uri":"a.glb"},{"uri":"b.glb","boundingVolume":` + region + `}]}}`, "region bounding volumes"},
		{"region as viewer request volume", `{"root":{"boundingVolume":` + box + `,"viewerRequestVolume":` + region + `}}`, "region bounding volumes"},
		{"singular transform", sampleTileset(transformJSON([16]float64{15: 1})), "its root transform cannot be inverted"},
		{"projective transform", sampleTileset(transformJSON([16]float64{0: 1, 5: 1, 10: 1, 3: 0.5, 15: 1})), "its root transform cannot be inverted"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ts := mustOpenTileset(t, writeTilesetFile(t, tc.content))
			if tc.wantReason == "" && ts.readOnlyReason != "" {
				t.Errorf("readOnlyReason = %q, want the tileset to be movable", ts.readOnlyReason)
			}
			if !strings.Contains(ts.readOnlyReason, tc.wantReason) {
				t.Errorf("readOnlyReason = %q, want it to contain %q", ts.readOnlyReason, tc.wantReason)
			}
		})
	}
}

func TestSaveRootTransformRewritesOnlyTheTransform(t *testing.T) {
	for name, original := range map[string]string{"with a transform": transformJSON(placedTransform), "without a transform": ""} {
		t.Run(name, func(t *testing.T) {
			path := writeTilesetFile(t, sampleTileset(original))
			before := decodeJSONFile(t, path)
			ts := mustOpenTileset(t, path)
			moved := columnMajor(rotationZ(30).Mul(linearPart(ts.rootTransform())), 4517600, 1096960, 4382040)

			if err := ts.saveRootTransform(moved); err != nil {
				t.Fatalf("saveRootTransform: %v", err)
			}

			after := decodeJSONFile(t, path)
			delete(before["root"].(map[string]any), "transform")
			delete(after["root"].(map[string]any), "transform")
			if !reflect.DeepEqual(after, before) {
				t.Errorf("saving changed more than the root transform\nbefore: %v\nafter:  %v", before, after)
			}
			data, _ := os.ReadFile(path)
			for _, literal := range []string{"18446744073709551615", "data/0.glb?a=1&b=2"} {
				if !strings.Contains(string(data), literal) {
					t.Errorf("saved file lost the literal %q: %s", literal, data)
				}
			}
			if got := mustOpenTileset(t, path).rootTransform(); got != moved {
				t.Errorf("reopened root transform = %v, want %v", got, moved)
			}
			if got := ts.rootTransform(); got != moved {
				t.Errorf("rootTransform after saving = %v, want %v", got, moved)
			}
		})
	}
}

func TestSaveRootTransformKeepsTheScaleOfTheCurrentTransform(t *testing.T) {
	scaledRoot := columnMajor(scaled(rotationZ(10), 2), 100, 200, 300)
	ts := mustOpenTileset(t, writeTilesetFile(t, sampleTileset(transformJSON(scaledRoot))))

	rotatedKeepingScale := columnMajor(rotationX(15).Mul(linearPart(scaledRoot)), 150, 250, 350)
	if err := ts.saveRootTransform(rotatedKeepingScale); err != nil {
		t.Errorf("a rotation of a scaled root transform was rejected: %v", err)
	}
	if err := ts.saveRootTransform(columnMajor(scaled(rotationZ(10), 3), 100, 200, 300)); err == nil {
		t.Error("changing the scale of the root transform was accepted")
	}
}

func TestSaveRootTransformRejectsChangesOtherThanMovesAndRotations(t *testing.T) {
	current := linearPart(placedTransform)
	shear := geom.Mat3{{1, 0.2, 0}, {0, 1, 0}, {0, 0, 1}}
	mirror := geom.Mat3{{-1, 0, 0}, {0, 1, 0}, {0, 0, 1}}
	notAffine := columnMajor(current, 1, 2, 3)
	notAffine[3] = 0.1
	withNaN := placedTransform
	withNaN[12] = math.NaN()
	tests := []struct {
		name      string
		transform [16]float64
		wantErr   string
	}{
		{"scale", columnMajor(scaled(current, 1.5), 1, 2, 3), "not scale, skew or mirror it"},
		{"shear", columnMajor(shear.Mul(current), 1, 2, 3), "not scale, skew or mirror it"},
		{"mirror", columnMajor(mirror.Mul(current), 1, 2, 3), "not scale, skew or mirror it"},
		{"projective row", notAffine, "end with the row 0 0 0 1"},
		{"not finite", withNaN, "must hold finite numbers"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			content := sampleTileset(transformJSON(placedTransform))
			path := writeTilesetFile(t, content)
			err := mustOpenTileset(t, path).saveRootTransform(tc.transform)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %v, want it to contain %q", err, tc.wantErr)
			}
			if data, _ := os.ReadFile(path); string(data) != content {
				t.Error("a rejected save changed the file")
			}
		})
	}
}

func TestSaveRootTransformRefusesTilesetsThatCannotBeMoved(t *testing.T) {
	content := `{"root":{"boundingVolume":{"region":[0.1,0.2,0.3,0.4,0,10]},"geometricError":1}}`
	path := writeTilesetFile(t, content)
	err := mustOpenTileset(t, path).saveRootTransform(identityTransform)
	if err == nil || !strings.Contains(err.Error(), "the tileset cannot be moved: it uses region bounding volumes") {
		t.Errorf("error = %v, want a read-only error", err)
	}
	if data, _ := os.ReadFile(path); string(data) != content {
		t.Error("the refused save changed the file")
	}
}

func TestSaveRootTransformSavesOverThePreviousSave(t *testing.T) {
	path := writeTilesetFile(t, sampleTileset(transformJSON(placedTransform)))
	ts := mustOpenTileset(t, path)
	for i, degrees := range []float64{10, 20} {
		moved := columnMajor(rotationZ(degrees).Mul(linearPart(placedTransform)), 4517590.9, 1096950.6, 4382030.8)
		if err := ts.saveRootTransform(moved); err != nil {
			t.Fatalf("save %d: %v", i+1, err)
		}
		if got := mustOpenTileset(t, path).rootTransform(); got != moved {
			t.Errorf("after save %d the file holds the transform %v, want %v", i+1, got, moved)
		}
	}
}

func TestSaveRootTransformLeavesOnlyTheTileset(t *testing.T) {
	path := writeTilesetFile(t, sampleTileset(transformJSON(placedTransform)))
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	moved := columnMajor(rotationZ(5).Mul(linearPart(placedTransform)), 1, 2, 3)
	if err := mustOpenTileset(t, path).saveRootTransform(moved); err != nil {
		t.Fatalf("saveRootTransform: %v", err)
	}

	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	slices.Sort(names)
	// No backup and no temporary file stay behind.
	if want := []string{"tileset.json"}; !reflect.DeepEqual(names, want) {
		t.Errorf("folder holds %v, want %v", names, want)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if after.Mode().Perm() != before.Mode().Perm() {
		t.Errorf("file mode changed from %v to %v", before.Mode().Perm(), after.Mode().Perm())
	}
}

func TestFailedSaveKeepsTheCurrentTransform(t *testing.T) {
	path := writeTilesetFile(t, sampleTileset(transformJSON(placedTransform)))
	ts := mustOpenTileset(t, path)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	moved := columnMajor(rotationZ(5).Mul(linearPart(placedTransform)), 1, 2, 3)
	if err := ts.saveRootTransform(moved); err == nil {
		t.Fatal("saving a deleted tileset succeeded")
	}
	if got := ts.rootTransform(); got != placedTransform {
		t.Errorf("rootTransform after a failed save = %v, want the unchanged %v", got, placedTransform)
	}
}
