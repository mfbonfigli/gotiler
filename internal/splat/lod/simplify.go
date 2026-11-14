// Package lod builds the coarser levels of detail of a splat tileset, merging
// close and similar splats into single splats that cover them.
//
// The merging follows the training-free simplification of NanoGS
// (arXiv:2603.16103), scoring merges with a closed-form bound in place of its
// Monte Carlo estimate.
package lod

import (
	"cmp"
	"errors"
	"math"
	"slices"
	"sync"

	"github.com/mfbonfigli/gotiler/v3/internal/geom"
	"github.com/mfbonfigli/gotiler/v3/internal/radix"
	"github.com/mfbonfigli/gotiler/v3/internal/splat"
)

const (
	// neighborCount is how many of its nearest splats each splat can merge
	// with.
	neighborCount = 8

	// colorWeight scales the cost of merging splats of different colors
	// against the cost of merging different shapes.
	colorWeight = 0.45

	// Splats farther apart than cellDistanceFactor times the spacing of the
	// target splats, or than extentDistanceFactor times the largest side of
	// the splats' bounds, never merge.
	cellDistanceFactor   = 1.5
	extentDistanceFactor = 0.04

	// minCellSize floors the spacing of the target splats.
	minCellSize = 1e-6

	// minScaleRatio floors the scales of a splat, relative to its largest,
	// when scoring merges, so that flat splats have a finite volume.
	minScaleRatio = 1e-4
	minScale      = 1e-12

	// minDeterminant floors the determinant of a covariance matrix, so that
	// its logarithm stays finite.
	minDeterminant = 1e-300
)

// workspaces holds the buffers of Simplify calls, reused across calls.
var workspaces = sync.Pool{New: func() any { return new(workspace) }}

// Simplify returns splats reduced toward target splats, ordered by
// SourceIndex.
//
// It works in passes. Each pass pairs every splat with its nearest neighbors,
// scores each pair by how much merging it would change the scene, and merges
// the cheapest pairs that share no splat, as many as are needed to reach the
// target. Splats farther apart than a distance that grows with the spacing
// the target splats would have never merge, so the result can hold more than
// target splats: distant structures stay apart instead of being bridged by
// long, thin splats.
//
// The result is the same for the same input in the same order. Its splats can
// share SHRest memory with the input, which is left unchanged. Simplify is
// safe for concurrent use.
func Simplify(splats []splat.Splat, target int) ([]splat.Splat, error) {
	if target < 1 && len(splats) > 0 {
		return nil, errors.New("the target number of splats must be at least 1")
	}
	ws := workspaces.Get().(*workspace)
	defer ws.release()
	current := splats
	for len(current) > target {
		next, err := ws.pass(current, target)
		if err != nil {
			return nil, err
		}
		if len(next) == len(current) {
			break
		}
		current = next
	}
	result := make([]splat.Splat, len(current))
	copy(result, current)
	splat.SortBySourceIndex(result)
	return result, nil
}

// workspace holds the buffers of one Simplify call.
type workspace struct {
	// passes holds the output of the passes, alternating between its two
	// buffers so that each pass reads the output of the previous one.
	passes    [2][]splat.Splat
	turn      int
	infos     []splatInfo
	tree      kdTree
	found     []neighbor
	neighbors []int32
	counts    []uint8
	edges     []radix.Item
	sorting   []radix.Item
	used      []bool
}

// newEdge returns a candidate merge of the splats at indices a and b, with
// a < b, as an item for radix.Sort. Its key is the bits of cost, which order
// like the costs, as these are never negative, and its value holds a in its
// upper half and b in its lower one, which orders like the pairs.
//
// A NaN cost, which only splats so large that their covariances overflow can
// give, has the highest key: those merges come last.
func newEdge(a, b int32, cost float64) radix.Item {
	return radix.Item{Key: math.Float64bits(cost), Value: uint64(a)<<32 | uint64(b)}
}

// edgeSplats returns the indices of the splats of the edge e.
func edgeSplats(e radix.Item) (a, b int32) { return int32(e.Value >> 32), int32(uint32(e.Value)) }

// pass merges the cheapest pairs of splats, up to the number needed to reach
// target, and returns the merged splats followed by the unmerged ones.
func (ws *workspace) pass(splats []splat.Splat, target int) ([]splat.Splat, error) {
	ws.infos = resize(ws.infos, len(splats))
	for i, s := range splats {
		ws.infos[i] = newSplatInfo(s)
	}
	ws.tree.build(splats)
	edges := ws.collectEdges(splats, mergeDistance(splats, target))
	ws.sortEdges(edges)

	pairs := min(len(splats)-target, len(splats)/2)
	used := resize(ws.used, len(splats))
	clear(used)
	ws.used = used
	next := ws.passes[ws.turn][:0]
	for _, e := range edges {
		if len(next) == pairs {
			break
		}
		a, b := edgeSplats(e)
		if used[a] || used[b] {
			continue
		}
		var acc accumulator
		acc.add(splats[a])
		acc.add(splats[b])
		merged, err := acc.finish()
		if err != nil {
			return nil, err
		}
		used[a], used[b] = true, true
		next = append(next, merged)
	}
	for i, s := range splats {
		if !used[i] {
			next = append(next, s)
		}
	}
	ws.passes[ws.turn] = next
	ws.turn ^= 1
	return next, nil
}

// collectEdges returns an edge for every pair of splats where one is among
// the nearest neighbors of the other, at most maxDistance apart, with its
// merge cost.
func (ws *workspace) collectEdges(splats []splat.Splat, maxDistance float64) []radix.Item {
	ws.neighbors = resize(ws.neighbors, len(splats)*neighborCount)
	ws.counts = resize(ws.counts, len(splats))
	// Querying in the order of the tree, rather than of the splats, keeps
	// consecutive queries in the same branches, which stay in the cache.
	for _, p := range ws.tree.points {
		i := int(p.index)
		ws.found = ws.tree.nearest(splats[i].Center, p.index, neighborCount, maxDistance*maxDistance, ws.found)
		ws.counts[i] = uint8(len(ws.found))
		for j, n := range ws.found {
			ws.neighbors[i*neighborCount+j] = n.index
		}
	}
	edges := ws.edges[:0]
	for i := range splats {
		for _, j := range ws.neighborsOf(i) {
			// A pair of mutual neighbors gets its edge from the first of them.
			if int(j) < i && slices.Contains(ws.neighborsOf(int(j)), int32(i)) {
				continue
			}
			a, b := min(int32(i), j), max(int32(i), j)
			edges = append(edges, newEdge(a, b, mergeCost(splats[a], splats[b], ws.infos[a], ws.infos[b])))
		}
	}
	ws.edges = edges
	return edges
}

// sortEdges orders edges by cost, then by their splats.
func (ws *workspace) sortEdges(edges []radix.Item) {
	ws.sorting = radix.Sort(edges, ws.sorting)
	// The sort keeps edges of the same cost in the order they were collected:
	// order those by their splats.
	for first := 0; first < len(edges); {
		last := first + 1
		for last < len(edges) && edges[last].Key == edges[first].Key {
			last++
		}
		if last-first > 1 {
			slices.SortFunc(edges[first:last], func(x, y radix.Item) int { return cmp.Compare(x.Value, y.Value) })
		}
		first = last
	}
}

func (ws *workspace) neighborsOf(i int) []int32 {
	return ws.neighbors[i*neighborCount : i*neighborCount+int(ws.counts[i])]
}

// release clears the splats the workspace holds, so that it keeps no SHRest
// memory of the caller alive, and returns it to the pool.
func (ws *workspace) release() {
	for i := range ws.passes {
		clear(ws.passes[i][:cap(ws.passes[i])])
		ws.passes[i] = ws.passes[i][:0]
	}
	ws.turn = 0
	workspaces.Put(ws)
}

// splatInfo holds the values of a splat that its merge costs use.
type splatInfo struct {
	weight         float64
	covariance     geom.Mat3
	logDeterminant float64
	color          geom.Vec3
}

func newSplatInfo(s splat.Splat) splatInfo {
	floor := math.Max(s.Scale.MaxComponent()*minScaleRatio, minScale)
	scale := geom.Vec3{X: math.Max(s.Scale.X, floor), Y: math.Max(s.Scale.Y, floor), Z: math.Max(s.Scale.Z, floor)}
	covariance := splat.CovarianceFromRotationScale(s.Rotation.RotationMatrix(), scale)
	return splatInfo{
		weight:         Weight(s),
		covariance:     covariance,
		logDeterminant: logDeterminant(covariance),
		color:          s.BaseColor(),
	}
}

// mergeCost scores merging splats a and b, with infos ia and ib.
//
// Its shape term is Runnalls' upper bound on the Kullback-Leibler divergence
// between the two weighted Gaussians and the single Gaussian that matches
// their moments:
//
//	(W·ln det C - wa·ln det Ca - wb·ln det Cb) / 2
//
// with W = wa + wb and C the covariance of the merged Gaussian. It is near
// zero for overlapping copies of a splat and grows with their distance
// relative to their size, with the angle between thin splats, and with their
// weight, so that detail merges last. Its color term grows with the weighted
// squared distance of their base colors.
func mergeCost(a, b splat.Splat, ia, ib splatInfo) float64 {
	total := ia.weight + ib.weight
	d := a.Center.Sub(b.Center)
	merged := ia.covariance.MulScalar(ia.weight / total).
		Add(ib.covariance.MulScalar(ib.weight / total)).
		Add(geom.Outer(d, d).MulScalar(ia.weight * ib.weight / (total * total)))
	// The bound is not negative in exact arithmetic: drop rounding errors. The
	// conversions round each product before it is subtracted, which arm64 would
	// otherwise fuse into one instruction: copies of a splat then cost exactly 0
	// on every platform.
	shape := math.Max(0, (float64(total*logDeterminant(merged))-float64(ia.weight*ia.logDeterminant)-float64(ib.weight*ib.logDeterminant))/2)
	return shape + colorWeight*total*ia.color.Sub(ib.color).LengthSquared()
}

func logDeterminant(m geom.Mat3) float64 {
	d := m.Determinant()
	if !(d >= minDeterminant) {
		d = minDeterminant
	}
	return math.Log(d)
}

// mergeDistance returns how far apart splats can be to merge, when reducing
// splats to target.
func mergeDistance(splats []splat.Splat, target int) float64 {
	var bounds geom.Bounds
	for _, s := range splats {
		bounds.IncludePoint(s.Center)
	}
	limit := cellSize(bounds.Size(), target) * cellDistanceFactor
	if extent := bounds.MaxExtent(); extent > 0 {
		limit = math.Min(limit, extent*extentDistanceFactor)
	}
	return limit
}

// cellSize returns the spacing of count splats spread evenly over a box of
// the given size. Sides of at most 5% of the longest side do not count, so
// that flat and thin scenes are measured by their area or length.
func cellSize(size geom.Vec3, count int) float64 {
	longest := size.MaxComponent()
	if !(longest > 0) {
		return minCellSize
	}
	product, dimensions := 1.0, 0
	for _, side := range [3]float64{size.X, size.Y, size.Z} {
		if side > longest*0.05 {
			product *= side
			dimensions++
		}
	}
	spacing := product / float64(count)
	switch dimensions {
	case 2:
		spacing = math.Sqrt(spacing)
	case 3:
		spacing = math.Cbrt(spacing)
	}
	return math.Max(minCellSize, spacing)
}

// resize returns s with length n, reallocated only if it is too small.
func resize[T any](s []T, n int) []T {
	if cap(s) < n {
		return make([]T, n)
	}
	return s[:n]
}
