package simplify

import (
	"math"
	"testing"

	"github.com/mfbonfigli/gotiler/v3/internal/mesh"
)

func bumps(x, y float64) float64 { return math.Sin(x/3) * math.Cos(y/2) }

func TestSimplifyStopsAtTheErrorLimit(t *testing.T) {
	free := mesh.Weld(grid(24, 99, bumps))
	freeError := Simplify(free, nil, nil, 100, 0)
	if len(free.Triangles) > 100 {
		t.Fatalf("without a limit, %d triangles are left, want at most 100", len(free.Triangles))
	}
	const limit = 0.05
	if freeError <= limit {
		t.Fatalf("without a limit the surface moved by %v: the limit of %v would not bind", freeError, limit)
	}
	m := mesh.Weld(grid(24, 99, bumps))

	moved := Simplify(m, nil, nil, 100, limit)

	if moved > limit {
		t.Errorf("the surface moved by %v, over the limit of %v", moved, limit)
	}
	if len(m.Triangles) <= len(free.Triangles) || len(m.Triangles) >= 2*24*24 {
		t.Errorf("%d triangles left: want fewer than the %d of the input, more than the %d without a limit", len(m.Triangles), 2*24*24, len(free.Triangles))
	}
}

func TestSimplifyKeepsWhatStandsOutOfAFlatSurface(t *testing.T) {
	// A flat floor with one corner of its cells raised into a spike, as a
	// post on a square: within a limit below its height, the floor goes down
	// to a few triangles and the spike stays where it is.
	spike := func(x, y float64) float64 {
		if x == 9 && y == 11 {
			return 5
		}
		return 0
	}
	m := mesh.Weld(grid(20, 99, spike))

	moved := Simplify(m, nil, nil, 2, 1)

	if moved > 1 {
		t.Errorf("the surface moved by %v, over the limit of 1", moved)
	}
	if len(m.Triangles) > 40 {
		t.Errorf("%d triangles left on a floor with one spike", len(m.Triangles))
	}
	found := false
	for _, tr := range m.Soup() {
		for _, p := range tr.P {
			if p == [3]float32{9, 11, 5} {
				found = true
			} else if p[2] != 0 {
				t.Fatalf("the floor is no longer flat at %v", p)
			}
		}
	}
	if !found {
		t.Error("the spike is gone")
	}
}

func TestSimplifyCountsWhatTheSurfaceMovedBefore(t *testing.T) {
	const limit = 0.2
	fresh := mesh.Weld(grid(24, 99, bumps))
	Simplify(fresh, nil, nil, 100, limit)

	// The same surface, already at three quarters of the limit everywhere.
	m := mesh.Weld(grid(24, 99, bumps))
	before := make([]float64, len(m.Positions))
	for i := range before {
		before[i] = 0.75 * limit
	}
	moved := Simplify(m, nil, before, 100, limit)

	if moved > limit || moved < 0.75*limit {
		t.Errorf("the surface lies %v from where it started, want from %v to %v", moved, 0.75*limit, limit)
	}
	if len(m.Triangles) <= len(fresh.Triangles) {
		t.Errorf("%d triangles left, no more than the %d of a surface that had not moved", len(m.Triangles), len(fresh.Triangles))
	}
	for _, value := range before {
		if value > limit {
			t.Fatalf("a position is recorded %v from where it started, over the limit", value)
		}
	}
}

func TestSimplifyKeepsOutlinesAndSeparatePartsWithinTheLimit(t *testing.T) {
	// Two floors, one unit apart, with a thin post of three sides between
	// them: within a small limit, the floors go down to two triangles each
	// with their outlines as they were, and stay apart.
	var triangles []mesh.Triangle
	triangles = append(triangles, grid(6, 99, flat)...)
	triangles = append(triangles, shifted(grid(6, 99, flat), 0, 0, 1)...)
	post := [3][2]float32{{2.2, 2.2}, {2.6, 2.2}, {2.4, 2.6}}
	for i := range 3 {
		a, b := post[i], post[(i+1)%3]
		triangles = append(triangles,
			mesh.Triangle{P: [3][3]float32{{a[0], a[1], 0}, {b[0], b[1], 0}, {b[0], b[1], 1}}},
			mesh.Triangle{P: [3][3]float32{{a[0], a[1], 0}, {b[0], b[1], 1}, {a[0], a[1], 1}}})
	}
	m := mesh.WeldPositions(triangles)

	Simplify(m, nil, nil, 4, 0.1)

	var low, high float64
	for _, tr := range m.Soup() {
		switch {
		case tr.P[0][2] == 0 && tr.P[1][2] == 0 && tr.P[2][2] == 0:
			low += tr.Area()
		case tr.P[0][2] == 1 && tr.P[1][2] == 1 && tr.P[2][2] == 1:
			high += tr.Area()
		}
	}
	if math.Abs(low-36) > 1e-3 || math.Abs(high-36) > 1e-3 {
		t.Errorf("the floors cover %v and %v, want 36 each: they shrank, tilted or joined", low, high)
	}
	if len(m.Triangles) != 2+2+6 {
		t.Errorf("%d triangles left, want 2 for each floor and the 6 of the post", len(m.Triangles))
	}
}
