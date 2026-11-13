package lod

import (
	"cmp"
	"math"
	"math/rand/v2"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/mfbonfigli/gotiler/v3/internal/geom"
	"github.com/mfbonfigli/gotiler/v3/internal/radix"
	"github.com/mfbonfigli/gotiler/v3/internal/splat"
)

func simplify(t *testing.T, splats []splat.Splat, target int) []splat.Splat {
	t.Helper()
	out, err := Simplify(splats, target)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func sourceIndices(splats []splat.Splat) []int {
	indices := make([]int, len(splats))
	for i, s := range splats {
		indices[i] = s.SourceIndex
	}
	return indices
}

// assertOrderedSubset checks that out is ordered by SourceIndex and holds
// each source index of in at most once.
func assertOrderedSubset(t *testing.T, out, in []splat.Splat) {
	t.Helper()
	inputs := map[int]bool{}
	for _, s := range in {
		inputs[s.SourceIndex] = true
	}
	for i, s := range out {
		if !inputs[s.SourceIndex] {
			t.Fatalf("splat %d has source index %d, which is not in the input", i, s.SourceIndex)
		}
		if i > 0 && s.SourceIndex <= out[i-1].SourceIndex {
			t.Fatalf("splat %d has source index %d after %d", i, s.SourceIndex, out[i-1].SourceIndex)
		}
	}
}

func TestSimplifyReachesTarget(t *testing.T) {
	grid := gridSplats(100)
	// 5000 takes one pass, 2000 three.
	for _, target := range []int{5000, 2000} {
		out := simplify(t, grid, target)
		if len(out) != target {
			t.Errorf("simplified %d splats to %d, want %d", len(grid), len(out), target)
		}
		assertOrderedSubset(t, out, grid)
	}
}

func TestSimplifyMergesNearbyPairs(t *testing.T) {
	splats := []splat.Splat{
		roundSplat(0, geom.Vec3{X: 0}),
		roundSplat(1, geom.Vec3{X: 0.1}),
		roundSplat(2, geom.Vec3{X: 10}),
		roundSplat(3, geom.Vec3{X: 10.1}),
	}
	out := simplify(t, splats, 2)
	if got := sourceIndices(out); !reflect.DeepEqual(got, []int{0, 2}) {
		t.Fatalf("source indices = %v, want [0 2]", got)
	}
	assertVec3(t, "first center", out[0].Center, geom.Vec3{X: 0.05})
	assertVec3(t, "second center", out[1].Center, geom.Vec3{X: 10.05})
}

func TestSimplifyMergesTheCheapestPairFirst(t *testing.T) {
	splats := []splat.Splat{
		roundSplat(0, geom.Vec3{X: 0.5}),
		roundSplat(1, geom.Vec3{X: 1}),
		roundSplat(2, geom.Vec3{X: 0}),
		roundSplat(3, geom.Vec3{X: 0}),
		roundSplat(4, geom.Vec3{X: 100}),
	}
	// The copies at 0 merge for free.
	out := simplify(t, splats, 4)
	if got := sourceIndices(out); !reflect.DeepEqual(got, []int{0, 1, 2, 4}) {
		t.Fatalf("source indices = %v, want [0 1 2 4]", got)
	}
	assertVec3(t, "merged center", out[2].Center, geom.Vec3{})
}

func TestSimplifyKeepsDistantSplatsApart(t *testing.T) {
	splats := []splat.Splat{
		roundSplat(0, geom.Vec3{X: 0}),
		roundSplat(1, geom.Vec3{X: 10}),
		roundSplat(2, geom.Vec3{X: 20}),
		roundSplat(3, geom.Vec3{X: 30}),
	}
	if out := simplify(t, splats, 1); !reflect.DeepEqual(out, splats) {
		t.Fatalf("simplified splats 10 units apart to %v, want them unchanged", out)
	}
}

func TestSimplifyPrefersSimilarColors(t *testing.T) {
	red, blue := colorSH0(1, 0, 0), colorSH0(0, 0, 1)
	splats := make([]splat.Splat, 4)
	for i, color := range []geom.Vec3{red, blue, red, blue} {
		splats[i] = roundSplat(i, geom.Vec3{})
		splats[i].SH0 = color
	}
	out := simplify(t, splats, 2)
	if len(out) != 2 {
		t.Fatalf("simplified to %d splats, want 2", len(out))
	}
	assertVec3(t, "first color", out[0].SH0, red)
	assertVec3(t, "second color", out[1].SH0, blue)
}

func TestSimplifyReturnsSmallInputsSorted(t *testing.T) {
	splats := randomSplats(1, 50, 3)
	slices.Reverse(splats)
	for _, target := range []int{50, 80} {
		out := simplify(t, splats, target)
		want := slices.Clone(splats)
		slices.Reverse(want)
		if !reflect.DeepEqual(out, want) {
			t.Fatalf("target %d: got %v, want the input in source order", target, out)
		}
	}
	if out := simplify(t, nil, 0); len(out) != 0 {
		t.Errorf("simplified no splats to %v", out)
	}
}

// TestSimplifyKeepsSplatsValid simplifies random splats of degree 3, and
// checks that the result is valid and takes its higher-degree coefficients
// from the input.
func TestSimplifyKeepsSplatsValid(t *testing.T) {
	splats := randomSplats(2, 5000, 15)
	coefficients := map[*geom.Vec3]bool{}
	for _, s := range splats {
		coefficients[&s.SHRest[0]] = true
	}
	out := simplify(t, splats, 2500)
	if len(out) < 2500 || len(out) > 2600 {
		t.Errorf("simplified to %d splats, want about 2500", len(out))
	}
	assertOrderedSubset(t, out, splats)
	for i, s := range out {
		if err := s.Validate(); err != nil {
			t.Fatalf("splat %d: %v", i, err)
		}
		if len(s.SHRest) != 15 || !coefficients[&s.SHRest[0]] {
			t.Fatalf("splat %d has coefficients that are not those of an input splat", i)
		}
	}
}

func TestSimplifyLeavesInputUnchanged(t *testing.T) {
	splats := randomSplats(3, 2000, 3)
	before := make([]splat.Splat, len(splats))
	for i, s := range splats {
		before[i] = s
		before[i].SHRest = slices.Clone(s.SHRest)
	}
	simplify(t, splats, 700)
	if !reflect.DeepEqual(splats, before) {
		t.Fatal("Simplify changed its input")
	}
}

// TestSimplifyIsDeterministic runs Simplify concurrently, so that the calls
// share pooled workspaces, and checks that they all give the same result.
func TestSimplifyIsDeterministic(t *testing.T) {
	splats := randomSplats(4, 3000, 0)
	want := simplify(t, splats, 1000)
	var wg sync.WaitGroup
	results := make([][]splat.Splat, 8)
	errs := make([]error, len(results))
	for i := range results {
		wg.Go(func() {
			// Other sizes resize the pooled buffers in between.
			if _, err := Simplify(randomSplats(uint64(10+i), 500*(i+1), 3), 100*(i+1)); err != nil {
				errs[i] = err
				return
			}
			results[i], errs[i] = Simplify(splats, 1000)
		})
	}
	wg.Wait()
	for i, got := range results {
		if errs[i] != nil {
			t.Fatal(errs[i])
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("call %d gave a different result", i)
		}
	}
}

func TestSimplifyErrors(t *testing.T) {
	if _, err := Simplify(randomSplats(5, 10, 0), 0); err == nil {
		t.Error("simplifying to no splats succeeded")
	}

	// Splats with incomplete spherical harmonics cannot be merged into valid
	// splats.
	splats := []splat.Splat{roundSplat(0, geom.Vec3{}), roundSplat(1, geom.Vec3{})}
	for i := range splats {
		splats[i].SHRest = make([]geom.Vec3, 2)
	}
	if _, err := Simplify(splats, 1); err == nil || !strings.Contains(err.Error(), "spherical harmonics") {
		t.Errorf("merging invalid splats gave error %v", err)
	}
}

func TestMergeCost(t *testing.T) {
	cost := func(a, b splat.Splat) float64 {
		return mergeCost(a, b, newSplatInfo(a), newSplatInfo(b))
	}
	base := roundSplat(0, geom.Vec3{})
	displaced := roundSplat(1, geom.Vec3{X: 0.05})
	farther := roundSplat(2, geom.Vec3{X: 0.1})
	recolored := roundSplat(3, geom.Vec3{})
	recolored.SH0 = colorSH0(1, 0, 0)

	if c := cost(base, base); c != 0 {
		t.Errorf("merging copies costs %v, want 0", c)
	}
	if c := cost(base, displaced); !(c > 0 && c < cost(base, farther)) {
		t.Errorf("merging splats costs %v at 0.05 apart and %v at 0.1, want it to grow with distance", c, cost(base, farther))
	}
	assertClose(t, "color cost", cost(base, recolored), colorWeight*2*Weight(base)*base.BaseColor().Sub(recolored.BaseColor()).LengthSquared())
	assertClose(t, "symmetry", cost(base, farther), cost(farther, base))

	// Heavier splats cost more to merge.
	heavy, heavyDisplaced := base, displaced
	heavy.Opacity, heavyDisplaced.Opacity = 1, 1
	if cost(heavy, heavyDisplaced) <= cost(base, displaced) {
		t.Errorf("merging opaque splats costs %v, no more than merging translucent ones, %v", cost(heavy, heavyDisplaced), cost(base, displaced))
	}

	// Thin splats on different planes cost more to merge than on one plane.
	flat := thinSplat(0, geom.Vec3{}, geom.IdentityQuat())
	upright := thinSplat(1, geom.Vec3{}, geom.Quat{X: math.Sqrt2 / 2, W: math.Sqrt2 / 2})
	if c := cost(flat, flat); c != 0 {
		t.Errorf("merging thin copies costs %v, want 0", c)
	}
	if c := cost(flat, upright); c <= cost(base, farther) {
		t.Errorf("merging crossed thin splats costs %v, no more than merging displaced round ones, %v", c, cost(base, farther))
	}

	// Splats of zero thickness still have finite costs.
	needle := roundSplat(4, geom.Vec3{})
	needle.Scale = geom.Vec3{X: 1}
	if c := cost(needle, displaced); math.IsInf(c, 0) || math.IsNaN(c) {
		t.Errorf("merging a needle costs %v", c)
	}
}

func TestLogDeterminant(t *testing.T) {
	assertClose(t, "log det", logDeterminant(geom.Mat3{{2, 0, 0}, {0, 3, 0}, {0, 0, 4}}), math.Log(24))
	for _, m := range []geom.Mat3{{}, {{-1, 0, 0}, {0, 1, 0}, {0, 0, 1}}, {{math.NaN(), 0, 0}, {0, 1, 0}, {0, 0, 1}}} {
		assertClose(t, "floored log det", logDeterminant(m), math.Log(minDeterminant))
	}
}

func TestCellSize(t *testing.T) {
	for _, test := range []struct {
		name  string
		size  geom.Vec3
		count int
		want  float64
	}{
		{"volume", geom.Vec3{X: 8, Y: 4, Z: 2}, 8, 2},
		{"area", geom.Vec3{X: 10, Y: 0.4, Z: 10}, 25, 2},
		{"line", geom.Vec3{X: 0.1, Y: 10, Z: 0.1}, 5, 2},
		{"point", geom.Vec3{}, 5, minCellSize},
		{"floor", geom.Vec3{X: 1}, 1e9, minCellSize},
	} {
		if got := cellSize(test.size, test.count); !closeTo(got, test.want) {
			t.Errorf("%s: cell size = %v, want %v", test.name, got, test.want)
		}
	}
}

func TestMergeDistance(t *testing.T) {
	line := func(length float64) []splat.Splat {
		return []splat.Splat{roundSplat(0, geom.Vec3{}), roundSplat(1, geom.Vec3{X: length})}
	}
	// The spacing of the target splats bounds the distance...
	assertClose(t, "few splats", mergeDistance(line(100), 1000), 0.1*cellDistanceFactor)
	// ...and so does the extent of the splats.
	assertClose(t, "many splats", mergeDistance(line(100), 10), 100*extentDistanceFactor)
	assertClose(t, "coincident splats", mergeDistance(line(0), 1), minCellSize*cellDistanceFactor)
}

// TestSortEdges checks that edges sort by cost and then by their splats,
// costs that tie included, like the comparison sort they replaced.
func TestSortEdges(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 4))
	costs := []float64{0, 5e-324, 1e-9, 0.5, 1, 3, math.MaxFloat64, math.Inf(1)}
	type pair struct {
		a, b int32
		cost float64
	}
	var pairs []pair
	for a := range int32(300) {
		for b := a + 1; b < 300; b += 1 + int32(rng.IntN(20)) {
			cost := rng.Float64() * 10
			if rng.IntN(2) == 0 {
				cost = costs[rng.IntN(len(costs))]
			}
			pairs = append(pairs, pair{a, b, cost})
		}
	}
	rng.Shuffle(len(pairs), func(i, j int) { pairs[i], pairs[j] = pairs[j], pairs[i] })
	edges := make([]radix.Item, len(pairs))
	for i, p := range pairs {
		edges[i] = newEdge(p.a, p.b, p.cost)
	}
	slices.SortFunc(pairs, func(x, y pair) int {
		return cmp.Or(cmp.Compare(x.cost, y.cost), cmp.Compare(x.a, y.a), cmp.Compare(x.b, y.b))
	})
	ws := new(workspace)
	ws.sortEdges(edges)
	for i, e := range edges {
		a, b := edgeSplats(e)
		if got, want := (pair{a, b, math.Float64frombits(e.Key)}), pairs[i]; got != want {
			t.Fatalf("edge %d of %d is %+v, want %+v", i, len(edges), got, want)
		}
	}
}

func BenchmarkSimplify(b *testing.B) {
	// The grid of gosplattiler's benchmark, for comparison.
	grid := make([]splat.Splat, 20_000)
	for i := range grid {
		grid[i] = roundSplat(i, geom.Vec3{X: float64(i % 200), Y: float64((i / 200) % 100), Z: float64(i%17) * 0.01})
		grid[i].SH0 = colorSH0(float64(i%255)/255, float64((i*7)%255)/255, float64((i*13)%255)/255)
	}
	for _, bench := range []struct {
		name   string
		splats []splat.Splat
		target int
	}{
		{"grid-20k", grid, 10_000},
		{"random-40k-degree3", randomSplats(1, 40_000, 15), 20_000},
	} {
		b.Run(bench.name, func(b *testing.B) {
			b.ReportAllocs()
			var out []splat.Splat
			for b.Loop() {
				var err error
				if out, err = Simplify(bench.splats, bench.target); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(len(out)), "splats")
		})
	}
}

// TestSimplifyMergesPilesInPairs checks that splats piled up on one point
// pair up at once: a pass over 2000 of them merges nearly 1000 pairs, as
// they do not all pick the same nearest neighbors.
func TestSimplifyMergesPilesInPairs(t *testing.T) {
	splats := randomSplats(6, 2000, 0)
	for i := range splats {
		splats[i].Center = geom.Vec3{X: 1, Y: 2, Z: 3}
	}
	ws := workspaces.Get().(*workspace)
	defer ws.release()
	next, err := ws.pass(splats, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if merged := len(splats) - len(next); merged < 950 {
		t.Errorf("a pass merged %d pairs of 2000 piled splats, want nearly 1000", merged)
	}
}
