//go:build ignore

// Generates expectations.json from the vendored fixtures using GDAL as the
// reference implementation. Run it only when adding fixtures, with the GDAL
// command line tools on PATH:
//
//	cd geotiff-colorizer/testdata && go run gen_expectations.go
//
// The committed expectations.json makes the fixture tests self-contained:
// regular test runs never invoke GDAL.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type expectation struct {
	File         string    `json:"file"`
	Width        int       `json:"width"`
	Height       int       `json:"height"`
	Bands        int       `json:"bands"`
	EPSG         int       `json:"epsg,omitempty"`
	GeoTransform []float64 `json:"geotransform,omitempty"`
	PixelIsPoint bool      `json:"pixelIsPoint,omitempty"`
	Tolerance    float64   `json:"tolerance,omitempty"`
	Points       []point   `json:"points"`
}

type point struct {
	X      int      `json:"x"`
	Y      int      `json:"y"`
	Values []string `json:"values"` // strings so NaN survives JSON
}

type gdalInfo struct {
	Size         []int     `json:"size"`
	GeoTransform []float64 `json:"geoTransform"`
	Bands        []struct {
		Type string `json:"type"`
	} `json:"bands"`
	Metadata map[string]map[string]string `json:"metadata"`
	Stac     struct {
		EPSG *int `json:"proj:epsg"`
	} `json:"stac"`
}

// sidecarFixtures are covered by dedicated tests and excluded from the table
// (their GDAL results depend on world files / PAM, which OpenDataset on a
// byte slice deliberately does not see).
var sidecarFixtures = map[string]bool{
	"byte_nogeoref.tif":            true,
	"byte_inconsistent_georef.tif": true,
	"projection_from_esri_xml.tif": true,
	"byte8_lzma.tif":               true, // paired-decode test fixtures
	"byte8_none.tif":               true,
}

func main() {
	files, err := filepath.Glob("*.tif")
	if err != nil {
		log.Fatal(err)
	}
	sort.Strings(files)
	var out []expectation
	for _, file := range files {
		if sidecarFixtures[file] {
			continue
		}
		info := readInfo(file)
		if len(info.Size) != 2 {
			log.Fatalf("%s: gdalinfo returned no size", file)
		}
		e := expectation{
			File:   file,
			Width:  info.Size[0],
			Height: info.Size[1],
			Bands:  len(info.Bands),
		}
		if info.Stac.EPSG != nil {
			e.EPSG = *info.Stac.EPSG
		}
		if len(info.GeoTransform) == 6 {
			e.GeoTransform = info.GeoTransform
		}
		if strings.EqualFold(info.Metadata[""]["AREA_OR_POINT"], "Point") {
			e.PixelIsPoint = true
		}
		compression := strings.ToUpper(info.Metadata["IMAGE_STRUCTURE"]["COMPRESSION"])
		floatType := false
		doubleType := false
		for _, b := range info.Bands {
			if b.Type == "Float32" {
				floatType = true
			}
			if b.Type == "Float64" {
				doubleType = true
			}
		}
		switch {
		case strings.Contains(compression, "JPEG") || strings.Contains(compression, "WEBP"):
			e.Tolerance = 5
		case doubleType:
			e.Tolerance = 1e-12
		case floatType:
			e.Tolerance = 1e-6
		}
		e.Points = samplePoints(file, e.Width, e.Height, len(info.Bands))
		out = append(out, e)
	}
	buf, err := json.MarshalIndent(out, "", " ")
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile("expectations.json", append(buf, '\n'), 0o644); err != nil {
		log.Fatal(err)
	}
	log.Printf("wrote expectations for %d fixtures", len(out))
}

func readInfo(file string) gdalInfo {
	raw, err := exec.Command("gdalinfo", "-json", file).Output()
	if err != nil {
		log.Fatalf("gdalinfo %s: %v", file, err)
	}
	var info gdalInfo
	if err := json.Unmarshal(raw, &info); err != nil {
		log.Fatalf("parse gdalinfo %s: %v", file, err)
	}
	return info
}

func samplePoints(file string, w, h, bands int) []point {
	candidates := [][2]int{{0, 0}, {w / 2, h / 2}, {w - 1, h - 1}, {1, h / 3}, {w / 3, h - 1}}
	seen := map[[2]int]bool{}
	var pts [][2]int
	var input strings.Builder
	for _, p := range candidates {
		if p[0] >= 0 && p[1] >= 0 && p[0] < w && p[1] < h && !seen[p] {
			seen[p] = true
			pts = append(pts, p)
			fmt.Fprintf(&input, "%d %d\n", p[0], p[1])
		}
	}
	cmd := exec.Command("gdallocationinfo", "-valonly", file)
	cmd.Stdin = strings.NewReader(input.String())
	raw, err := cmd.Output()
	if err != nil {
		log.Fatalf("gdallocationinfo %s: %v", file, err)
	}
	fields := strings.Fields(string(raw))
	if len(fields) != len(pts)*bands {
		log.Fatalf("%s: gdallocationinfo returned %d values, want %d", file, len(fields), len(pts)*bands)
	}
	for _, f := range fields {
		if _, err := strconv.ParseFloat(f, 64); err != nil {
			log.Fatalf("%s: unparseable value %q", file, f)
		}
	}
	out := make([]point, len(pts))
	for i, p := range pts {
		out[i] = point{X: p[0], Y: p[1], Values: fields[i*bands : (i+1)*bands]}
	}
	return out
}
