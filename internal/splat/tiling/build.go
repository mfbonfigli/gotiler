package tiling

import (
	"cmp"
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/mfbonfigli/gotiler/v3/internal/geom"
	"github.com/mfbonfigli/gotiler/v3/internal/splat"
	"github.com/mfbonfigli/gotiler/v3/internal/splat/glb"
	"github.com/mfbonfigli/gotiler/v3/internal/splat/lod"
	"github.com/mfbonfigli/gotiler/v3/internal/splat/spz"
	"github.com/mfbonfigli/gotiler/v3/internal/splat/store"
)

const (
	// lodRatio is the share of its children's splats a tile keeps.
	lodRatio = 0.5

	// The geometric error of a tile is geometricErrorScale times the spacing
	// of its splats over its bounds, grown by geometricErrorGrowth for each
	// level of detail its splats are merged from, and at least
	// minErrorGrowth times that of its children.
	//
	// The scale is half the one of point clouds: splats cover the space
	// between their centers, so viewers need fewer of them. On a 106 million
	// splat aerial scene, it shows the same image as the point cloud scale with
	// 40% fewer splats up close and 70% fewer from afar.
	geometricErrorScale  = 2
	geometricErrorGrowth = 1.1
	minErrorGrowth       = 1.001
)

// tile is a built tile of the tileset.
type tile struct {
	id     string
	bounds geom.Bounds
	// geometricError is 0 for leaves, which hold all their splats.
	geometricError float64
	children       []*tile
	// count is the number of splats of the tile's content.
	count int
	// levels is how many times the splats of the store reps were merged.
	levels int
	// reps is the store of the splats that the parent tile merges: the tile's
	// content, or fewer splats for leaves holding more than a tile's worth.
	reps string
}

// builder builds the tiles of a routed plan, from the leaves up, writing
// their content to dataDir and their temporary stores to tempDir.
type builder struct {
	tempDir, dataDir string
	shCount          int
	perTile          int
	geFactor         float64
	version          spz.Version
	progress         *phaseReporter
	tiles            atomic.Int64
}

// job is the building of the tile of one node of the plan, which can start
// once its children are built.
type job struct {
	node     *node
	parent   *job
	children [2]*job
	// waiting counts the children still to build.
	waiting atomic.Int32
	// result is nil for a subtree without splats.
	result *tile
}

// build builds the tiles of plan with workers in parallel and returns the
// root tile, or nil when no leaf holds splats.
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
						cancel(err)
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
	if n.isLeaf() {
		if n.count == 0 {
			return nil
		}
		t, err := b.buildLeaf(n)
		j.result = t
		return err
	}
	left, right := j.children[0].result, j.children[1].result
	switch {
	case left == nil:
		j.result = right
		return nil
	case right == nil:
		j.result = left
		return nil
	}
	t, err := b.buildInner(n, left, right)
	j.result = t
	return err
}

// buildLeaf writes the content of a leaf: all its splats, ordered by
// SourceIndex so that the output does not depend on the order of routing.
func (b *builder) buildLeaf(n *node) (*tile, error) {
	splats, err := store.ReadAll(n.path, b.shCount)
	if err != nil {
		return nil, err
	}
	sortBySourceIndex(splats)
	if err := b.writeContent(n.id, splats); err != nil {
		return nil, err
	}
	t := &tile{id: n.id, bounds: n.bounds, count: len(splats), reps: n.path}
	// A leaf with more than a tile's worth of splats still holds them all,
	// but hands its parent fewer.
	if len(splats) > b.perTile {
		reps, err := lod.Simplify(splats, lodTarget(len(splats)))
		if err != nil {
			return nil, err
		}
		if t.reps, err = b.writeReps(n.id, reps); err != nil {
			return nil, err
		}
		t.levels = 1
		if err := os.Remove(n.path); err != nil {
			return nil, err
		}
	}
	return t, nil
}

// buildInner merges the splats the children of n hand up into the content of
// the tile of n.
//
// It merges the splats of both children together, so that merges cross the
// split between them: merging each side apart would leave a seam of denser
// splats along the split, at every level up to the root.
func (b *builder) buildInner(n *node, left, right *tile) (*tile, error) {
	var splats []splat.Splat
	for _, child := range []*tile{left, right} {
		reps, err := store.ReadAll(child.reps, b.shCount)
		if err != nil {
			return nil, err
		}
		if err := os.Remove(child.reps); err != nil {
			return nil, err
		}
		// The store of a leaf holds its splats in the order of routing: order
		// them, so that the merge does not depend on it.
		sortBySourceIndex(reps)
		splats = append(splats, reps...)
	}
	content, err := lod.Simplify(splats, lodTarget(left.count)+lodTarget(right.count))
	if err != nil {
		return nil, err
	}
	if err := b.writeContent(n.id, content); err != nil {
		return nil, err
	}
	reps, err := b.writeReps(n.id, content)
	if err != nil {
		return nil, err
	}

	// Merged splats can reach past the bounds of the splats they merge.
	bounds := left.bounds
	bounds.Include(right.bounds)
	for _, s := range content {
		bounds.Include(s.Bounds(boundsSigmas))
	}
	t := &tile{
		id:       n.id,
		bounds:   bounds,
		children: []*tile{left, right},
		count:    len(content),
		levels:   max(left.levels, right.levels) + 1,
		reps:     reps,
	}
	t.geometricError = geometricError(bounds, len(content), t.levels, b.geFactor)
	for _, child := range t.children {
		t.geometricError = math.Max(t.geometricError, child.geometricError*minErrorGrowth)
	}
	return t, nil
}

// writeContent writes the content of the tile id.
func (b *builder) writeContent(id string, splats []splat.Splat) error {
	f, err := os.Create(filepath.Join(b.dataDir, contentName(id)))
	if err != nil {
		return err
	}
	if err := glb.Write(f, splats, b.version); err != nil {
		return errors.Join(err, f.Close())
	}
	if err := f.Close(); err != nil {
		return err
	}
	b.tiles.Add(1)
	b.progress.add(1)
	return nil
}

// writeReps writes the splats the tile id hands its parent to a new store,
// and returns its path.
func (b *builder) writeReps(id string, splats []splat.Splat) (string, error) {
	path := filepath.Join(b.tempDir, "reps_"+id+".bin")
	w, err := store.Create(path, b.shCount)
	if err != nil {
		return "", err
	}
	if err := w.Write(splats); err != nil {
		return "", errors.Join(err, w.Close())
	}
	return path, w.Close()
}

func sortBySourceIndex(splats []splat.Splat) {
	slices.SortFunc(splats, func(a, b splat.Splat) int { return cmp.Compare(a.SourceIndex, b.SourceIndex) })
}

// contentName returns the name of the content file of the tile id.
func contentName(id string) string { return "tile_" + id + ".glb" }

// lodTarget returns how many splats a tile keeps of count splats.
func lodTarget(count int) int {
	return max(1, int(math.Ceil(float64(count)*lodRatio)))
}

// geometricError returns the geometric error of count splats spread over
// bounds and merged levels times, scaled by factor.
//
// Like for point clouds, the error grows with the spacing of the splats over
// the surface of the bounds, as rendering them stands for the surface they
// sample.
func geometricError(bounds geom.Bounds, count, levels int, factor float64) float64 {
	size := bounds.Size()
	area := size.X*size.Y + size.X*size.Z + size.Y*size.Z
	if count <= 0 || area <= 0 {
		return 0
	}
	return math.Sqrt(area/float64(count)) * geometricErrorScale * factor * math.Pow(geometricErrorGrowth, float64(levels))
}
