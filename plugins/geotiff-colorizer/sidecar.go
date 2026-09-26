package geotiffcolorizer

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// sidecarGeoref carries georeferencing read from files next to a TIFF, in
// GDAL precedence order: PAM (.aux.xml) beats internal TIFF tags, which beat
// world files (.tfw/.wld). Geotransforms use the GDAL element order
// [C A B F D E] mapped from x = A*i + B*j + C, y = D*i + E*j + F.
type sidecarGeoref struct {
	pamGeoTransform   []float64
	pamSRS            string
	pamNoData         map[int]float64
	pamScale          map[int]float64
	pamOffset         map[int]float64
	worldGeoTransform []float64
}

func (s *sidecarGeoref) hasCRS() bool {
	return s != nil && s.pamSRS != ""
}

func (s *sidecarGeoref) empty() bool {
	return s == nil || (len(s.pamGeoTransform) == 0 && s.pamSRS == "" &&
		len(s.pamNoData) == 0 && len(s.pamScale) == 0 && len(s.pamOffset) == 0 &&
		len(s.worldGeoTransform) == 0)
}

// loadSidecarFiles collects georeferencing sidecars for filename. Missing or
// malformed sidecars are ignored; the TIFF's own metadata then applies.
func loadSidecarFiles(filename string) *sidecarGeoref {
	s := &sidecarGeoref{}
	if data, err := os.ReadFile(filename + ".aux.xml"); err == nil {
		parsePAMDataset(data, s)
	}
	for _, candidate := range worldFileCandidates(filename) {
		data, err := os.ReadFile(candidate)
		if err != nil {
			continue
		}
		if gt := parseWorldFile(data); gt != nil {
			s.worldGeoTransform = gt
			break
		}
	}
	if s.empty() {
		return nil
	}
	return s
}

// worldFileCandidates lists world-file names for an image path following the
// GDAL conventions: first+last extension letter + "w" (tif -> tfw), the full
// extension + "w" (tif -> tifw), and the generic .wld.
func worldFileCandidates(filename string) []string {
	ext := filepath.Ext(filename)
	base := strings.TrimSuffix(filename, ext)
	e := strings.TrimPrefix(ext, ".")
	var out []string
	if len(e) >= 2 {
		out = append(out, base+"."+string(e[0])+string(e[len(e)-1])+"w")
	}
	if len(e) >= 1 {
		out = append(out, base+"."+e+"w")
	}
	return append(out, base+".wld")
}

// parseWorldFile converts the six world-file values to a GDAL-order
// geotransform. World files describe the center of the top-left pixel, so the
// origin is shifted back by half a pixel. Trailing commas appear in the wild
// and are tolerated.
func parseWorldFile(data []byte) []float64 {
	fields := strings.FieldsFunc(string(data), func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\r' || r == '\n' || r == ','
	})
	if len(fields) < 6 {
		return nil
	}
	vals := make([]float64, 6)
	for i := range vals {
		v, err := strconv.ParseFloat(fields[i], 64)
		if err != nil {
			return nil
		}
		vals[i] = v
	}
	a, d, b, e := vals[0], vals[1], vals[2], vals[3]
	c := vals[4] - 0.5*a - 0.5*b
	f := vals[5] - 0.5*d - 0.5*e
	return []float64{c, a, b, f, d, e}
}

type pamDatasetXML struct {
	GeoTransform string       `xml:"GeoTransform"`
	SRS          string       `xml:"SRS"`
	Bands        []pamBandXML `xml:"PAMRasterBand"`
}

type pamBandXML struct {
	Band        int    `xml:"band,attr"`
	NoDataValue string `xml:"NoDataValue"`
	Scale       string `xml:"Scale"`
	Offset      string `xml:"Offset"`
}

func parsePAMDataset(data []byte, s *sidecarGeoref) {
	var doc pamDatasetXML
	if err := xml.Unmarshal(data, &doc); err != nil {
		return
	}
	if gt := parseGeoTransformList(doc.GeoTransform); gt != nil {
		s.pamGeoTransform = gt
	}
	s.pamSRS = strings.TrimSpace(doc.SRS)
	for _, band := range doc.Bands {
		index := band.Band - 1
		if index < 0 {
			continue
		}
		if v, err := strconv.ParseFloat(strings.TrimSpace(band.NoDataValue), 64); err == nil {
			if s.pamNoData == nil {
				s.pamNoData = make(map[int]float64)
			}
			s.pamNoData[index] = v
		}
		if v, err := strconv.ParseFloat(strings.TrimSpace(band.Scale), 64); err == nil {
			if s.pamScale == nil {
				s.pamScale = make(map[int]float64)
			}
			s.pamScale[index] = v
		}
		if v, err := strconv.ParseFloat(strings.TrimSpace(band.Offset), 64); err == nil {
			if s.pamOffset == nil {
				s.pamOffset = make(map[int]float64)
			}
			s.pamOffset[index] = v
		}
	}
}

func parseGeoTransformList(text string) []float64 {
	fields := strings.FieldsFunc(text, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\r' || r == '\n'
	})
	if len(fields) != 6 {
		return nil
	}
	vals := make([]float64, 6)
	for i := range vals {
		v, err := strconv.ParseFloat(fields[i], 64)
		if err != nil {
			return nil
		}
		vals[i] = v
	}
	return vals
}

func rasterTransformFromGeoTransform(gt []float64) (RasterTransform, error) {
	return newRasterTransform(gt[1], gt[2], gt[0], gt[4], gt[5], gt[3])
}

// crsFromWKT returns "EPSG:code" when the WKT's outermost authority is an
// EPSG code, otherwise the WKT itself.
func crsFromWKT(wkt string) string {
	wkt = strings.TrimSpace(wkt)
	const marker = `AUTHORITY["EPSG","`
	idx := strings.LastIndex(wkt, marker)
	if idx < 0 {
		return wkt
	}
	rest := wkt[idx+len(marker):]
	end := strings.IndexByte(rest, '"')
	if end <= 0 {
		return wkt
	}
	code := rest[:end]
	if _, err := strconv.Atoi(code); err != nil {
		return wkt
	}
	// Only trust the authority if it closes the WKT (i.e. it belongs to the
	// top-level CRS node, not a nested datum or unit).
	for _, r := range rest[end:] {
		if r != '"' && r != ']' && r != ' ' {
			return wkt
		}
	}
	return "EPSG:" + code
}

// overrideNoDataValues expands base to one entry per band (repeating the last
// value, matching noDataForBand) and applies per-band overrides.
func overrideNoDataValues(base []*float64, samples int, overrides map[int]float64) []*float64 {
	out := make([]*float64, samples)
	for i := range out {
		out[i] = noDataForBand(base, i)
	}
	for band, v := range overrides {
		if band >= 0 && band < samples {
			value := v
			out[band] = &value
		}
	}
	return out
}
