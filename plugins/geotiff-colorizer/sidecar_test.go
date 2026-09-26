package geotiffcolorizer

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func writeSidecarFixture(t *testing.T, dir, name string, cfg testTIFFConfig) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, buildTestGeoTIFF(t, binary.LittleEndian, cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func grayNoGeorefConfig() testTIFFConfig {
	return testTIFFConfig{
		width:      4,
		height:     4,
		samples:    1,
		photo:      tiffPhotometricBlackIsZero,
		compress:   tiffCompressionNone,
		blocks:     [][]byte{make([]byte, 16)},
		omitGeoKey: true,
	}
}

func TestWorldFileProvidesTransform(t *testing.T) {
	for _, ext := range []string{".tfw", ".wld"} {
		t.Run(ext, func(t *testing.T) {
			dir := t.TempDir()
			path := writeSidecarFixture(t, dir, "image.tif", grayNoGeorefConfig())
			// World-file convention: values are pixel-center based and lines may
			// carry trailing commas (seen in GDAL's own test data).
			world := "0.2,\n0\n0\n-0.2\n250000.10001\n5886999.9\n"
			if err := os.WriteFile(filepath.Join(dir, "image"+ext), []byte(world), 0o644); err != nil {
				t.Fatal(err)
			}
			ds, err := OpenDatasetFile(path)
			if err != nil {
				t.Fatalf("OpenDatasetFile: %v", err)
			}
			defer ds.Close()
			if !ds.HasTransform {
				t.Fatal("expected transform from world file")
			}
			got := []float64{ds.Transform.C, ds.Transform.A, ds.Transform.B, ds.Transform.F, ds.Transform.D, ds.Transform.E}
			want := []float64{250000.00001, 0.2, 0, 5887000, 0, -0.2}
			for i := range want {
				if math.Abs(got[i]-want[i]) > 1e-9 {
					t.Fatalf("geotransform[%d] = %.17g, want %.17g (full %v)", i, got[i], want[i], got)
				}
			}
		})
	}
}

func TestWorldFileNotUsedWhenInternalTransformExists(t *testing.T) {
	dir := t.TempDir()
	cfg := grayNoGeorefConfig()
	cfg.scale = []float64{60, 60, 0}
	cfg.tiepoint = []float64{0, 0, 0, 440720, 3751320, 0}
	path := writeSidecarFixture(t, dir, "image.tif", cfg)
	if err := os.WriteFile(filepath.Join(dir, "image.tfw"), []byte("1\n0\n0\n-1\n100\n200\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ds, err := OpenDatasetFile(path)
	if err != nil {
		t.Fatalf("OpenDatasetFile: %v", err)
	}
	defer ds.Close()
	if ds.Transform.A != 60 || ds.Transform.C != 440720 {
		t.Fatalf("internal transform expected, got %+v", ds.Transform)
	}
}

func TestPAMGeoTransformOverridesInternal(t *testing.T) {
	dir := t.TempDir()
	cfg := grayNoGeorefConfig()
	cfg.scale = []float64{60, 60, 0}
	cfg.tiepoint = []float64{0, 0, 0, 440720, 3751320, 0}
	path := writeSidecarFixture(t, dir, "image.tif", cfg)
	pam := `<PAMDataset><SRS>LOCAL_CS["PAM"]</SRS><GeoTransform>1,2,3,4,5,6</GeoTransform></PAMDataset>`
	if err := os.WriteFile(path+".aux.xml", []byte(pam), 0o644); err != nil {
		t.Fatal(err)
	}
	ds, err := OpenDatasetFile(path)
	if err != nil {
		t.Fatalf("OpenDatasetFile: %v", err)
	}
	defer ds.Close()
	got := []float64{ds.Transform.C, ds.Transform.A, ds.Transform.B, ds.Transform.F, ds.Transform.D, ds.Transform.E}
	want := []float64{1, 2, 3, 4, 5, 6}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("geotransform[%d] = %v, want %v (full %v)", i, got[i], want[i], got)
		}
	}
	if ds.CRS != `LOCAL_CS["PAM"]` {
		t.Fatalf("CRS = %q, want PAM SRS", ds.CRS)
	}
}

func TestPAMBandMetadataOverrides(t *testing.T) {
	dir := t.TempDir()
	cfg := grayNoGeorefConfig()
	cfg.blocks = [][]byte{{
		255, 1, 2, 3,
		4, 5, 6, 7,
		8, 9, 10, 11,
		12, 13, 14, 15,
	}}
	path := writeSidecarFixture(t, dir, "image.tif", cfg)
	pam := `<PAMDataset>
  <SRS>PROJCS["NAD27 / UTM zone 11N",AUTHORITY["EPSG","26711"]]</SRS>
  <PAMRasterBand band="1">
    <NoDataValue>255</NoDataValue>
    <Scale>2</Scale>
    <Offset>10</Offset>
  </PAMRasterBand>
</PAMDataset>`
	if err := os.WriteFile(path+".aux.xml", []byte(pam), 0o644); err != nil {
		t.Fatal(err)
	}
	ds, err := OpenDatasetFile(path)
	if err != nil {
		t.Fatalf("OpenDatasetFile: %v", err)
	}
	defer ds.Close()
	if ds.CRS != "EPSG:26711" {
		t.Fatalf("CRS = %q, want EPSG:26711 extracted from PAM WKT", ds.CRS)
	}
	band := ds.Bands[0]
	if band.NoData == nil || *band.NoData != 255 {
		t.Fatalf("Bands[0].NoData = %v, want 255", band.NoData)
	}
	if band.Scale != 2 || band.Offset != 10 {
		t.Fatalf("Bands[0] scale/offset = %v/%v, want 2/10", band.Scale, band.Offset)
	}
	if _, ok, err := ds.Sample(0, 0, 0); err != nil || ok {
		t.Fatalf("Sample(0,0) on nodata pixel: ok=%v err=%v, want masked", ok, err)
	}
	v, ok, err := ds.Sample(1, 0, 0)
	if err != nil || !ok || v != 1*2+10 {
		t.Fatalf("Sample(1,0) = %v ok=%v err=%v, want 12", v, ok, err)
	}
}

func TestWithoutSidecarFilesDisablesSidecars(t *testing.T) {
	dir := t.TempDir()
	path := writeSidecarFixture(t, dir, "image.tif", grayNoGeorefConfig())
	pam := `<PAMDataset><GeoTransform>1,2,3,4,5,6</GeoTransform></PAMDataset>`
	if err := os.WriteFile(path+".aux.xml", []byte(pam), 0o644); err != nil {
		t.Fatal(err)
	}
	ds, err := OpenDatasetFile(path, WithoutSidecarFiles())
	if err != nil {
		t.Fatalf("OpenDatasetFile: %v", err)
	}
	defer ds.Close()
	if ds.HasTransform {
		t.Fatal("sidecar transform applied despite WithoutSidecarFiles")
	}
}

// A color GeoTIFF with no internal georeferencing but a world file and a PAM
// SRS must be usable as an orthophoto (the strict checks accept sidecar CRS
// and transform sources).
func TestOpenOrthophotoFileWithSidecarGeoref(t *testing.T) {
	dir := t.TempDir()
	rgb := make([]byte, 4*4*3)
	for i := range rgb {
		rgb[i] = byte(i)
	}
	cfg := testTIFFConfig{
		width:      4,
		height:     4,
		samples:    3,
		bits:       []uint16{8, 8, 8},
		photo:      tiffPhotometricRGB,
		compress:   tiffCompressionNone,
		blocks:     [][]byte{rgb},
		omitGeoKey: true,
	}
	path := writeSidecarFixture(t, dir, "ortho.tif", cfg)
	if err := os.WriteFile(filepath.Join(dir, "ortho.tfw"), []byte("1\n0\n0\n-1\n100.5\n200.5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pam := `<PAMDataset><SRS>PROJCS["x",AUTHORITY["EPSG","32611"]]</SRS></PAMDataset>`
	if err := os.WriteFile(path+".aux.xml", []byte(pam), 0o644); err != nil {
		t.Fatal(err)
	}
	ortho, err := OpenOrthophotoFile(path)
	if err != nil {
		t.Fatalf("OpenOrthophotoFile: %v", err)
	}
	defer ortho.Close()
	if ortho.CRS != "EPSG:32611" {
		t.Fatalf("CRS = %q, want EPSG:32611", ortho.CRS)
	}
	// world file center (100.5, 200.5) -> pixel (0,0) spans x [100,101].
	c, ok, err := ortho.SampleModel(100.5, 200.5, SampleNearest)
	if err != nil || !ok {
		t.Fatalf("SampleModel: ok=%v err=%v", ok, err)
	}
	if c.R != 0 || c.G != 1 || c.B != 2 {
		t.Fatalf("SampleModel color = %+v, want pixel (0,0) = {0 1 2}", c)
	}
}

// Spot checks against GDAL's own corpus sidecar files.
func TestGDALCorpusSidecarGeoref(t *testing.T) {
	root := "testdata"
	for _, tc := range []struct {
		rel  string
		want []float64 // GDAL geotransform order
	}{
		{rel: "byte_nogeoref.tif", want: []float64{1, 2, 3, 4, 5, 6}},                                  // PAM wins over .tfw
		{rel: "byte_inconsistent_georef.tif", want: []float64{1, 2, 3, 4, 5, 6}},                       // PAM wins over internal tags
		{rel: "projection_from_esri_xml.tif", want: []float64{250000.00001, 0.2, 0, 5887000, 0, -0.2}}, // world file
	} {
		t.Run(tc.rel, func(t *testing.T) {
			ds, err := OpenDatasetFile(filepath.Join(root, tc.rel))
			if err != nil {
				t.Fatalf("OpenDatasetFile: %v", err)
			}
			defer ds.Close()
			if !ds.HasTransform {
				t.Fatal("expected sidecar transform")
			}
			got := []float64{ds.Transform.C, ds.Transform.A, ds.Transform.B, ds.Transform.F, ds.Transform.D, ds.Transform.E}
			for i := range tc.want {
				if math.Abs(got[i]-tc.want[i]) > 1e-9 {
					t.Fatalf("geotransform[%d] = %.17g, want %.17g (full %v)", i, got[i], tc.want[i], got)
				}
			}
		})
	}
}
