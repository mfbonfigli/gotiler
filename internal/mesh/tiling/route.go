package tiling

import (
	"cmp"
	"context"
	"errors"
	"math"
	"path/filepath"
	"slices"
	"sync"

	"github.com/mfbonfigli/gotiler/v3/internal/mesh"
)

// routeBatch is how many triangles a worker splits at a time.
const routeBatch = 1 << 14

// corner is a corner of a triangle being cut.
type corner struct {
	p  [3]float32
	uv [2]float32
}

// cut returns the point where the edge between a and b crosses the plane.
//
// It starts from the same end of the edge whichever way round the edge is
// given, so that the two triangles that share the edge, which give it in
// opposite directions, get the same point to the last bit, and no crack
// opens between their halves. The point lies exactly on the plane.
func cut(a, b corner, pl plane) corner {
	if lower(b.p, a.p) {
		a, b = b, a
	}
	t := (float64(pl.value) - float64(a.p[pl.axis])) / (float64(b.p[pl.axis]) - float64(a.p[pl.axis]))
	var c corner
	for k := range 3 {
		c.p[k] = float32(float64(a.p[k]) + t*(float64(b.p[k])-float64(a.p[k])))
	}
	c.p[pl.axis] = pl.value
	for k := range 2 {
		c.uv[k] = float32(float64(a.uv[k]) + t*(float64(b.uv[k])-float64(a.uv[k])))
	}
	return c
}

// lower orders positions by their coordinates.
func lower(a, b [3]float32) bool {
	for k := range 3 {
		if a[k] != b[k] {
			return a[k] < b[k]
		}
	}
	return false
}

// side returns -1, 0 or 1 for a position below, on or above the plane.
func side(p [3]float32, pl plane) int {
	switch {
	case p[pl.axis] < pl.value:
		return -1
	case p[pl.axis] > pl.value:
		return 1
	}
	return 0
}

// split cuts t along the plane and passes the triangles of each part to
// below and above. A triangle that the plane does not cross goes whole to
// its side, and one that lies in the plane goes above.
func split(t *mesh.Triangle, pl plane, below, above func(*mesh.Triangle) error) error {
	var sides [3]int
	lowest, highest := 0, 0
	for c := range 3 {
		sides[c] = side(t.P[c], pl)
		lowest, highest = min(lowest, sides[c]), max(highest, sides[c])
	}
	if lowest == 0 {
		return above(t)
	}
	if highest <= 0 {
		return below(t)
	}
	// The plane crosses t: walk around it, keeping for each part the corners
	// on its side and on the plane, and the crossings.
	var parts [2][4]corner
	var counts [2]int
	for c := range 3 {
		next := (c + 1) % 3
		here := corner{t.P[c], t.UV[c]}
		if sides[c] <= 0 {
			parts[0][counts[0]] = here
			counts[0]++
		}
		if sides[c] >= 0 {
			parts[1][counts[1]] = here
			counts[1]++
		}
		if sides[c]*sides[next] < 0 {
			crossing := cut(here, corner{t.P[next], t.UV[next]}, pl)
			for i := range parts {
				parts[i][counts[i]] = crossing
				counts[i]++
			}
		}
	}
	for i, emit := range []func(*mesh.Triangle) error{below, above} {
		for k := 2; k < counts[i]; k++ {
			piece := mesh.Triangle{Material: t.Material}
			for c, from := range [3]corner{parts[i][0], parts[i][k-1], parts[i][k]} {
				piece.P[c], piece.UV[c] = from.p, from.uv
			}
			if piece.Degenerate() {
				continue
			}
			if err := emit(&piece); err != nil {
				return err
			}
		}
	}
	return nil
}

// crossings appends to out the points where the planes of the subtree of n
// cut the segment from a to b.
//
// It cuts the segment as the triangles that hold it as an edge are cut on
// their way down the subtree: at each plane it crosses, into two parts that
// go on down each side.
func (n *node) crossings(a, b [3]float32, out [][3]float32) [][3]float32 {
	if n.isLeaf() {
		return out
	}
	sideA, sideB := side(a, n.split), side(b, n.split)
	switch {
	case sideA*sideB < 0:
		crossing := cut(corner{p: a}, corner{p: b}, n.split).p
		out = append(out, crossing)
		if sideA > 0 {
			a, b = b, a
		}
		out = n.children[0].crossings(a, crossing, out)
		return n.children[1].crossings(crossing, b, out)
	case min(sideA, sideB) == 0:
		return n.children[1].crossings(a, b, out)
	}
	return n.children[0].crossings(a, b, out)
}

// refine passes t to emit, cut into several triangles if it has an edge on
// the plane that splits n.
//
// The triangles on both sides of the plane share that edge, but each side is
// cut further by planes of its own, which add corners along the edge that
// the other side would not have: the surfaces would not meet corner to
// corner, and would come apart once simplified. So both sides get the
// corners that either adds, here, before they part.
func (n *node) refine(t *mesh.Triangle, emit func(*mesh.Triangle) error) error {
	axis, value := n.split.axis, n.split.value
	for c := range 3 {
		next, third := (c+1)%3, (c+2)%3
		if t.P[c][axis] != value || t.P[next][axis] != value || t.P[third][axis] == value {
			continue
		}
		a, b := t.P[c], t.P[next]
		if lower(b, a) {
			a, b = b, a
		}
		points := n.children[1].crossings(a, b, n.children[0].crossings(a, b, nil))
		if len(points) == 0 {
			break
		}
		// Order the points along the edge, by the coordinate it spans most.
		along := 0
		for k := 1; k < 3; k++ {
			if math.Abs(float64(b[k])-float64(a[k])) > math.Abs(float64(b[along])-float64(a[along])) {
				along = k
			}
		}
		position := func(p [3]float32) float64 {
			return (float64(p[along]) - float64(t.P[c][along])) / (float64(t.P[next][along]) - float64(t.P[c][along]))
		}
		slices.SortFunc(points, func(p, q [3]float32) int { return cmp.Compare(position(p), position(q)) })
		points = slices.Compact(points)
		from := corner{t.P[c], t.UV[c]}
		for i := 0; i <= len(points); i++ {
			to := corner{t.P[next], t.UV[next]}
			if i < len(points) {
				at := position(points[i])
				to.p = points[i]
				for k := range 2 {
					to.uv[k] = float32(float64(t.UV[c][k]) + at*(float64(t.UV[next][k])-float64(t.UV[c][k])))
				}
			}
			piece := mesh.Triangle{Material: t.Material}
			piece.P = [3][3]float32{from.p, to.p, t.P[third]}
			piece.UV = [3][2]float32{from.uv, to.uv, t.UV[third]}
			from = to
			if piece.Degenerate() {
				continue
			}
			if err := emit(&piece); err != nil {
				return err
			}
		}
		return nil
	}
	return emit(t)
}

// add sends t down the partition from n, cutting it along the planes it
// crosses, and stores its pieces in the leaves they reach.
func (n *node) add(t *mesh.Triangle, dir string) error {
	if !n.isLeaf() {
		below := func(piece *mesh.Triangle) error { return n.children[0].add(piece, dir) }
		above := func(piece *mesh.Triangle) error { return n.children[1].add(piece, dir) }
		return split(t, n.split,
			func(piece *mesh.Triangle) error { return n.refine(piece, below) },
			func(piece *mesh.Triangle) error { return n.refine(piece, above) })
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.store == nil {
		n.path = filepath.Join(dir, "leaf_"+n.id+".bin")
		var err error
		if n.store, err = createStore(n.path); err != nil {
			return err
		}
	}
	if n.count == 0 {
		n.low, n.high = t.P[0], t.P[0]
	}
	for _, p := range t.P {
		for a := range 3 {
			n.low[a], n.high[a] = min(n.low[a], p[a]), max(n.high[a], p[a])
		}
	}
	n.count++
	return n.store.write(t)
}

// route splits the triangles of the store at path among the leaves of plan,
// in stores of their own in dir, with workers in parallel.
func route(ctx context.Context, path string, plan *node, dir string, workers int, progress *phaseReporter) error {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	batches := make(chan []mesh.Triangle, workers)
	var wg sync.WaitGroup
	for range max(1, workers) {
		wg.Go(func() {
			for batch := range batches {
				for i := range batch {
					if err := plan.add(&batch[i], dir); err != nil {
						cancel(err)
						break
					}
				}
				progress.add(len(batch))
			}
		})
	}
	err := scanStore(path, routeBatch, func(batch []mesh.Triangle) error {
		select {
		case batches <- batch:
			return nil
		case <-ctx.Done():
			return context.Cause(ctx)
		}
	})
	close(batches)
	wg.Wait()
	for _, leaf := range plan.leaves(nil) {
		if leaf.store != nil {
			err = errors.Join(err, leaf.store.close())
			leaf.store = nil
		}
	}
	if cause := context.Cause(ctx); cause != nil && err == nil {
		err = cause
	}
	return err
}
