package tiling

import (
	"cmp"
	"math"
	"slices"
	"sync"

	"github.com/mfbonfigli/gotiler/v3/internal/geom"
)

// maxPlanDepth is the deepest a leaf of the partition can be. Splits halve
// the weight of the splats, not their number, so regions of many faint
// splats need deeper subtrees than balanced splits would; the limit only
// keeps extreme weights from planning a chain of splits, and the names of
// tile files short.
const maxPlanDepth = 64

// node is a node of the spatial partition, a binary tree whose leaves hold up
// to a tile's worth of splats each.
//
// The tree is planned from samples of the splats. Each inner node splits its
// space by a plane perpendicular to one axis, through the weighted median of
// its samples along the axis of their largest weighted variance, so that
// dense and opaque regions weigh more than sparse, faint ones.
type node struct {
	// id names the node by its path from the root: "0" for the root, then
	// "0" or "1" appended for the side of each split.
	id    string
	depth int
	// axis and split define the plane of an inner node: splats with a lower
	// coordinate along axis go to children[0], higher ones to children[1].
	axis     uint8
	split    float64
	children [2]*node
	// planned is how many splats the samples predict the node holds.
	planned int

	// Routing fills the fields below, for leaves only.

	// ordinal is the position of the leaf among the leaves, left to right.
	ordinal int
	// path is the store of the splats routed to the leaf.
	path string
	// mu guards count and bounds, the number of splats routed to the leaf
	// and their bounds out to boundsSigmas.
	mu     sync.Mutex
	count  int
	bounds geom.Bounds
}

func (n *node) isLeaf() bool { return n.children[0] == nil }

// buildPlan plans the partition of count splats, perTile in each leaf, from
// samples of them, which it reorders.
func buildPlan(samples []sample, count, perTile int) *node {
	return planNode(samples, count, "0", 0, perTile)
}

// planNode plans the subtree of count splats represented by samples. It
// splits until a node fits a tile, unless the samples cannot be split or the
// node is maxPlanDepth deep.
func planNode(samples []sample, count int, id string, depth, perTile int) *node {
	n := &node{id: id, depth: depth, planned: count}
	if count <= perTile || depth >= maxPlanDepth || len(samples) < 2 {
		return n
	}
	var stats [3]axisStats
	for _, s := range samples {
		stats[0].add(s.center.X, s.weight)
		stats[1].add(s.center.Y, s.weight)
		stats[2].add(s.center.Z, s.weight)
	}
	axis := uint8(0)
	for a := uint8(1); a < 3; a++ {
		if stats[a].variance() > stats[axis].variance() {
			axis = a
		}
	}
	if !(stats[axis].variance() > 0) {
		return n
	}
	split := weightedMedian(samples, axis)

	// The split goes through a sample, and the samples vary along the axis,
	// so both sides get samples.
	var sides [2][]sample
	for _, s := range samples {
		side := sideOf(s.center, axis, split, len(sides[0]), len(sides[1]))
		sides[side] = append(sides[side], s)
	}
	// The samples are a random subset: scale their split to all the splats.
	left := int(math.Round(float64(count) * float64(len(sides[0])) / float64(len(samples))))
	left = min(max(left, 1), count-1)
	n.axis, n.split = axis, split
	n.children[0] = planNode(sides[0], left, id+"0", depth+1, perTile)
	n.children[1] = planNode(sides[1], count-left, id+"1", depth+1, perTile)
	return n
}

// sideOf returns 0 if center lies below the split plane and 1 if above. A
// center on the plane goes to the side holding fewer splats so far, left or
// right, the left one on a tie.
func sideOf(center geom.Vec3, axis uint8, split float64, left, right int) int {
	c := coordinate(center, axis)
	switch {
	case c < split:
		return 0
	case c > split:
		return 1
	case left <= right:
		return 0
	default:
		return 1
	}
}

// leafOf returns the leaf of the partition that holds center. Centers on a
// split plane go to the side that the plan expects to hold fewer splats.
func (n *node) leafOf(center geom.Vec3) *node {
	for !n.isLeaf() {
		n = n.children[sideOf(center, n.axis, n.split, n.children[0].planned, n.children[1].planned)]
	}
	return n
}

// leaves appends the leaves of the subtree to out, left to right.
func (n *node) leaves(out []*node) []*node {
	if n.isLeaf() {
		return append(out, n)
	}
	return n.children[1].leaves(n.children[0].leaves(out))
}

// axisStats accumulates the weighted mean and variance of values with
// Welford's online algorithm.
type axisStats struct {
	weight, mean, m2 float64
}

func (s *axisStats) add(value, weight float64) {
	if !(weight > 0) || math.IsInf(weight, 0) {
		return
	}
	total := s.weight + weight
	delta := value - s.mean
	s.mean += delta * weight / total
	s.m2 += weight * delta * (value - s.mean)
	s.weight = total
}

func (s axisStats) variance() float64 {
	if s.weight == 0 {
		return 0
	}
	return s.m2 / s.weight
}

// weightedMedian sorts samples along axis and returns the coordinate of the
// first sample at which the running total of their weights reaches half.
func weightedMedian(samples []sample, axis uint8) float64 {
	slices.SortFunc(samples, func(a, b sample) int {
		if c := cmp.Compare(coordinate(a.center, axis), coordinate(b.center, axis)); c != 0 {
			return c
		}
		return cmp.Compare(a.weight, b.weight)
	})
	var total float64
	for _, s := range samples {
		total += s.weight
	}
	if !(total > 0) || math.IsInf(total, 0) {
		return coordinate(samples[len(samples)/2].center, axis)
	}
	var sum float64
	last := len(samples) - 1
	for _, s := range samples[:last] {
		if sum += s.weight; sum >= total/2 {
			return coordinate(s.center, axis)
		}
	}
	return coordinate(samples[last].center, axis)
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
