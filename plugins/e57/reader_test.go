package e57

import (
	"strings"
	"testing"

	libe57 "github.com/mfbonfigli/golibe57/e57"
	"github.com/mfbonfigli/gotiler/v3/tiler/model"
	"github.com/mfbonfigli/gotiler/v3/tiler/plugin"
)

func TestRegistersE57Reader(t *testing.T) {
	if _, ok := plugin.PointCloudReaderFactoryFor(".e57"); !ok {
		t.Fatal("expected .e57 reader to be registered")
	}
	if !plugin.IsPointCloudExtension("SCAN.E57") {
		t.Fatal("expected E57 extension to be supported case-insensitively")
	}
}

func TestReaderRequiresCRS(t *testing.T) {
	_, err := NewReader("scan.e57", "", nil)
	if err == nil {
		t.Fatal("expected missing CRS error")
	}
	if !strings.Contains(err.Error(), "CRS must be provided") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNormalizeToUint8(t *testing.T) {
	cases := []struct {
		name     string
		value    float64
		min      float64
		rng      float64
		expected uint8
	}{
		{name: "degenerate", value: 5, min: 5, rng: 0, expected: 0},
		{name: "below", value: -1, min: 0, rng: 10, expected: 0},
		{name: "above", value: 11, min: 0, rng: 10, expected: 255},
		{name: "midpoint", value: 5, min: 0, rng: 10, expected: 128},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeToUint8(tc.value, tc.min, tc.rng); got != tc.expected {
				t.Fatalf("normalizeToUint8() = %d, want %d", got, tc.expected)
			}
		})
	}
}

func TestEncodeAttributesEmitsOnlyRequestedCanonicalAttributes(t *testing.T) {
	reader := &E57Reader{
		requested: buildRequestedMap(model.NewAttributes(" IntensitY", model.AttrReturnNumber, "raw_color_red", "timestamp")),
		// Identity intensity normalization range so the raw value maps directly.
		intensityMin:    0,
		intensityRange:  1,
		hasColorInvalid: true,
		hasTimeInvalid:  true,
	}
	// Hand-build the schema and encode plan the way buildSchema/advanceScan
	// would for a scan carrying intensity, returns, color and timestamps.
	markAllAbsent(&reader.plan)
	reader.schema = []model.AttributeDescriptor{
		{Name: model.AttrIntensity, Type: model.AttributeUint16},
		{Name: model.AttrReturnNumber, Type: model.AttributeUint8},
		{Name: "raw_color_red", Type: model.AttributeUint16},
		{Name: "timestamp", Type: model.AttributeFloat64},
	}
	reader.plan.intensity = 0
	reader.plan.returnNumber = 2
	reader.plan.rawColorRed = 3
	reader.plan.timestamp = 5
	reader.plan.size = 13
	reader.act = reader.plan

	pt := &libe57.Point{
		Intensity:        0.5,
		ReturnIndex:      1,
		R:                42,
		ColorInvalid:     0,
		Timestamp:        123.5,
		TimestampInvalid: 0,
	}

	blob := reader.encodeAttributesLocked(pt)
	if len(blob) != reader.plan.size {
		t.Fatalf("expected %d packed bytes, got %d", reader.plan.size, len(blob))
	}
	entries, size, err := model.AttributeSchemaLayout(reader.schema)
	if err != nil {
		t.Fatalf("schema layout: %v", err)
	}
	if size != reader.plan.size {
		t.Fatalf("schema layout size %d does not match plan size %d", size, reader.plan.size)
	}
	view := model.NewAttributeView(entries, blob)
	assertViewValue(t, view, model.AttrIntensity, uint16(32768))
	assertViewValue(t, view, model.AttrReturnNumber, uint8(2))
	assertViewValue(t, view, "raw_color_red", uint16(42))
	assertViewValue(t, view, "timestamp", 123.5)
}

func assertViewValue(t *testing.T, view model.AttributeView, name string, want any) {
	t.Helper()
	i := view.Index(model.CanonicalAttributeName(name))
	if i < 0 {
		t.Fatalf("attribute %q missing from view", name)
	}
	got, err := view.Value(i)
	if err != nil {
		t.Fatalf("attribute %q: decode failed: %v", name, err)
	}
	if got != want {
		t.Fatalf("attribute %q value = %#v, want %#v", name, got, want)
	}
}

func TestNormalizeToUint16(t *testing.T) {
	cases := []struct {
		name     string
		value    float64
		min      float64
		rng      float64
		expected uint16
	}{
		{name: "degenerate", value: 5, min: 5, rng: 0, expected: 0},
		{name: "below", value: -1, min: 0, rng: 10, expected: 0},
		{name: "above", value: 11, min: 0, rng: 10, expected: 65535},
		{name: "midpoint", value: 5, min: 0, rng: 10, expected: 32768},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeToUint16(tc.value, tc.min, tc.rng); got != tc.expected {
				t.Fatalf("normalizeToUint16() = %d, want %d", got, tc.expected)
			}
		})
	}
}

func TestIntegerAttrType(t *testing.T) {
	cases := []struct {
		min, max int64
		want     model.AttributeType
	}{
		{0, 255, model.AttributeUint8},
		{0, 256, model.AttributeUint16},
		{0, 65535, model.AttributeUint16},
		{0, 65536, model.AttributeUint32},
		{0, 4294967295, model.AttributeUint32},
		{0, 4294967296, model.AttributeUint64},
		{-1, 100, model.AttributeInt8},
		{-128, 127, model.AttributeInt8},
		{-129, 127, model.AttributeInt16},
		{-32768, 32767, model.AttributeInt16},
		{-1, 32768, model.AttributeInt32},
		{-2147483648, 2147483647, model.AttributeInt32},
		{-1, 2147483648, model.AttributeInt64},
	}
	for _, c := range cases {
		if got := integerAttrType(c.min, c.max); got != c.want {
			t.Errorf("integerAttrType(%d, %d) = %q, want %q", c.min, c.max, got, c.want)
		}
	}
}

func TestConvertExtraValue(t *testing.T) {
	cases := []struct {
		typ  model.AttributeType
		in   float64
		want any
		ok   bool
	}{
		{model.AttributeUint8, 200.4, uint8(200), true},
		{model.AttributeUint16, 60000, uint16(60000), true},
		{model.AttributeUint32, 4000000000, uint32(4000000000), true},
		{model.AttributeInt8, -5.6, int8(-6), true},
		{model.AttributeInt16, -30000, int16(-30000), true},
		{model.AttributeInt32, -2000000000, int32(-2000000000), true},
		{model.AttributeInt64, 1 << 50, int64(1 << 50), true},
		{model.AttributeFloat32, 1.5, float32(1.5), true},
		{model.AttributeFloat64, -2.25, float64(-2.25), true},
		{model.AttributeBool, 1, nil, false},
	}
	for _, c := range cases {
		got, ok := convertExtraValue(c.typ, c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("convertExtraValue(%q, %v) = (%#v, %v), want (%#v, %v)", c.typ, c.in, got, ok, c.want, c.ok)
		}
	}
}
