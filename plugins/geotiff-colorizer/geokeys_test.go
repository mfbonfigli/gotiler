package geotiffcolorizer

import (
	"encoding/binary"
	"strings"
	"testing"
)

func TestUserDefinedProjectedCRSDoesNotFallbackToBaseGeographicEPSG(t *testing.T) {
	geo := &GeoTIFFMetadata{Keys: map[uint16]*GeoTIFFKey{}}
	putShort := func(id, v uint16) {
		geo.Keys[id] = &GeoTIFFKey{KeyID: id, Type: GTTagTypeShort, Count: 1, rawValue: v}
	}
	putDouble := func(id uint16, v float64) {
		geo.Keys[id] = &GeoTIFFKey{KeyID: id, Type: GTTagTypeDouble, Count: 1, rawValue: v}
	}
	putString := func(id uint16, v string) {
		geo.Keys[id] = &GeoTIFFKey{KeyID: id, Type: GTTagTypeString, Count: uint16(len(v)), rawValue: v}
	}

	putShort(1024, 32767)
	putString(1026, "WGS 84 / Pseudo-Mercator")
	putShort(2048, 4326)
	putShort(3072, 32767)
	putShort(3074, 32767)
	putShort(3075, 7)
	putShort(3076, 9001)
	putDouble(3080, 0)
	putDouble(3081, 0)
	putDouble(3082, 0)
	putDouble(3083, 0)
	putDouble(3092, 1)

	if code, ok := geo.HorizontalEPSG(); ok {
		t.Fatalf("HorizontalEPSG = %d, true; want no EPSG fallback for user-defined projected CRS", code)
	}
	crs := geo.CRS()
	if !strings.HasPrefix(crs, `PROJCS["WGS 84 / Pseudo-Mercator"`) {
		t.Fatalf("CRS = %q, want projected WKT named from GTCitation", crs)
	}
	if !strings.Contains(crs, `AUTHORITY["EPSG","4326"]`) {
		t.Fatalf("CRS = %q, want base geographic EPSG authority retained", crs)
	}
}

func TestParseGeoTIFFKeysResolvesExternalStorage(t *testing.T) {
	ascii := []byte("Custom name|")
	doubles := encDoubles(binary.LittleEndian, 6378137)
	directory := encShorts(binary.LittleEndian,
		1, 1, 2, 4,
		1024, 0, 1, 2,
		2049, tiffTagGeoAsciiParams, uint16(len(ascii)), 0,
		2057, tiffTagGeoDoubleParams, 1, 0,
		2062, tiffTagGeoKeyDirectory, 2, 20,
		7, 8,
	)

	geo, err := ParseGeoTIFFKeys(binary.LittleEndian, directory, doubles, ascii)
	if err != nil {
		t.Fatalf("ParseGeoTIFFKeys: %v", err)
	}
	if got := geo.Key(1024).Name(); got != "GTModelTypeGeoKey" {
		t.Fatalf("Name = %q", got)
	}
	if got := geo.Key(1024).AsShorts(); len(got) != 1 || got[0] != 2 {
		t.Fatalf("AsShorts inline = %#v", got)
	}
	if got := geo.Key(2049).AsStrings(); len(got) != 1 || got[0] != "Custom name" {
		t.Fatalf("AsStrings = %#v", got)
	}
	if got := geo.Key(2057).AsDoubles(); len(got) != 1 || got[0] != 6378137 {
		t.Fatalf("AsDoubles = %#v", got)
	}
	if got := geo.Key(2062).AsShorts(); len(got) != 2 || got[0] != 7 || got[1] != 8 {
		t.Fatalf("AsShorts directory = %#v", got)
	}
	if GeoTIFFKeyName(1024) != "GTModelTypeGeoKey" {
		t.Fatalf("GeoTIFFKeyName did not resolve key 1024")
	}
}

func TestParseGeoTIFFKeysRejectsBadStorage(t *testing.T) {
	_, err := ParseGeoTIFFKeys(binary.LittleEndian, encShorts(binary.LittleEndian,
		1, 1, 1, 1,
		2049, tiffTagGeoAsciiParams, 10, 0,
	), nil, []byte("short"))
	if err == nil {
		t.Fatalf("ParseGeoTIFFKeys succeeded with ASCII value outside GeoAsciiParams")
	}

	_, err = ParseGeoTIFFKeys(binary.LittleEndian, encShorts(binary.LittleEndian,
		1, 1, 1, 1,
		1024, 999, 1, 0,
	), nil, nil)
	if err == nil {
		t.Fatalf("ParseGeoTIFFKeys succeeded with unsupported key location")
	}
}

func TestUserDefinedGeodeticWKT(t *testing.T) {
	geo := &GeoTIFFMetadata{Keys: map[uint16]*GeoTIFFKey{}}
	putShort := func(id, v uint16) {
		geo.Keys[id] = &GeoTIFFKey{KeyID: id, Type: GTTagTypeShort, Count: 1, rawValue: v}
	}
	putDouble := func(id uint16, v float64) {
		geo.Keys[id] = &GeoTIFFKey{KeyID: id, Type: GTTagTypeDouble, Count: 1, rawValue: v}
	}
	putString := func(id uint16, v string) {
		geo.Keys[id] = &GeoTIFFKey{KeyID: id, Type: GTTagTypeString, Count: uint16(len(v)), rawValue: v}
	}

	putShort(1024, 2)
	putShort(2048, 32767)
	putString(2049, "Custom Geographic")
	putShort(2050, 32767)
	putShort(2051, 32767)
	putShort(2054, 32767)
	putDouble(2055, 0.0174532925199433)
	putShort(2056, 32767)
	putDouble(2057, 7000000)
	putDouble(2058, 6990000)
	putDouble(2061, 12.5)

	wkt := geo.WKT()
	for _, want := range []string{
		`GEOGCS["Custom Geographic"`,
		`SPHEROID["Custom Geographic",7000000,700]`,
		`PRIMEM["Custom Geographic",12.5]`,
		`UNIT["user-defined angular unit",0.0174532925199433]`,
	} {
		if !strings.Contains(wkt, want) {
			t.Fatalf("WKT = %q, want fragment %q", wkt, want)
		}
	}
}

func TestCompoundVerticalWKT(t *testing.T) {
	geo := &GeoTIFFMetadata{Keys: map[uint16]*GeoTIFFKey{}}
	putShort := func(id, v uint16) {
		geo.Keys[id] = &GeoTIFFKey{KeyID: id, Type: GTTagTypeShort, Count: 1, rawValue: v}
	}
	putString := func(id uint16, v string) {
		geo.Keys[id] = &GeoTIFFKey{KeyID: id, Type: GTTagTypeString, Count: uint16(len(v)), rawValue: v}
	}

	putShort(1024, 2)
	putShort(2048, 32767)
	putString(2049, "Custom Geographic")
	putShort(2050, 6326)
	putShort(2051, 8901)
	putShort(2054, 9102)
	putShort(4096, 32767)
	putString(4097, "Custom height")
	putShort(4098, 5100)
	putShort(4099, 9001)

	wkt := geo.WKT()
	if !strings.HasPrefix(wkt, `COMPD_CS["Custom Geographic"`) {
		t.Fatalf("WKT = %q, want compound CRS", wkt)
	}
	if !strings.Contains(wkt, `VERT_CS["Custom height"`) {
		t.Fatalf("WKT = %q, want vertical CRS", wkt)
	}
	if code, ok := geo.VerticalEPSG(); ok || code != 0 {
		t.Fatalf("VerticalEPSG = %d,%v; want no standard EPSG for user-defined vertical CRS", code, ok)
	}
}
