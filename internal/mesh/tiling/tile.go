// Package tiling turns textured triangle meshes into 3D Tiles tilesets.
package tiling

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/mfbonfigli/gotiler/v3/internal/geom"
	"github.com/mfbonfigli/gotiler/v3/internal/mesh"
	"github.com/mfbonfigli/gotiler/v3/internal/mesh/glb"
	"github.com/mfbonfigli/gotiler/v3/internal/mesh/obj"
	"github.com/mfbonfigli/gotiler/v3/internal/mesh/texture"
	"github.com/mfbonfigli/gotiler/v3/internal/tileset"
	"github.com/mfbonfigli/gotiler/v3/tiler/tree"
)

const (
	// dataDirName is the folder of the tile contents, next to tileset.json.
	dataDirName = "data"
	// sampleCount is how many triangles the partition is planned from.
	sampleCount = 1 << 20
	// textureWorkers bounds how many input textures are decoded at a time:
	// each takes four bytes of memory a pixel while it is.
	textureWorkers = 4
	// atlasOverhead is how many pixels of a texture it takes, about, to hold
	// a pixel of the input textures that a triangle covers: charts are
	// packed as rectangles, with padding around them.
	atlasOverhead = 2
	// splitReportEvery is how many triangles pass between progress reports
	// of splitting.
	splitReportEvery = 100_000
)

// Options configure Tile. Start from DefaultOptions.
type Options struct {
	// TrianglesPerTile is how many triangles a tile holds, about, at most:
	// leaves are planned from samples, and cutting the surface along their
	// borders adds triangles. Leaves hold fewer where the textures of that
	// many would not fit MaxTextureSize, and tiles above them up to twice as
	// many where the shape of the surface needs them.
	TrianglesPerTile int
	// MaxTextureSize is the largest side of the texture of a tile, in
	// pixels. Tiles whose textures would be larger get them scaled down.
	MaxTextureSize int
	// TextureQuality is the JPEG quality of the textures, from 1 to 100.
	TextureQuality int
	// Compress compresses the vertices of the tiles with
	// EXT_meshopt_compression.
	Compress bool
	// UpAxis names the input axis that points up: x, y or z, each optionally
	// signed, as in "-y". Empty means z.
	UpAxis string
	// GECorrection scales the geometric errors of the tiles: higher values
	// make viewers switch to finer tiles sooner.
	GECorrection float64
	// RootTransform is the transform of the root tile, a 4x4 matrix in
	// column-major order, as 3D Tiles stores it, or nil for none. It places
	// the tileset, whose tiles hold the mesh relative to Result.Origin.
	RootTransform *[16]float64
	// MaxTilesPerTileset is the most tiles a tileset JSON file holds: larger
	// tilesets move subtrees into external tilesets in the data folder. 0
	// writes a single tileset.json.
	MaxTilesPerTileset int
	// Workers is how many tiles are processed in parallel.
	Workers int
	// Progress receives the progress of the phases listed in Phases. It can
	// be nil.
	Progress tree.ProgressReporter
}

// DefaultOptions returns the default options: 50,000 triangles per tile,
// textures of up to 2048 pixels at JPEG quality 85, compressed vertices, Z
// up, no root transform, 2,000 tiles per tileset file and one worker per
// CPU.
func DefaultOptions() Options {
	return Options{
		TrianglesPerTile:   50_000,
		MaxTextureSize:     2048,
		TextureQuality:     85,
		Compress:           true,
		GECorrection:       1,
		MaxTilesPerTileset: tileset.DefaultMaxTiles,
		Workers:            runtime.NumCPU(),
	}
}

// Validate reports the first option out of range.
func (o Options) Validate() error {
	switch {
	case o.TrianglesPerTile < 1:
		return fmt.Errorf("the triangles per tile must be at least 1, not %d", o.TrianglesPerTile)
	case o.MaxTextureSize < 64:
		return fmt.Errorf("the largest texture size must be at least 64, not %d", o.MaxTextureSize)
	case o.TextureQuality < 1 || o.TextureQuality > 100:
		return fmt.Errorf("the texture quality must be from 1 to 100, not %d", o.TextureQuality)
	case !(o.GECorrection > 0) || math.IsInf(o.GECorrection, 0):
		return fmt.Errorf("the geometric error correction must be positive, not %v", o.GECorrection)
	case o.MaxTilesPerTileset < 0:
		return fmt.Errorf("the most tiles per tileset file must be 0 or more, not %d", o.MaxTilesPerTileset)
	case o.Workers < 1:
		return fmt.Errorf("the workers must be at least 1, not %d", o.Workers)
	}
	_, err := upAxis(o.UpAxis)
	return err
}

// Result describes a tileset that Tile wrote.
type Result struct {
	// Read is how many triangles the input holds, and Triangles how many
	// the tiles hold together, over all levels of detail.
	Read, Triangles int64
	// Tiles is the number of tiles, and Depth the number of levels below the
	// root.
	Tiles, Depth int
	// Origin is the point, in the input frame turned up, that the tiles hold
	// the mesh relative to.
	Origin geom.Vec3
	// Bounds are the bounds of the mesh, relative to Origin.
	Bounds geom.Bounds
	// GeometricError is the geometric error of the root tile.
	GeometricError float64
	// MissingTextures lists the textures that could not be read: their
	// triangles show the plain color of their material.
	MissingTextures []string
}

// Tile writes the textured mesh of the OBJ file at source as a 3D Tiles
// tileset to dir: tileset.json and the tile contents in its data folder,
// glTF files with a texture each.
//
// It cuts the surface along the planes of a binary partition of the space,
// planned from a sample of the triangles as for point clouds, so that each
// leaf tile holds one region of the mesh as it is, with the parts of the
// input textures it uses. Each tile above holds the surface of its two
// children with half the triangles, less the parts too small to show, and a
// texture of half the pixels, drawn anew from theirs. Tiles keep the
// positions on the planes that bound them, so that neighbors meet whatever
// their level of detail.
//
// It works on temporary files in dir, so that neither the mesh nor its
// textures need to fit in memory, and removes them before returning.
func Tile(ctx context.Context, source, dir string, opts Options) (result Result, err error) {
	if err := opts.Validate(); err != nil {
		return Result{}, err
	}
	turn, _ := upAxis(opts.UpAxis)
	dataDir := filepath.Join(dir, dataDirName)
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return Result{}, err
	}
	tempDir, err := os.MkdirTemp(dir, ".gotiler-mesh-")
	if err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(tempDir) //nolint:errcheck

	var size int64
	if stat, err := os.Stat(source); err == nil {
		size = stat.Size()
	}
	reading := newPhaseReporter(opts.Progress, Phases[0].Name, size, 1)
	reading.milestone("reading the mesh")
	in, err := readInput(ctx, source, tempDir, turn, reading)
	if err != nil {
		return Result{}, err
	}
	reading.milestone(fmt.Sprintf("read %d triangles", in.info.Triangles))
	if in.info.Triangles == 0 {
		return Result{}, errors.New("the mesh has no triangles")
	}

	textures := newPhaseReporter(opts.Progress, Phases[1].Name, int64(len(in.info.Materials)), 1)
	textures.milestone("reading the textures")
	sources, missing, err := openTextures(ctx, in.info.Materials, in.used, tempDir, min(opts.Workers, textureWorkers), textures)
	defer func() {
		for _, s := range sources {
			if closer, ok := s.(io.Closer); ok {
				err = errors.Join(err, closer.Close())
			}
		}
	}()
	if err != nil {
		return Result{}, err
	}
	textures.milestone(fmt.Sprintf("read %d textures", len(sources)-len(missing)))

	splitting := newPhaseReporter(opts.Progress, Phases[2].Name, in.info.Triangles, splitReportEvery)
	splitting.milestone("splitting the mesh into tiles")
	// A leaf holds its part of the input textures as they are: it is made
	// small enough for that part to fit the texture of a tile, with the
	// room that packing it takes.
	for i := range in.samples {
		width, height := sources[in.sampleMaterials[i]].Size()
		in.samples[i].pixels *= float32(width) * float32(height)
	}
	perLeaf := budget{
		triangles: opts.TrianglesPerTile,
		pixels:    float64(opts.MaxTextureSize) * float64(opts.MaxTextureSize) / atlasOverhead,
	}
	plan := buildPlan(in.samples, int(in.info.Triangles), perLeaf)
	if err := route(ctx, in.path, plan, tempDir, opts.Workers, splitting); err != nil {
		return Result{}, err
	}
	if err := os.Remove(in.path); err != nil {
		return Result{}, err
	}
	splitting.milestone("split the mesh")

	// Leaves store their positions on one grid, fine enough for the largest:
	// the positions that neighbors share then come out identical.
	var leafSize float64
	for _, leaf := range plan.leaves(nil) {
		if leaf.count > 0 {
			leafSize = max(leafSize, leaf.size())
		}
	}
	count := countTiles(plan)
	building := newPhaseReporter(opts.Progress, Phases[3].Name, int64(count), 1)
	building.milestone(fmt.Sprintf("building %d tiles", count))
	b := &builder{
		tempDir:        tempDir,
		dataDir:        dataDir,
		sources:        sources,
		maxTextureSize: opts.MaxTextureSize,
		maxTriangles:   maxOverTarget * opts.TrianglesPerTile,
		geFactor:       opts.GECorrection,
		content:        glb.Options{Quality: opts.TextureQuality, Compress: opts.Compress, StepExponent: glb.StepExponent(leafSize)},
		progress:       building,
	}
	root, err := b.build(ctx, plan, opts.Workers)
	if err != nil {
		return Result{}, err
	}
	if err := writeTileset(dir, root, opts.RootTransform, opts.MaxTilesPerTileset); err != nil {
		return Result{}, err
	}
	building.milestone("wrote tileset.json")
	origin := turn(in.info.Origin)
	return Result{
		Read:            in.info.Triangles,
		Triangles:       b.triangles.Load(),
		Tiles:           int(b.tiles.Load()),
		Depth:           depth(root),
		Origin:          geom.Vec3{X: origin[0], Y: origin[1], Z: origin[2]},
		Bounds:          root.bounds,
		GeometricError:  root.geometricError,
		MissingTextures: missing,
	}, nil
}

// input is the mesh read into a store of triangles, in the Z-up frame.
type input struct {
	path string
	info obj.Info
	// samples holds a sample of the triangles, with their surface in
	// texture coordinates as pixels, and sampleMaterials their materials.
	samples         []sample
	sampleMaterials []uint32
	// used tells, for each material, whether a triangle refers to it.
	used []bool
}

// readInput reads the OBJ file at source into a store of triangles in dir,
// turned to the Z-up frame, and samples their centers.
func readInput(ctx context.Context, source, dir string, turn func([3]float64) [3]float64, progress *phaseReporter) (*input, error) {
	in := &input{path: filepath.Join(dir, "input.bin")}
	store, err := createStore(in.path)
	if err != nil {
		return nil, err
	}
	// A fixed seed: the same input gives the same tileset.
	random := rand.New(rand.NewPCG(1, 2))
	seen := 0
	var last int64
	in.info, err = obj.Read(source, obj.Options{
		TempDir: dir,
		Progress: func(read int64) {
			progress.add(int(read - last))
			last = read
		},
	}, func(t *mesh.Triangle) error {
		if seen%4096 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		for c := range 3 {
			p := turn([3]float64{float64(t.P[c][0]), float64(t.P[c][1]), float64(t.P[c][2])})
			t.P[c] = [3]float32{float32(p[0]), float32(p[1]), float32(p[2])}
		}
		for int(t.Material) >= len(in.used) {
			in.used = append(in.used, false)
		}
		in.used[t.Material] = true
		// Keep each triangle with the same chance: the first sampleCount,
		// then each new one in place of a random earlier one.
		seen++
		ux, uy := float64(t.UV[1][0])-float64(t.UV[0][0]), float64(t.UV[1][1])-float64(t.UV[0][1])
		vx, vy := float64(t.UV[2][0])-float64(t.UV[0][0]), float64(t.UV[2][1])-float64(t.UV[0][1])
		s := sample{center: center(t), pixels: float32(math.Abs(ux*vy-uy*vx) / 2)}
		if len(in.samples) < sampleCount {
			in.samples, in.sampleMaterials = append(in.samples, s), append(in.sampleMaterials, t.Material)
		} else if k := random.IntN(seen); k < sampleCount {
			in.samples[k], in.sampleMaterials[k] = s, t.Material
		}
		return store.write(t)
	})
	if err := errors.Join(err, store.close()); err != nil {
		return nil, err
	}
	progress.setTotal(in.info.Size)
	return in, nil
}

// openTextures converts the textures of the materials that are used to raw
// image files in dir, workers at a time, and returns a source for each
// material, with the paths of the textures that could not be read: their
// materials get a source of their plain color.
func openTextures(ctx context.Context, materials []mesh.Material, used []bool, dir string, workers int, progress *phaseReporter) ([]texture.Source, []string, error) {
	sources := make([]texture.Source, len(materials))
	failed := make([]bool, len(materials))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range max(1, workers) {
		wg.Go(func() {
			for i := range jobs {
				m := materials[i]
				sources[i] = texture.Solid(m.Color)
				if m.Texture != "" && i < len(used) && used[i] && ctx.Err() == nil {
					raw := filepath.Join(dir, fmt.Sprintf("texture_%d.img", i))
					if err := texture.Convert(m.Texture, raw); err != nil {
						failed[i] = true
					} else if file, err := texture.Open(raw); err != nil {
						failed[i] = true
					} else {
						sources[i] = file
					}
				}
				progress.add(1)
			}
		})
	}
	for i := range materials {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	var missing []string
	for i, f := range failed {
		if f {
			missing = append(missing, materials[i].Texture)
		}
	}
	return sources, missing, ctx.Err()
}

// upAxis returns the function that turns a point of a frame whose given
// axis points up into the Z-up frame of the tiles. The axis is x, y or z,
// optionally signed; empty means z.
func upAxis(name string) (func([3]float64) [3]float64, error) {
	switch strings.ToLower(strings.TrimPrefix(name, "+")) {
	case "", "z":
		return func(p [3]float64) [3]float64 { return p }, nil
	case "-z":
		return func(p [3]float64) [3]float64 { return [3]float64{p[0], -p[1], -p[2]} }, nil
	case "y":
		return func(p [3]float64) [3]float64 { return [3]float64{p[0], -p[2], p[1]} }, nil
	case "-y":
		return func(p [3]float64) [3]float64 { return [3]float64{p[0], p[2], -p[1]} }, nil
	case "x":
		return func(p [3]float64) [3]float64 { return [3]float64{-p[2], p[1], p[0]} }, nil
	case "-x":
		return func(p [3]float64) [3]float64 { return [3]float64{p[2], p[1], -p[0]} }, nil
	}
	return nil, fmt.Errorf("the up axis must be x, y or z, optionally signed, not %q", name)
}

// countTiles returns how many tiles the routed plan makes: one per leaf with
// triangles, and one per node with triangles on both sides.
func countTiles(n *node) int {
	if n.isLeaf() {
		return min(n.count, 1)
	}
	left, right := countTiles(n.children[0]), countTiles(n.children[1])
	if left > 0 && right > 0 {
		return left + right + 1
	}
	return left + right
}

// depth returns the number of levels of tiles below t.
func depth(t *tile) int {
	d := 0
	for _, child := range t.children {
		d = max(d, depth(child)+1)
	}
	return d
}

// writeTileset writes the tileset.json of the tiles under root to dir, with
// the given root transform, in column-major order, if not nil. A tileset of
// more than maxTiles tiles moves subtrees into external tilesets in the data
// folder, unless maxTiles is 0.
//
// Tiles refine by replacement: a tile stands for the surface of its
// children.
func writeTileset(dir string, root *tile, transform *[16]float64, maxTiles int) error {
	rootTile := toTile(root)
	rootTile.Transform = transform
	create := func(name string) (io.WriteCloser, error) {
		return os.Create(filepath.Join(dir, filepath.FromSlash(name)))
	}
	_, err := tileset.Write(create, rootTile, tileset.Header{Version: "1.1"}, tileset.RootOnly{}, tileset.Options{MaxTiles: maxTiles})
	return err
}

func toTile(t *tile) *tileset.Tile {
	c, half := t.bounds.Center(), t.bounds.Size().MulScalar(0.5)
	out := &tileset.Tile{
		Box:            [12]float64{c.X, c.Y, c.Z, half.X, 0, 0, 0, half.Y, 0, 0, 0, half.Z},
		GeometricError: t.geometricError,
		Refine:         "REPLACE",
		Content:        path.Join(dataDirName, contentName(t.id)),
	}
	for _, child := range t.children {
		out.Children = append(out.Children, toTile(child))
	}
	return out
}
