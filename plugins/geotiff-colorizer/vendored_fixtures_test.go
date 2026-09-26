package geotiffcolorizer

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// The fixtures in testdata/ are vendored from GDAL's autotest suite or
// generated with gdal_translate (see testdata/README.md). Their expected
// shapes, geotransforms, CRS codes and sample pixel values were captured
// once from GDAL into testdata/expectations.json; the test itself has no
// GDAL dependency and runs self-contained on CI.

type fixtureExpectation struct {
	File         string    `json:"file"`
	Width        int       `json:"width"`
	Height       int       `json:"height"`
	Bands        int       `json:"bands"`
	EPSG         int       `json:"epsg"`
	GeoTransform []float64 `json:"geotransform"`
	PixelIsPoint bool      `json:"pixelIsPoint"`
	Tolerance    float64   `json:"tolerance"`
	Points       []struct {
		X      int      `json:"x"`
		Y      int      `json:"y"`
		Values []string `json:"values"`
	} `json:"points"`
}

func TestVendoredFixturesMatchGDALExpectations(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "expectations.json"))
	if err != nil {
		t.Fatalf("read expectations: %v", err)
	}
	var fixtures []fixtureExpectation
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatalf("parse expectations: %v", err)
	}
	if len(fixtures) == 0 {
		t.Fatal("no fixture expectations")
	}
	for _, fixture := range fixtures {
		t.Run(fixture.File, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", fixture.File))
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			ds, err := OpenDataset(fixture.File, data, WithBlockCacheSize(2))
			if err != nil {
				t.Fatalf("OpenDataset: %v", err)
			}
			if ds.Width != fixture.Width || ds.Height != fixture.Height {
				t.Fatalf("size = %dx%d, want %dx%d", ds.Width, ds.Height, fixture.Width, fixture.Height)
			}
			if len(ds.Bands) != fixture.Bands {
				t.Fatalf("bands = %d, want %d", len(ds.Bands), fixture.Bands)
			}
			if fixture.EPSG != 0 {
				if want := fmt.Sprintf("EPSG:%d", fixture.EPSG); ds.CRS != want {
					t.Fatalf("CRS = %q, want %q", ds.CRS, want)
				}
			}
			assertFixtureGeoTransform(t, ds, fixture)

			for _, p := range fixture.Points {
				got, ok, err := ds.RawPixel(p.X, p.Y)
				if err != nil {
					t.Fatalf("RawPixel(%d,%d): %v", p.X, p.Y, err)
				}
				if !ok {
					t.Fatalf("RawPixel(%d,%d) missed", p.X, p.Y)
				}
				if len(got) != len(p.Values) {
					t.Fatalf("RawPixel(%d,%d) bands = %d, want %d", p.X, p.Y, len(got), len(p.Values))
				}
				for band, text := range p.Values {
					want, err := strconv.ParseFloat(text, 64)
					if err != nil {
						t.Fatalf("bad expectation %q: %v", text, err)
					}
					if !floatNear(got[band], want, fixture.Tolerance) {
						t.Fatalf("RawPixel(%d,%d)[%d] = %.17g, want %.17g", p.X, p.Y, band, got[band], want)
					}
				}
			}
		})
	}
}

func assertFixtureGeoTransform(t *testing.T, ds *Dataset, fixture fixtureExpectation) {
	t.Helper()
	if len(fixture.GeoTransform) != 6 {
		return
	}
	if !ds.HasTransform {
		t.Fatalf("dataset has no transform, want %v", fixture.GeoTransform)
	}
	gt := fixture.GeoTransform
	wantC, wantF := gt[0], gt[3]
	if fixture.PixelIsPoint && ds.RasterType == RasterPixelIsPoint {
		// GDAL reports PixelIsPoint rasters shifted to corner convention;
		// this reader keeps the tiepoint at the sample point.
		wantC = gt[0] + 0.5*gt[1] + 0.5*gt[2]
		wantF = gt[3] + 0.5*gt[4] + 0.5*gt[5]
	}
	got := []float64{ds.Transform.A, ds.Transform.B, ds.Transform.D, ds.Transform.E, ds.Transform.C, ds.Transform.F}
	want := []float64{gt[1], gt[2], gt[4], gt[5], wantC, wantF}
	for i := range got {
		tolerance := 1e-9 * math.Max(1, math.Abs(want[i]))
		if math.Abs(got[i]-want[i]) > tolerance {
			t.Fatalf("geotransform[%d] = %.17g, want %.17g (full %v, want %v)", i, got[i], want[i], got, want)
		}
	}
}

func floatNear(got, want, tolerance float64) bool {
	if math.IsNaN(got) || math.IsNaN(want) {
		return math.IsNaN(got) && math.IsNaN(want)
	}
	if math.IsInf(got, 0) || math.IsInf(want, 0) {
		return got == want
	}
	if tolerance == 0 {
		return got == want
	}
	return math.Abs(got-want) <= tolerance
}
