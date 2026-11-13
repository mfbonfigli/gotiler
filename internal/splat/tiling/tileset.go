package tiling

import (
	"io"
	"os"
	"path"
	"path/filepath"

	"github.com/mfbonfigli/gotiler/v3/internal/geom"
	"github.com/mfbonfigli/gotiler/v3/internal/splat/glb"
	"github.com/mfbonfigli/gotiler/v3/internal/tileset"
)

// contentGLTF is the 3D Tiles extension for glTF content, which declares the
// glTF extensions the content needs. CesiumJS renders splats only when it
// lists the splat extensions as required.
const contentGLTF = "3DTILES_content_gltf"

// contentGLTFExtension is the 3DTILES_content_gltf object of a tileset.
type contentGLTFExtension struct {
	ExtensionsUsed     []string `json:"extensionsUsed"`
	ExtensionsRequired []string `json:"extensionsRequired"`
}

// writeTileset writes the tileset.json of the tiles under root to dir, with
// the given root transform, in column-major order, if not nil. A tileset of
// more than maxTiles tiles moves subtrees into external tilesets in the data
// folder, unless maxTiles is 0. Every file declares the splat extensions,
// though CesiumJS reads them from tileset.json only.
//
// Tiles refine by replacement: the merged splats of a tile stand for the
// splats of its children, and drawing both would double them.
func writeTileset(dir string, root *tile, transform *[16]float64, maxTiles int) error {
	splatExtensions := []string{glb.ExtensionGaussianSplatting, glb.ExtensionSPZ}
	header := tileset.Header{
		Version:            "1.1",
		ExtensionsUsed:     []string{contentGLTF},
		ExtensionsRequired: []string{contentGLTF},
		Extensions:         map[string]any{contentGLTF: contentGLTFExtension{ExtensionsUsed: splatExtensions, ExtensionsRequired: splatExtensions}},
	}
	rootTile := toTile(root)
	rootTile.Transform = transform
	create := func(name string) (io.WriteCloser, error) {
		return os.Create(filepath.Join(dir, filepath.FromSlash(name)))
	}
	_, err := tileset.Write(create, rootTile, header, tileset.RootOnly{}, tileset.Options{MaxTiles: maxTiles})
	return err
}

func toTile(t *tile) *tileset.Tile {
	out := &tileset.Tile{
		Box:            box(t.bounds),
		GeometricError: t.geometricError,
		Refine:         "REPLACE",
		Content:        path.Join(dataDirName, contentName(t.id)),
	}
	for _, child := range t.children {
		out.Children = append(out.Children, toTile(child))
	}
	return out
}

// box returns the 3D Tiles box of bounds: its center, then the vectors from
// the center to the middle of its faces along X, Y and Z.
func box(b geom.Bounds) [12]float64 {
	c, half := b.Center(), b.Size().MulScalar(0.5)
	return [12]float64{c.X, c.Y, c.Z, half.X, 0, 0, 0, half.Y, 0, 0, 0, half.Z}
}
