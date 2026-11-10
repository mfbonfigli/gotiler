package tiling

import (
	"math"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"github.com/mfbonfigli/gotiler/v3/internal/geom"
)

func uniformSamples(seed uint64, n int, size geom.Vec3) []sample {
	rng := rand.New(rand.NewPCG(seed, 5))
	samples := make([]sample, n)
	for i := range samples {
		samples[i] = sample{center: geom.Vec3{X: rng.Float64() * size.X, Y: rng.Float64() * size.Y, Z: rng.Float64() * size.Z}, weight: 1}
	}
	return samples
}

// checkPlan checks the invariants of a planned subtree: the planned counts
// of children add up, children are named after their parent, and every leaf
// fits a tile.
func checkPlan(t *testing.T, n *node, perTile int) {
	t.Helper()
	if n.isLeaf() {
		if n.planned > perTile {
			t.Errorf("leaf %s plans %d splats at depth %d", n.id, n.planned, n.depth)
		}
		return
	}
	left, right := n.children[0], n.children[1]
	if left.planned+right.planned != n.planned || left.planned < 1 || right.planned < 1 {
		t.Errorf("node %s plans %d splats, its children %d and %d", n.id, n.planned, left.planned, right.planned)
	}
	if left.id != n.id+"0" || right.id != n.id+"1" || left.depth != n.depth+1 || right.depth != n.depth+1 {
		t.Errorf("node %s has children %s and %s at depths %d and %d", n.id, left.id, right.id, left.depth, right.depth)
	}
	checkPlan(t, left, perTile)
	checkPlan(t, right, perTile)
}

func TestBuildPlanBalancesUniformSplats(t *testing.T) {
	samples := uniformSamples(1, 16_000, geom.Vec3{X: 40, Y: 20, Z: 10})
	plan := buildPlan(slices.Clone(samples), len(samples), 1100)
	checkPlan(t, plan, 1100)
	leaves := plan.leaves(nil)
	if len(leaves) != 16 {
		t.Fatalf("%d leaves, want 16", len(leaves))
	}
	for i, leaf := range leaves {
		if leaf.depth != 4 || leaf.planned < 900 || leaf.planned > 1100 {
			t.Errorf("leaf %d, %s, plans %d splats at depth %d", i, leaf.id, leaf.planned, leaf.depth)
		}
		if i > 0 && leaf.id <= leaves[i-1].id {
			t.Errorf("leaf %s comes after %s", leaf.id, leaves[i-1].id)
		}
	}
	// The longest side has the largest variance: the root splits it in half.
	if plan.axis != 0 || plan.split < 19 || plan.split > 21 {
		t.Errorf("the root splits axis %d at %v, want X at about 20", plan.axis, plan.split)
	}
}

func TestBuildPlanScalesSamplesToTheSplats(t *testing.T) {
	samples := uniformSamples(2, 10_000, geom.Vec3{X: 10, Y: 10, Z: 10})
	const count = 1_000_000
	plan := buildPlan(samples, count, 20_000)
	checkPlan(t, plan, 20_000)
	if leaves := plan.leaves(nil); len(leaves) != 64 {
		t.Errorf("%d leaves, want 64", len(leaves))
	}
}

// TestBuildPlanGoesDeeperForFaintSplats plans a scene where most splats are
// faint and packed in one corner. Splits halve the weight, so the corner
// gets a deeper subtree than balanced splits would need.
func TestBuildPlanGoesDeeperForFaintSplats(t *testing.T) {
	samples := uniformSamples(3, 2000, geom.Vec3{X: 10, Y: 1, Z: 1})
	for _, s := range uniformSamples(4, 18_000, geom.Vec3{X: 1, Y: 1, Z: 1}) {
		samples = append(samples, sample{center: s.center.Add(geom.Vec3{X: 10}), weight: 0.001})
	}
	plan := buildPlan(samples, len(samples), 1000)
	checkPlan(t, plan, 1000)
	deepest := 0
	for _, leaf := range plan.leaves(nil) {
		deepest = max(deepest, leaf.depth)
	}
	// Balanced splits would need 5 levels for 20 tiles.
	if deepest <= 5 {
		t.Errorf("the deepest leaf is %d levels deep, want more than 5", deepest)
	}
}

// TestBuildPlanStopsAtTheDepthLimit plans samples whose weights double from
// one to the next, so that each split cuts off only the heaviest ones.
func TestBuildPlanStopsAtTheDepthLimit(t *testing.T) {
	samples := make([]sample, 200)
	for i := range samples {
		samples[i] = sample{center: geom.Vec3{X: float64(i)}, weight: math.Ldexp(1, i)}
	}
	plan := buildPlan(samples, 1_000_000_000, 1)
	deepest := 0
	for _, leaf := range plan.leaves(nil) {
		deepest = max(deepest, leaf.depth)
		if len(leaf.id) != leaf.depth+1 {
			t.Errorf("leaf %s is %d levels deep", leaf.id, leaf.depth)
		}
	}
	if deepest != maxPlanDepth {
		t.Errorf("the deepest leaf is %d levels deep, want %d", deepest, maxPlanDepth)
	}
}

func TestBuildPlanWeighsTheSplats(t *testing.T) {
	// Light samples spread far along X, heavy ones along Y.
	var samples []sample
	for i := range 1000 {
		samples = append(samples, sample{center: geom.Vec3{X: float64(i%100) - 50}, weight: 0.01})
		samples = append(samples, sample{center: geom.Vec3{Y: float64(i%10) * 2}, weight: 1})
	}
	plan := buildPlan(samples, len(samples), 1000)
	// Weighted, Y varies most: (0..18 by 2) has a variance of 33, X's 833
	// counts a hundredth as much. Half the weight lies at Y ≤ 8.
	if plan.axis != 1 || plan.split != 8 {
		t.Errorf("the root splits axis %d at %v, want Y at 8", plan.axis, plan.split)
	}
}

func TestBuildPlanSplitsTiesEvenly(t *testing.T) {
	// Half the samples lie on one plane, which the split goes through.
	var samples []sample
	for i := range 1000 {
		samples = append(samples, sample{center: geom.Vec3{X: 5, Y: float64(i) * 1e-3}, weight: 1})
		samples = append(samples, sample{center: geom.Vec3{X: 10 + float64(i)}, weight: 1})
	}
	plan := buildPlan(samples, len(samples), 1000)
	if plan.axis != 0 || plan.split != 5 {
		t.Fatalf("the root splits axis %d at %v, want X at 5", plan.axis, plan.split)
	}
	// The samples on the plane come first, and alternate between the sides
	// while both are equal.
	if l, r := plan.children[0].planned, plan.children[1].planned; l != 500 || r != 1500 {
		t.Errorf("the children plan %d and %d splats, want 500 and 1500", l, r)
	}
	// Splats on the plane go to the side planned to hold fewer.
	if leaf := plan.leafOf(geom.Vec3{X: 5}); leaf != plan.children[0].leafOf(geom.Vec3{X: 5}) {
		t.Errorf("a splat on the split plane went to leaf %s", leaf.id)
	}
}

func TestBuildPlanStopsWhenSamplesCannotSplit(t *testing.T) {
	samples := make([]sample, 100)
	for i := range samples {
		samples[i] = sample{center: geom.Vec3{X: 1, Y: 2, Z: 3}, weight: 1}
	}
	for name, plan := range map[string]*node{
		"same position": buildPlan(samples, 1_000_000, 1000),
		"one sample":    buildPlan(samples[:1], 1_000_000, 1000),
		"fits a tile":   buildPlan(uniformSamples(3, 100, geom.Vec3{X: 1, Y: 1, Z: 1}), 1000, 1000),
	} {
		if !plan.isLeaf() || plan.id != "0" || plan.depth != 0 {
			t.Errorf("%s: the plan split, %+v", name, plan)
		}
	}
}

// TestLeafOfFollowsTheSplits checks that leafOf returns, for random points,
// the leaf whose region holds them.
func TestLeafOfFollowsTheSplits(t *testing.T) {
	plan := buildPlan(uniformSamples(4, 5000, geom.Vec3{X: 10, Y: 30, Z: 5}), 5000, 300)
	rng := rand.New(rand.NewPCG(6, 7))
	for range 5000 {
		p := geom.Vec3{X: rng.Float64()*14 - 2, Y: rng.Float64()*34 - 2, Z: rng.Float64()*9 - 2}
		leaf := plan.leafOf(p)
		for n := plan; !n.isLeaf(); {
			side := 0
			if coordinate(p, n.axis) > n.split {
				side = 1
			}
			n = n.children[side]
			if !strings.HasPrefix(leaf.id, n.id) {
				t.Fatalf("point %+v is beyond the split of node %s, but went to leaf %s", p, n.id, leaf.id)
			}
		}
	}
}

func TestWeightedMedian(t *testing.T) {
	samples := []sample{
		{center: geom.Vec3{Z: 3}, weight: 1},
		{center: geom.Vec3{Z: 1}, weight: 1},
		{center: geom.Vec3{Z: 2}, weight: 5},
		{center: geom.Vec3{Z: 4}, weight: 1},
	}
	if got := weightedMedian(samples, 2); got != 2 {
		t.Errorf("weighted median = %v, want 2", got)
	}
	for i, want := range []float64{1, 2, 3, 4} {
		if samples[i].center.Z != want {
			t.Errorf("sample %d is at %v, want the samples sorted", i, samples[i].center.Z)
		}
	}
	// The heaviest sample can be the last.
	samples[3].weight = 10
	if got := weightedMedian(samples, 2); got != 4 {
		t.Errorf("weighted median = %v, want 4", got)
	}
	// Unusable weights fall back to the middle sample.
	for i := range samples {
		samples[i].weight = 0
	}
	if got := weightedMedian(samples, 2); got != 3 {
		t.Errorf("median without weights = %v, want 3", got)
	}
}

func TestAxisStatsSkipsUnusableWeights(t *testing.T) {
	var s axisStats
	for _, v := range []float64{1, 2, 3} {
		s.add(v, 2)
	}
	s.add(100, 0)
	s.add(100, -1)
	s.add(100, math.Inf(1))
	if !closeTo(s.variance(), 2.0/3) || !closeTo(s.mean, 2) {
		t.Errorf("mean %v and variance %v, want 2 and 2/3", s.mean, s.variance())
	}
	if (axisStats{}).variance() != 0 {
		t.Error("an empty variance is not 0")
	}
}
