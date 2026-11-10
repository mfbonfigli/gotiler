package lod

import (
	"github.com/mfbonfigli/gotiler/v3/internal/geom"
	"github.com/mfbonfigli/gotiler/v3/internal/splat"
)

// kdTree finds the nearest splat centers to a point. It is an implicit tree:
// each range of points has its node at the middle, the points before it on
// one side of the node's splitting plane and the points after it on the
// other.
type kdTree struct {
	points []kdPoint
}

type kdPoint struct {
	center geom.Vec3
	index  int32
	axis   uint8
}

// neighbor is a point found by a query, with its squared distance.
type neighbor struct {
	distanceSquared float64
	index           int32
}

// build makes the tree hold the centers of splats, reusing its memory.
func (t *kdTree) build(splats []splat.Splat) {
	t.points = resize(t.points, len(splats))
	for i, s := range splats {
		t.points[i] = kdPoint{center: s.Center, index: int32(i)}
	}
	t.split(t.points)
}

// split arranges points into a subtree, splitting it along its longest side.
func (t *kdTree) split(points []kdPoint) {
	if len(points) < 2 {
		return
	}
	var bounds geom.Bounds
	for _, p := range points {
		bounds.IncludePoint(p.center)
	}
	size := bounds.Size()
	axis := uint8(0)
	if size.Y > size.X {
		axis = 1
	}
	if size.Z > coordinate(size, axis) {
		axis = 2
	}
	middle := len(points) / 2
	selectNth(points, middle, axis)
	points[middle].axis = axis
	t.split(points[:middle])
	t.split(points[middle+1:])
}

// nearest appends to found[:0] the k points nearest to query, at most
// sqrt(maxDistanceSquared) away and other than the point at index self,
// ordered by distance and then by index, and returns it.
func (t *kdTree) nearest(query geom.Vec3, self int32, k int, maxDistanceSquared float64, found []neighbor) []neighbor {
	search := kdSearch{query: query, self: self, k: k, limit: maxDistanceSquared, found: found[:0]}
	search.visit(t.points)
	return search.found
}

type kdSearch struct {
	query geom.Vec3
	self  int32
	k     int
	limit float64
	found []neighbor
}

func (s *kdSearch) visit(points []kdPoint) {
	if len(points) == 0 {
		return
	}
	middle := len(points) / 2
	node := points[middle]
	if node.index != s.self {
		if d := node.center.Sub(s.query).LengthSquared(); d <= s.limit {
			s.insert(neighbor{distanceSquared: d, index: node.index})
		}
	}
	offset := coordinate(s.query, node.axis) - coordinate(node.center, node.axis)
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

// insert adds n to the found neighbors, dropping the farthest once there are
// k of them, and tightens the search limit when the list is full.
func (s *kdSearch) insert(n neighbor) {
	if len(s.found) == s.k {
		if !closer(n, s.found[s.k-1]) {
			return
		}
		s.found = s.found[:s.k-1]
	}
	i := len(s.found)
	s.found = append(s.found, n)
	for ; i > 0 && closer(n, s.found[i-1]); i-- {
		s.found[i] = s.found[i-1]
	}
	s.found[i] = n
	if len(s.found) == s.k {
		s.limit = s.found[s.k-1].distanceSquared
	}
}

func closer(a, b neighbor) bool {
	if a.distanceSquared != b.distanceSquared {
		return a.distanceSquared < b.distanceSquared
	}
	return a.index < b.index
}

// selectNth reorders points so that the one at index n is where sorting them
// along axis, then by index, would put it, with no point after it that sorts
// before it and none before it that sorts after it.
func selectNth(points []kdPoint, n int, axis uint8) {
	less := func(a, b kdPoint) bool {
		ca, cb := coordinate(a.center, axis), coordinate(b.center, axis)
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
		if less(points[mid], points[lo]) {
			points[mid], points[lo] = points[lo], points[mid]
		}
		if less(points[hi], points[lo]) {
			points[hi], points[lo] = points[lo], points[hi]
		}
		if less(points[hi], points[mid]) {
			points[hi], points[mid] = points[mid], points[hi]
		}
		pivot := points[mid]
		i, j := lo, hi
		for i <= j {
			for less(points[i], pivot) {
				i++
			}
			for less(pivot, points[j]) {
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

func coordinate(v geom.Vec3, axis uint8) float64 {
	switch axis {
	case 1:
		return v.Y
	case 2:
		return v.Z
	default:
		return v.X
	}
}
