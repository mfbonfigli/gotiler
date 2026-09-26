package geotiffcolorizer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGeoTIFFTestDataDatasetCompatibility(t *testing.T) {
	root := os.Getenv("GEOTIFF_TEST_DATA")
	if root == "" {
		t.Skip("set GEOTIFF_TEST_DATA to run external GeoTIFF corpus assertions")
	}

	fixtures := []struct {
		rel          string
		width        int
		height       int
		bands        int
		crsPrefix    string
		hasTransform bool
	}{
		{"files/GA4886_VanderfordGlacier_2022_EGM2008_64m-epsg3031.cog", 2581, 1998, 1, "EPSG:3031", true},
		{"files/GeogToWGS84GeoKey5.tif", 101, 101, 1, `GEOGCS["User-defined geographic CRS"`, true},
		{"files/LisbonElevation.tif", 547, 421, 1, "EPSG:4326", true},
		{"files/abetow-ERD2018-EBIRD_SCIENCE-20191109-a5cf4cb2_hr_2018_abundance_median.tiff", 7074, 5630, 52, `PROJCS["unnamed"`, true},
		{"files/bremen_sea_ice_conc_2022_9_9.tif", 1264, 1327, 1, "EPSG:3031", true},
		{"files/dom1_32_356_5699_1_nw_2020.tif", 1000, 1000, 1, "EPSG:25832", true},
		{"files/eu_pasture.tiff", 860, 638, 1, "EPSG:4326", true},
		{"files/ga_ls_tc_pc_cyear_3_x17y37_2022--P1Y_final_wet_pc_50_LQ.tif", 1600, 1600, 1, "EPSG:3577", true},
		{"files/gadas-cyprus.tif", 1259, 495, 4, "EPSG:4326", true},
		{"files/gadas-world.tif", 1220, 580, 4, "", true},
		{"files/gadas.tif", 968, 475, 4, `PROJCS["WGS 84 / Pseudo-Mercator"`, true},
		{"files/gfw-azores.tif", 125, 40, 1, "EPSG:4326", true},
		{"files/gpm_1d.20240617.tif", 1500, 400, 1, "EPSG:4326", true},
		{"files/lcv_landuse.cropland_hyde_p_10km_s0..0cm_2016_v3.2.tif", 4320, 1792, 1, "EPSG:4326", true},
		{"files/no_pixelscale_or_tiepoints.tiff", 598, 279, 1, "EPSG:4326", true},
		{"files/nt_20201024_f18_nrt_s.tif", 316, 332, 1, `PROJCS["unknown"`, true},
		{"files/nz_habitat_anticross_4326_1deg.tif", 360, 31, 1, "EPSG:4326", true},
		{"files/umbra_mount_yasur.tiff", 2000, 2000, 1, "EPSG:32759", true},
		{"files/utm.tif", 100, 100, 1, "EPSG:32617", true},
		{"files/vestfold.tif", 950, 880, 1, `PROJCS["unknown"`, true},
		{"files/wildfires.tiff", 1052, 784, 3, "EPSG:4326", true},
		{"files/wind_direction.tif", 232, 193, 1, "EPSG:4326", true},
	}

	for _, fixture := range fixtures {
		t.Run(fixture.rel, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(fixture.rel)))
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			ds, err := OpenDataset(fixture.rel, data, WithBlockCacheSize(4))
			if err != nil {
				t.Fatalf("OpenDataset: %v", err)
			}
			if ds.Width != fixture.width || ds.Height != fixture.height || len(ds.Bands) != fixture.bands {
				t.Fatalf("shape = %dx%d bands=%d, want %dx%d bands=%d", ds.Width, ds.Height, len(ds.Bands), fixture.width, fixture.height, fixture.bands)
			}
			if ds.HasTransform != fixture.hasTransform {
				t.Fatalf("HasTransform = %v, want %v", ds.HasTransform, fixture.hasTransform)
			}
			if fixture.crsPrefix == "" {
				if ds.CRS != "" {
					t.Fatalf("CRS = %q, want empty", ds.CRS)
				}
			} else if !strings.HasPrefix(ds.CRS, fixture.crsPrefix) {
				t.Fatalf("CRS = %q, want prefix %q", ds.CRS, fixture.crsPrefix)
			}
			if _, ok, err := ds.RawPixel(0, 0); err != nil || !ok {
				t.Fatalf("RawPixel(0,0) ok=%v err=%v", ok, err)
			}
		})
	}
}
