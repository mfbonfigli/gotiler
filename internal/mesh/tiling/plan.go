package tiling

import (
	"cmp"
	"slices"
	"sync"

	"github.com/mfbonfigli/gotiler/v3/internal/mesh"
)

// maxPlanDepth is the deepest a leaf of the partition can be. It keeps the
// names of the tile files short.
const maxPlanDepth = 48

// plane is a plane perpendicular to an axis, at value along it.
type plane struct {
	axis  uint8
	value float32
}

// node is a node of the spatial partition, a binary tree whose leaves hold
// about a tile's worth of triangles each, and no more of the input textures
// than fit the texture of a tile.
//
// The tree is planned like that of point clouds and splats: from samples of
// the input, each inner node splits its space by a plane perpendicular to
// the axis its samples spread most along, through their median, so that both
// sides hold as many triangles.
type node struct {
	// id names the node by its path from the root: "0" for the root, then
	// "0" or "1" appended for the side of each split.
	id string
	// split is the plane of an inner node: what lies below it goes to
	// children[0], what lies above to children[1].
	split    plane
	children [2]*node
	// faces are the planes that bound the space of the node: those of the
	// nodes above it. The surface is cut along them, so the border of the
	// node's mesh that lies on them meets that of other nodes.
	faces []plane
	// planned is how many triangles the samples predict the node holds.
	planned int

	// Routing fills the fields below, for leaves only.
	path  string
	mu    sync.Mutex
	store *storeWriter
	count int
	// low and high are the corners of the box around the triangles routed to
	// the leaf.
	low, high [3]float32
}

// size returns the largest extent of the triangles of a leaf along an axis.
func (n *node) size() float64 {
	var size float64
	for a := range 3 {
		size = max(size, float64(n.high[a])-float64(n.low[a]))
	}
	return size
}

func (n *node) isLeaf() bool { return n.children[0] == nil }

// sample is a triangle of the input that the partition is planned from.
type sample struct {
	center [3]float32
	// pixels is the surface of the triangle in its texture, in pixels.
	pixels float32
}

// budget is what a leaf of the partition can hold.
type budget struct {
	triangles int
	// pixels is how many pixels of texture its triangles can cover.
	pixels float64
}

// buildPlan plans the partition of count triangles, within the budget in
// each leaf, from a sample of them, which it reorders.
func buildPlan(samples []sample, count int, perLeaf budget) *node {
	return planNode(samples, count, "0", nil, perLeaf)
}

func planNode(samples []sample, count int, id string, faces []plane, perLeaf budget) *node {
	n := &node{id: id, faces: faces, planned: count}
	var pixels float64
	for _, s := range samples {
		pixels += float64(s.pixels)
	}
	if len(samples) > 0 {
		// The samples are a random subset: scale theirs to all triangles.
		pixels *= float64(count) / float64(len(samples))
	}
	fits := count <= perLeaf.triangles && (perLeaf.pixels <= 0 || pixels <= perLeaf.pixels)
	if fits || len(id) > maxPlanDepth || len(samples) < 2 {
		return n
	}
	var mean, spread [3]float64
	for _, s := range samples {
		for a := range 3 {
			mean[a] += float64(s.center[a]) / float64(len(samples))
		}
	}
	for _, s := range samples {
		for a := range 3 {
			d := float64(s.center[a]) - mean[a]
			spread[a] += d * d
		}
	}
	axis := uint8(0)
	for a := uint8(1); a < 3; a++ {
		if spread[a] > spread[axis] {
			axis = a
		}
	}
	slices.SortFunc(samples, func(a, b sample) int {
		switch {
		case a.center[axis] < b.center[axis]:
			return -1
		case a.center[axis] > b.center[axis]:
			return 1
		}
		if c := slices.Compare(a.center[:], b.center[:]); c != 0 {
			return c
		}
		return cmp.Compare(a.pixels, b.pixels)
	})
	value := samples[len(samples)/2].center[axis]
	// Samples at the median go above it: move down to the first of them.
	below := len(samples) / 2
	for below > 0 && samples[below-1].center[axis] == value {
		below--
	}
	if below == 0 {
		// The lower half lies at one coordinate: nothing to split.
		return n
	}
	// The samples are a random subset: scale their split to all triangles.
	left := min(max(count*below/len(samples), 1), count-1)
	n.split = plane{axis: axis, value: value}
	childFaces := append(slices.Clone(faces), n.split)
	n.children[0] = planNode(samples[:below], left, id+"0", childFaces, perLeaf)
	n.children[1] = planNode(samples[below:], count-left, id+"1", childFaces, perLeaf)
	return n
}

// leaves appends the leaves of the subtree to out, left to right.
func (n *node) leaves(out []*node) []*node {
	if n.isLeaf() {
		return append(out, n)
	}
	return n.children[1].leaves(n.children[0].leaves(out))
}

// onFace reports whether p lies on a plane that bounds the node.
func (n *node) onFace(p [3]float32) bool {
	for _, f := range n.faces {
		if p[f.axis] == f.value {
			return true
		}
	}
	return false
}

// center returns the middle of t.
func center(t *mesh.Triangle) [3]float32 {
	var c [3]float32
	for a := range 3 {
		c[a] = float32((float64(t.P[0][a]) + float64(t.P[1][a]) + float64(t.P[2][a])) / 3)
	}
	return c
}
