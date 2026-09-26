package geotiffcolorizer

import (
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// GeoTIFFTagType describes the resolved storage type for one GeoKey.
type GeoTIFFTagType int

const (
	GTTagTypeShort GeoTIFFTagType = iota
	GTTagTypeDouble
	GTTagTypeString
)

// GeoTIFFKey is one resolved entry from the GeoKeyDirectoryTag.
type GeoTIFFKey struct {
	KeyID    uint16
	Type     GeoTIFFTagType
	Count    uint16
	rawValue any // uint16 | []uint16 | float64 | []float64 | string
}

func (k *GeoTIFFKey) Name() string {
	if k == nil {
		return ""
	}
	return GeoTIFFKeyName(int(k.KeyID))
}

func (k *GeoTIFFKey) AsShort() uint16 {
	switch v := k.rawValue.(type) {
	case uint16:
		return v
	case []uint16:
		if len(v) == 0 {
			return 0
		}
		return v[0]
	default:
		panic("GeoTIFFKey is not a SHORT value")
	}
}

func (k *GeoTIFFKey) AsShorts() []uint16 {
	switch v := k.rawValue.(type) {
	case uint16:
		return []uint16{v}
	case []uint16:
		out := make([]uint16, len(v))
		copy(out, v)
		return out
	default:
		panic("GeoTIFFKey is not a SHORT value")
	}
}

func (k *GeoTIFFKey) AsDouble() float64 {
	switch v := k.rawValue.(type) {
	case float64:
		return v
	case []float64:
		if len(v) == 0 {
			return 0
		}
		return v[0]
	default:
		panic("GeoTIFFKey is not a DOUBLE value")
	}
}

func (k *GeoTIFFKey) AsDoubles() []float64 {
	switch v := k.rawValue.(type) {
	case float64:
		return []float64{v}
	case []float64:
		out := make([]float64, len(v))
		copy(out, v)
		return out
	default:
		panic("GeoTIFFKey is not a DOUBLE value")
	}
}

func (k *GeoTIFFKey) AsString() string {
	if k == nil {
		return ""
	}
	return k.rawValue.(string)
}

func (k *GeoTIFFKey) AsStrings() []string {
	s := k.AsString()
	if s == "" {
		return nil
	}
	return strings.Split(s, "|")
}

// GeoTIFFMetadata is the resolved GeoTIFF GeoKey directory.
type GeoTIFFMetadata struct {
	DirectoryVersion uint16
	KeyRevision      uint16
	MinorRevision    uint16
	Keys             map[uint16]*GeoTIFFKey
}

func parseGeoKeysFromIFD(ifd *tiffIFD) (*GeoTIFFMetadata, error) {
	dir, ok := ifd.raw(tiffTagGeoKeyDirectory)
	if !ok {
		return nil, fmt.Errorf("missing mandatory GeoKeyDirectoryTag")
	}
	doubleParams, _ := ifd.raw(tiffTagGeoDoubleParams)
	asciiParams, _ := ifd.raw(tiffTagGeoAsciiParams)
	return ParseGeoTIFFKeys(ifd.order, dir, doubleParams, asciiParams)
}

// ParseGeoTIFFKeys parses GeoTIFF keys from tag payloads. The byte order must
// be the byte order declared by the containing TIFF file.
func ParseGeoTIFFKeys(order binary.ByteOrder, directoryData, doubleParamsData, asciiParamsData []byte) (*GeoTIFFMetadata, error) {
	if len(directoryData) < 8 {
		return nil, fmt.Errorf("GeoKeyDirectoryTag too short (%d bytes, need >= 8)", len(directoryData))
	}
	if len(directoryData)%2 != 0 {
		return nil, fmt.Errorf("GeoKeyDirectoryTag has odd byte length %d", len(directoryData))
	}
	dirShorts := make([]uint16, len(directoryData)/2)
	for i := range dirShorts {
		dirShorts[i] = order.Uint16(directoryData[i*2 : i*2+2])
	}

	geo := &GeoTIFFMetadata{
		DirectoryVersion: dirShorts[0],
		KeyRevision:      dirShorts[1],
		MinorRevision:    dirShorts[2],
		Keys:             make(map[uint16]*GeoTIFFKey, dirShorts[3]),
	}
	if geo.DirectoryVersion != 1 {
		return nil, fmt.Errorf("unsupported GeoTIFF key directory version %d", geo.DirectoryVersion)
	}
	if geo.KeyRevision != 1 {
		return nil, fmt.Errorf("unsupported GeoTIFF key revision %d", geo.KeyRevision)
	}
	if geo.MinorRevision > 2 {
		return nil, fmt.Errorf("unsupported GeoTIFF minor revision %d", geo.MinorRevision)
	}

	numKeys := int(dirShorts[3])
	neededShorts := 4 + numKeys*4
	if neededShorts > len(dirShorts) {
		return nil, fmt.Errorf("GeoKeyDirectoryTag declares %d keys but only has %d shorts", numKeys, len(dirShorts))
	}
	for i := 0; i < numKeys; i++ {
		base := 4 + i*4
		keyID := dirShorts[base]
		location := dirShorts[base+1]
		count := dirShorts[base+2]
		valueOffset := dirShorts[base+3]
		if count == 0 {
			continue
		}
		key, err := resolveGeoKey(order, dirShorts, doubleParamsData, asciiParamsData, keyID, location, count, valueOffset)
		if err != nil {
			return nil, err
		}
		geo.Keys[keyID] = key
	}
	return geo, nil
}

func resolveGeoKey(order binary.ByteOrder, dirShorts []uint16, doubleData, asciiData []byte, keyID, location, count, valueOffset uint16) (*GeoTIFFKey, error) {
	switch location {
	case 0:
		if count != 1 {
			return nil, fmt.Errorf("GeoKey %d inline SHORT has count %d, want 1", keyID, count)
		}
		return &GeoTIFFKey{KeyID: keyID, Type: GTTagTypeShort, Count: count, rawValue: valueOffset}, nil
	case tiffTagGeoKeyDirectory:
		start := int(valueOffset)
		end := start + int(count)
		if start < 0 || end > len(dirShorts) {
			return nil, fmt.Errorf("GeoKey %d SHORT array outside GeoKeyDirectoryTag", keyID)
		}
		vals := make([]uint16, count)
		copy(vals, dirShorts[start:end])
		if count == 1 {
			return &GeoTIFFKey{KeyID: keyID, Type: GTTagTypeShort, Count: count, rawValue: vals[0]}, nil
		}
		return &GeoTIFFKey{KeyID: keyID, Type: GTTagTypeShort, Count: count, rawValue: vals}, nil
	case tiffTagGeoDoubleParams:
		byteOff := int(valueOffset) * 8
		byteEnd := byteOff + int(count)*8
		if byteOff < 0 || byteEnd > len(doubleData) {
			return nil, fmt.Errorf("GeoKey %d DOUBLE array outside GeoDoubleParamsTag", keyID)
		}
		vals := make([]float64, count)
		for i := range vals {
			start := byteOff + i*8
			vals[i] = math.Float64frombits(order.Uint64(doubleData[start : start+8]))
		}
		if count == 1 {
			return &GeoTIFFKey{KeyID: keyID, Type: GTTagTypeDouble, Count: count, rawValue: vals[0]}, nil
		}
		return &GeoTIFFKey{KeyID: keyID, Type: GTTagTypeDouble, Count: count, rawValue: vals}, nil
	case tiffTagGeoAsciiParams:
		start := int(valueOffset)
		end := start + int(count)
		if start < 0 || end > len(asciiData) {
			return nil, fmt.Errorf("GeoKey %d ASCII string outside GeoAsciiParamsTag", keyID)
		}
		s := strings.TrimRight(string(asciiData[start:end]), "\x00")
		s = strings.TrimRight(s, "|")
		return &GeoTIFFKey{KeyID: keyID, Type: GTTagTypeString, Count: count, rawValue: s}, nil
	default:
		return nil, fmt.Errorf("GeoKey %d references unsupported TIFF tag location %d", keyID, location)
	}
}

func (g *GeoTIFFMetadata) Key(id uint16) *GeoTIFFKey {
	if g == nil {
		return nil
	}
	return g.Keys[id]
}

func (g *GeoTIFFMetadata) Short(id uint16) uint16 {
	key := g.Key(id)
	if key == nil || key.Type != GTTagTypeShort {
		return 0
	}
	return key.AsShort()
}

func (g *GeoTIFFMetadata) Double(id uint16) (float64, bool) {
	key := g.Key(id)
	if key == nil || key.Type != GTTagTypeDouble {
		return 0, false
	}
	return key.AsDouble(), true
}

func (g *GeoTIFFMetadata) String(id uint16) string {
	key := g.Key(id)
	if key == nil || key.Type != GTTagTypeString {
		return ""
	}
	return key.AsString()
}

// HorizontalEPSG returns the horizontal EPSG CRS code, if encoded as a standard
// GeoTIFF 1.1 ProjectedCRSGeoKey or GeodeticCRSGeoKey value.
func (g *GeoTIFFMetadata) HorizontalEPSG() (uint16, bool) {
	if g == nil {
		return 0, false
	}
	model := g.Short(1024)
	switch model {
	case 1:
		return g.standardShort(3072)
	case 2, 3:
		if code, ok := g.standardShort(2048); ok {
			return code, true
		}
		return 0, false
	}
	if code, ok := g.standardShort(3072); ok {
		return code, true
	}
	if g.isProjected() {
		return 0, false
	}
	if code, ok := g.standardShort(2048); ok {
		return code, true
	}
	return 0, false
}

func (g *GeoTIFFMetadata) VerticalEPSG() (uint16, bool) {
	if g == nil {
		return 0, false
	}
	return g.standardShort(4096)
}

// CRS returns an EPSG string when possible, otherwise a WKT1 string synthesized
// from user-defined GeoKeys when enough information is present.
func (g *GeoTIFFMetadata) CRS() string {
	if g == nil {
		return ""
	}
	if hor, ok := g.HorizontalEPSG(); ok {
		if vert, ok := g.VerticalEPSG(); ok {
			return fmt.Sprintf("EPSG:%d+%d", hor, vert)
		}
		return fmt.Sprintf("EPSG:%d", hor)
	}
	return g.WKT()
}

func (g *GeoTIFFMetadata) WKT() string {
	if g == nil {
		return ""
	}
	horizontal := ""
	if g.isProjected() {
		horizontal = g.projectedWKT()
	} else if g.isGeodetic() {
		horizontal = g.geogWKT()
	}
	vertical := g.verticalWKT()
	if horizontal != "" && vertical != "" {
		return fmt.Sprintf("COMPD_CS[%s,%s,%s]", quoteWKT(g.compoundName()), horizontal, vertical)
	}
	if horizontal != "" {
		return horizontal
	}
	return vertical
}

func (g *GeoTIFFMetadata) standardShort(keyID uint16) (uint16, bool) {
	code := g.Short(keyID)
	if isGeoTIFFStandardEPSG(code) {
		return code, true
	}
	return 0, false
}

func isGeoTIFFStandardEPSG(code uint16) bool {
	return code >= 1024 && code <= 32766
}

func isGeoTIFFUserDefined(code uint16) bool {
	return code == 32767
}

func (g *GeoTIFFMetadata) isProjected() bool {
	model := g.Short(1024)
	return model == 1 || isGeoTIFFUserDefined(g.Short(3072)) || g.Short(3075) != 0 || g.Short(3074) != 0
}

func (g *GeoTIFFMetadata) isGeodetic() bool {
	model := g.Short(1024)
	return model == 2 || model == 3 || isGeoTIFFUserDefined(g.Short(2048)) || g.Short(2048) != 0 || g.Short(2050) != 0
}

func (g *GeoTIFFMetadata) projectedWKT() string {
	methodCode := g.Short(3075)
	method, ok := projectionMethodWKTNames[methodCode]
	if !ok {
		return ""
	}
	geog := g.geogWKT()
	if geog == "" {
		return ""
	}
	name := firstNonEmpty(g.String(1026), g.String(3073), "User-defined projected CRS")
	unit := g.linearUnit(3076, 3077)
	var b strings.Builder
	b.WriteString("PROJCS[")
	b.WriteString(quoteWKT(cleanCitation(name)))
	b.WriteByte(',')
	b.WriteString(geog)
	b.WriteString(",PROJECTION[")
	b.WriteString(quoteWKT(method))
	b.WriteByte(']')
	for _, param := range projectionWKTParams {
		if v, ok := g.Double(param.keyID); ok {
			b.WriteString(",PARAMETER[")
			b.WriteString(quoteWKT(param.name))
			b.WriteByte(',')
			b.WriteString(formatWKTFloat(v))
			b.WriteByte(']')
		}
	}
	b.WriteByte(',')
	b.WriteString(unit.wkt("UNIT"))
	if code := g.Short(3072); isGeoTIFFStandardEPSG(code) {
		b.WriteString(authorityWKT(code))
	}
	b.WriteByte(']')
	return b.String()
}

func (g *GeoTIFFMetadata) geogWKT() string {
	if code := g.Short(2048); isGeoTIFFStandardEPSG(code) {
		if wkt, ok := geographicCRSWKT(code); ok {
			return wkt
		}
	}
	name := firstNonEmpty(g.String(2049), g.String(1026), "User-defined geographic CRS")
	datum := g.datumWKT()
	if datum == "" {
		return ""
	}
	unit := g.angularUnit(2054, 2055)
	var b strings.Builder
	b.WriteString("GEOGCS[")
	b.WriteString(quoteWKT(cleanCitation(name)))
	b.WriteByte(',')
	b.WriteString(datum)
	b.WriteByte(',')
	b.WriteString(g.primeMeridianWKT())
	b.WriteByte(',')
	b.WriteString(unit.wkt("UNIT"))
	if code := g.Short(2048); isGeoTIFFStandardEPSG(code) {
		b.WriteString(authorityWKT(code))
	}
	b.WriteByte(']')
	return b.String()
}

func (g *GeoTIFFMetadata) datumWKT() string {
	code := g.Short(2050)
	if def, ok := datumDefs[code]; ok {
		ellipsoid := ellipsoidDefs[def.ellipsoid]
		return fmt.Sprintf("DATUM[%s,%s%s]", quoteWKT(def.name), ellipsoid.wkt(), authorityWKT(code))
	}
	name := firstNonEmpty(g.String(2049), "User-defined datum")
	ellipsoid := g.ellipsoidWKT()
	if ellipsoid == "" {
		return ""
	}
	if isGeoTIFFStandardEPSG(code) {
		return fmt.Sprintf("DATUM[%s,%s%s]", quoteWKT(cleanCitation(name)), ellipsoid, authorityWKT(code))
	}
	return fmt.Sprintf("DATUM[%s,%s]", quoteWKT(cleanCitation(name)), ellipsoid)
}

func (g *GeoTIFFMetadata) ellipsoidWKT() string {
	code := g.Short(2056)
	if def, ok := ellipsoidDefs[code]; ok {
		return def.wkt()
	}
	semiMajor, ok := g.Double(2057)
	if !ok {
		return ""
	}
	invFlattening, hasInvFlattening := g.Double(2059)
	if !hasInvFlattening {
		if semiMinor, hasSemiMinor := g.Double(2058); hasSemiMinor && semiMajor != semiMinor {
			invFlattening = semiMajor / (semiMajor - semiMinor)
		}
	}
	name := firstNonEmpty(g.String(2049), "User-defined ellipsoid")
	var b strings.Builder
	b.WriteString("SPHEROID[")
	b.WriteString(quoteWKT(cleanCitation(name)))
	b.WriteByte(',')
	b.WriteString(formatWKTFloat(semiMajor))
	b.WriteByte(',')
	if hasInvFlattening || invFlattening != 0 {
		b.WriteString(formatWKTFloat(invFlattening))
	} else {
		b.WriteByte('0')
	}
	if isGeoTIFFStandardEPSG(code) {
		b.WriteString(authorityWKT(code))
	}
	b.WriteByte(']')
	return b.String()
}

func (g *GeoTIFFMetadata) primeMeridianWKT() string {
	code := g.Short(2051)
	if def, ok := primeMeridianDefs[code]; ok {
		return fmt.Sprintf("PRIMEM[%s,%s%s]", quoteWKT(def.name), formatWKTFloat(def.longitude), authorityWKT(code))
	}
	longitude, ok := g.Double(2061)
	if !ok {
		longitude = 0
	}
	name := "Greenwich"
	if isGeoTIFFUserDefined(code) {
		name = cleanCitation(firstNonEmpty(g.String(2049), "User-defined prime meridian"))
	}
	if isGeoTIFFStandardEPSG(code) {
		return fmt.Sprintf("PRIMEM[%s,%s%s]", quoteWKT(name), formatWKTFloat(longitude), authorityWKT(code))
	}
	return fmt.Sprintf("PRIMEM[%s,%s]", quoteWKT(name), formatWKTFloat(longitude))
}

func (g *GeoTIFFMetadata) verticalWKT() string {
	code := g.Short(4096)
	if code == 0 {
		return ""
	}
	name := firstNonEmpty(g.String(4097), fmt.Sprintf("EPSG:%d", code))
	unit := g.linearUnit(4099, 0)
	datumCode := g.Short(4098)
	datumName := firstNonEmpty(g.String(4097), "User-defined vertical datum")
	var b strings.Builder
	b.WriteString("VERT_CS[")
	b.WriteString(quoteWKT(cleanCitation(name)))
	b.WriteString(",VERT_DATUM[")
	b.WriteString(quoteWKT(cleanCitation(datumName)))
	b.WriteString(",2005")
	if isGeoTIFFStandardEPSG(datumCode) {
		b.WriteString(authorityWKT(datumCode))
	}
	b.WriteByte(']')
	b.WriteByte(',')
	b.WriteString(unit.wkt("UNIT"))
	if isGeoTIFFStandardEPSG(code) {
		b.WriteString(authorityWKT(code))
	}
	b.WriteByte(']')
	return b.String()
}

func (g *GeoTIFFMetadata) linearUnit(codeKeyID, sizeKeyID uint16) wktUnit {
	code := g.Short(codeKeyID)
	if def, ok := linearUnitDefs[code]; ok {
		return def
	}
	if isGeoTIFFUserDefined(code) && sizeKeyID != 0 {
		if size, ok := g.Double(sizeKeyID); ok {
			return wktUnit{name: "user-defined linear unit", conv: size}
		}
	}
	return linearUnitDefs[9001]
}

func (g *GeoTIFFMetadata) angularUnit(codeKeyID, sizeKeyID uint16) wktUnit {
	code := g.Short(codeKeyID)
	if def, ok := angularUnitDefs[code]; ok {
		return def
	}
	if isGeoTIFFUserDefined(code) && sizeKeyID != 0 {
		if size, ok := g.Double(sizeKeyID); ok {
			return wktUnit{name: "user-defined angular unit", conv: size}
		}
	}
	return angularUnitDefs[9102]
}

func (g *GeoTIFFMetadata) compoundName() string {
	return firstNonEmpty(g.String(1026), g.String(3073), g.String(2049), "Compound CRS")
}

type wktUnit struct {
	name string
	conv float64
	code uint16
}

func (u wktUnit) wkt(keyword string) string {
	s := fmt.Sprintf("%s[%s,%s", keyword, quoteWKT(u.name), formatWKTFloat(u.conv))
	if u.code != 0 {
		s += authorityWKT(u.code)
	}
	return s + "]"
}

type ellipsoidDef struct {
	name          string
	semiMajor     float64
	invFlattening float64
	code          uint16
}

func (e ellipsoidDef) wkt() string {
	return fmt.Sprintf("SPHEROID[%s,%s,%s%s]", quoteWKT(e.name), formatWKTFloat(e.semiMajor), formatWKTFloat(e.invFlattening), authorityWKT(e.code))
}

type datumDef struct {
	name      string
	ellipsoid uint16
}

type primeMeridianDef struct {
	name      string
	longitude float64
}

type projectionParam struct {
	keyID uint16
	name  string
}

var linearUnitDefs = map[uint16]wktUnit{
	9001: {name: "metre", conv: 1, code: 9001},
	9002: {name: "foot", conv: 0.3048, code: 9002},
	9003: {name: "US survey foot", conv: 1200.0 / 3937.0, code: 9003},
}

var angularUnitDefs = map[uint16]wktUnit{
	9101: {name: "radian", conv: 1, code: 9101},
	9102: {name: "degree", conv: math.Pi / 180, code: 9102},
	9103: {name: "arc-minute", conv: math.Pi / 10800, code: 9103},
	9104: {name: "arc-second", conv: math.Pi / 648000, code: 9104},
	9105: {name: "grad", conv: math.Pi / 200, code: 9105},
	9106: {name: "gon", conv: math.Pi / 200, code: 9106},
}

var ellipsoidDefs = map[uint16]ellipsoidDef{
	7001: {name: "Airy 1830", semiMajor: 6377563.396, invFlattening: 299.3249646, code: 7001},
	7004: {name: "Bessel 1841", semiMajor: 6377397.155, invFlattening: 299.1528128, code: 7004},
	7008: {name: "Clarke 1866", semiMajor: 6378206.4, invFlattening: 294.9786982, code: 7008},
	7019: {name: "GRS 1980", semiMajor: 6378137, invFlattening: 298.257222101, code: 7019},
	7022: {name: "International 1924", semiMajor: 6378388, invFlattening: 297, code: 7022},
	7030: {name: "WGS 84", semiMajor: 6378137, invFlattening: 298.257223563, code: 7030},
	7035: {name: "Sphere", semiMajor: 6371000, invFlattening: 0, code: 7035},
	7048: {name: "GRS 1980 Authalic Sphere", semiMajor: 6371007, invFlattening: 0, code: 7048},
}

var datumDefs = map[uint16]datumDef{
	6267: {name: "North American Datum 1927", ellipsoid: 7008},
	6269: {name: "North American Datum 1983", ellipsoid: 7019},
	6326: {name: "World Geodetic System 1984", ellipsoid: 7030},
}

var primeMeridianDefs = map[uint16]primeMeridianDef{
	8901: {name: "Greenwich", longitude: 0},
}

var projectionMethodWKTNames = map[uint16]string{
	1:  "Transverse_Mercator",
	2:  "Transverse_Mercator",
	3:  "Hotine_Oblique_Mercator",
	4:  "Laborde_Oblique_Mercator",
	5:  "Hotine_Oblique_Mercator",
	6:  "Oblique_Mercator",
	7:  "Mercator_1SP",
	8:  "Lambert_Conformal_Conic_2SP",
	9:  "Lambert_Conformal_Conic_1SP",
	10: "Lambert_Azimuthal_Equal_Area",
	11: "Albers_Conic_Equal_Area",
	12: "Azimuthal_Equidistant",
	13: "Equidistant_Conic",
	14: "Stereographic",
	15: "Polar_Stereographic",
	16: "Oblique_Stereographic",
	17: "Equirectangular",
	18: "Cassini_Soldner",
	19: "Gnomonic",
	20: "Miller_Cylindrical",
	21: "Orthographic",
	22: "Polyconic",
	23: "Robinson",
	24: "Sinusoidal",
	25: "VanDerGrinten",
	26: "New_Zealand_Map_Grid",
	27: "Transverse_Mercator_South_Orientated",
}

var projectionWKTParams = []projectionParam{
	{keyID: 3078, name: "standard_parallel_1"},
	{keyID: 3079, name: "standard_parallel_2"},
	{keyID: 3080, name: "central_meridian"},
	{keyID: 3081, name: "latitude_of_origin"},
	{keyID: 3082, name: "false_easting"},
	{keyID: 3083, name: "false_northing"},
	{keyID: 3084, name: "longitude_of_false_origin"},
	{keyID: 3085, name: "latitude_of_false_origin"},
	{keyID: 3086, name: "easting_at_false_origin"},
	{keyID: 3087, name: "northing_at_false_origin"},
	{keyID: 3088, name: "longitude_of_center"},
	{keyID: 3089, name: "latitude_of_center"},
	{keyID: 3090, name: "easting_at_center"},
	{keyID: 3091, name: "northing_at_center"},
	{keyID: 3092, name: "scale_factor"},
	{keyID: 3093, name: "scale_factor"},
	{keyID: 3094, name: "azimuth"},
	{keyID: 3095, name: "straight_vertical_longitude_from_pole"},
}

func geographicCRSWKT(code uint16) (string, bool) {
	switch code {
	case 4267:
		return geographicCRSWKTFromDatum("NAD27", code, 6267), true
	case 4269:
		return geographicCRSWKTFromDatum("NAD83", code, 6269), true
	case 4326:
		return geographicCRSWKTFromDatum("WGS 84", code, 6326), true
	}
	return "", false
}

func geographicCRSWKTFromDatum(name string, crsCode, datumCode uint16) string {
	def := datumDefs[datumCode]
	ellipsoid := ellipsoidDefs[def.ellipsoid]
	return fmt.Sprintf("GEOGCS[%s,DATUM[%s,%s%s],PRIMEM[%s,0%s],UNIT[%s,%s%s]%s]",
		quoteWKT(name),
		quoteWKT(def.name),
		ellipsoid.wkt(),
		authorityWKT(datumCode),
		quoteWKT("Greenwich"),
		authorityWKT(8901),
		quoteWKT("degree"),
		formatWKTFloat(math.Pi/180),
		authorityWKT(9102),
		authorityWKT(crsCode),
	)
}

func authorityWKT(code uint16) string {
	if code == 0 {
		return ""
	}
	return fmt.Sprintf(",AUTHORITY[%s,%s]", quoteWKT("EPSG"), quoteWKT(strconv.Itoa(int(code))))
}

func cleanCitation(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '|'); i >= 0 {
		s = s[:i]
	}
	return firstNonEmpty(strings.TrimSpace(s), "unnamed")
}

func quoteWKT(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

func formatWKTFloat(v float64) string {
	return strconv.FormatFloat(v, 'g', 15, 64)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func GeoTIFFKeyName(keyID int) string {
	return geotiffKeyNames[keyID]
}

var geotiffKeyNames = map[int]string{
	1024: "GTModelTypeGeoKey",
	1025: "GTRasterTypeGeoKey",
	1026: "GTCitationGeoKey",

	2048: "GeodeticCRSGeoKey",
	2049: "GeodeticCitationGeoKey",
	2050: "GeodeticDatumGeoKey",
	2051: "PrimeMeridianGeoKey",
	2052: "GeogLinearUnitsGeoKey",
	2053: "GeogLinearUnitSizeGeoKey",
	2054: "GeogAngularUnitsGeoKey",
	2055: "GeogAngularUnitSizeGeoKey",
	2056: "EllipsoidGeoKey",
	2057: "EllipsoidSemiMajorAxisGeoKey",
	2058: "EllipsoidSemiMinorAxisGeoKey",
	2059: "EllipsoidInvFlatteningGeoKey",
	2060: "GeogAzimuthUnitsGeoKey",
	2061: "PrimeMeridianLongitudeGeoKey",
	2062: "GeogTOWGS84GeoKey",

	3072: "ProjectedCRSGeoKey",
	3073: "ProjectedCitationGeoKey",
	3074: "ProjectionGeoKey",
	3075: "ProjMethodGeoKey",
	3076: "ProjLinearUnitsGeoKey",
	3077: "ProjLinearUnitSizeGeoKey",
	3078: "ProjStdParallel1GeoKey",
	3079: "ProjStdParallel2GeoKey",
	3080: "ProjNatOriginLongGeoKey",
	3081: "ProjNatOriginLatGeoKey",
	3082: "ProjFalseEastingGeoKey",
	3083: "ProjFalseNorthingGeoKey",
	3084: "ProjFalseOriginLongGeoKey",
	3085: "ProjFalseOriginLatGeoKey",
	3086: "ProjFalseOriginEastingGeoKey",
	3087: "ProjFalseOriginNorthingGeoKey",
	3088: "ProjCenterLongGeoKey",
	3089: "ProjCenterLatGeoKey",
	3090: "ProjCenterEastingGeoKey",
	3091: "ProjCenterNorthingGeoKey",
	3092: "ProjScaleAtNatOriginGeoKey",
	3093: "ProjScaleAtCenterGeoKey",
	3094: "ProjAzimuthAngleGeoKey",
	3095: "ProjStraightVertPoleLongGeoKey",

	4096: "VerticalGeoKey",
	4097: "VerticalCitationGeoKey",
	4098: "VerticalDatumGeoKey",
	4099: "VerticalUnitsGeoKey",
}
