package geotiffcolorizer

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
)

// splitJPEGQuantTables splits an interchange JPEG stream into an abbreviated
// tables stream (SOI + DQT segments + EOI) and an abbreviated image stream
// (SOI + everything else). This mimics how libtiff/GDAL store JPEG-in-TIFF:
// quantization tables live only in the JPEGTables tag while each strip/tile
// still starts with its own SOI marker.
func splitJPEGQuantTables(t *testing.T, full []byte) (tables, strip []byte) {
	t.Helper()
	if len(full) < 4 || full[0] != 0xff || full[1] != 0xd8 {
		t.Fatal("not a JPEG stream")
	}
	tables = []byte{0xff, 0xd8}
	strip = []byte{0xff, 0xd8}
	pos := 2
	for pos+4 <= len(full) {
		if full[pos] != 0xff {
			t.Fatalf("bad JPEG marker byte at %d", pos)
		}
		marker := full[pos+1]
		if marker == 0xda { // SOS: entropy-coded data through EOI stays in the strip
			strip = append(strip, full[pos:]...)
			tables = append(tables, 0xff, 0xd9)
			return tables, strip
		}
		segLen := int(binary.BigEndian.Uint16(full[pos+2:])) + 2
		if pos+segLen > len(full) {
			t.Fatal("truncated JPEG segment")
		}
		seg := full[pos : pos+segLen]
		if marker == 0xdb {
			tables = append(tables, seg...)
		} else {
			strip = append(strip, seg...)
		}
		pos += segLen
	}
	t.Fatal("JPEG stream has no SOS marker")
	return nil, nil
}

// A JPEG strip that starts with SOI but keeps its quantization tables only in
// the JPEGTables tag must decode with those shared tables, not without them.
func TestOpenDatasetJPEGAbbreviatedStripWithSOI(t *testing.T) {
	const size = 16
	src := image.NewGray(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			src.Pix[y*src.Stride+x] = uint8(16*x + y)
		}
	}
	var full bytes.Buffer
	if err := jpeg.Encode(&full, src, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	decoded, err := jpeg.Decode(bytes.NewReader(full.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	tables, strip := splitJPEGQuantTables(t, full.Bytes())

	data := buildTestGeoTIFF(t, binary.LittleEndian, testTIFFConfig{
		width:      size,
		height:     size,
		samples:    1,
		photo:      tiffPhotometricBlackIsZero,
		compress:   tiffCompressionJPEG,
		blocks:     [][]byte{strip},
		jpegTables: tables,
		modelType:  2,
		rasterType: 1,
		epsg:       4326,
	})
	ds, err := OpenDataset("jpeg-abbrev", data)
	if err != nil {
		t.Fatalf("OpenDataset: %v", err)
	}
	for _, p := range [][2]int{{0, 0}, {1, 0}, {7, 3}, {15, 15}} {
		want, _, _, _ := decoded.At(p[0], p[1]).RGBA()
		got, ok, err := ds.RawSample(p[0], p[1], 0)
		if err != nil || !ok {
			t.Fatalf("RawSample(%d,%d): ok=%v err=%v", p[0], p[1], ok, err)
		}
		if uint32(got) != want>>8 {
			t.Fatalf("RawSample(%d,%d) = %v, want %d", p[0], p[1], got, want>>8)
		}
	}
}

// Sparse (offset/count zero) chunky blocks must fill the GDAL nodata value
// into every pixel, not just the first pixel of each row.
func TestSparseChunkyBlockFillsNoDataAllPixels(t *testing.T) {
	for _, tc := range []struct {
		name string
		bits []uint16
	}{
		{name: "rgb8", bits: []uint16{8, 8, 8}},
		{name: "rgb16", bits: []uint16{16, 16, 16}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := buildTestGeoTIFF(t, binary.LittleEndian, testTIFFConfig{
				width:      4,
				height:     3,
				samples:    3,
				bits:       tc.bits,
				photo:      tiffPhotometricRGB,
				compress:   tiffCompressionNone,
				blocks:     [][]byte{{}},
				gdalNoData: "7",
				modelType:  2,
				rasterType: 1,
				epsg:       4326,
			})
			ds, err := OpenDataset("sparse", data)
			if err != nil {
				t.Fatalf("OpenDataset: %v", err)
			}
			for y := 0; y < 3; y++ {
				for x := 0; x < 4; x++ {
					vals, ok, err := ds.RawPixel(x, y)
					if err != nil || !ok {
						t.Fatalf("RawPixel(%d,%d): ok=%v err=%v", x, y, ok, err)
					}
					for b, v := range vals {
						if v != 7 {
							t.Fatalf("RawPixel(%d,%d)[%d] = %v, want nodata 7", x, y, b, v)
						}
					}
				}
			}
		})
	}
}

// A negative ModelPixelScale Y value is non-compliant but appears in the wild;
// GDAL and libtiff interpret it the same way as a positive value (model Y
// decreases as raster row increases). The reader must not flip the raster.
func TestNegativeModelPixelScaleYMatchesPositive(t *testing.T) {
	block := make([]byte, 4*4)
	for i := range block {
		block[i] = byte(i)
	}
	open := func(scaleY float64) *Dataset {
		data := buildTestGeoTIFF(t, binary.LittleEndian, testTIFFConfig{
			width:      4,
			height:     4,
			samples:    1,
			photo:      tiffPhotometricBlackIsZero,
			compress:   tiffCompressionNone,
			blocks:     [][]byte{block},
			scale:      []float64{60, scaleY, 0},
			tiepoint:   []float64{0, 0, 0, 440720, 3751320, 0},
			modelType:  1,
			rasterType: 1,
			epsg:       32611,
		})
		ds, err := OpenDataset("scaley", data)
		if err != nil {
			t.Fatalf("OpenDataset: %v", err)
		}
		return ds
	}
	positive := open(60)
	negative := open(-60)
	if !positive.HasTransform || !negative.HasTransform {
		t.Fatal("expected transforms")
	}
	if positive.Transform != negative.Transform {
		t.Fatalf("negative ScaleY transform %+v differs from positive %+v", negative.Transform, positive.Transform)
	}
	if got := negative.Transform.E; got != -60 {
		t.Fatalf("Transform.E = %v, want -60", got)
	}
}

// Blocks whose data covers only the leading rows (a libtiff-tolerated
// shortcut used by some writers for edge tiles) must decode with the missing
// tail zero-filled instead of failing the whole read.
func TestShortBlockDataIsZeroPadded(t *testing.T) {
	twoRows := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	t.Run("uncompressed-tile", func(t *testing.T) {
		data := buildTestGeoTIFF(t, binary.LittleEndian, testTIFFConfig{
			width:      4,
			height:     4,
			samples:    1,
			photo:      tiffPhotometricBlackIsZero,
			compress:   tiffCompressionNone,
			blocks:     [][]byte{twoRows},
			tileWidth:  4,
			tileHeight: 4,
			modelType:  2,
			rasterType: 1,
			epsg:       4326,
		})
		assertShortBlockPixels(t, data)
	})
	t.Run("packbits-strip", func(t *testing.T) {
		var packed bytes.Buffer
		packed.WriteByte(byte(len(twoRows) - 1)) // one literal run
		packed.Write(twoRows)
		data := buildTestGeoTIFF(t, binary.LittleEndian, testTIFFConfig{
			width:      4,
			height:     4,
			samples:    1,
			photo:      tiffPhotometricBlackIsZero,
			compress:   tiffCompressionPackBits,
			blocks:     [][]byte{packed.Bytes()},
			modelType:  2,
			rasterType: 1,
			epsg:       4326,
		})
		assertShortBlockPixels(t, data)
	})
}

func assertShortBlockPixels(t *testing.T, data []byte) {
	t.Helper()
	ds, err := OpenDataset("short-block", data)
	if err != nil {
		t.Fatalf("OpenDataset: %v", err)
	}
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			want := float64(0)
			if y < 2 {
				want = float64(y*4 + x + 1)
			}
			got, ok, err := ds.RawSample(x, y, 0)
			if err != nil || !ok {
				t.Fatalf("RawSample(%d,%d): ok=%v err=%v", x, y, ok, err)
			}
			if got != want {
				t.Fatalf("RawSample(%d,%d) = %v, want %v", x, y, got, want)
			}
		}
	}
}

// Hostile or corrupt headers can declare blocks whose decoded form would
// require absurd allocations. Opening must fail up front instead of letting
// the first pixel read attempt a multi-gigabyte allocation.
func TestOpenDatasetRejectsHugeDecodedBlocks(t *testing.T) {
	data := buildTestGeoTIFF(t, binary.LittleEndian, testTIFFConfig{
		width:      1 << 20,
		height:     1 << 20,
		samples:    1,
		photo:      tiffPhotometricBlackIsZero,
		compress:   tiffCompressionNone,
		blocks:     [][]byte{{0}},
		modelType:  2,
		rasterType: 1,
		epsg:       4326,
	})
	if _, err := OpenDataset("huge", data); err == nil {
		t.Fatal("OpenDataset accepted a 8 TiB decoded block under the default limit")
	}
	if _, err := OpenDataset("huge", data, WithMaxDecodedBlockMemory(1<<50)); err != nil {
		t.Fatalf("OpenDataset with raised limit: %v", err)
	}
}

// libtiff-written LZMA TIFFs use an XZ container with a Delta+LZMA2 filter
// chain. The fixture pair was generated with GDAL:
//
//	gdal_translate -srcwin 0 0 8 8 -co COMPRESS=LZMA byte.tif byte8_lzma.tif
//	gdal_translate -srcwin 0 0 8 8 byte.tif byte8_none.tif
func TestOpenDatasetLibtiffLZMA(t *testing.T) {
	lzmaData, err := os.ReadFile(filepath.Join("testdata", "byte8_lzma.tif"))
	if err != nil {
		t.Fatal(err)
	}
	noneData, err := os.ReadFile(filepath.Join("testdata", "byte8_none.tif"))
	if err != nil {
		t.Fatal(err)
	}
	lzma, err := OpenDataset("byte8_lzma", lzmaData)
	if err != nil {
		t.Fatalf("OpenDataset(lzma): %v", err)
	}
	plain, err := OpenDataset("byte8_none", noneData)
	if err != nil {
		t.Fatalf("OpenDataset(none): %v", err)
	}
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			want, ok, err := plain.RawSample(x, y, 0)
			if err != nil || !ok {
				t.Fatalf("reference RawSample(%d,%d): ok=%v err=%v", x, y, ok, err)
			}
			got, ok, err := lzma.RawSample(x, y, 0)
			if err != nil || !ok {
				t.Fatalf("lzma RawSample(%d,%d): ok=%v err=%v", x, y, ok, err)
			}
			if got != want {
				t.Fatalf("lzma RawSample(%d,%d) = %v, want %v", x, y, got, want)
			}
		}
	}
}
