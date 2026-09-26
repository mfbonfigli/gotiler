package encoding

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mfbonfigli/gotiler/v3/tiler/model"
)

func TestAttributeOutputName(t *testing.T) {
	cases := map[string]string{
		"intensity":       "INTENSITY",
		"classification":  "CLASSIFICATION",
		"return_number":   "RETURN_NUMBER",
		"gps_time":        "GPS_TIME",
		"Amplification":   "AMPLIFICATION",
		"incidence_angle": "INCIDENCE_ANGLE",
	}
	for in, want := range cases {
		if got := AttributeOutputName(in); got != want {
			t.Errorf("AttributeOutputName(%q) = %q, want %q", in, got, want)
		}
	}
	if got := AttributePrimitiveName("intensity"); got != "_INTENSITY" {
		t.Errorf("AttributePrimitiveName(intensity) = %q, want _INTENSITY", got)
	}
}

func TestTypeSupportMatrix(t *testing.T) {
	cases := []struct {
		typ  model.AttributeType
		pnts bool
		gltf bool
	}{
		{model.AttributeInt8, true, true},
		{model.AttributeUint8, true, true},
		{model.AttributeBool, true, true},
		{model.AttributeInt16, true, true},
		{model.AttributeUint16, true, true},
		{model.AttributeInt32, true, false},
		{model.AttributeUint32, true, false},
		{model.AttributeInt64, false, false},
		{model.AttributeUint64, false, false},
		{model.AttributeFloat32, true, true},
		{model.AttributeFloat64, true, true}, // glTF via float32 downcast
	}
	for _, c := range cases {
		if got := PntsSupportsType(c.typ); got != c.pnts {
			t.Errorf("PntsSupportsType(%q) = %v, want %v", c.typ, got, c.pnts)
		}
		if got := GltfVertexSupportsType(c.typ); got != c.gltf {
			t.Errorf("GltfVertexSupportsType(%q) = %v, want %v", c.typ, got, c.gltf)
		}
	}
	if got := GltfEffectiveType(model.AttributeFloat64); got != model.AttributeFloat32 {
		t.Errorf("GltfEffectiveType(float64) = %q, want float32", got)
	}
	if got := GltfEffectiveType(model.AttributeUint16); got != model.AttributeUint16 {
		t.Errorf("GltfEffectiveType(uint16) = %q, want uint16", got)
	}
}

func TestBuildGltfMetadataJSON(t *testing.T) {
	got, err := BuildGltfMetadataJSON(nil)
	if err != nil || got != "" {
		t.Fatalf("empty columns: got (%q, %v), want empty string", got, err)
	}

	got, err = BuildGltfMetadataJSON([]AttributeColumn{
		{Summary: model.AttributeSummary{Name: model.AttrIntensity, Type: model.AttributeUint16}, Offset: 0, Size: 2},
		{Summary: model.AttributeSummary{Name: "gps_time", Type: model.AttributeFloat64}, Offset: 2, Size: 8},
	})
	if err != nil {
		t.Fatalf("build metadata JSON: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(got), &doc); err != nil {
		t.Fatalf("metadata JSON should be valid: %v", err)
	}
	// float64 attributes must be declared with the stored type (FLOAT32).
	if !strings.Contains(got, `"GPS_TIME":{"type":"SCALAR","componentType":"FLOAT32","required":true}`) {
		t.Errorf("GPS_TIME should be declared FLOAT32, got:\n%s", got)
	}
	if !strings.Contains(got, `"INTENSITY":{"type":"SCALAR","componentType":"UINT16","required":true}`) {
		t.Errorf("INTENSITY should be declared UINT16, got:\n%s", got)
	}
	if !strings.Contains(got, `"attribute":"_GPS_TIME"`) {
		t.Errorf("propertyAttributes should reference _GPS_TIME, got:\n%s", got)
	}
}

func TestMetadataPropertyID(t *testing.T) {
	cases := map[string]string{
		"INTENSITY":         "INTENSITY",
		"GPS_TIME":          "GPS_TIME",
		"TRUE-VIEW.ANGLE":   "TRUE_VIEW_ANGLE",
		"3D_FIELD":          "_3D_FIELD",
		"":                  "_",
		"ÅNGLE":             "_NGLE",
		"already_valid_123": "already_valid_123",
	}
	for in, want := range cases {
		if got := MetadataPropertyID(in); got != want {
			t.Errorf("MetadataPropertyID(%q) = %q, want %q", in, got, want)
		}
	}
}
