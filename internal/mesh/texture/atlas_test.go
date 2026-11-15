package texture

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/mfbonfigli/gotiler/v3/internal/mesh"
)

// numbered returns an image whose pixels each have a color of their own:
// red is the column, green the row and blue the given tag.
func numbered(width, height int, tag uint8) *Image {
	img := NewImage(width, height)
	for y := range height {
		for x := range width {
			copy(img.Pix[(y*width+x)*channels:], []byte{uint8(x), uint8(y), tag})
		}
	}
	return img
}

func pixel(img *Image, u, v float64) [3]uint8 {
	x := min(int(u*float64(img.Width)), img.Width-1)
	y := min(int(v*float64(img.Height)), img.Height-1)
	p := img.Pix[(y*img.Width+x)*channels:]
	return [3]uint8{p[0], p[1], p[2]}
}

// quad returns two triangles that cover the rectangle of texture coordinates
// from u0, v0 to u1, v1 of a material, at the given height.
func quad(u0, v0, u1, v1 float32, material uint32, z float32) []mesh.Triangle {
	corners := [4][2]float32{{u0, v0}, {u1, v0}, {u1, v1}, {u0, v1}}
	var out []mesh.Triangle
	for _, index := range [2][3]int{{0, 1, 2}, {0, 2, 3}} {
		t := mesh.Triangle{Material: material}
		for c, i := range index {
			t.UV[c] = corners[i]
			t.P[c] = [3]float32{corners[i][0], corners[i][1], z}
		}
		out = append(out, t)
	}
	return out
}

// centroids returns the texture coordinates of the middle of each triangle.
func centroids(m *mesh.Indexed) [][2]float64 {
	var out [][2]float64
	for _, t := range m.Soup() {
		var c [2]float64
		for _, uv := range t.UV {
			c[0] += float64(uv[0]) / 3
			c[1] += float64(uv[1]) / 3
		}
		out = append(out, c)
	}
	return out
}

func TestPackKeepsTheColorsUnderEachTriangle(t *testing.T) {
	sources := []Source{numbered(64, 64, 1), numbered(32, 16, 2)}
	triangles := append(quad(0.25, 0.25, 0.75, 0.5, 0, 0), quad(0.5, 0, 1, 1, 1, 1)...)
	m := mesh.Weld(triangles)
	before := centroids(m)

	atlas, err := Pack(m, sources, Options{Scale: 1, MaxSize: 256})

	if err != nil {
		t.Fatal(err)
	}
	if atlas.Width%4 != 0 || atlas.Height%4 != 0 {
		t.Errorf("atlas is %dx%d, not multiples of 4", atlas.Width, atlas.Height)
	}
	// The charts take 36x20 and 20x20 pixels with their padding.
	if atlas.Width*atlas.Height > 1500 {
		t.Errorf("atlas is %dx%d for charts of 32x16 and 16x16 pixels", atlas.Width, atlas.Height)
	}
	after := centroids(m)
	for i, tr := range triangles {
		want := pixel(sources[tr.Material].(*Image), before[i][0], before[i][1])
		if got := pixel(atlas, after[i][0], after[i][1]); got != want {
			t.Errorf("triangle %d shows %v, want %v", i, got, want)
		}
	}
	for _, v := range m.Vertices {
		if v.Material != 0 || v.UV[0] < 0 || v.UV[0] > 1 || v.UV[1] < 0 || v.UV[1] > 1 {
			t.Fatalf("vertex %+v does not refer to the atlas", v)
		}
	}
}

func TestPackReadsUpwardTextureCoordinatesAndRepeats(t *testing.T) {
	source := numbered(32, 32, 7)
	// The second quad lies one repeat of the image to the right and above.
	triangles := append(quad(0, 0.5, 0.25, 1, 0, 0), quad(1.5, 1.25, 1.75, 1.5, 0, 1)...)
	m := mesh.Weld(triangles)
	before := centroids(m)

	atlas, err := Pack(m, []Source{source}, Options{Scale: 1, MaxSize: 256, VUp: true})

	if err != nil {
		t.Fatal(err)
	}
	after := centroids(m)
	for i := range triangles {
		u, v := before[i][0], before[i][1]
		want := pixel(source, u-math.Floor(u), 1-(v-math.Floor(v)))
		if got := pixel(atlas, after[i][0], after[i][1]); got != want {
			t.Errorf("triangle %d shows %v, want %v", i, got, want)
		}
	}
}

func TestPackScalesDownAndAverages(t *testing.T) {
	source := numbered(64, 64, 0)
	m := mesh.Weld(quad(0, 0, 1, 1, 0, 0))

	atlas, err := Pack(m, []Source{source}, Options{Scale: 0.5, MaxSize: 256})

	if err != nil {
		t.Fatal(err)
	}
	if atlas.Width != 36 || atlas.Height != 36 {
		t.Fatalf("atlas is %dx%d, want 36x36 for a chart of 32x32 with its padding", atlas.Width, atlas.Height)
	}
	// The pixel at 10, 20 of the chart averages columns 20-21 and rows 40-41.
	got := atlas.Pix[((padding+20)*atlas.Width+padding+10)*channels:][:3]
	if !bytes.Equal(got, []byte{21, 41, 0}) && !bytes.Equal(got, []byte{20, 40, 0}) {
		t.Errorf("pixel = %v, want the average of columns 20-21 and rows 40-41", got)
	}
}

func TestPackShrinksChartsToFitTheLargestAtlas(t *testing.T) {
	m := mesh.Weld(quad(0, 0, 1, 1, 0, 0))

	atlas, err := Pack(m, []Source{numbered(200, 200, 0)}, Options{Scale: 1, MaxSize: 64})

	if err != nil {
		t.Fatal(err)
	}
	if atlas.Width > 64 || atlas.Height > 64 {
		t.Errorf("atlas is %dx%d, over the limit of 64", atlas.Width, atlas.Height)
	}
}

func TestPackManySmallChartsInASquareAtlas(t *testing.T) {
	// Charts of a pixel each, far apart in the source: they pack into about
	// as many rows as columns, not into one tall column.
	var triangles []mesh.Triangle
	for i := range 400 {
		u := float32(i%20) / 20
		v := float32(i/20) / 20
		triangles = append(triangles, mesh.Triangle{
			P:  [3][3]float32{{float32(i), 0, 0}, {float32(i) + 1, 0, 0}, {float32(i), 1, 0}},
			UV: [3][2]float32{{u, v}, {u + 0.001, v}, {u, v + 0.001}},
		})
	}
	m := mesh.Weld(triangles)

	atlas, err := Pack(m, []Source{numbered(200, 200, 0)}, Options{Scale: 1, MaxSize: 128})

	if err != nil {
		t.Fatal(err)
	}
	// 400 charts of 5x5 pixels with their padding take 10,000 pixels.
	if atlas.Width*atlas.Height > 13000 || max(atlas.Width, atlas.Height) > 2*min(atlas.Width, atlas.Height) {
		t.Errorf("atlas is %dx%d, want close to a square of 100 pixels", atlas.Width, atlas.Height)
	}
}

func TestConvertAndRawFilesRoundTrip(t *testing.T) {
	dir := t.TempDir()
	original := image.NewNRGBA(image.Rect(0, 0, 5, 3))
	for y := range 3 {
		for x := range 5 {
			original.Set(x, y, color.NRGBA{R: uint8(10 * x), G: uint8(20 * y), B: 200, A: 255})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, original); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(dir, "in.png")
	if err := os.WriteFile(source, encoded.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	raw := filepath.Join(dir, "in.raw")
	if err := Convert(source, raw); err != nil {
		t.Fatal(err)
	}

	file, err := Open(raw)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close() //nolint:errcheck
	if w, h := file.Size(); w != 5 || h != 3 {
		t.Fatalf("size = %dx%d", w, h)
	}
	region := make([]byte, 2*2*channels)
	if err := file.Read(3, 1, 2, 2, region); err != nil {
		t.Fatal(err)
	}
	if want := []byte{30, 20, 200, 40, 20, 200, 30, 40, 200, 40, 40, 200}; !bytes.Equal(region, want) {
		t.Errorf("region = %v, want %v", region, want)
	}
	if err := file.Read(4, 2, 2, 2, region); err == nil {
		t.Error("a region outside the image was read")
	}

	whole, err := ReadFile(raw)
	if err != nil {
		t.Fatal(err)
	}
	copyPath := filepath.Join(dir, "copy.raw")
	if err := whole.WriteFile(copyPath); err != nil {
		t.Fatal(err)
	}
	again, err := ReadFile(copyPath)
	if err != nil || !bytes.Equal(again.Pix, whole.Pix) {
		t.Errorf("the copy differs: %v", err)
	}
	var out bytes.Buffer
	if err := whole.EncodeJPEG(&out, 90); err != nil {
		t.Fatal(err)
	}
	if decoded, err := jpeg.Decode(&out); err != nil || decoded.Bounds().Dx() != 5 {
		t.Errorf("the JPEG does not decode: %v", err)
	}
}
