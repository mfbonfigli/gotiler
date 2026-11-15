package texture

import (
	"math"
	"testing"

	"github.com/mfbonfigli/gotiler/v3/internal/mesh"
)

// cube returns the 12 triangles of a cube of the given side, facing out.
// Each face maps to a column of its own of an image six columns wide, so
// that its red tells which face it is: 20, 60, 100, 140, 180 and 220 for
// the faces toward -x, +x, -y, +y, -z and +z.
func cube(side float32) ([]mesh.Triangle, *Image) {
	img := NewImage(60, 10)
	for y := range 10 {
		for x := range 60 {
			copy(img.Pix[(y*60+x)*channels:], []byte{uint8(20 + 40*(x/10)), 0, 0})
		}
	}
	var out []mesh.Triangle
	for face := range 6 {
		axis, positive := face/2, face%2 == 1
		u, v := (axis+1)%3, (axis+2)%3
		var corners [4][3]float32
		for i, c := range [4][2]float32{{0, 0}, {1, 0}, {1, 1}, {0, 1}} {
			corners[i][u], corners[i][v] = c[0]*side, c[1]*side
			if positive {
				corners[i][axis] = side
			}
		}
		// Seen from outside, the corners of a positive face turn
		// counterclockwise in this order, and those of a negative one the
		// other way.
		order := [2][3]int{{0, 1, 2}, {0, 2, 3}}
		if !positive {
			order = [2][3]int{{0, 2, 1}, {0, 3, 2}}
		}
		for _, index := range order {
			var t mesh.Triangle
			for c, i := range index {
				t.P[c] = corners[i]
				// The middle of the column of the face.
				t.UV[c] = [2]float32{(float32(face)*10 + 5) / 60, 0.5}
			}
			out = append(out, t)
		}
	}
	return out, img
}

func TestBakeDrawsEachSideOfASolidFromItsOwnDirection(t *testing.T) {
	detail, img := cube(8)
	surface := mesh.WeldPositions(detail)

	m, atlas, texel, err := Bake(surface, detail, []*Image{img}, BakeOptions{TexelSize: 0.25, MaxSize: 512})

	if err != nil {
		t.Fatal(err)
	}
	if texel != 0.25 {
		t.Errorf("texel = %v, want 0.25 as asked", texel)
	}
	if len(m.Triangles) != 12 || len(m.Vertices) != 24 {
		t.Fatalf("%d triangles and %d vertices, want 12 and 24: four for each face", len(m.Triangles), len(m.Vertices))
	}
	for _, tr := range m.Soup() {
		n := mesh.Normal(tr.P[0], tr.P[1], tr.P[2])
		face := 0
		for a := range 3 {
			if n[a] > 0 {
				face = 2*a + 1
			} else if n[a] < 0 {
				face = 2 * a
			}
		}
		var u, v float64
		for _, uv := range tr.UV {
			u += float64(uv[0]) / 3
			v += float64(uv[1]) / 3
		}
		got := pixel(atlas, u, v)
		if want := uint8(20 + 40*face); got[0] != want {
			t.Errorf("the face %d shows red %d, want %d", face, got[0], want)
		}
		// A face of 8 units takes 32 pixels at 4 pixels a unit.
		du := math.Abs(float64(tr.UV[1][0]-tr.UV[0][0])) * float64(atlas.Width)
		dv := math.Abs(float64(tr.UV[1][1]-tr.UV[0][1])) * float64(atlas.Height)
		if side := math.Max(du, dv); math.Abs(side-32) > 1e-3 {
			t.Errorf("an edge of the face %d spans %.3f pixels, want 32", face, side)
		}
	}
}

func TestBakeFollowsTheDetailUnderASimplerSurface(t *testing.T) {
	// The detail: a square of 16 by 16 cells with small bumps, whose image
	// grows red with x and green with y. The surface: two flat triangles.
	img := NewImage(64, 64)
	for y := range 64 {
		for x := range 64 {
			copy(img.Pix[(y*64+x)*channels:], []byte{uint8(4 * x), uint8(4 * y), 0})
		}
	}
	var detail []mesh.Triangle
	corner := func(x, y int) ([3]float32, [2]float32) {
		z := float32(0.3 * math.Sin(float64(x)) * math.Cos(float64(y)))
		return [3]float32{float32(x), float32(y), z}, [2]float32{float32(x) / 16, float32(y) / 16}
	}
	for y := range 16 {
		for x := range 16 {
			for _, cells := range [2][3][2]int{{{0, 0}, {1, 0}, {1, 1}}, {{0, 0}, {1, 1}, {0, 1}}} {
				var t mesh.Triangle
				for c, d := range cells {
					t.P[c], t.UV[c] = corner(x+d[0], y+d[1])
				}
				detail = append(detail, t)
			}
		}
	}
	flat := []mesh.Triangle{
		{P: [3][3]float32{{0, 0, 0}, {16, 0, 0}, {16, 16, 0}}},
		{P: [3][3]float32{{0, 0, 0}, {16, 16, 0}, {0, 16, 0}}},
	}

	m, atlas, _, err := Bake(mesh.WeldPositions(flat), detail, []*Image{img}, BakeOptions{TexelSize: 0.5, MaxSize: 256})

	if err != nil {
		t.Fatal(err)
	}
	// Points across the surface show the color of the detail above or
	// below them.
	tr := m.Soup()[0]
	for _, w := range [][3]float64{{0.6, 0.3, 0.1}, {0.2, 0.5, 0.3}, {0.1, 0.2, 0.7}, {0.34, 0.33, 0.33}} {
		var x, y, u, v float64
		for c := range 3 {
			x += w[c] * float64(tr.P[c][0])
			y += w[c] * float64(tr.P[c][1])
			u += w[c] * float64(tr.UV[c][0])
			v += w[c] * float64(tr.UV[c][1])
		}
		got := pixel(atlas, u, v)
		if math.Abs(float64(got[0])-255*x/16) > 12 || math.Abs(float64(got[1])-255*y/16) > 12 {
			t.Errorf("the point %.1f, %.1f shows %v, want about %.0f, %.0f", x, y, got, 255*x/16, 255*y/16)
		}
	}
}

func TestBakeMakesPixelsLargerToFitTheAtlas(t *testing.T) {
	detail, img := cube(8)

	_, atlas, texel, err := Bake(mesh.WeldPositions(detail), detail, []*Image{img}, BakeOptions{TexelSize: 0.01, MaxSize: 128})

	if err != nil {
		t.Fatal(err)
	}
	if atlas.Width > 128 || atlas.Height > 128 {
		t.Errorf("atlas is %dx%d, over the limit of 128", atlas.Width, atlas.Height)
	}
	if texel <= 0.01 {
		t.Errorf("texel = %v, want it larger than asked", texel)
	}
	if _, _, _, err := Bake(mesh.WeldPositions(detail), detail, []*Image{img}, BakeOptions{MaxSize: 128}); err == nil {
		t.Error("a pixel of no size was accepted")
	}
}
