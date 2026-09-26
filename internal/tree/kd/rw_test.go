package kd

import (
	"bytes"
	"io"
	"testing"

	"github.com/mfbonfigli/gotiler/v3/tiler/model"
)

// TestFilePointWriterReader_RoundtripValues verifies that every field in
// model.Point survives a WriteBatch → NextBatch round-trip through the binary
// on-disk format.
func TestFilePointWriterReader_RoundtripValues(t *testing.T) {
	path := t.TempDir() + "/pts.bin"

	want := []model.Point{
		{X: 1.5, Y: 2.5, Z: 3.5, R: 10, G: 20, B: 30},
		{X: 4.0, Y: 5.0, Z: 6.0, R: 255, G: 128, B: 64},
		{X: -1.0, Y: -2.0, Z: -3.0, R: 0, G: 0, B: 0},
	}

	w, err := NewFilePointWriter(path)
	if err != nil {
		t.Fatalf("NewFilePointWriter: %v", err)
	}
	if err := w.WriteBatch(want); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close writer: %v", err)
	}

	r, err := NewFilePointReader(path)
	if err != nil {
		t.Fatalf("NewFilePointReader: %v", err)
	}
	defer r.Close()

	if r.NumPoints() != len(want) {
		t.Fatalf("NumPoints: got %d want %d", r.NumPoints(), len(want))
	}

	got, readErr := r.NextBatch(make([]model.Point, 0, len(want)))
	if readErr != nil && readErr != io.EOF {
		t.Fatalf("NextBatch: %v", readErr)
	}
	if len(got) != len(want) {
		t.Fatalf("len(got)=%d want %d", len(got), len(want))
	}

	for i, wpt := range want {
		g := got[i]
		if g.R != wpt.R || g.G != wpt.G || g.B != wpt.B {
			t.Errorf("pt%d RGB: got (%d,%d,%d) want (%d,%d,%d)", i, g.R, g.G, g.B, wpt.R, wpt.G, wpt.B)
		}
	}
}

func TestFilePointWriterReader_RoundtripGenericAttributes(t *testing.T) {
	path := t.TempDir() + "/pts_attrs.bin"
	summaries := []model.AttributeSummary{
		{RequestedName: " IntensitY", Name: model.CanonicalAttributeName(" IntensitY"), Type: model.AttributeUint16},
		{RequestedName: "amplification", Name: "amplification", Type: model.AttributeFloat32},
	}

	packValues := func(intensity uint16, amplification float32) model.AttributeValues {
		entries, size := model.AttributeLayout(summaries)
		out := make(model.AttributeValues, size)
		if err := model.EncodeAttributeValue(out[entries[0].Offset:entries[0].Offset+entries[0].Size], entries[0].Type, intensity); err != nil {
			t.Fatalf("encode intensity: %v", err)
		}
		if err := model.EncodeAttributeValue(out[entries[1].Offset:entries[1].Offset+entries[1].Size], entries[1].Type, amplification); err != nil {
			t.Fatalf("encode amplification: %v", err)
		}
		return out
	}

	want := []model.Point{
		{X: 1, Y: 2, Z: 3, Attributes: packValues(42, 1.5)},
		{X: 4, Y: 5, Z: 6, Attributes: packValues(0, 0)},
	}

	w, err := NewFilePointWriterWithAttributes(path, summaries)
	if err != nil {
		t.Fatalf("NewFilePointWriterWithAttributes: %v", err)
	}
	if err := w.WriteBatch(want); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close writer: %v", err)
	}

	r, err := NewFilePointReaderWithAttributes(path, summaries)
	if err != nil {
		t.Fatalf("NewFilePointReaderWithAttributes: %v", err)
	}
	defer r.Close()

	got, readErr := r.NextBatch(make([]model.Point, 0, len(want)))
	if readErr != nil && readErr != io.EOF {
		t.Fatalf("NextBatch: %v", readErr)
	}
	if len(got) != len(want) {
		t.Fatalf("len(got)=%d want %d", len(got), len(want))
	}
	for i := range got {
		if !bytes.Equal(got[i].Attributes, want[i].Attributes) {
			t.Errorf("point %d attribute values: got %v want %v", i, got[i].Attributes, want[i].Attributes)
		}
	}
}
