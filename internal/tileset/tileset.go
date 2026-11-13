// Package tileset writes the tileset.json of a tile tree, for both the point
// cloud and the Gaussian splat tilers. A large tree is split into external
// tilesets, so viewers read only the coarse top of the tree before showing
// it and load the rest as the camera gets close.
package tileset

import (
	"encoding/json"
	"fmt"
	"io"
	"path"
	"strings"
)

// RootFilename is the name of the tileset's entry file.
const RootFilename = "tileset.json"

// DefaultMaxTiles is the default of Options.MaxTiles for the tilers: a file
// of about 2,000 tiles takes about 500 KB, read in moments, while a tileset
// below it keeps a single tileset.json.
const DefaultMaxTiles = 2000

// Tile is one tile of a tileset.
type Tile struct {
	// Box is the 3D Tiles box bounding the tile: its center, then the
	// vectors from the center to the middle of its faces.
	Box            [12]float64
	GeometricError float64
	// Refine is "ADD" or "REPLACE".
	Refine string
	// Transform is the tile's transform, a 4x4 matrix in column-major order,
	// or nil for none.
	Transform *[16]float64
	// Content is the URI of the tile's content, relative to the tileset root,
	// as in "data/0d.glb", or empty for a tile without content.
	Content  string
	Children []*Tile
}

// Header holds what every file of a tileset declares.
type Header struct {
	// Version is the 3D Tiles version, as in "1.1".
	Version            string
	ExtensionsUsed     []string
	ExtensionsRequired []string
	Extensions         map[string]any
}

// RootOnly holds what only the root tileset.json carries, as viewers read it
// from there: dataset-wide properties, metadata schema and metadata. A nil
// field is left out.
type RootOnly struct {
	Properties any
	Schema     any
	Metadata   any
}

// Options configure Write.
type Options struct {
	// MaxTiles is the most tiles a file holds before the subtrees under its
	// top levels move into external tilesets. 0 keeps every tile in
	// tileset.json.
	MaxTiles int
}

// Result describes a written tileset.
type Result struct {
	// Files is how many tileset JSON files were written, tileset.json
	// included.
	Files int
}

// Create opens a file of the tileset for writing, given its path relative to
// the tileset root, such as "tileset.json" or "data/0d.json".
type Create func(name string) (io.WriteCloser, error)

type fileJSON struct {
	Asset              assetJSON      `json:"asset"`
	ExtensionsUsed     []string       `json:"extensionsUsed,omitempty"`
	ExtensionsRequired []string       `json:"extensionsRequired,omitempty"`
	Extensions         map[string]any `json:"extensions,omitempty"`
	Properties         any            `json:"properties,omitempty"`
	Schema             any            `json:"schema,omitempty"`
	Metadata           any            `json:"metadata,omitempty"`
	GeometricError     float64        `json:"geometricError"`
	Root               *tileJSON      `json:"root"`
}

type assetJSON struct {
	Version string `json:"version"`
}

type tileJSON struct {
	BoundingVolume boundingVolumeJSON `json:"boundingVolume"`
	GeometricError float64            `json:"geometricError"`
	Refine         string             `json:"refine,omitempty"`
	Transform      *[16]float64       `json:"transform,omitempty"`
	Content        *contentJSON       `json:"content,omitempty"`
	Children       []*tileJSON        `json:"children,omitempty"`
}

type boundingVolumeJSON struct {
	Box [12]float64 `json:"box"`
}

type contentJSON struct {
	URI string `json:"uri"`
}

// Write writes the tileset of root through create: tileset.json and, when
// the tree holds more than opts.MaxTiles tiles, external tilesets.
//
// A subtree that fits in opts.MaxTiles tiles, and is not too small to be
// worth a request of its own, goes into an external tileset next to the
// content of its root tile: the subtree under "data/0d.glb" goes into
// "data/0d.json". The file it hangs from keeps a tile in its place, with the
// same bounding volume and geometric error, whose content is the external
// tileset. Larger subtrees stay in the file as its top levels, so the root
// file holds the coarse levels of the whole tree. Each file holds at most
// about opts.MaxTiles tiles.
//
// The external tilesets are written first and tileset.json last, as an
// archive may finalize itself when tileset.json is closed.
func Write(create Create, root *Tile, header Header, rootOnly RootOnly, opts Options) (Result, error) {
	if root == nil {
		return Result{}, fmt.Errorf("the tileset has no root tile")
	}
	if opts.MaxTiles < 0 {
		return Result{}, fmt.Errorf("the most tiles per tileset file must be 0 or more, not %d", opts.MaxTiles)
	}
	w := &writer{create: create, header: header, maxTiles: opts.MaxTiles, sizes: map[*Tile]int{}, names: map[string]bool{}}
	w.size(root)
	w.names[RootFilename] = true
	if err := w.writeFile(RootFilename, root, rootOnly); err != nil {
		return Result{}, err
	}
	return Result{Files: w.files}, nil
}

type writer struct {
	create   Create
	header   Header
	maxTiles int
	// sizes holds the number of tiles of each subtree.
	sizes map[*Tile]int
	// names holds the file names taken, to keep external tilesets apart.
	names map[string]bool
	files int
}

func (w *writer) size(t *Tile) int {
	n := 1
	for _, c := range t.Children {
		n += w.size(c)
	}
	w.sizes[t] = n
	return n
}

// writeFile writes the file name holding the subtree of root, after the
// external tilesets it refers to.
func (w *writer) writeFile(name string, root *Tile, rootOnly RootOnly) error {
	dir := path.Dir(name)
	rootJSON, externals := w.layout(root, dir)
	for _, e := range externals {
		if err := w.writeFile(e.name, e.root, RootOnly{}); err != nil {
			return err
		}
	}
	file := fileJSON{
		Asset:              assetJSON{Version: w.header.Version},
		ExtensionsUsed:     w.header.ExtensionsUsed,
		ExtensionsRequired: w.header.ExtensionsRequired,
		Extensions:         w.header.Extensions,
		Properties:         rootOnly.Properties,
		Schema:             rootOnly.Schema,
		Metadata:           rootOnly.Metadata,
		GeometricError:     root.GeometricError,
		Root:               rootJSON,
	}
	data, err := json.Marshal(file)
	if err != nil {
		return fmt.Errorf("could not encode %s: %w", name, err)
	}
	out, err := w.create(name)
	if err != nil {
		return err
	}
	if _, err := out.Write(data); err != nil {
		_ = out.Close()
		return fmt.Errorf("could not write %s: %w", name, err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("could not write %s: %w", name, err)
	}
	w.files++
	return nil
}

type external struct {
	name string
	root *Tile
}

// layout returns the tiles of the file holding the subtree of root, in the
// folder dir, and the subtrees moved into external tilesets.
func (w *writer) layout(root *Tile, dir string) (*tileJSON, []external) {
	rootJSON := w.tileJSON(root, dir)
	if w.maxTiles == 0 || w.sizes[root] <= w.maxTiles {
		w.addSubtree(rootJSON, root, dir)
		return rootJSON, nil
	}
	// Subtrees this small stay in the file, while it has room: a file of
	// their own would cost a request for few tiles.
	small := w.maxTiles / 8
	type item struct {
		tile *Tile
		json *tileJSON
	}
	var externals []external
	queue := []item{{root, rootJSON}}
	tiles := 1
	for len(queue) > 0 {
		parent := queue[0]
		queue = queue[1:]
		for _, child := range parent.tile.Children {
			size := w.sizes[child]
			switch {
			case size <= small && tiles+size <= w.maxTiles:
				childJSON := w.tileJSON(child, dir)
				w.addSubtree(childJSON, child, dir)
				parent.json.Children = append(parent.json.Children, childJSON)
				tiles += size
			case size <= w.maxTiles || tiles+len(queue) >= w.maxTiles:
				// The tile in its place takes no transform: the external
				// tileset's root keeps it.
				name := w.externalName(child)
				externals = append(externals, external{name, child})
				standIn := w.tileJSON(child, dir)
				standIn.Transform = nil
				standIn.Content = &contentJSON{URI: relative(dir, name)}
				parent.json.Children = append(parent.json.Children, standIn)
				tiles++
			default:
				childJSON := w.tileJSON(child, dir)
				parent.json.Children = append(parent.json.Children, childJSON)
				queue = append(queue, item{child, childJSON})
				tiles++
			}
		}
	}
	return rootJSON, externals
}

// addSubtree adds the descendants of t to its JSON tile.
func (w *writer) addSubtree(j *tileJSON, t *Tile, dir string) {
	for _, child := range t.Children {
		childJSON := w.tileJSON(child, dir)
		w.addSubtree(childJSON, child, dir)
		j.Children = append(j.Children, childJSON)
	}
}

// tileJSON returns the JSON tile of t, without children, in a file in the
// folder dir.
func (w *writer) tileJSON(t *Tile, dir string) *tileJSON {
	j := &tileJSON{
		BoundingVolume: boundingVolumeJSON{Box: t.Box},
		GeometricError: t.GeometricError,
		Refine:         t.Refine,
		Transform:      t.Transform,
	}
	if t.Content != "" {
		j.Content = &contentJSON{URI: relative(dir, t.Content)}
	}
	return j
}

// externalName returns the file name of the external tileset holding the
// subtree of t: the name of its content with a .json extension, next to it.
func (w *writer) externalName(t *Tile) string {
	base := "tileset"
	if t.Content != "" {
		base = strings.TrimSuffix(t.Content, path.Ext(t.Content))
	}
	name := base + ".json"
	for i := 2; w.names[name]; i++ {
		name = fmt.Sprintf("%s_%d.json", base, i)
	}
	w.names[name] = true
	return name
}

// relative returns the URI of target, a path relative to the tileset root,
// as seen from a file in the folder dir.
func relative(dir, target string) string {
	if dir == "." || dir == "" || strings.Contains(target, "://") || strings.HasPrefix(target, "/") {
		return target
	}
	from := strings.Split(dir, "/")
	to := strings.Split(target, "/")
	common := 0
	for common < len(from) && common < len(to)-1 && from[common] == to[common] {
		common++
	}
	parts := make([]string, 0, len(from)-common+len(to)-common)
	for range from[common:] {
		parts = append(parts, "..")
	}
	parts = append(parts, to[common:]...)
	return strings.Join(parts, "/")
}
