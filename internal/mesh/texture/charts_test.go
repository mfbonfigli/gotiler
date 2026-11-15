package texture

import (
	"testing"

	"github.com/mfbonfigli/gotiler/v3/internal/mesh"
)

func TestBakeMergesSmallChartsOfRoughPartsIntoTheSurfaceAround(t *testing.T) {
	// A flat field of 30 by 30 cells with a rough patch in its middle, whose
	// corners rise and fall by two units from one to the next: its triangles
	// are too steep to face up as the field does, each a chart of its own.
	height := func(x, y int) float32 {
		if x > 12 && x < 18 && y > 12 && y < 18 {
			return float32(2 * ((x + y) % 2))
		}
		return 0
	}
	var triangles []mesh.Triangle
	for y := range 30 {
		for x := range 30 {
			for _, cells := range [2][3][2]int{{{0, 0}, {1, 0}, {1, 1}}, {{0, 0}, {1, 1}, {0, 1}}} {
				var tr mesh.Triangle
				for c, d := range cells {
					tr.P[c] = [3]float32{float32(x + d[0]), float32(y + d[1]), height(x+d[0], y+d[1])}
				}
				triangles = append(triangles, tr)
			}
		}
	}
	surface := mesh.WeldPositions(triangles)

	m, atlas, _, err := Bake(surface, triangles, []*Image{Solid([3]uint8{90, 120, 60})}, BakeOptions{TexelSize: 0.5, MaxSize: 512})

	if err != nil {
		t.Fatal(err)
	}
	// A chart of its own for each rough triangle would give each of its
	// corners a vertex of its own: about 100 more.
	if extra := len(m.Vertices) - len(m.Positions); extra > 10 {
		t.Errorf("%d vertices for %d positions: the rough patch is in charts of its own", len(m.Vertices), len(m.Positions))
	}
	// The field takes 60 by 60 pixels, and its padding.
	if atlas.Width > 72 || atlas.Height > 72 {
		t.Errorf("atlas is %dx%d for a field of 60x60 pixels", atlas.Width, atlas.Height)
	}
	if got := pixel(atlas, 0.5, 0.5); got != [3]uint8{90, 120, 60} {
		t.Errorf("the middle of the atlas shows %v", got)
	}
}
