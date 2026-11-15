package tiling

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/mfbonfigli/gotiler/v3/internal/geom"
	"github.com/mfbonfigli/gotiler/v3/internal/mesh"
	"github.com/mfbonfigli/gotiler/v3/internal/mesh/glb"
	"github.com/mfbonfigli/gotiler/v3/internal/mesh/simplify"
	"github.com/mfbonfigli/gotiler/v3/internal/mesh/texture"
)

const (
	// lodRatio is the share of its children's triangles a tile keeps.
	lodRatio = 0.5
	// texelErrorScale is how many pixels of its texture make the geometric
	// error of a tile. Viewers replace a tile by its children once its error
	// covers 16 pixels of the screen by default: at this scale they do so
	// when a pixel of the texture covers one pixel of the screen.
	texelErrorScale = 16
	// maxErrorPixels is how far the simplification of a tile can move its
	// surface from the input, in pixels of its texture: as much as shows
	// when the tile is replaced. A surface that moved further, because it
	// would otherwise hold too many triangles, counts as a texture of pixels
	// that much larger for the geometric error.
	maxErrorPixels = 2
	// maxOverTarget is how many times the triangles of a tile, as the
	// options set them, a tile above the leaves can hold, where its shape
	// needs them, before the limit of its simplification is raised.
	maxOverTarget = 2
	// maxRaises is how many times that limit can be raised, by √2 each
	// time, to get within that.
	maxRaises = 8
	// minPartPixels is the least size of a part of the surface that a tile
	// keeps, from corner to corner of its bounds, in pixels of its texture.
	// Viewers show a tile while its pixels are smaller than those of the
	// screen, so smaller parts would cover a few pixels at most.
	minPartPixels = 4
	// clusterCells is the largest cell of the grid that the positions of a
	// tile are merged on when collapses cannot reduce it, as a multiple of
	// the limit of its simplification. Tiles that are still over then stay
	// so.
	clusterCells = 4
	// plainTextureSize is the side, in pixels, of the texture that stands
	// for a surface of plain colors.
	plainTextureSize = 64
	// minErrorGrowth is the least ratio between the geometric error of a
	// tile and that of its children.
	minErrorGrowth = 1.001
)

// tile is a built tile of the tileset.
type tile struct {
	id     string
	bounds geom.Bounds
	// geometricError is 0 for leaves, which hold the input as it is.
	geometricError float64
	// surfaceError is how far the surface of the tile can lie from the
	// input, and moved the same around each corner of its triangles, in
	// the order of the store at reps. Both are zero for leaves.
	surfaceError float64
	moved        []float32
	children     []*tile
	// count is the number of triangles of the tile's content.
	count int
	// texel is the length, on the surface, of the side of a pixel of the
	// tile's texture, and area the surface of the leaves under the tile.
	texel, area float64
	// The tile hands its parent its content: the triangles in a store at
	// reps and their texture in a raw image file at atlas.
	reps, atlas string
}

// builder builds the tiles of a routed plan, from the leaves up, writing
// their content to dataDir and their temporary files to tempDir.
type builder struct {
	tempDir, dataDir string
	sources          []texture.Source
	maxTextureSize   int
	// maxTriangles is the most triangles a tile above the leaves holds.
	maxTriangles int
	geFactor     float64
	content      glb.Options
	progress     *phaseReporter
	tiles        atomic.Int64
	triangles    atomic.Int64
}

// job is the building of the tile of one node of the plan, which can start
// once its children are built.
type job struct {
	node     *node
	parent   *job
	children [2]*job
	// waiting counts the children still to build.
	waiting atomic.Int32
	// result is nil for a subtree without triangles.
	result *tile
}

// build builds the tiles of plan with workers in parallel and returns the
// root tile, or nil when no leaf holds triangles.
//
// A node with one empty child has no tile of its own: its other child takes
// its place.
func (b *builder) build(ctx context.Context, plan *node, workers int) (*tile, error) {
	var leaves []*job
	var jobs int
	var newJob func(n *node, parent *job) *job
	newJob = func(n *node, parent *job) *job {
		j := &job{node: n, parent: parent}
		jobs++
		if n.isLeaf() {
			leaves = append(leaves, j)
			return j
		}
		j.waiting.Store(2)
		j.children[0] = newJob(n.children[0], j)
		j.children[1] = newJob(n.children[1], j)
		return j
	}
	root := newJob(plan, nil)
	// The queue holds every job at most once, so sends never block.
	ready := make(chan *job, jobs)
	for _, j := range leaves {
		ready <- j
	}
	done := make(chan struct{})
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	var wg sync.WaitGroup
	for range max(1, workers) {
		wg.Go(func() {
			for {
				select {
				case <-ctx.Done():
					return
				case <-done:
					return
				case j := <-ready:
					if err := b.run(j); err != nil {
						cancel(fmt.Errorf("tile %s: %w", j.node.id, err))
						return
					}
					if j.parent == nil {
						close(done)
					} else if j.parent.waiting.Add(-1) == 0 {
						ready <- j.parent
					}
				}
			}
		})
	}
	wg.Wait()
	if err := context.Cause(ctx); err != nil {
		return nil, err
	}
	return root.result, nil
}

// run builds the tile of j, whose children are built.
func (b *builder) run(j *job) error {
	n := j.node
	var err error
	if n.isLeaf() {
		if n.count > 0 {
			j.result, err = b.buildLeaf(n)
		}
		return err
	}
	left, right := j.children[0].result, j.children[1].result
	switch {
	case left == nil:
		j.result = right
	case right == nil:
		j.result = left
	default:
		j.result, err = b.buildInner(n, left, right)
	}
	return err
}

// buildLeaf writes the content of a leaf: its triangles as they are, with
// the parts of the input textures they use packed into an atlas.
func (b *builder) buildLeaf(n *node) (*tile, error) {
	triangles, err := readStore(n.path)
	if err != nil {
		return nil, err
	}
	if err := os.Remove(n.path); err != nil {
		return nil, err
	}
	// The store holds the triangles in the order of routing: order them, so
	// that the tile does not depend on it.
	sortTriangles(triangles)
	m := mesh.Weld(triangles)
	atlas, err := texture.Pack(m, b.sources, texture.Options{Scale: 1, MaxSize: b.maxTextureSize, VUp: true})
	if err != nil {
		return nil, err
	}
	t := &tile{id: n.id, count: len(m.Triangles)}
	t.texel, t.area = texelSize(m, atlas)
	return t, b.finish(t, m, atlas)
}

// buildInner makes the content of the tile of n from that of its children:
// their surfaces together, simplified to half as many triangles, with a new
// texture drawn from theirs at half the pixels.
//
// The children meet along the plane that splits n, where the surface was
// cut: their positions there coincide, so the simplification works across
// the cut and leaves no seam in the surface. The positions on the planes
// that bound n stay as they are, in this tile and in every tile below it:
// a neighbor across such a plane holds the same positions there, whatever
// its level of detail, and no crack opens between them.
//
// The simplification ignores the textures of the children, whose charts
// would otherwise bound how far it can go: scanned meshes come with many
// small ones. The texture of the tile is drawn anew on the simplified
// surface.
func (b *builder) buildInner(n *node, left, right *tile) (*tile, error) {
	var detail []mesh.Triangle
	var sources []*texture.Image
	for i, child := range []*tile{left, right} {
		reps, err := readStore(child.reps)
		if err != nil {
			return nil, err
		}
		for k := range reps {
			reps[k].Material = uint32(i)
		}
		detail = append(detail, reps...)
		atlas, err := texture.ReadFile(child.atlas)
		if err != nil {
			return nil, err
		}
		sources = append(sources, atlas)
		if err := errors.Join(os.Remove(child.reps), os.Remove(child.atlas)); err != nil {
			return nil, err
		}
	}
	surface := mesh.WeldPositions(detail)
	locked := make([]bool, len(surface.Positions))
	for i, p := range surface.Positions {
		locked[i] = n.onFace(p)
	}
	// How far the surface of the children lies from the input, around each
	// position: the simplification works within what is left of the limit.
	moved := make([]float64, len(surface.Positions))
	offset := 0
	for _, child := range []*tile{left, right} {
		for i, value := range child.moved {
			corner := surface.Triangles[offset+i/3][i%3]
			moved[corner] = math.Max(moved[corner], float64(value))
		}
		offset += child.count
		child.moved = nil
	}
	// Half the triangles over the same surface are about √2 times as long:
	// so are the pixels, to keep as many on each triangle. The pixels of the
	// children count by the surface they cover, so that a child with hardly
	// any surface does not set the size for both.
	area := left.area + right.area
	pixels := left.area/(left.texel*left.texel) + right.area/(right.texel*right.texel)
	texel := math.Sqrt2 * math.Sqrt(area/pixels)
	target := lodTarget(left.count) + lodTarget(right.count)
	// The surface is simplified within a few pixels of the texture, so that
	// its shape holds at the distance the tile is shown from. Where that
	// leaves more triangles than a tile can hold, the limit is raised, as
	// often as it takes: the tile then keeps its texture, and a geometric
	// error that tells viewers to show it from farther away. Parts of a few
	// pixels go first, so that the triangles are spent on what shows.
	limit := maxErrorPixels * texel
	var surfaceError float64
	for raised := 0; ; raised++ {
		simplify.DropSmall(surface, locked, math.Max(minPartPixels*texel, limit))
		surfaceError = simplify.Simplify(surface, locked, moved, target, limit)
		if len(surface.Triangles) <= b.maxTriangles || raised == maxRaises {
			break
		}
		limit *= math.Sqrt2
	}
	simplify.DropSmall(surface, locked, math.Max(minPartPixels*texel, limit))
	if len(surface.Triangles) == 0 {
		// Nothing but small parts: keep them, simplified.
		surface = mesh.WeldPositions(detail)
		surfaceError = simplify.Simplify(surface, locked, moved, target, limit)
	}
	// What is still over is a surface that collapses cannot reduce,
	// whatever the error: merging the positions of the cells of a grid can,
	// at the cost of its shape.
	if len(surface.Triangles) > b.maxTriangles {
		surfaceError = simplify.Cluster(surface, locked, moved, b.maxTriangles, clusterCells*limit)
	}
	m, atlas, texel, err := texture.Bake(surface, detail, sources, texture.BakeOptions{TexelSize: texel, MaxSize: b.maxTextureSize})
	if err != nil {
		return nil, err
	}
	t := &tile{
		id:           n.id,
		children:     []*tile{left, right},
		count:        len(m.Triangles),
		texel:        texel,
		area:         area,
		surfaceError: surfaceError,
	}
	t.moved = make([]float32, 0, 3*len(m.Triangles))
	for _, corners := range m.Triangles {
		for _, v := range corners {
			t.moved = append(t.moved, float32(moved[m.Vertices[v].Position]))
		}
	}
	t.bounds.Include(left.bounds)
	t.bounds.Include(right.bounds)
	t.geometricError = texelErrorScale * math.Max(texel, t.surfaceError/maxErrorPixels) * b.geFactor
	for _, child := range t.children {
		t.geometricError = math.Max(t.geometricError, child.geometricError*minErrorGrowth)
	}
	return t, b.finish(t, m, atlas)
}

// finish writes the content of t, the mesh m textured by atlas, and the
// files that hand it to the parent of t.
func (b *builder) finish(t *tile, m *mesh.Indexed, atlas *texture.Image) error {
	triangles := m.Soup()
	for i := range triangles {
		for _, p := range triangles[i].P {
			t.bounds.IncludePoint(geom.Vec3{X: float64(p[0]), Y: float64(p[1]), Z: float64(p[2])})
		}
	}
	f, err := os.Create(filepath.Join(b.dataDir, contentName(t.id)))
	if err != nil {
		return err
	}
	if err := glb.Write(f, m, atlas, b.content); err != nil {
		return errors.Join(err, f.Close())
	}
	if err := f.Close(); err != nil {
		return err
	}
	t.reps = filepath.Join(b.tempDir, "reps_"+t.id+".bin")
	t.atlas = filepath.Join(b.tempDir, "reps_"+t.id+".img")
	if err := errors.Join(writeStore(t.reps, triangles), atlas.WriteFile(t.atlas)); err != nil {
		return err
	}
	b.tiles.Add(1)
	b.triangles.Add(int64(len(triangles)))
	b.progress.add(1)
	return nil
}

// contentName returns the name of the content file of the tile id.
func contentName(id string) string { return "tile_" + id + ".glb" }

// lodTarget returns how many triangles a tile keeps of count triangles.
func lodTarget(count int) int {
	return max(1, int(math.Ceil(float64(count)*lodRatio)))
}

// texelSize returns the length, on the surface of m, of the side of a pixel
// of atlas, over the triangles of m together, and their surface.
func texelSize(m *mesh.Indexed, atlas *texture.Image) (texel, surface float64) {
	var pixels float64
	for _, t := range m.Triangles {
		var p [3][3]float32
		var uv [3][2]float64
		for c, index := range t {
			v := m.Vertices[index]
			p[c] = m.Positions[v.Position]
			uv[c] = [2]float64{float64(v.UV[0]) * float64(atlas.Width), float64(v.UV[1]) * float64(atlas.Height)}
		}
		surface += mesh.Area(p[0], p[1], p[2])
		pixels += math.Abs((uv[1][0]-uv[0][0])*(uv[2][1]-uv[0][1])-(uv[1][1]-uv[0][1])*(uv[2][0]-uv[0][0])) / 2
	}
	// A surface of no extent still needs a size above zero.
	surface = math.Max(surface, math.SmallestNonzeroFloat32)
	if pixels <= 0 {
		// The triangles map to single points of the texture, as those of a
		// plain color do: any texture shows them, so a small one will do.
		return math.Sqrt(surface) / plainTextureSize, surface
	}
	return math.Sqrt(surface / pixels), surface
}
