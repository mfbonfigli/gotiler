package tiling

import (
	"encoding/json"
	"os"
	"path"
	"path/filepath"

	"github.com/mfbonfigli/gotiler/v3/internal/geom"
	"github.com/mfbonfigli/gotiler/v3/internal/splat/glb"
)

// contentGLTF is the 3D Tiles extension for glTF content, which declares the
// glTF extensions the content needs. CesiumJS renders splats only when it
// lists the splat extensions as required.
const contentGLTF = "3DTILES_content_gltf"

type tilesetJSON struct {
	Asset              assetJSON           `json:"asset"`
	ExtensionsUsed     []string            `json:"extensionsUsed"`
	ExtensionsRequired []string            `json:"extensionsRequired"`
	Extensions         map[string]gltfJSON `json:"extensions"`
	GeometricError     float64             `json:"geometricError"`
	Root               tileJSON            `json:"root"`
}

type assetJSON struct {
	Version string `json:"version"`
}

type gltfJSON struct {
	ExtensionsUsed     []string `json:"extensionsUsed"`
	ExtensionsRequired []string `json:"extensionsRequired"`
}

type tileJSON struct {
	BoundingVolume boundingVolumeJSON `json:"boundingVolume"`
	GeometricError float64            `json:"geometricError"`
	Refine         string             `json:"refine"`
	Transform      *[16]float64       `json:"transform,omitempty"`
	Content        contentJSON        `json:"content"`
	Children       []tileJSON         `json:"children,omitempty"`
}

type boundingVolumeJSON struct {
	Box [12]float64 `json:"box"`
}

type contentJSON struct {
	URI string `json:"uri"`
}

// writeTileset writes the tileset.json of the tiles under root to dir, with
// the given root transform, in column-major order, if not nil.
//
// Tiles refine by replacement: the merged splats of a tile stand for the
// splats of its children, and drawing both would double them.
func writeTileset(dir string, root *tile, transform *[16]float64) error {
	splatExtensions := []string{glb.ExtensionGaussianSplatting, glb.ExtensionSPZ}
	tileset := tilesetJSON{
		Asset:              assetJSON{Version: "1.1"},
		ExtensionsUsed:     []string{contentGLTF},
		ExtensionsRequired: []string{contentGLTF},
		Extensions:         map[string]gltfJSON{contentGLTF: {ExtensionsUsed: splatExtensions, ExtensionsRequired: splatExtensions}},
		GeometricError:     root.geometricError,
		Root:               tileToJSON(root),
	}
	tileset.Root.Transform = transform
	data, err := json.Marshal(tileset)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "tileset.json"), data, 0o644)
}

func tileToJSON(t *tile) tileJSON {
	out := tileJSON{
		BoundingVolume: boundingVolumeJSON{Box: box(t.bounds)},
		GeometricError: t.geometricError,
		Refine:         "REPLACE",
		Content:        contentJSON{URI: path.Join(dataDirName, contentName(t.id))},
	}
	for _, child := range t.children {
		out.Children = append(out.Children, tileToJSON(child))
	}
	return out
}

// box returns the 3D Tiles box of bounds: its center, then the vectors from
// the center to the middle of its faces along X, Y and Z.
func box(b geom.Bounds) [12]float64 {
	c, half := b.Center(), b.Size().MulScalar(0.5)
	return [12]float64{c.X, c.Y, c.Z, half.X, 0, 0, 0, half.Y, 0, 0, 0, half.Z}
}
