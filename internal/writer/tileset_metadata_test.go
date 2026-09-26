package writer

import (
	"bytes"
	"encoding/json"
	"io"
	"testing"

	"github.com/mfbonfigli/gotiler/v3/internal/testtree"
	"github.com/mfbonfigli/gotiler/v3/tiler/geom"
	"github.com/mfbonfigli/gotiler/v3/tiler/model"
	"github.com/mfbonfigli/gotiler/v3/tiler/plugin"
)

// writeTilesetJSON runs writeSquashedTileset for the given encoder against a
// mock tree carrying the given attribute summaries and returns the parsed
// tileset.json.
func writeTilesetJSON(t *testing.T, encoderID string, summaries []model.AttributeSummary) map[string]any {
	t.Helper()
	root := &testtree.MockNode{
		Pts:         geom.NewLinkedPointStream(&geom.LinkedPoint{Pt: geom.NewPoint(1, 2, 3, 1, 2, 3)}, 1),
		TotalNumPts: 1,
		Bounds:      geom.NewBoundingBox(0, 1, 0, 1, 0, 1),
		Root:        true,
		Summaries:   summaries,
	}
	buf := &bytes.Buffer{}
	w, err := NewWriter("", WithEncoder(encoderID), WithWriterProvider(func(name string) (io.WriteCloser, error) {
		return &captureWriteCloser{Buffer: buf, closeFn: func([]byte) {}}, nil
	}))
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	if err := w.writeSquashedTileset(root, w.writerProvider); err != nil {
		t.Fatalf("writeSquashedTileset: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal tileset.json: %v", err)
	}
	return out
}

func testSummaries() []model.AttributeSummary {
	return []model.AttributeSummary{
		{Name: "intensity", Type: model.AttributeUint16, Min: uint64(12), Max: uint64(833)},
		{Name: "gps_time", Type: model.AttributeFloat64, Min: float64(1.5), Max: float64(99.25)},
		{Name: "synthetic", Type: model.AttributeBool, Min: false, Max: true},
		// mutator-only input: must not be exported
		{Name: "amplitude", Type: model.AttributeFloat32, Min: float64(1), Max: float64(2), SkipIncomplete: true},
		// requested but never observed: must not be exported
		{Name: "missing", Type: model.AttributeUint8},
	}
}

func TestTilesetMetadata11EmitsMinMaxProperties(t *testing.T) {
	out := writeTilesetJSON(t, plugin.EncoderGLB, testSummaries())

	metadata, ok := out["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("expected tileset metadata entity, got %v", out["metadata"])
	}
	if metadata["class"] != "dataset" {
		t.Fatalf("expected metadata class dataset, got %v", metadata["class"])
	}
	props := metadata["properties"].(map[string]any)
	want := map[string]float64{
		"MIN_INTENSITY": 12, "MAX_INTENSITY": 833,
		"MIN_GPS_TIME": 1.5, "MAX_GPS_TIME": 99.25,
		"MIN_SYNTHETIC": 0, "MAX_SYNTHETIC": 1,
	}
	if len(props) != len(want) {
		t.Fatalf("expected %d metadata properties, got %v", len(want), props)
	}
	for name, value := range want {
		if got, ok := props[name].(float64); !ok || got != value {
			t.Fatalf("property %s: expected %v, got %v", name, value, props[name])
		}
	}

	schema, ok := out["schema"].(map[string]any)
	if !ok {
		t.Fatalf("expected tileset metadata schema, got %v", out["schema"])
	}
	if schema["id"] != "gotiler_dataset" {
		t.Fatalf("expected schema id gotiler_dataset, got %v", schema["id"])
	}
	classProps := schema["classes"].(map[string]any)["dataset"].(map[string]any)["properties"].(map[string]any)
	if len(classProps) != len(want) {
		t.Fatalf("expected %d class properties, got %v", len(want), classProps)
	}
	intensity := classProps["MIN_INTENSITY"].(map[string]any)
	if intensity["type"] != "SCALAR" || intensity["componentType"] != "UINT16" {
		t.Fatalf("expected UINT16 scalar for MIN_INTENSITY, got %v", intensity)
	}
	gpsTime := classProps["MAX_GPS_TIME"].(map[string]any)
	if gpsTime["componentType"] != "FLOAT64" {
		t.Fatalf("expected FLOAT64 for MAX_GPS_TIME, got %v", gpsTime)
	}
	// 1.0-style properties must not appear in a 1.1 tileset
	if _, ok := out["properties"]; ok {
		t.Fatal("expected no 1.0 properties dictionary in a 1.1 tileset")
	}
}

func TestTilesetMetadata10EmitsPropertiesDictionary(t *testing.T) {
	out := writeTilesetJSON(t, plugin.EncoderPNTS, testSummaries())

	props, ok := out["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected 1.0 properties dictionary, got %v", out["properties"])
	}
	want := map[string][2]float64{
		"INTENSITY": {12, 833},
		"GPS_TIME":  {1.5, 99.25},
		"SYNTHETIC": {0, 1},
	}
	if len(props) != len(want) {
		t.Fatalf("expected %d properties, got %v", len(want), props)
	}
	for name, minmax := range want {
		entry := props[name].(map[string]any)
		if entry["minimum"] != minmax[0] || entry["maximum"] != minmax[1] {
			t.Fatalf("property %s: expected %v, got %v", name, minmax, entry)
		}
	}
	// 1.1 metadata must not appear in a 1.0 tileset
	if _, ok := out["schema"]; ok {
		t.Fatal("expected no metadata schema in a 1.0 tileset")
	}
	if _, ok := out["metadata"]; ok {
		t.Fatal("expected no metadata entity in a 1.0 tileset")
	}
}

func TestTilesetMetadataOmittedWithoutAttributes(t *testing.T) {
	for _, encoderID := range []string{plugin.EncoderGLB, plugin.EncoderPNTS} {
		out := writeTilesetJSON(t, encoderID, nil)
		for _, key := range []string{"properties", "schema", "metadata"} {
			if _, ok := out[key]; ok {
				t.Fatalf("%s: expected no %s without attributes", encoderID, key)
			}
		}
	}
}

func TestTilesetMetadataKeepsIntegerPrecision(t *testing.T) {
	summaries := []model.AttributeSummary{
		{Name: "big", Type: model.AttributeUint64, Min: uint64(0), Max: uint64(1) << 62},
	}
	root := &testtree.MockNode{
		Pts:         geom.NewLinkedPointStream(&geom.LinkedPoint{Pt: geom.NewPoint(1, 2, 3, 1, 2, 3)}, 1),
		TotalNumPts: 1,
		Root:        true,
		Summaries:   summaries,
	}
	buf := &bytes.Buffer{}
	w, err := NewWriter("", WithWriterProvider(func(name string) (io.WriteCloser, error) {
		return &captureWriteCloser{Buffer: buf, closeFn: func([]byte) {}}, nil
	}))
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	if err := w.writeSquashedTileset(root, w.writerProvider); err != nil {
		t.Fatalf("writeSquashedTileset: %v", err)
	}
	// uint64 must be marshaled as an exact integer, not a float64
	if !bytes.Contains(buf.Bytes(), []byte(`"MAX_BIG":4611686018427387904`)) {
		t.Fatalf("expected exact integer max in output, got %s", buf.String())
	}
}
