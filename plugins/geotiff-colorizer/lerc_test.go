package geotiffcolorizer

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

// The LERC fixtures are lossless (MAX_Z_ERROR=0), so decoded values must
// match the uncompressed reference rasters exactly.
func TestOpenDatasetLERCCorpus(t *testing.T) {
	root := "testdata"
	gcore := "testdata"

	openRef := func(t *testing.T, path string) *Dataset {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		ds, err := OpenDataset(filepath.Base(path), data)
		if err != nil {
			t.Fatalf("open %s: %v", path, err)
		}
		return ds
	}

	for _, tc := range []struct {
		lerc string
		ref  string
	}{
		{filepath.Join(root, "byte_LERC.tif"), filepath.Join(root, "byte_NONE.tif")},
		{filepath.Join(root, "byte_LERC_tiled.tif"), filepath.Join(root, "byte_NONE.tif")},
		{filepath.Join(root, "byte_LERC_DEFLATE.tif"), filepath.Join(root, "byte_NONE.tif")},
		{filepath.Join(root, "byte_LERC_ZSTD.tif"), filepath.Join(root, "byte_NONE.tif")},
		{filepath.Join(root, "rgbsmall_LERC.tif"), filepath.Join(root, "rgbsmall_NONE.tif")},
		{filepath.Join(root, "rgbsmall_LERC_separate.tif"), filepath.Join(root, "rgbsmall_NONE.tif")},
		{filepath.Join(root, "rgbsmall_LERC_tiled.tif"), filepath.Join(root, "rgbsmall_NONE.tif")},
		{filepath.Join(root, "rgbsmall_LERC_tiled_separate.tif"), filepath.Join(root, "rgbsmall_NONE.tif")},
		{filepath.Join(root, "lerc_int16.tif"), filepath.Join(gcore, "int16.tif")},
		{filepath.Join(root, "lerc_uint16.tif"), filepath.Join(gcore, "uint16.tif")},
		{filepath.Join(root, "lerc_int32.tif"), filepath.Join(gcore, "int32.tif")},
		{filepath.Join(root, "lerc_uint32.tif"), filepath.Join(gcore, "uint32.tif")},
	} {
		t.Run(filepath.Base(tc.lerc), func(t *testing.T) {
			lerc := openRef(t, tc.lerc)
			ref := openRef(t, tc.ref)
			if lerc.Width != ref.Width || lerc.Height != ref.Height || len(lerc.Bands) != len(ref.Bands) {
				t.Fatalf("shape %dx%d/%d, ref %dx%d/%d", lerc.Width, lerc.Height, len(lerc.Bands), ref.Width, ref.Height, len(ref.Bands))
			}
			for y := 0; y < lerc.Height; y++ {
				for x := 0; x < lerc.Width; x++ {
					got, okG, errG := lerc.RawPixel(x, y)
					want, okW, errW := ref.RawPixel(x, y)
					if errG != nil || errW != nil || !okG || !okW {
						t.Fatalf("(%d,%d): got ok=%v err=%v, ref ok=%v err=%v", x, y, okG, errG, okW, errW)
					}
					for b := range want {
						if got[b] != want[b] {
							t.Fatalf("(%d,%d) band %d: got %v want %v", x, y, b, got[b], want[b])
						}
					}
				}
			}
		})
	}
}

// Float LERC blobs carry a validity mask; masked pixels decode to NaN, the
// same behavior as libtiff.
func TestOpenDatasetLERCFloatMask(t *testing.T) {
	for _, name := range []string{"lerc_float32_with_mask.tif", "lerc_float64_with_mask.tif"} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", name))
			if err != nil {
				t.Fatal(err)
			}
			ds, err := OpenDataset(name, data)
			if err != nil {
				t.Fatalf("OpenDataset: %v", err)
			}
			nan, valid := 0, 0
			for y := 0; y < ds.Height; y++ {
				for x := 0; x < ds.Width; x++ {
					v, ok, err := ds.RawSample(x, y, 0)
					if err != nil || !ok {
						t.Fatalf("(%d,%d): ok=%v err=%v", x, y, ok, err)
					}
					if math.IsNaN(v) {
						nan++
					} else {
						valid++
					}
				}
			}
			if nan == 0 || valid == 0 {
				t.Fatalf("expected both masked and valid pixels, nan=%d valid=%d", nan, valid)
			}
		})
	}
}
