package geotiffcolorizer

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"math"
	"sort"
	"testing"

	"golang.org/x/image/webp"
)

func TestOpenOrthophotoStripRGBPixelIsArea(t *testing.T) {
	pixels := []byte{
		10, 20, 30, 40, 50, 60,
		70, 80, 90, 100, 110, 120,
	}
	data := buildTestGeoTIFF(t, binary.LittleEndian, testTIFFConfig{
		width:      2,
		height:     2,
		samples:    3,
		bits:       []uint16{8, 8, 8},
		photo:      tiffPhotometricRGB,
		compress:   tiffCompressionNone,
		blocks:     [][]byte{pixels},
		rowsPer:    2,
		scale:      []float64{10, 10, 0},
		tiepoint:   []float64{0, 0, 0, 100, 200, 0},
		modelType:  1,
		rasterType: 1,
		epsg:       32632,
	})

	ortho, err := OpenOrthophoto("strip.tif", data)
	if err != nil {
		t.Fatalf("OpenOrthophoto: %v", err)
	}
	if ortho.CRS != "EPSG:32632" {
		t.Fatalf("CRS = %q, want EPSG:32632", ortho.CRS)
	}
	i, j := ortho.ModelToRaster(115, 185)
	if math.Abs(i-1.5) > 1e-12 || math.Abs(j-1.5) > 1e-12 {
		t.Fatalf("ModelToRaster = (%v,%v), want (1.5,1.5)", i, j)
	}
	c, ok, err := ortho.SampleModel(115, 185, SampleNearest)
	if err != nil || !ok {
		t.Fatalf("SampleModel ok=%v err=%v", ok, err)
	}
	if c != (Color{R: 100, G: 110, B: 120, A: 255}) {
		t.Fatalf("color = %#v", c)
	}
}

func TestOpenOrthophotoBigEndianGeoKeys(t *testing.T) {
	data := buildTestGeoTIFF(t, binary.BigEndian, testTIFFConfig{
		width:      1,
		height:     1,
		samples:    3,
		bits:       []uint16{8, 8, 8},
		photo:      tiffPhotometricRGB,
		compress:   tiffCompressionNone,
		blocks:     [][]byte{{1, 2, 3}},
		rowsPer:    1,
		scale:      []float64{0.1, 0.1, 0},
		tiepoint:   []float64{0, 0, 0, 12, 45, 0},
		modelType:  2,
		rasterType: 1,
		epsg:       4326,
	})
	ortho, err := OpenOrthophoto("be.tif", data)
	if err != nil {
		t.Fatalf("OpenOrthophoto: %v", err)
	}
	if ortho.CRS != "EPSG:4326" {
		t.Fatalf("CRS = %q, want EPSG:4326", ortho.CRS)
	}
	c, ok, err := ortho.Pixel(0, 0)
	if err != nil || !ok {
		t.Fatalf("Pixel ok=%v err=%v", ok, err)
	}
	if c != (Color{R: 1, G: 2, B: 3, A: 255}) {
		t.Fatalf("color = %#v", c)
	}
}

func TestOpenOrthophotoTiledRGB(t *testing.T) {
	// 3x2 image, 2x2 tiles. Each tile payload is full 2x2 tile-sized data.
	tile0 := []byte{
		1, 2, 3, 4, 5, 6,
		7, 8, 9, 10, 11, 12,
	}
	tile1 := []byte{
		20, 21, 22, 0, 0, 0,
		30, 31, 32, 0, 0, 0,
	}
	data := buildTestGeoTIFF(t, binary.LittleEndian, testTIFFConfig{
		width:      3,
		height:     2,
		samples:    3,
		bits:       []uint16{8, 8, 8},
		photo:      tiffPhotometricRGB,
		compress:   tiffCompressionNone,
		blocks:     [][]byte{tile0, tile1},
		tileWidth:  2,
		tileHeight: 2,
		scale:      []float64{1, 1, 0},
		tiepoint:   []float64{0, 0, 0, 0, 2, 0},
		modelType:  1,
		rasterType: 1,
		epsg:       32632,
	})
	ortho, err := OpenOrthophoto("tile.tif", data)
	if err != nil {
		t.Fatalf("OpenOrthophoto: %v", err)
	}
	c, ok, err := ortho.Pixel(2, 1)
	if err != nil || !ok {
		t.Fatalf("Pixel ok=%v err=%v", ok, err)
	}
	if c != (Color{R: 30, G: 31, B: 32, A: 255}) {
		t.Fatalf("color = %#v", c)
	}
}

func TestOpenOrthophotoDeflateAnd16Bit(t *testing.T) {
	raw := encShorts(binary.LittleEndian, 0, 65535, 32768)
	var compressed bytes.Buffer
	zw := zlib.NewWriter(&compressed)
	if _, err := zw.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	data := buildTestGeoTIFF(t, binary.LittleEndian, testTIFFConfig{
		width:      1,
		height:     1,
		samples:    3,
		bits:       []uint16{16, 16, 16},
		photo:      tiffPhotometricRGB,
		compress:   tiffCompressionDeflate,
		blocks:     [][]byte{compressed.Bytes()},
		rowsPer:    1,
		scale:      []float64{1, 1, 0},
		tiepoint:   []float64{0, 0, 0, 0, 1, 0},
		modelType:  1,
		rasterType: 1,
		epsg:       32632,
	})
	ortho, err := OpenOrthophoto("deflate.tif", data)
	if err != nil {
		t.Fatalf("OpenOrthophoto: %v", err)
	}
	c, ok, err := ortho.Pixel(0, 0)
	if err != nil || !ok {
		t.Fatalf("Pixel ok=%v err=%v", ok, err)
	}
	if c != (Color{R: 0, G: 255, B: 128, A: 255}) {
		t.Fatalf("color = %#v", c)
	}
}

func TestOpenOrthophotoJPEG(t *testing.T) {
	jpegBlock := makeJPEGBlock(t, color.RGBA{R: 200, G: 40, B: 80, A: 255})
	data := buildTestGeoTIFF(t, binary.LittleEndian, testTIFFConfig{
		width:      1,
		height:     1,
		samples:    3,
		bits:       []uint16{8, 8, 8},
		photo:      tiffPhotometricYCbCr,
		compress:   tiffCompressionJPEG,
		blocks:     [][]byte{jpegBlock},
		rowsPer:    1,
		scale:      []float64{1, 1, 0},
		tiepoint:   []float64{0, 0, 0, 0, 1, 0},
		modelType:  1,
		rasterType: 1,
		epsg:       32632,
	})
	ortho, err := OpenOrthophoto("jpeg.tif", data)
	if err != nil {
		t.Fatalf("OpenOrthophoto: %v", err)
	}
	c, ok, err := ortho.Pixel(0, 0)
	if err != nil || !ok {
		t.Fatalf("Pixel ok=%v err=%v", ok, err)
	}
	if !nearColor(c, Color{R: 200, G: 40, B: 80, A: 255}, 3) {
		t.Fatalf("color = %#v", c)
	}
}

func TestOpenOrthophotoJPEGWithTables(t *testing.T) {
	fullJPEG := makeJPEGBlock(t, color.RGBA{R: 12, G: 180, B: 70, A: 255})
	tables, scan := splitJPEGForTables(t, fullJPEG)
	data := buildTestGeoTIFF(t, binary.LittleEndian, testTIFFConfig{
		width:      1,
		height:     1,
		samples:    3,
		bits:       []uint16{8, 8, 8},
		photo:      tiffPhotometricRGB,
		compress:   tiffCompressionJPEG,
		blocks:     [][]byte{scan},
		rowsPer:    1,
		jpegTables: tables,
		scale:      []float64{1, 1, 0},
		tiepoint:   []float64{0, 0, 0, 0, 1, 0},
		modelType:  1,
		rasterType: 1,
		epsg:       32632,
	})
	ortho, err := OpenOrthophoto("jpeg-tables.tif", data)
	if err != nil {
		t.Fatalf("OpenOrthophoto: %v", err)
	}
	c, ok, err := ortho.Pixel(0, 0)
	if err != nil || !ok {
		t.Fatalf("Pixel ok=%v err=%v", ok, err)
	}
	if !nearColor(c, Color{R: 12, G: 180, B: 70, A: 255}, 4) {
		t.Fatalf("color = %#v", c)
	}
}

func TestOpenOrthophotoTiledJPEG(t *testing.T) {
	left := makeJPEGBlock(t, color.RGBA{R: 220, G: 20, B: 40, A: 255})
	right := makeJPEGBlock(t, color.RGBA{R: 30, G: 60, B: 210, A: 255})
	data := buildTestGeoTIFF(t, binary.LittleEndian, testTIFFConfig{
		width:      2,
		height:     1,
		samples:    3,
		bits:       []uint16{8, 8, 8},
		photo:      tiffPhotometricYCbCr,
		compress:   tiffCompressionJPEG,
		blocks:     [][]byte{left, right},
		tileWidth:  1,
		tileHeight: 1,
		scale:      []float64{1, 1, 0},
		tiepoint:   []float64{0, 0, 0, 0, 1, 0},
		modelType:  1,
		rasterType: 1,
		epsg:       32632,
	})
	ortho, err := OpenOrthophoto("jpeg-tiled.tif", data)
	if err != nil {
		t.Fatalf("OpenOrthophoto: %v", err)
	}
	c, ok, err := ortho.Pixel(1, 0)
	if err != nil || !ok {
		t.Fatalf("Pixel ok=%v err=%v", ok, err)
	}
	if !nearColor(c, Color{R: 30, G: 60, B: 210, A: 255}, 4) {
		t.Fatalf("color = %#v", c)
	}
}

func TestOpenOrthophotoUncompressedYCbCrSubsampled(t *testing.T) {
	// TIFF Class Y chunky order stores all Y samples in the data unit first,
	// followed by the shared Cb and Cr samples.
	data := buildTestGeoTIFF(t, binary.LittleEndian, testTIFFConfig{
		width:      2,
		height:     2,
		samples:    3,
		bits:       []uint16{8, 8, 8},
		photo:      tiffPhotometricYCbCr,
		compress:   tiffCompressionNone,
		blocks:     [][]byte{{100, 110, 120, 130, 128, 128}},
		rowsPer:    2,
		yCbCrSub:   []uint16{2, 2},
		scale:      []float64{1, 1, 0},
		tiepoint:   []float64{0, 0, 0, 0, 2, 0},
		modelType:  1,
		rasterType: 1,
		epsg:       32632,
	})
	ortho, err := OpenOrthophoto("ycbcr.tif", data)
	if err != nil {
		t.Fatalf("OpenOrthophoto: %v", err)
	}
	c, ok, err := ortho.Pixel(1, 1)
	if err != nil || !ok {
		t.Fatalf("Pixel ok=%v err=%v", ok, err)
	}
	if c != (Color{R: 130, G: 130, B: 130, A: 255}) {
		t.Fatalf("color = %#v", c)
	}
}

func TestOpenOrthophotoWebP(t *testing.T) {
	webpBlock := mustBase64(t, tinyWebPBase64)
	want := decodeWebPFixtureColor(t, webpBlock)
	data := buildTestGeoTIFF(t, binary.LittleEndian, testTIFFConfig{
		width:      1,
		height:     1,
		samples:    4,
		bits:       []uint16{8, 8, 8, 8},
		photo:      tiffPhotometricRGB,
		compress:   tiffCompressionWebP,
		blocks:     [][]byte{webpBlock},
		rowsPer:    1,
		extra:      []uint16{2},
		scale:      []float64{1, 1, 0},
		tiepoint:   []float64{0, 0, 0, 0, 1, 0},
		modelType:  1,
		rasterType: 1,
		epsg:       32632,
	})
	ortho, err := OpenOrthophoto("webp.tif", data)
	if err != nil {
		t.Fatalf("OpenOrthophoto: %v", err)
	}
	c, ok, err := ortho.Pixel(0, 0)
	if err != nil || !ok {
		t.Fatalf("Pixel ok=%v err=%v", ok, err)
	}
	if c != want {
		t.Fatalf("color = %#v, want %#v", c, want)
	}
}

func TestOpenOrthophotoModelTransformation(t *testing.T) {
	matrix := []float64{
		0, -2, 0, 100,
		2, 0, 0, 200,
		0, 0, 0, 0,
		0, 0, 0, 1,
	}
	data := buildTestGeoTIFF(t, binary.LittleEndian, testTIFFConfig{
		width:      2,
		height:     2,
		samples:    3,
		bits:       []uint16{8, 8, 8},
		photo:      tiffPhotometricRGB,
		compress:   tiffCompressionNone,
		blocks:     [][]byte{{1, 1, 1, 2, 2, 2, 3, 3, 3, 4, 4, 4}},
		rowsPer:    2,
		matrix:     matrix,
		modelType:  1,
		rasterType: 1,
		epsg:       32632,
	})
	ortho, err := OpenOrthophoto("matrix.tif", data)
	if err != nil {
		t.Fatalf("OpenOrthophoto: %v", err)
	}
	i, j := ortho.ModelToRaster(97, 203)
	if math.Abs(i-1.5) > 1e-12 || math.Abs(j-1.5) > 1e-12 {
		t.Fatalf("ModelToRaster = (%v,%v), want (1.5,1.5)", i, j)
	}
	c, ok, err := ortho.SampleModel(97, 203, SampleNearest)
	if err != nil || !ok {
		t.Fatalf("SampleModel ok=%v err=%v", ok, err)
	}
	if c != (Color{R: 4, G: 4, B: 4, A: 255}) {
		t.Fatalf("color = %#v", c)
	}
}

func TestOpenDatasetSignedInt16(t *testing.T) {
	data := buildTestGeoTIFF(t, binary.LittleEndian, testTIFFConfig{
		width:      2,
		height:     1,
		samples:    1,
		bits:       []uint16{16},
		formats:    []uint16{tiffSampleFormatSigned},
		photo:      tiffPhotometricBlackIsZero,
		compress:   tiffCompressionNone,
		blocks:     [][]byte{encInt16s(binary.LittleEndian, -10, 20)},
		rowsPer:    1,
		scale:      []float64{1, 1, 0},
		tiepoint:   []float64{0, 0, 0, 0, 1, 0},
		modelType:  1,
		rasterType: 1,
		epsg:       32632,
	})
	ds, err := OpenDataset("signed.tif", data)
	if err != nil {
		t.Fatalf("OpenDataset: %v", err)
	}
	if ds.Bands[0].SampleFormat != SampleFormatSigned {
		t.Fatalf("SampleFormat = %v, want signed", ds.Bands[0].SampleFormat)
	}
	got, ok, err := ds.Sample(0, 0, 0)
	if err != nil || !ok {
		t.Fatalf("Sample ok=%v err=%v", ok, err)
	}
	if got != -10 {
		t.Fatalf("Sample = %v, want -10", got)
	}
}

func TestOpenDatasetFloat32(t *testing.T) {
	data := buildTestGeoTIFF(t, binary.LittleEndian, testTIFFConfig{
		width:      2,
		height:     1,
		samples:    1,
		bits:       []uint16{32},
		formats:    []uint16{tiffSampleFormatFloat},
		photo:      tiffPhotometricBlackIsZero,
		compress:   tiffCompressionNone,
		blocks:     [][]byte{encFloat32s(binary.LittleEndian, 1.25, -2.5)},
		rowsPer:    1,
		scale:      []float64{1, 1, 0},
		tiepoint:   []float64{0, 0, 0, 0, 1, 0},
		modelType:  1,
		rasterType: 1,
		epsg:       32632,
	})
	ds, err := OpenDataset("float.tif", data)
	if err != nil {
		t.Fatalf("OpenDataset: %v", err)
	}
	got, ok, err := ds.Sample(1, 0, 0)
	if err != nil || !ok {
		t.Fatalf("Sample ok=%v err=%v", ok, err)
	}
	if got != -2.5 {
		t.Fatalf("Sample = %v, want -2.5", got)
	}
	if _, err := ds.AsOrthophoto(); err == nil {
		t.Fatalf("AsOrthophoto succeeded for float analytical raster")
	}
}

func TestOpenDatasetFloatPredictor3(t *testing.T) {
	raw := encFloat32s(binary.LittleEndian, 1.5, 2.5, 4)
	encoded := encodeFloatingPointPredictorFixture(binary.LittleEndian, raw, 4, 1)
	data := buildTestGeoTIFF(t, binary.LittleEndian, testTIFFConfig{
		width:      3,
		height:     1,
		samples:    1,
		bits:       []uint16{32},
		formats:    []uint16{tiffSampleFormatFloat},
		photo:      tiffPhotometricBlackIsZero,
		compress:   tiffCompressionNone,
		predictor:  3,
		blocks:     [][]byte{encoded},
		rowsPer:    1,
		scale:      []float64{1, 1, 0},
		tiepoint:   []float64{0, 0, 0, 0, 1, 0},
		modelType:  1,
		rasterType: 1,
		epsg:       32632,
	})
	ds, err := OpenDataset("predictor3.tif", data)
	if err != nil {
		t.Fatalf("OpenDataset: %v", err)
	}
	for x, want := range []float64{1.5, 2.5, 4} {
		got, ok, err := ds.Sample(x, 0, 0)
		if err != nil || !ok {
			t.Fatalf("Sample(%d) ok=%v err=%v", x, ok, err)
		}
		if got != want {
			t.Fatalf("Sample(%d) = %v, want %v", x, got, want)
		}
	}
}

func TestOpenDatasetBitPackedUnsigned(t *testing.T) {
	data := buildTestGeoTIFF(t, binary.LittleEndian, testTIFFConfig{
		width:      4,
		height:     1,
		samples:    1,
		bits:       []uint16{2},
		photo:      tiffPhotometricBlackIsZero,
		compress:   tiffCompressionNone,
		blocks:     [][]byte{{0b00011011}},
		rowsPer:    1,
		scale:      []float64{1, 1, 0},
		tiepoint:   []float64{0, 0, 0, 0, 1, 0},
		modelType:  1,
		rasterType: 1,
		epsg:       32632,
	})
	ds, err := OpenDataset("packed.tif", data)
	if err != nil {
		t.Fatalf("OpenDataset: %v", err)
	}
	for x, want := range []float64{0, 1, 2, 3} {
		got, ok, err := ds.Sample(x, 0, 0)
		if err != nil || !ok {
			t.Fatalf("Sample(%d) ok=%v err=%v", x, ok, err)
		}
		if got != want {
			t.Fatalf("Sample(%d) = %v, want %v", x, got, want)
		}
	}
}

func TestOpenDatasetPermissiveMissingGeoKeys(t *testing.T) {
	data := buildTestGeoTIFF(t, binary.LittleEndian, testTIFFConfig{
		width:      1,
		height:     1,
		samples:    1,
		bits:       []uint16{8},
		photo:      tiffPhotometricBlackIsZero,
		compress:   tiffCompressionNone,
		blocks:     [][]byte{{7}},
		rowsPer:    1,
		scale:      []float64{1, 1, 0},
		tiepoint:   []float64{0, 0, 0, 0, 1, 0},
		omitGeoKey: true,
	})
	ds, err := OpenDataset("plain.tif", data)
	if err != nil {
		t.Fatalf("OpenDataset permissive: %v", err)
	}
	if ds.CRS != "" || ds.ModelType != ModelTypeUnknown || !ds.HasTransform {
		t.Fatalf("metadata = CRS %q model %d hasTransform %v", ds.CRS, ds.ModelType, ds.HasTransform)
	}
	if _, err := OpenDataset("plain.tif", data, WithStrictGeoTIFF()); err == nil {
		t.Fatalf("strict OpenDataset succeeded without GeoKeys")
	}
}

func TestOpenDatasetGeoTIFFMinorRevision2(t *testing.T) {
	data := buildTestGeoTIFF(t, binary.LittleEndian, testTIFFConfig{
		width:      1,
		height:     1,
		samples:    1,
		bits:       []uint16{8},
		photo:      tiffPhotometricBlackIsZero,
		compress:   tiffCompressionNone,
		blocks:     [][]byte{{9}},
		rowsPer:    1,
		scale:      []float64{1, 1, 0},
		tiepoint:   []float64{0, 0, 0, 0, 1, 0},
		modelType:  2,
		rasterType: 1,
		epsg:       4326,
		geoMinor:   2,
	})
	ds, err := OpenDataset("geotiff11.tif", data, WithStrictGeoTIFF())
	if err != nil {
		t.Fatalf("OpenDataset: %v", err)
	}
	if ds.GeoKeys.MinorRevision != 2 || ds.CRS != "EPSG:4326" {
		t.Fatalf("GeoKeys minor=%d CRS=%q", ds.GeoKeys.MinorRevision, ds.CRS)
	}
}

func TestJPEGPayloadHelpersPreserveBinaryWhitespace(t *testing.T) {
	scan := []byte{0x20, 0xff, 0xda, 0x00, 0x0a}
	if got := jpegScanPayload(scan); !bytes.Equal(got, scan) {
		t.Fatalf("jpegScanPayload = % x, want % x", got, scan)
	}
	tables := []byte{0xff, 0xd8, 0x20, 0x09, 0xff, 0xd9}
	wantTables := []byte{0x20, 0x09}
	if got := jpegTablesPayload(tables); !bytes.Equal(got, wantTables) {
		t.Fatalf("jpegTablesPayload = % x, want % x", got, wantTables)
	}
}

func TestFloat64sPreservesZeroDenominatorRationalSlots(t *testing.T) {
	raw := make([]byte, 16)
	binary.LittleEndian.PutUint32(raw[0:4], 1)
	binary.LittleEndian.PutUint32(raw[4:8], 0)
	binary.LittleEndian.PutUint32(raw[8:12], 1)
	binary.LittleEndian.PutUint32(raw[12:16], 2)
	ifd := &tiffIFD{
		order: binary.LittleEndian,
		tags: map[uint16]tiffValue{
			123: {typ: tiffTypeRational, raw: raw},
		},
	}
	got := ifd.float64s(123)
	if len(got) != 2 {
		t.Fatalf("len(float64s) = %d, want 2", len(got))
	}
	if !math.IsNaN(got[0]) || got[1] != 0.5 {
		t.Fatalf("float64s = %#v, want [NaN 0.5]", got)
	}
}

func TestCompressedBlockCannotExpandPastExpectedSize(t *testing.T) {
	var compressed bytes.Buffer
	zw := zlib.NewWriter(&compressed)
	if _, err := zw.Write([]byte{1, 2}); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	data := buildTestGeoTIFF(t, binary.LittleEndian, testTIFFConfig{
		width:      1,
		height:     1,
		samples:    1,
		bits:       []uint16{8},
		photo:      tiffPhotometricBlackIsZero,
		compress:   tiffCompressionDeflate,
		blocks:     [][]byte{compressed.Bytes()},
		rowsPer:    1,
		scale:      []float64{1, 1, 0},
		tiepoint:   []float64{0, 0, 0, 0, 1, 0},
		modelType:  1,
		rasterType: 1,
		epsg:       32632,
	})
	ds, err := OpenDataset("over-expanded.tif", data)
	if err != nil {
		t.Fatalf("OpenDataset: %v", err)
	}
	if _, _, err := ds.RawPixel(0, 0); err == nil {
		t.Fatalf("RawPixel succeeded for over-expanded compressed block")
	}
}

func TestBilinearColorUsesPremultipliedAlpha(t *testing.T) {
	got := bilerpColor(
		Color{R: 255, A: 255},
		Color{},
		Color{R: 255, A: 255},
		Color{},
		0.5,
		0.5,
	)
	if got != (Color{R: 255, A: 128}) {
		t.Fatalf("bilerpColor = %#v, want red without dark halo", got)
	}
}

func TestOpenOrthophotoShortPaletteColorMap(t *testing.T) {
	data := buildTestGeoTIFF(t, binary.LittleEndian, testTIFFConfig{
		width:      1,
		height:     1,
		samples:    1,
		bits:       []uint16{8},
		photo:      tiffPhotometricPalette,
		compress:   tiffCompressionNone,
		blocks:     [][]byte{{1}},
		rowsPer:    1,
		colorMap:   []uint16{0, 65535, 0, 0, 0, 0},
		scale:      []float64{1, 1, 0},
		tiepoint:   []float64{0, 0, 0, 0, 1, 0},
		modelType:  1,
		rasterType: 1,
		epsg:       32632,
	})
	ortho, err := OpenOrthophoto("short-palette.tif", data)
	if err != nil {
		t.Fatalf("OpenOrthophoto: %v", err)
	}
	c, ok, err := ortho.Pixel(0, 0)
	if err != nil || !ok {
		t.Fatalf("Pixel ok=%v err=%v", ok, err)
	}
	if c != (Color{R: 255, A: 255}) {
		t.Fatalf("color = %#v, want red from short palette", c)
	}
}

func TestOpenOrthophotoSeparatePlanarRGBA(t *testing.T) {
	data := buildTestGeoTIFF(t, binary.LittleEndian, testTIFFConfig{
		width:      1,
		height:     1,
		samples:    4,
		bits:       []uint16{8, 8, 8, 8},
		photo:      tiffPhotometricRGB,
		compress:   tiffCompressionNone,
		planar:     tiffPlanarSeparate,
		blocks:     [][]byte{{10}, {20}, {30}, {40}},
		rowsPer:    1,
		extra:      []uint16{2},
		scale:      []float64{1, 1, 0},
		tiepoint:   []float64{0, 0, 0, 0, 1, 0},
		modelType:  1,
		rasterType: 1,
		epsg:       32632,
	})
	ortho, err := OpenOrthophoto("separate-rgba.tif", data)
	if err != nil {
		t.Fatalf("OpenOrthophoto: %v", err)
	}
	c, ok, err := ortho.Pixel(0, 0)
	if err != nil || !ok {
		t.Fatalf("Pixel ok=%v err=%v", ok, err)
	}
	if c != (Color{R: 10, G: 20, B: 30, A: 40}) {
		t.Fatalf("color = %#v", c)
	}
}

func TestOpenDatasetPackBits(t *testing.T) {
	data := buildTestGeoTIFF(t, binary.LittleEndian, testTIFFConfig{
		width:      4,
		height:     1,
		samples:    1,
		bits:       []uint16{8},
		photo:      tiffPhotometricBlackIsZero,
		compress:   tiffCompressionPackBits,
		blocks:     [][]byte{{1, 9, 8, 0xff, 7}},
		rowsPer:    1,
		scale:      []float64{1, 1, 0},
		tiepoint:   []float64{0, 0, 0, 0, 1, 0},
		modelType:  1,
		rasterType: 1,
		epsg:       32632,
	})
	ds, err := OpenDataset("packbits.tif", data)
	if err != nil {
		t.Fatalf("OpenDataset: %v", err)
	}
	for x, want := range []float64{9, 8, 7, 7} {
		got, ok, err := ds.RawSample(x, 0, 0)
		if err != nil || !ok {
			t.Fatalf("RawSample(%d) ok=%v err=%v", x, ok, err)
		}
		if got != want {
			t.Fatalf("RawSample(%d) = %v, want %v", x, got, want)
		}
	}
}

func TestOpenDatasetHorizontalPredictor2Uint32(t *testing.T) {
	data := buildTestGeoTIFF(t, binary.LittleEndian, testTIFFConfig{
		width:      3,
		height:     1,
		samples:    1,
		bits:       []uint16{32},
		photo:      tiffPhotometricBlackIsZero,
		compress:   tiffCompressionNone,
		predictor:  2,
		blocks:     [][]byte{encLongs(binary.LittleEndian, 10, 5, 7)},
		rowsPer:    1,
		scale:      []float64{1, 1, 0},
		tiepoint:   []float64{0, 0, 0, 0, 1, 0},
		modelType:  1,
		rasterType: 1,
		epsg:       32632,
	})
	ds, err := OpenDataset("predictor2-uint32.tif", data)
	if err != nil {
		t.Fatalf("OpenDataset: %v", err)
	}
	got, ok, err := ds.RawSample(2, 0, 0)
	if err != nil || !ok {
		t.Fatalf("RawSample ok=%v err=%v", ok, err)
	}
	if got != 22 {
		t.Fatalf("RawSample = %v, want 22", got)
	}
}

func TestOpenDatasetFloat16(t *testing.T) {
	data := buildTestGeoTIFF(t, binary.LittleEndian, testTIFFConfig{
		width:      2,
		height:     1,
		samples:    1,
		bits:       []uint16{16},
		formats:    []uint16{tiffSampleFormatFloat},
		photo:      tiffPhotometricBlackIsZero,
		compress:   tiffCompressionNone,
		blocks:     [][]byte{encShorts(binary.LittleEndian, 0x3c00, 0xc000)},
		rowsPer:    1,
		scale:      []float64{1, 1, 0},
		tiepoint:   []float64{0, 0, 0, 0, 1, 0},
		modelType:  1,
		rasterType: 1,
		epsg:       32632,
	})
	ds, err := OpenDataset("float16.tif", data)
	if err != nil {
		t.Fatalf("OpenDataset: %v", err)
	}
	for x, want := range []float64{1, -2} {
		got, ok, err := ds.RawSample(x, 0, 0)
		if err != nil || !ok {
			t.Fatalf("RawSample(%d) ok=%v err=%v", x, ok, err)
		}
		if got != want {
			t.Fatalf("RawSample(%d) = %v, want %v", x, got, want)
		}
	}
}

func TestOpenDatasetGDALNoDataScaleOffset(t *testing.T) {
	data := buildTestGeoTIFF(t, binary.LittleEndian, testTIFFConfig{
		width:      2,
		height:     1,
		samples:    1,
		bits:       []uint16{8},
		photo:      tiffPhotometricBlackIsZero,
		compress:   tiffCompressionNone,
		blocks:     [][]byte{{5, 6}},
		rowsPer:    1,
		gdalNoData: "5",
		gdalMeta:   `<GDALMetadata><Item name="SCALE" sample="0">2</Item><Item name="OFFSET" sample="0">10</Item></GDALMetadata>`,
		scale:      []float64{1, 1, 0},
		tiepoint:   []float64{0, 0, 0, 0, 1, 0},
		modelType:  1,
		rasterType: 1,
		epsg:       32632,
	})
	ds, err := OpenDataset("gdal-meta.tif", data)
	if err != nil {
		t.Fatalf("OpenDataset: %v", err)
	}
	if got, ok, err := ds.Sample(0, 0, 0); err != nil || ok || got != 0 {
		t.Fatalf("NoData Sample = %v ok=%v err=%v, want ok=false", got, ok, err)
	}
	got, ok, err := ds.Sample(1, 0, 0)
	if err != nil || !ok {
		t.Fatalf("Sample ok=%v err=%v", ok, err)
	}
	if got != 22 {
		t.Fatalf("scaled Sample = %v, want 22", got)
	}
	vals, ok, err := ds.Pixel(1, 0)
	if err != nil || !ok {
		t.Fatalf("Pixel ok=%v err=%v", ok, err)
	}
	if len(vals) != 1 || vals[0] != 22 {
		t.Fatalf("Pixel = %#v, want [22]", vals)
	}
	mx, my := ds.Transform.RasterToModel(1.5, 0.5)
	if mx != 1.5 || my != 0.5 {
		t.Fatalf("RasterToModel = (%v,%v), want (1.5,0.5)", mx, my)
	}
}

func TestOpenOrthophotoBilinearSampleModel(t *testing.T) {
	data := buildTestGeoTIFF(t, binary.LittleEndian, testTIFFConfig{
		width:      2,
		height:     2,
		samples:    3,
		bits:       []uint16{8, 8, 8},
		photo:      tiffPhotometricRGB,
		compress:   tiffCompressionNone,
		blocks:     [][]byte{{0, 0, 0, 100, 0, 0, 0, 100, 0, 100, 100, 0}},
		rowsPer:    2,
		scale:      []float64{1, 1, 0},
		tiepoint:   []float64{0, 0, 0, 0, 2, 0},
		modelType:  1,
		rasterType: 1,
		epsg:       32632,
	})
	ortho, err := OpenOrthophoto("bilinear.tif", data)
	if err != nil {
		t.Fatalf("OpenOrthophoto: %v", err)
	}
	c, ok, err := ortho.SampleModel(1, 1, SampleBilinear)
	if err != nil || !ok {
		t.Fatalf("SampleModel ok=%v err=%v", ok, err)
	}
	if c != (Color{R: 50, G: 50, B: 0, A: 255}) {
		t.Fatalf("color = %#v", c)
	}
}

func TestOpenDatasetUsesBlockCacheOption(t *testing.T) {
	data := buildTestGeoTIFF(t, binary.LittleEndian, testTIFFConfig{
		width:      1,
		height:     1,
		samples:    1,
		bits:       []uint16{8},
		photo:      tiffPhotometricBlackIsZero,
		compress:   tiffCompressionNone,
		blocks:     [][]byte{{42}},
		rowsPer:    1,
		scale:      []float64{1, 1, 0},
		tiepoint:   []float64{0, 0, 0, 0, 1, 0},
		modelType:  1,
		rasterType: 1,
		epsg:       32632,
	})
	ds, err := OpenDataset("cache-option.tif", data, WithBlockCacheSize(1))
	if err != nil {
		t.Fatalf("OpenDataset: %v", err)
	}
	if ds.decoder.cache.max != 1 {
		t.Fatalf("cache max = %d, want 1", ds.decoder.cache.max)
	}
	if ds.decoder.colorCache.max != 1 {
		t.Fatalf("color cache max = %d, want 1", ds.decoder.colorCache.max)
	}
}

func TestOpenOrthophotoComputesCacheSizeFromMemoryTarget(t *testing.T) {
	data := buildTestGeoTIFF(t, binary.LittleEndian, testTIFFConfig{
		width:      2,
		height:     2,
		samples:    3,
		bits:       []uint16{8, 8, 8},
		photo:      tiffPhotometricRGB,
		compress:   tiffCompressionNone,
		blocks:     [][]byte{{10, 20, 30}, {40, 50, 60}, {70, 80, 90}, {100, 110, 120}},
		tileWidth:  1,
		tileHeight: 1,
		scale:      []float64{1, 1, 0},
		tiepoint:   []float64{0, 0, 0, 0, 2, 0},
		modelType:  1,
		rasterType: 1,
		epsg:       32632,
	})
	// 16 bytes split evenly across the two caches: 8 for each.
	ortho, err := OpenOrthophoto("cache-memory.tif", data, WithBlockCacheMemory(16))
	if err != nil {
		t.Fatalf("OpenOrthophoto: %v", err)
	}
	if ortho.decoder.colorCache.max != 2 {
		t.Fatalf("color cache max = %d, want 2", ortho.decoder.colorCache.max)
	}
	if ortho.decoder.cache.max != 1 {
		t.Fatalf("sample cache max = %d, want 1", ortho.decoder.cache.max)
	}
}

func TestDefaultBlockCacheMemoryCapsAtBlockCount(t *testing.T) {
	data := buildTestGeoTIFF(t, binary.LittleEndian, testTIFFConfig{
		width:      2,
		height:     2,
		samples:    3,
		bits:       []uint16{8, 8, 8},
		photo:      tiffPhotometricRGB,
		compress:   tiffCompressionNone,
		blocks:     [][]byte{{10, 20, 30}, {40, 50, 60}, {70, 80, 90}, {100, 110, 120}},
		tileWidth:  1,
		tileHeight: 1,
		scale:      []float64{1, 1, 0},
		tiepoint:   []float64{0, 0, 0, 0, 2, 0},
		modelType:  1,
		rasterType: 1,
		epsg:       32632,
	})
	ortho, err := OpenOrthophoto("cache-default.tif", data)
	if err != nil {
		t.Fatalf("OpenOrthophoto: %v", err)
	}
	if ortho.decoder.colorCache.max != 4 {
		t.Fatalf("color cache max = %d, want all 4 blocks", ortho.decoder.colorCache.max)
	}
}

func TestOpenDatasetReaderAtDoesNotReadWholeFile(t *testing.T) {
	data := buildTestGeoTIFF(t, binary.LittleEndian, testTIFFConfig{
		width:      1,
		height:     1,
		samples:    1,
		bits:       []uint16{8},
		photo:      tiffPhotometricBlackIsZero,
		compress:   tiffCompressionNone,
		blocks:     [][]byte{{42}},
		rowsPer:    1,
		scale:      []float64{1, 1, 0},
		tiepoint:   []float64{0, 0, 0, 0, 1, 0},
		modelType:  1,
		rasterType: 1,
		epsg:       32632,
	})
	padded := append(append([]byte(nil), data...), make([]byte, 1<<20)...)
	reader := &countingReaderAt{data: padded}

	ds, err := OpenDatasetReaderAt("readerat.tif", reader, int64(len(padded)), WithBlockCacheSize(1))
	if err != nil {
		t.Fatalf("OpenDatasetReaderAt: %v", err)
	}
	got, ok, err := ds.RawSample(0, 0, 0)
	if err != nil || !ok {
		t.Fatalf("RawSample ok=%v err=%v", ok, err)
	}
	if got != 42 {
		t.Fatalf("RawSample = %v, want 42", got)
	}
	if reader.maxRead >= len(padded)/2 {
		t.Fatalf("largest ReadAt = %d bytes, file size = %d; reader appears to load the whole file", reader.maxRead, len(padded))
	}
	if reader.totalRead >= int64(len(padded)/2) {
		t.Fatalf("total ReadAt = %d bytes, file size = %d; reader appears to load the whole file", reader.totalRead, len(padded))
	}
}

func TestOrthophotoSampleModelCachedBlockDoesNotAllocate(t *testing.T) {
	data := buildTestGeoTIFF(t, binary.LittleEndian, testTIFFConfig{
		width:      2,
		height:     2,
		samples:    3,
		bits:       []uint16{8, 8, 8},
		photo:      tiffPhotometricRGB,
		compress:   tiffCompressionNone,
		blocks:     [][]byte{{10, 20, 30, 40, 50, 60, 70, 80, 90, 100, 110, 120}},
		rowsPer:    2,
		scale:      []float64{1, 1, 0},
		tiepoint:   []float64{0, 0, 0, 0, 2, 0},
		modelType:  1,
		rasterType: 1,
		epsg:       32632,
	})
	ortho, err := OpenOrthophoto("alloc.tif", data, WithBlockCacheSize(1))
	if err != nil {
		t.Fatalf("OpenOrthophoto: %v", err)
	}
	if _, ok, err := ortho.SampleModel(0, 2, SampleNearest); err != nil || !ok {
		t.Fatalf("warm SampleModel ok=%v err=%v", ok, err)
	}

	var got Color
	var ok bool
	allocs := testing.AllocsPerRun(1000, func() {
		got, ok, err = ortho.SampleModel(0, 2, SampleNearest)
	})
	if err != nil || !ok {
		t.Fatalf("SampleModel ok=%v err=%v", ok, err)
	}
	if got != (Color{R: 10, G: 20, B: 30, A: 255}) {
		t.Fatalf("SampleModel color = %#v", got)
	}
	if allocs != 0 {
		t.Fatalf("SampleModel cached block allocs = %g, want 0", allocs)
	}
}

func TestOrthophotoPixelUsesCompactColorCache(t *testing.T) {
	data := buildTestGeoTIFF(t, binary.LittleEndian, testTIFFConfig{
		width:      2,
		height:     2,
		samples:    3,
		bits:       []uint16{8, 8, 8},
		photo:      tiffPhotometricRGB,
		compress:   tiffCompressionNone,
		blocks:     [][]byte{{10, 20, 30, 40, 50, 60, 70, 80, 90, 100, 110, 120}},
		rowsPer:    2,
		scale:      []float64{1, 1, 0},
		tiepoint:   []float64{0, 0, 0, 0, 2, 0},
		modelType:  1,
		rasterType: 1,
		epsg:       32632,
	})
	ortho, err := OpenOrthophoto("color-cache.tif", data, WithBlockCacheSize(4))
	if err != nil {
		t.Fatalf("OpenOrthophoto: %v", err)
	}
	got, ok, err := ortho.Pixel(0, 0)
	if err != nil || !ok {
		t.Fatalf("Pixel ok=%v err=%v", ok, err)
	}
	if got != (Color{R: 10, G: 20, B: 30, A: 255}) {
		t.Fatalf("Pixel color = %#v", got)
	}
	if ortho.decoder.colorCache.list.Len() != 1 {
		t.Fatalf("color cache len = %d, want 1", ortho.decoder.colorCache.list.Len())
	}
	if ortho.decoder.cache.list.Len() != 0 {
		t.Fatalf("float sample cache len = %d, want 0", ortho.decoder.cache.list.Len())
	}
}

type countingReaderAt struct {
	data      []byte
	maxRead   int
	totalRead int64
}

func (r *countingReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if len(p) > r.maxRead {
		r.maxRead = len(p)
	}
	r.totalRead += int64(len(p))
	if off < 0 || off >= int64(len(r.data)) {
		return 0, io.EOF
	}
	n := copy(p, r.data[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func TestRasterHelperBranches(t *testing.T) {
	if got := bilerp(0, 100, 100, 200, 0.5, 0.5); got != 100 {
		t.Fatalf("bilerp = %d, want 100", got)
	}
	if got := bilerp(0, 0, 0, 0, -1, -1); got != 0 {
		t.Fatalf("bilerp low clamp = %d, want 0", got)
	}
	if got := bilerp(255, 255, 255, 255, 2, 2); got != 255 {
		t.Fatalf("bilerp high clamp = %d, want 255", got)
	}

	block := (&rasterDecoder{
		width:        1,
		height:       1,
		samples:      4,
		photometric:  tiffPhotometricRGB,
		rowsPerStrip: 1,
		extraSamples: []uint64{2},
	}).blankBlock(0)
	if block.values[3] != 255 {
		t.Fatalf("blank alpha = %v, want 255", block.values[3])
	}

	white := (&rasterDecoder{samples: 1, bits: []uint64{8}, formats: []uint64{tiffSampleFormatUnsigned}, photometric: tiffPhotometricWhiteIsZero}).samplesToColor([]float64{0})
	if white != (Color{R: 255, G: 255, B: 255, A: 255}) {
		t.Fatalf("white-is-zero color = %#v", white)
	}
	missingPalette := (&rasterDecoder{samples: 1, photometric: tiffPhotometricPalette}).samplesToColor([]float64{7})
	if missingPalette != (Color{A: 255}) {
		t.Fatalf("missing palette color = %#v", missingPalette)
	}
}

func TestApplyIntegerPredictorRowWidths(t *testing.T) {
	row8 := []byte{1, 1, 1}
	if err := applyIntegerPredictorRow(row8, 1, 1, binary.LittleEndian); err != nil {
		t.Fatalf("8-bit predictor: %v", err)
	}
	if !bytes.Equal(row8, []byte{1, 2, 3}) {
		t.Fatalf("8-bit row = %v, want [1 2 3]", row8)
	}

	row16 := encShorts(binary.LittleEndian, 1, 1, 1)
	if err := applyIntegerPredictorRow(row16, 1, 2, binary.LittleEndian); err != nil {
		t.Fatalf("16-bit predictor: %v", err)
	}
	got16 := [3]uint16{binary.LittleEndian.Uint16(row16[0:2]), binary.LittleEndian.Uint16(row16[2:4]), binary.LittleEndian.Uint16(row16[4:6])}
	if got16 != [3]uint16{1, 2, 3} {
		t.Fatalf("16-bit row = %v, want [1 2 3]", got16)
	}

	row64 := make([]byte, 24)
	binary.LittleEndian.PutUint64(row64[0:8], 1)
	binary.LittleEndian.PutUint64(row64[8:16], 1)
	binary.LittleEndian.PutUint64(row64[16:24], 1)
	if err := applyIntegerPredictorRow(row64, 1, 8, binary.LittleEndian); err != nil {
		t.Fatalf("64-bit predictor: %v", err)
	}
	if got := binary.LittleEndian.Uint64(row64[16:24]); got != 3 {
		t.Fatalf("64-bit last sample = %d, want 3", got)
	}
}

func TestFloat16SpecialValues(t *testing.T) {
	if got := math.Signbit(float16ToFloat64(0x8000)); !got {
		t.Fatalf("negative zero lost sign")
	}
	if got := float16ToFloat64(0x7c00); !math.IsInf(got, 1) {
		t.Fatalf("float16 +Inf = %v", got)
	}
	if got := float16ToFloat64(0x0001); got <= 0 {
		t.Fatalf("float16 subnormal = %v, want positive", got)
	}
}

func nearColor(got, want Color, tolerance uint8) bool {
	return channelNear(got.R, want.R, tolerance) &&
		channelNear(got.G, want.G, tolerance) &&
		channelNear(got.B, want.B, tolerance) &&
		channelNear(got.A, want.A, tolerance)
}

func channelNear(got, want, tolerance uint8) bool {
	if got > want {
		return got-want <= tolerance
	}
	return want-got <= tolerance
}

type testTIFFConfig struct {
	width      int
	height     int
	samples    int
	bits       []uint16
	formats    []uint16
	photo      uint64
	compress   uint64
	predictor  uint16
	planar     uint16
	blocks     [][]byte
	rowsPer    int
	tileWidth  int
	tileHeight int
	scale      []float64
	tiepoint   []float64
	matrix     []float64
	jpegTables []byte
	extra      []uint16
	colorMap   []uint16
	gdalNoData string
	gdalMeta   string
	yCbCrSub   []uint16
	modelType  uint16
	rasterType uint16
	epsg       uint16
	geoMinor   uint16
	omitGeoKey bool
	omitBits   bool
	fillOrder  uint16
}

type testTag struct {
	id    uint16
	typ   uint16
	count uint64
	raw   []byte
}

func buildTestGeoTIFF(t *testing.T, order binary.ByteOrder, cfg testTIFFConfig) []byte {
	t.Helper()
	if cfg.rowsPer == 0 {
		cfg.rowsPer = cfg.height
	}
	if cfg.samples == 0 {
		cfg.samples = 1
	}
	if len(cfg.bits) == 0 {
		cfg.bits = []uint16{8}
	}
	if len(cfg.formats) == 0 {
		cfg.formats = make([]uint16, cfg.samples)
		for i := range cfg.formats {
			cfg.formats[i] = tiffSampleFormatUnsigned
		}
	}
	if cfg.geoMinor == 0 {
		cfg.geoMinor = 1
	}
	if cfg.planar == 0 {
		cfg.planar = tiffPlanarChunky
	}
	var tags []testTag
	add := func(id, typ uint16, count uint64, raw []byte) {
		tags = append(tags, testTag{id: id, typ: typ, count: count, raw: raw})
	}
	add(tiffTagImageWidth, tiffTypeLong, 1, encLongs(order, uint32(cfg.width)))
	add(tiffTagImageLength, tiffTypeLong, 1, encLongs(order, uint32(cfg.height)))
	if !cfg.omitBits {
		add(tiffTagBitsPerSample, tiffTypeShort, uint64(len(cfg.bits)), encShorts(order, cfg.bits...))
	}
	if cfg.fillOrder != 0 {
		add(tiffTagFillOrder, tiffTypeShort, 1, encShorts(order, cfg.fillOrder))
	}
	add(tiffTagCompression, tiffTypeShort, 1, encShorts(order, uint16(cfg.compress)))
	add(tiffTagPhotometric, tiffTypeShort, 1, encShorts(order, uint16(cfg.photo)))
	add(tiffTagSamplesPerPixel, tiffTypeShort, 1, encShorts(order, uint16(cfg.samples)))
	add(tiffTagPlanarConfig, tiffTypeShort, 1, encShorts(order, cfg.planar))
	add(tiffTagSampleFormat, tiffTypeShort, uint64(len(cfg.formats)), encShorts(order, cfg.formats...))
	if cfg.predictor != 0 {
		add(tiffTagPredictor, tiffTypeShort, 1, encShorts(order, cfg.predictor))
	}
	if len(cfg.extra) > 0 {
		add(tiffTagExtraSamples, tiffTypeShort, uint64(len(cfg.extra)), encShorts(order, cfg.extra...))
	}
	if len(cfg.colorMap) > 0 {
		add(tiffTagColorMap, tiffTypeShort, uint64(len(cfg.colorMap)), encShorts(order, cfg.colorMap...))
	}
	if cfg.gdalMeta != "" {
		add(tiffTagGDALMetadata, tiffTypeASCII, uint64(len(cfg.gdalMeta)+1), append([]byte(cfg.gdalMeta), 0))
	}
	if cfg.gdalNoData != "" {
		add(tiffTagGDALNodata, tiffTypeASCII, uint64(len(cfg.gdalNoData)+1), append([]byte(cfg.gdalNoData), 0))
	}
	if len(cfg.jpegTables) > 0 {
		add(tiffTagJPEGTables, tiffTypeUndefined, uint64(len(cfg.jpegTables)), cfg.jpegTables)
	}
	if len(cfg.yCbCrSub) > 0 {
		add(tiffTagYCbCrSubSampling, tiffTypeShort, uint64(len(cfg.yCbCrSub)), encShorts(order, cfg.yCbCrSub...))
	}
	if cfg.tileWidth > 0 {
		add(tiffTagTileWidth, tiffTypeLong, 1, encLongs(order, uint32(cfg.tileWidth)))
		add(tiffTagTileLength, tiffTypeLong, 1, encLongs(order, uint32(cfg.tileHeight)))
		add(tiffTagTileOffsets, tiffTypeLong, uint64(len(cfg.blocks)), make([]byte, len(cfg.blocks)*4))
		add(tiffTagTileByteCounts, tiffTypeLong, uint64(len(cfg.blocks)), encBlockCounts(order, cfg.blocks))
	} else {
		add(tiffTagRowsPerStrip, tiffTypeLong, 1, encLongs(order, uint32(cfg.rowsPer)))
		add(tiffTagStripOffsets, tiffTypeLong, uint64(len(cfg.blocks)), make([]byte, len(cfg.blocks)*4))
		add(tiffTagStripByteCounts, tiffTypeLong, uint64(len(cfg.blocks)), encBlockCounts(order, cfg.blocks))
	}
	if len(cfg.scale) > 0 {
		add(tiffTagModelPixelScale, tiffTypeDouble, uint64(len(cfg.scale)), encDoubles(order, cfg.scale...))
	}
	if len(cfg.tiepoint) > 0 {
		add(tiffTagModelTiepoint, tiffTypeDouble, uint64(len(cfg.tiepoint)), encDoubles(order, cfg.tiepoint...))
	}
	if len(cfg.matrix) > 0 {
		add(tiffTagModelTransformation, tiffTypeDouble, uint64(len(cfg.matrix)), encDoubles(order, cfg.matrix...))
	}
	if !cfg.omitGeoKey {
		crsKey := uint16(3072)
		if cfg.modelType == 2 || cfg.modelType == 3 {
			crsKey = 2048
		}
		add(tiffTagGeoKeyDirectory, tiffTypeShort, 16, encShorts(order,
			1, 1, cfg.geoMinor, 3,
			1024, 0, 1, cfg.modelType,
			1025, 0, 1, cfg.rasterType,
			crsKey, 0, 1, cfg.epsg,
		))
	}

	sort.Slice(tags, func(i, j int) bool { return tags[i].id < tags[j].id })

	headerLen := 8
	ifdLen := 2 + len(tags)*12 + 4
	valueOffset := uint32(headerLen + ifdLen)
	external := make([][]byte, len(tags))
	for i := range tags {
		if len(tags[i].raw) > 4 {
			external[i] = tags[i].raw
			valueOffset += uint32(len(tags[i].raw))
		}
	}
	blockOffsets := make([]uint32, len(cfg.blocks))
	for i, block := range cfg.blocks {
		blockOffsets[i] = valueOffset
		valueOffset += uint32(len(block))
	}
	for i := range tags {
		if tags[i].id == tiffTagStripOffsets || tags[i].id == tiffTagTileOffsets {
			tags[i].raw = encOffsets(order, blockOffsets)
			if len(tags[i].raw) > 4 {
				external[i] = tags[i].raw
			}
		}
	}

	var out bytes.Buffer
	if order == binary.LittleEndian {
		out.WriteString("II")
	} else {
		out.WriteString("MM")
	}
	writeU16(&out, order, 42)
	writeU32(&out, order, 8)
	writeU16(&out, order, uint16(len(tags)))
	currentExternalOffset := uint32(headerLen + ifdLen)
	for i, tag := range tags {
		writeU16(&out, order, tag.id)
		writeU16(&out, order, tag.typ)
		writeU32(&out, order, uint32(tag.count))
		if len(tag.raw) <= 4 {
			field := make([]byte, 4)
			copy(field, tag.raw)
			out.Write(field)
		} else {
			writeU32(&out, order, currentExternalOffset)
			currentExternalOffset += uint32(len(external[i]))
		}
	}
	writeU32(&out, order, 0)
	for _, raw := range external {
		if len(raw) > 0 {
			out.Write(raw)
		}
	}
	for _, block := range cfg.blocks {
		out.Write(block)
	}
	return out.Bytes()
}

func encRepeatedShort(order binary.ByteOrder, value uint16, n int) []byte {
	vals := make([]uint16, n)
	for i := range vals {
		vals[i] = value
	}
	return encShorts(order, vals...)
}

func encBlockCounts(order binary.ByteOrder, blocks [][]byte) []byte {
	vals := make([]uint32, len(blocks))
	for i, block := range blocks {
		vals[i] = uint32(len(block))
	}
	return encLongs(order, vals...)
}

func encOffsets(order binary.ByteOrder, vals []uint32) []byte {
	return encLongs(order, vals...)
}

func encShorts(order binary.ByteOrder, vals ...uint16) []byte {
	out := make([]byte, len(vals)*2)
	for i, v := range vals {
		order.PutUint16(out[i*2:], v)
	}
	return out
}

func encInt16s(order binary.ByteOrder, vals ...int16) []byte {
	out := make([]byte, len(vals)*2)
	for i, v := range vals {
		order.PutUint16(out[i*2:], uint16(v))
	}
	return out
}

func encLongs(order binary.ByteOrder, vals ...uint32) []byte {
	out := make([]byte, len(vals)*4)
	for i, v := range vals {
		order.PutUint32(out[i*4:], v)
	}
	return out
}

func encDoubles(order binary.ByteOrder, vals ...float64) []byte {
	out := make([]byte, len(vals)*8)
	for i, v := range vals {
		order.PutUint64(out[i*8:], math.Float64bits(v))
	}
	return out
}

func encFloat32s(order binary.ByteOrder, vals ...float32) []byte {
	out := make([]byte, len(vals)*4)
	for i, v := range vals {
		order.PutUint32(out[i*4:], math.Float32bits(v))
	}
	return out
}

func encodeFloatingPointPredictorFixture(order binary.ByteOrder, raw []byte, sampleBytes, stride int) []byte {
	sampleCount := len(raw) / sampleBytes
	tmp := make([]byte, len(raw))
	for sample := 0; sample < sampleCount; sample++ {
		for b := 0; b < sampleBytes; b++ {
			dstByte := b
			if order == binary.LittleEndian {
				dstByte = sampleBytes - b - 1
			}
			tmp[dstByte*sampleCount+sample] = raw[sample*sampleBytes+b]
		}
	}
	for i := len(tmp) - 1; i >= stride; i-- {
		tmp[i] -= tmp[i-stride]
	}
	return tmp
}

func writeU16(buf *bytes.Buffer, order binary.ByteOrder, v uint16) {
	var tmp [2]byte
	order.PutUint16(tmp[:], v)
	buf.Write(tmp[:])
}

func writeU32(buf *bytes.Buffer, order binary.ByteOrder, v uint32) {
	var tmp [4]byte
	order.PutUint32(tmp[:], v)
	buf.Write(tmp[:])
}

func makeJPEGBlock(t *testing.T, c color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.SetRGBA(0, 0, c)
	var jpegBlock bytes.Buffer
	if err := jpeg.Encode(&jpegBlock, img, &jpeg.Options{Quality: 100}); err != nil {
		t.Fatal(err)
	}
	return jpegBlock.Bytes()
}

func splitJPEGForTables(t *testing.T, jpegData []byte) ([]byte, []byte) {
	t.Helper()
	if len(jpegData) < 4 || jpegData[0] != 0xff || jpegData[1] != 0xd8 {
		t.Fatalf("fixture is not a JPEG stream")
	}
	sos := -1
	for i := 2; i+1 < len(jpegData); i++ {
		if jpegData[i] == 0xff && jpegData[i+1] == 0xda {
			sos = i
			break
		}
	}
	if sos < 0 {
		t.Fatalf("fixture JPEG has no SOS marker")
	}
	tables := append([]byte(nil), jpegData[:sos]...)
	tables = append(tables, 0xff, 0xd9)
	scan := append([]byte(nil), jpegData[sos:len(jpegData)-2]...)
	return tables, scan
}

func mustBase64(t *testing.T, s string) []byte {
	t.Helper()
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func decodeWebPFixtureColor(t *testing.T, data []byte) Color {
	t.Helper()
	img, err := webp.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode WebP fixture: %v", err)
	}
	r, g, b, a := img.At(img.Bounds().Min.X, img.Bounds().Min.Y).RGBA()
	return Color{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: uint8(a >> 8)}
}

const tinyWebPBase64 = "UklGRiIAAABXRUJQVlA4IBYAAAAwAQCdASoBAAEADsD+JaQAA3AAAAAA"
