package geotiffcolorizer

import (
	"encoding/binary"
	"math/bits"
	"testing"
)

func edgeTestOrtho(t *testing.T, tiled bool) *Orthophoto {
	t.Helper()
	// 4x4 RGB with per-pixel distinct values: pixel (x,y) = (10x, 10y, 100+x+y)
	pix := func(x, y int) []byte {
		return []byte{byte(10 * x), byte(10 * y), byte(100 + x + y)}
	}
	cfg := testTIFFConfig{
		width:      4,
		height:     4,
		samples:    3,
		bits:       []uint16{8, 8, 8},
		photo:      tiffPhotometricRGB,
		compress:   tiffCompressionNone,
		scale:      []float64{1, 1, 0},
		tiepoint:   []float64{0, 0, 0, 100, 200, 0},
		modelType:  1,
		rasterType: 1,
		epsg:       32632,
	}
	if tiled {
		cfg.tileWidth, cfg.tileHeight = 2, 2
		for _, tile := range [][2]int{{0, 0}, {2, 0}, {0, 2}, {2, 2}} {
			var block []byte
			for y := tile[1]; y < tile[1]+2; y++ {
				for x := tile[0]; x < tile[0]+2; x++ {
					block = append(block, pix(x, y)...)
				}
			}
			cfg.blocks = append(cfg.blocks, block)
		}
	} else {
		var block []byte
		for y := 0; y < 4; y++ {
			for x := 0; x < 4; x++ {
				block = append(block, pix(x, y)...)
			}
		}
		cfg.blocks = [][]byte{block}
	}
	data := buildTestGeoTIFF(t, binary.LittleEndian, cfg)
	ortho, err := OpenOrthophoto("edge.tif", data)
	if err != nil {
		t.Fatalf("OpenOrthophoto: %v", err)
	}
	return ortho
}

// Bilinear sampling must succeed for every model point inside the raster
// footprint, including the outer half-pixel band, clamping the 2x2
// neighborhood at the borders.
func TestBilinearSamplesFullRasterFootprint(t *testing.T) {
	ortho := edgeTestOrtho(t, false)
	toModel := func(i, j float64) (float64, float64) {
		return ortho.Transform.RasterToModel(i, j)
	}

	// Top-left margin: both neighbors clamp to pixel (0,0).
	mx, my := toModel(0.2, 0.2)
	c, ok, err := ortho.SampleModel(mx, my, SampleBilinear)
	if err != nil || !ok {
		t.Fatalf("top-left margin: ok=%v err=%v", ok, err)
	}
	if c.R != 0 || c.G != 0 || c.B != 100 {
		t.Fatalf("top-left margin color = %+v, want pixel (0,0) {0 0 100}", c)
	}

	// Bottom-right margin clamps to pixel (3,3).
	mx, my = toModel(3.8, 3.8)
	c, ok, err = ortho.SampleModel(mx, my, SampleBilinear)
	if err != nil || !ok {
		t.Fatalf("bottom-right margin: ok=%v err=%v", ok, err)
	}
	if c.R != 30 || c.G != 30 || c.B != 106 {
		t.Fatalf("bottom-right margin color = %+v, want pixel (3,3) {30 30 106}", c)
	}

	// Top edge midway between pixels 1 and 2: horizontal interpolation only.
	mx, my = toModel(2.0, 0.2)
	c, ok, err = ortho.SampleModel(mx, my, SampleBilinear)
	if err != nil || !ok {
		t.Fatalf("top edge: ok=%v err=%v", ok, err)
	}
	if c.R != 15 || c.G != 0 {
		t.Fatalf("top edge color = %+v, want R=15 (mid of 10,20) G=0", c)
	}

	// Points outside the raster stay missing.
	for _, p := range [][2]float64{{-0.2, 1}, {1, -0.2}, {4.2, 1}, {1, 4.2}} {
		mx, my = toModel(p[0], p[1])
		if _, ok, _ := ortho.SampleModel(mx, my, SampleBilinear); ok {
			t.Fatalf("point at raster (%g,%g) outside footprint reported ok", p[0], p[1])
		}
	}
}

// The interpolation quad may span up to four tiles; the result must match the
// single-strip layout exactly.
func TestBilinearAcrossTileBoundaries(t *testing.T) {
	strip := edgeTestOrtho(t, false)
	tiled := edgeTestOrtho(t, true)
	for _, rc := range [][2]float64{{2.0, 2.0}, {2.0, 1.3}, {1.3, 2.0}, {1.9, 2.4}} {
		mx, my := strip.Transform.RasterToModel(rc[0], rc[1])
		want, okW, errW := strip.SampleModel(mx, my, SampleBilinear)
		got, okG, errG := tiled.SampleModel(mx, my, SampleBilinear)
		if errW != nil || errG != nil || !okW || !okG {
			t.Fatalf("raster (%g,%g): strip ok=%v err=%v, tiled ok=%v err=%v", rc[0], rc[1], okW, errW, okG, errG)
		}
		if want != got {
			t.Fatalf("raster (%g,%g): tiled %+v != strip %+v", rc[0], rc[1], got, want)
		}
	}
}

// TIFF6: BitsPerSample defaults to 1 when the tag is absent (bilevel images).
func TestBitsPerSampleDefaultsToOne(t *testing.T) {
	// 10x2 bilevel: rows 0b1100_1010 0b11xx_xxxx, all-zero second row
	data := buildTestGeoTIFF(t, binary.LittleEndian, testTIFFConfig{
		width:      10,
		height:     2,
		samples:    1,
		omitBits:   true,
		photo:      tiffPhotometricBlackIsZero,
		compress:   tiffCompressionNone,
		blocks:     [][]byte{{0xCA, 0xC0, 0x00, 0x00}},
		omitGeoKey: true,
	})
	ds, err := OpenDataset("bilevel", data)
	if err != nil {
		t.Fatalf("OpenDataset: %v", err)
	}
	if ds.Bands[0].BitsPerSample != 1 {
		t.Fatalf("BitsPerSample = %d, want default 1", ds.Bands[0].BitsPerSample)
	}
	want := []float64{1, 1, 0, 0, 1, 0, 1, 0, 1, 1}
	for x, w := range want {
		v, ok, err := ds.RawSample(x, 0, 0)
		if err != nil || !ok || v != w {
			t.Fatalf("RawSample(%d,0) = %v ok=%v err=%v, want %v", x, v, ok, err, w)
		}
	}
}

// FillOrder=2 stores bits LSB-first within each byte; decoded values must
// match the equivalent FillOrder=1 file.
func TestFillOrder2DecodesLikeReversedBits(t *testing.T) {
	msb := []byte{0xCA, 0xC0, 0x35, 0x40}
	lsb := make([]byte, len(msb))
	for i, b := range msb {
		lsb[i] = bits.Reverse8(b)
	}
	cfg := testTIFFConfig{
		width:      10,
		height:     2,
		samples:    1,
		bits:       []uint16{1},
		photo:      tiffPhotometricBlackIsZero,
		compress:   tiffCompressionNone,
		omitGeoKey: true,
	}
	cfg.blocks = [][]byte{msb}
	ref, err := OpenDataset("msb", buildTestGeoTIFF(t, binary.LittleEndian, cfg))
	if err != nil {
		t.Fatalf("OpenDataset(msb): %v", err)
	}
	cfg.blocks = [][]byte{lsb}
	cfg.fillOrder = 2
	got, err := OpenDataset("lsb", buildTestGeoTIFF(t, binary.LittleEndian, cfg))
	if err != nil {
		t.Fatalf("OpenDataset(lsb): %v", err)
	}
	for y := 0; y < 2; y++ {
		for x := 0; x < 10; x++ {
			w, _, _ := ref.RawSample(x, y, 0)
			v, ok, err := got.RawSample(x, y, 0)
			if err != nil || !ok || v != w {
				t.Fatalf("RawSample(%d,%d) = %v ok=%v err=%v, want %v", x, y, v, ok, err, w)
			}
		}
	}
}

// TIFF6 section 18: ExtraSamples=1 marks associated alpha with premultiplied
// color components; the decoded Color must be un-premultiplied so it matches
// the unassociated convention used everywhere else.
func TestAssociatedAlphaIsUnpremultiplied(t *testing.T) {
	// stored premultiplied (50, 25, 12) with alpha 128 -> true color ~(100, 50, 24)
	data := buildTestGeoTIFF(t, binary.LittleEndian, testTIFFConfig{
		width:      1,
		height:     1,
		samples:    4,
		bits:       []uint16{8, 8, 8, 8},
		photo:      tiffPhotometricRGB,
		compress:   tiffCompressionNone,
		extra:      []uint16{1}, // associated alpha
		blocks:     [][]byte{{50, 25, 12, 128}},
		omitGeoKey: true,
	})
	ds, err := OpenDataset("assoc-alpha", data)
	if err != nil {
		t.Fatalf("OpenDataset: %v", err)
	}
	c, ok, err := ds.decoder.colorAt(0, 0)
	if err != nil || !ok {
		t.Fatalf("colorAt: ok=%v err=%v", ok, err)
	}
	want := Color{R: 100, G: 50, B: 24, A: 128}
	if c != want {
		t.Fatalf("color = %+v, want %+v", c, want)
	}
}

// Unassociated alpha (ExtraSamples=2) must stay untouched.
func TestUnassociatedAlphaPassesThrough(t *testing.T) {
	data := buildTestGeoTIFF(t, binary.LittleEndian, testTIFFConfig{
		width:      1,
		height:     1,
		samples:    4,
		bits:       []uint16{8, 8, 8, 8},
		photo:      tiffPhotometricRGB,
		compress:   tiffCompressionNone,
		extra:      []uint16{2},
		blocks:     [][]byte{{50, 25, 12, 128}},
		omitGeoKey: true,
	})
	ds, err := OpenDataset("unassoc-alpha", data)
	if err != nil {
		t.Fatalf("OpenDataset: %v", err)
	}
	c, _, err := ds.decoder.colorAt(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := Color{R: 50, G: 25, B: 12, A: 128}
	if c != want {
		t.Fatalf("color = %+v, want %+v", c, want)
	}
}

func TestUnpremultiplyRGBA16(t *testing.T) {
	// 16-bit premultiplied half-alpha mid-gray -> full value
	r, g, b, a := unpremultiplyRGBA16(0x4000, 0x2000, 0x1000, 0x8000)
	if r != 0x80 || g != 0x40 || b != 0x20 || a != 0x80 {
		t.Fatalf("got %d %d %d %d", r, g, b, a)
	}
	if r, g, b, a := unpremultiplyRGBA16(0x1234, 0x5678, 0x9abc, 0); r != 0 || g != 0 || b != 0 || a != 0 {
		t.Fatal("zero alpha must produce transparent black")
	}
	if r, _, _, a := unpremultiplyRGBA16(0xffff, 0, 0, 0xffff); r != 0xff || a != 0xff {
		t.Fatal("opaque values must pass through")
	}
}
