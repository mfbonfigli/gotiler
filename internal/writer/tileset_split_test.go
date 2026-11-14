package writer

import (
	"bytes"
	"encoding/json"
	"io"
	"path"
	"strings"
	"testing"

	"github.com/mfbonfigli/gotiler/v3/internal/testtree"
	"github.com/mfbonfigli/gotiler/v3/tiler/geom"
	"github.com/mfbonfigli/gotiler/v3/tiler/plugin"
)

// mockBinaryTree returns a full binary tree of mock nodes of the given depth,
// each holding one point.
func mockBinaryTree(depth int) *testtree.MockNode {
	n := &testtree.MockNode{
		Pts:         geom.NewLinkedPointStream(&geom.LinkedPoint{Pt: geom.NewPoint(1, 2, 3, 1, 2, 3)}, 1),
		TotalNumPts: 1,
		Bounds:      geom.NewBoundingBox(0, 1, 0, 1, 0, 1),
		GeomError:   float64(depth),
	}
	if depth > 0 {
		n.ChildNodes[0] = mockBinaryTree(depth - 1)
		n.ChildNodes[1] = mockBinaryTree(depth - 1)
	}
	return n
}

// writeTilesetFiles runs writeTileset with the given most tiles per file and
// returns the files written, by name, and the order they were written in.
func writeTilesetFiles(t *testing.T, root *testtree.MockNode, maxTiles int) (map[string][]byte, []string) {
	t.Helper()
	files := map[string][]byte{}
	var order []string
	w, err := NewWriter("", WithEncoder(plugin.EncoderGLB), WithMaxTilesPerTileset(maxTiles), WithWriterProvider(func(name string) (io.WriteCloser, error) {
		return &captureWriteCloser{Buffer: &bytes.Buffer{}, closeFn: func(b []byte) {
			files[name] = b
			order = append(order, name)
		}}, nil
	}))
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	if err := w.writeTileset(root, w.writerProvider); err != nil {
		t.Fatalf("writeTileset: %v", err)
	}
	return files, order
}

func TestWriterSplitsLargeTilesetsIntoExternalTilesets(t *testing.T) {
	root := mockBinaryTree(6) // 127 tiles
	root.Root = true
	root.Summaries = testSummaries()
	files, order := writeTilesetFiles(t, root, 20)

	if len(files) < 2 || order[len(order)-1] != "tileset.json" {
		t.Fatalf("files written in the order %v", order)
	}
	contents := map[string]int{}
	for name, data := range files {
		var doc struct {
			Asset    map[string]string `json:"asset"`
			Metadata any               `json:"metadata"`
			Schema   any               `json:"schema"`
			Root     json.RawMessage   `json:"root"`
		}
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if doc.Asset["version"] != "1.1" {
			t.Errorf("%s has version %q", name, doc.Asset["version"])
		}
		isRoot := name == "tileset.json"
		if (doc.Metadata != nil) != isRoot || (doc.Schema != nil) != isRoot {
			t.Errorf("%s: metadata %v, schema %v", name, doc.Metadata, doc.Schema)
		}
		if !isRoot && (!strings.HasPrefix(name, "data/") || !strings.HasSuffix(name, "d.json")) {
			t.Errorf("external tileset %s is not named after its root content", name)
		}
		// every tile content, resolved from the file's folder
		for _, uri := range contentURIs(doc.Root) {
			if !strings.HasSuffix(uri, ".json") {
				contents[path.Join(path.Dir(name), uri)]++
			}
		}
	}
	if len(contents) != 127 {
		t.Errorf("the tileset holds %d contents, want 127", len(contents))
	}
	for content, n := range contents {
		if n != 1 || !strings.HasPrefix(content, "data/") {
			t.Errorf("%s appears %d times", content, n)
		}
	}
}

func TestWriterKeepsOneTilesetFileByDefault(t *testing.T) {
	root := mockBinaryTree(6)
	root.Root = true
	files, _ := writeTilesetFiles(t, root, 0)
	if len(files) != 1 || files["tileset.json"] == nil {
		t.Errorf("files written: %d", len(files))
	}
}

// contentURIs returns the content URIs of a JSON tile and its descendants.
func contentURIs(raw json.RawMessage) []string {
	var tile struct {
		Content *struct {
			URI string `json:"uri"`
		} `json:"content"`
		Children []json.RawMessage `json:"children"`
	}
	if json.Unmarshal(raw, &tile) != nil {
		return nil
	}
	var uris []string
	if tile.Content != nil {
		uris = append(uris, tile.Content.URI)
	}
	for _, child := range tile.Children {
		uris = append(uris, contentURIs(child)...)
	}
	return uris
}
