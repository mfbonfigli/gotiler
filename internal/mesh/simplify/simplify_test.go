package simplify

import (
	"math"
	"testing"

	"github.com/mfbonfigli/gotiler/v3/internal/mesh"
)

// grid returns a square of n by n cells in the plane z = height(x, y), two
// triangles a cell. The cells left of seam use the material 0 and the others
// the material 1, each with texture coordinates of its own, so that a seam
// runs along x = seam. A seam of n or more leaves one material.
func grid(n, seam int, height func(x, y float64) float64) []mesh.Triangle {
	var out []mesh.Triangle
	corner := func(x, y int, material uint32) ([3]float32, [2]float32) {
		p := [3]float32{float32(x), float32(y), float32(height(float64(x), float64(y)))}
		uv := [2]float32{float32(x) / float32(n), float32(y) / float32(n)}
		if material == 1 {
			uv[0] = float32(x-seam) / float32(n)
		}
		return p, uv
	}
	for y := range n {
		for x := range n {
			material := uint32(0)
			if x >= seam {
				material = 1
			}
			for _, cells := range [2][3][2]int{{{0, 0}, {1, 0}, {1, 1}}, {{0, 0}, {1, 1}, {0, 1}}} {
				t := mesh.Triangle{Material: material}
				for c, d := range cells {
					t.P[c], t.UV[c] = corner(x+d[0], y+d[1], material)
				}
				out = append(out, t)
			}
		}
	}
	return out
}

func flat(x, y float64) float64 { return 0 }

func totalArea(m *mesh.Indexed) float64 {
	var area float64
	for _, t := range m.Soup() {
		area += t.Area()
	}
	return area
}

func TestSimplifyFlatGridReachesTheTargetWithoutError(t *testing.T) {
	m := mesh.Weld(grid(20, 99, flat))

	err := Simplify(m, nil, nil, 200, 0)

	if len(m.Triangles) > 200 {
		t.Errorf("%d triangles left, want at most 200", len(m.Triangles))
	}
	if err > 1e-6 {
		t.Errorf("error = %v on a flat grid", err)
	}
	if area := totalArea(m); math.Abs(area-400) > 1e-3 {
		t.Errorf("area = %v, want 400: the border moved or triangles folded", area)
	}
	for _, tr := range m.Soup() {
		if n := mesh.Normal(tr.P[0], tr.P[1], tr.P[2]); n[2] <= 0 {
			t.Fatalf("triangle %v flipped", tr.P)
		}
	}
}

func TestSimplifyKeepsLockedPositions(t *testing.T) {
	m := mesh.Weld(grid(16, 99, flat))
	locked := make([]bool, len(m.Positions))
	edge := 0
	for i, p := range m.Positions {
		if p[0] == 0 || p[0] == 16 || p[1] == 0 || p[1] == 16 {
			locked[i] = true
			edge++
		}
	}

	Simplify(m, locked, nil, 1, 0)

	used := map[[3]float32]bool{}
	for _, tr := range m.Soup() {
		for _, p := range tr.P {
			used[p] = true
		}
	}
	kept := 0
	for i, p := range m.Positions {
		if locked[i] && used[p] {
			kept++
		}
	}
	if kept != edge {
		t.Errorf("%d of %d locked positions are still used", kept, edge)
	}
	// Only the border is left: its 64 positions need at least 62 triangles.
	if len(m.Triangles) > 70 {
		t.Errorf("%d triangles left around a locked border of 64 positions", len(m.Triangles))
	}
	if area := totalArea(m); math.Abs(area-256) > 1e-3 {
		t.Errorf("area = %v, want 256", area)
	}
}

func TestSimplifyKeepsSeamsAndTextureCoordinates(t *testing.T) {
	m := mesh.Weld(grid(20, 8, flat))

	Simplify(m, nil, nil, 150, 0)

	if len(m.Triangles) > 150 {
		t.Errorf("%d triangles left, want at most 150", len(m.Triangles))
	}
	var area [2]float64
	for _, tr := range m.Soup() {
		area[tr.Material] += tr.Area()
		for c, p := range tr.P {
			// Each side keeps the texture coordinates of its own material,
			// and no triangle crosses the seam.
			u := p[0] / 20
			if tr.Material == 1 {
				u = (p[0] - 8) / 20
			}
			if tr.UV[c] != [2]float32{u, p[1] / 20} {
				t.Fatalf("corner %v of material %d has texture coordinates %v", p, tr.Material, tr.UV[c])
			}
			if (tr.Material == 0 && p[0] > 8) || (tr.Material == 1 && p[0] < 8) {
				t.Fatalf("a triangle of material %d reaches x = %v, across the seam", tr.Material, p[0])
			}
		}
	}
	if math.Abs(area[0]-160) > 1e-3 || math.Abs(area[1]-240) > 1e-3 {
		t.Errorf("areas = %v, want 160 and 240", area)
	}
}

func TestSimplifyFollowsTheSurface(t *testing.T) {
	// A ridge along x = 10: the flat sides simplify, the ridge line stays.
	ridge := func(x, y float64) float64 { return 5 - math.Abs(x-10)/2 }
	m := mesh.Weld(grid(20, 99, ridge))

	err := Simplify(m, nil, nil, 100, 0)

	if len(m.Triangles) > 100 {
		t.Errorf("%d triangles left, want at most 100", len(m.Triangles))
	}
	if err > 1e-3 {
		t.Errorf("error = %v, though the surface is two planes", err)
	}
	for _, tr := range m.Soup() {
		for _, p := range tr.P {
			if want := float32(ridge(float64(p[0]), float64(p[1]))); p[2] != want {
				t.Fatalf("position %v left the surface", p)
			}
		}
	}
}

func TestSimplifyIsDeterministic(t *testing.T) {
	bumps := func(x, y float64) float64 { return math.Sin(x/3) * math.Cos(y/2) }
	a, b := mesh.Weld(grid(24, 11, bumps)), mesh.Weld(grid(24, 11, bumps))

	errA, errB := Simplify(a, nil, nil, 300, 0), Simplify(b, nil, nil, 300, 0)

	if errA != errB || len(a.Triangles) != len(b.Triangles) {
		t.Fatalf("runs differ: %v with %d triangles, %v with %d", errA, len(a.Triangles), errB, len(b.Triangles))
	}
	for i := range a.Triangles {
		if a.Triangles[i] != b.Triangles[i] {
			t.Fatalf("triangle %d differs between runs", i)
		}
	}
	if errA <= 0 {
		t.Error("a curved surface simplified without error")
	}
}

// shifted returns triangles moved by the given offset.
func shifted(triangles []mesh.Triangle, x, y, z float32) []mesh.Triangle {
	out := make([]mesh.Triangle, len(triangles))
	for i, t := range triangles {
		for c := range 3 {
			t.P[c][0] += x
			t.P[c][1] += y
			t.P[c][2] += z
		}
		out[i] = t
	}
	return out
}

func TestDropSmallRemovesPartsBelowTheSizeUnlessKept(t *testing.T) {
	// A square of 20 units and two specks of 1 unit, one of them far above.
	triangles := grid(20, 99, flat)
	triangles = append(triangles, shifted(grid(1, 99, flat), 40, 0, 0)...)
	triangles = append(triangles, shifted(grid(1, 99, flat), 5, 5, 30)...)
	m := mesh.WeldPositions(triangles)
	keep := make([]bool, len(m.Positions))
	for i, p := range m.Positions {
		keep[i] = p[2] == 30 && p[0] == 5 && p[1] == 5
	}

	removed := DropSmall(m, keep, 3)

	if removed != 2 || len(m.Triangles) != 802 {
		t.Fatalf("removed %d triangles and left %d, want 2 and 802", removed, len(m.Triangles))
	}
	for _, tr := range m.Soup() {
		if tr.P[0][0] >= 40 {
			t.Fatalf("the speck at x = 40 is still there: %v", tr.P)
		}
	}
	if removed := DropSmall(m, nil, 3); removed != 2 {
		t.Errorf("without the kept position, %d triangles went, want the 2 of the other speck", removed)
	}
	if removed := DropSmall(m, nil, 100); removed != 800 || len(m.Triangles) != 0 {
		t.Errorf("a size above that of the square removed %d triangles", removed)
	}
}

func TestClusterReachesTheTargetOnAnySurface(t *testing.T) {
	// Steep ridges, one every two units, of 3,200 triangles.
	ridges := func(x, y float64) float64 { return 20 * math.Abs(math.Mod(x, 2)-1) }
	m := mesh.Weld(grid(40, 99, ridges))
	const target = 100

	moved := Cluster(m, nil, nil, target, 100)

	if len(m.Triangles) > target || len(m.Triangles) < target/4 {
		t.Errorf("%d triangles left, want close under %d", len(m.Triangles), target)
	}
	if moved <= 0 || moved > 30 {
		t.Errorf("positions moved up to %v, want within the size of a cell", moved)
	}
	seen := map[[3][3]float32]bool{}
	for _, tr := range m.Soup() {
		if tr.Degenerate() {
			t.Fatalf("a triangle without surface is left: %v", tr.P)
		}
		for _, p := range tr.P {
			// Positions are positions of the input, on the surface.
			if want := float32(ridges(float64(p[0]), float64(p[1]))); p[2] != want {
				t.Fatalf("position %v is not on the surface", p)
			}
		}
		if seen[tr.P] {
			t.Fatalf("the triangle %v is there twice", tr.P)
		}
		seen[tr.P] = true
	}
}

func TestClusterKeepsLockedPositionsAndSmallMeshes(t *testing.T) {
	m := mesh.Weld(grid(16, 99, flat))
	locked := make([]bool, len(m.Positions))
	border := map[[3]float32]bool{}
	for i, p := range m.Positions {
		if p[0] == 0 || p[0] == 16 || p[1] == 0 || p[1] == 16 {
			locked[i] = true
			border[p] = true
		}
	}

	Cluster(m, locked, nil, 80, 100)

	if len(m.Triangles) > 80 {
		t.Errorf("%d triangles left, want at most 80", len(m.Triangles))
	}
	for _, tr := range m.Soup() {
		for _, p := range tr.P {
			delete(border, p)
		}
	}
	if len(border) != 0 {
		t.Errorf("%d locked positions are no longer used", len(border))
	}
	small := mesh.Weld(grid(2, 99, flat))
	if moved := Cluster(small, nil, nil, 100, 100); moved != 0 || len(small.Triangles) != 8 {
		t.Errorf("a mesh under its target changed: %d triangles, moved %v", len(small.Triangles), moved)
	}
	// Cells too small to reach the target still merge what they can.
	limited := mesh.Weld(grid(16, 99, flat))
	moved := Cluster(limited, nil, nil, 10, 2)
	if n := len(limited.Triangles); n >= 512 || n < 64 || moved > 2*math.Sqrt(3) {
		t.Errorf("cells of 2 units left %d triangles of 512 and moved positions by %v", n, moved)
	}
}
