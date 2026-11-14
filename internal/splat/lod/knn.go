package lod

import (
	"github.com/mfbonfigli/gotiler/v3/internal/geom"
	"github.com/mfbonfigli/gotiler/v3/internal/splat"
)

// leafSize is the most points a leaf of the tree holds. Scanning a few points
// costs less than splitting them down to single points and recursing into
// each.
const leafSize = 8

// kdTree finds the nearest splat centers to a point. It is an implicit tree:
// a range of more than leafSize points has its node at the middle, the points
// before it on one side of the node's splitting plane and the points after it
// on the other, while a smaller range is a leaf, searched point by point.
//
// The search is exact, so the shape of the tree changes how fast queries run,
// never what they find.
type kdTree struct {
	points []kdPoint
}

type kdPoint struct {
	center [3]float64
	index  int32
	axis   uint8
}

// neighbor is a point found by a query, with its squared distance.
type neighbor struct {
	distanceSquared float64
	index           int32
}

// kdBox bounds a range of points of the tree.
type kdBox struct {
	min, max [3]float64
}

// build makes the tree hold the centers of splats, reusing its memory.
func (t *kdTree) build(splats []splat.Splat) {
	t.points = resize(t.points, len(splats))
	var bounds kdBox
	for i, s := range splats {
		c := [3]float64{s.Center.X, s.Center.Y, s.Center.Z}
		t.points[i] = kdPoint{center: c, index: int32(i)}
		for axis := range 3 {
			if i == 0 || c[axis] < bounds.min[axis] {
				bounds.min[axis] = c[axis]
			}
			if i == 0 || c[axis] > bounds.max[axis] {
				bounds.max[axis] = c[axis]
			}
		}
	}
	t.split(t.points, bounds)
}

// split arranges points, within bounds, into a subtree, splitting it along
// the longest side of bounds. The two halves are bounded by bounds cut at the
// node: looser than measuring each half, which would read every point at
// every level, but as good for choosing the splitting axes.
func (t *kdTree) split(points []kdPoint, bounds kdBox) {
	if len(points) <= leafSize {
		return
	}
	axis := uint8(0)
	if bounds.max[1]-bounds.min[1] > bounds.max[0]-bounds.min[0] {
		axis = 1
	}
	if bounds.max[2]-bounds.min[2] > bounds.max[axis]-bounds.min[axis] {
		axis = 2
	}
	middle := len(points) / 2
	selectNth(points, middle, axis)
	points[middle].axis = axis
	cut := points[middle].center[axis]
	low, high := bounds, bounds
	low.max[axis], high.min[axis] = cut, cut
	t.split(points[:middle], low)
	t.split(points[middle+1:], high)
}

// nearest appends to found[:0] the k points nearest to query, at most
// sqrt(maxDistanceSquared) away and other than the point at index self, and
// returns it. They are ordered by distance, and points at the same distance
// by their index, counted onwards from self and wrapping around.
//
// Counting from self keeps splats that pile up on one point from all picking
// the same few neighbors among the pile: they pick the next ones instead, so
// that most of them can merge in pairs at once.
func (t *kdTree) nearest(query geom.Vec3, self int32, k int, maxDistanceSquared float64, found []neighbor) []neighbor {
	search := kdSearch{query: [3]float64{query.X, query.Y, query.Z}, self: self, k: k, limit: maxDistanceSquared, found: found[:0]}
	search.visit(t.points)
	return search.found
}

type kdSearch struct {
	query [3]float64
	self  int32
	k     int
	limit float64
	found []neighbor
}

func (s *kdSearch) visit(points []kdPoint) {
	if len(points) <= leafSize {
		for i := range points {
			s.consider(&points[i])
		}
		return
	}
	middle := len(points) / 2
	node := &points[middle]
	s.consider(node)
	offset := s.query[node.axis] - node.center[node.axis]
	near, far := points[:middle], points[middle+1:]
	if offset > 0 {
		near, far = far, near
	}
	s.visit(near)
	// Points beyond the splitting plane are at least offset away.
	if offset*offset <= s.limit {
		s.visit(far)
	}
}

// consider adds p to the found neighbors if it is within the search limit.
func (s *kdSearch) consider(p *kdPoint) {
	if p.index == s.self {
		return
	}
	dx, dy, dz := p.center[0]-s.query[0], p.center[1]-s.query[1], p.center[2]-s.query[2]
	if d := dx*dx + dy*dy + dz*dz; d <= s.limit {
		s.insert(neighbor{distanceSquared: d, index: p.index})
	}
}

// insert adds n to the found neighbors, dropping the farthest once there are
// k of them, and tightens the search limit when the list is full.
func (s *kdSearch) insert(n neighbor) {
	if len(s.found) == s.k {
		if !s.closer(n, s.found[s.k-1]) {
			return
		}
		s.found = s.found[:s.k-1]
	}
	i := len(s.found)
	s.found = append(s.found, n)
	for ; i > 0 && s.closer(n, s.found[i-1]); i-- {
		s.found[i] = s.found[i-1]
	}
	s.found[i] = n
	if len(s.found) == s.k {
		s.limit = s.found[s.k-1].distanceSquared
	}
}

func (s *kdSearch) closer(a, b neighbor) bool {
	if a.distanceSquared != b.distanceSquared {
		return a.distanceSquared < b.distanceSquared
	}
	return uint32(a.index-s.self) < uint32(b.index-s.self)
}

// selectNth reorders points so that the one at index n is where sorting them
// along axis, then by index, would put it, with no point after it that sorts
// before it and none before it that sorts after it.
func selectNth(points []kdPoint, n int, axis uint8) {
	less := func(a, b *kdPoint) bool {
		ca, cb := a.center[axis], b.center[axis]
		if ca != cb {
			return ca < cb
		}
		return a.index < b.index
	}
	lo, hi := 0, len(points)-1
	for lo < hi {
		// Hoare partition around the median of three, which keeps the
		// selection fast on points that are already sorted.
		mid := lo + (hi-lo)/2
		if less(&points[mid], &points[lo]) {
			points[mid], points[lo] = points[lo], points[mid]
		}
		if less(&points[hi], &points[lo]) {
			points[hi], points[lo] = points[lo], points[hi]
		}
		if less(&points[hi], &points[mid]) {
			points[hi], points[mid] = points[mid], points[hi]
		}
		pivot := points[mid]
		i, j := lo, hi
		for i <= j {
			for less(&points[i], &pivot) {
				i++
			}
			for less(&pivot, &points[j]) {
				j--
			}
			if i <= j {
				points[i], points[j] = points[j], points[i]
				i++
				j--
			}
		}
		switch {
		case n <= j:
			hi = j
		case n >= i:
			lo = i
		default:
			return
		}
	}
}
