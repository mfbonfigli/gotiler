package compression

import (
	"bytes"
	"io"
	"testing"

	"github.com/mfbonfigli/gotiler/v3/tiler/geom"
	"github.com/mfbonfigli/gotiler/v3/tiler/model"
)

// encodeWith writes node through the given encoder instance and returns the GLB bytes.
func encodeWith(t *testing.T, encoder *CompressedGltfEncoder, node mockNode) []byte {
	t.Helper()
	writes := map[string][]byte{}
	wp := func(name string) (io.WriteCloser, error) {
		return &captureWriteCloser{closeFn: func(data []byte) {
			writes[name] = append([]byte(nil), data...)
		}}, nil
	}
	if err := encoder.Write(node, wp, ""); err != nil {
		t.Fatalf("write compressed GLB: %v", err)
	}
	glb, ok := writes["d.glb"]
	if !ok {
		t.Fatalf("expected d.glb to be written, got files %#v", writes)
	}
	return glb
}

// TestCompressedEncoderPooledBufferReuseAcrossTiles guards the encoder's buffer
// reuse: an encoder whose pooled buffers are dirty from a previous tile must
// produce output byte-identical to a fresh encoder for the same node — no
// stale attribute values or padding may leak between tiles.
func TestCompressedEncoderPooledBufferReuseAcrossTiles(t *testing.T) {
	attrs := model.NewAttributes("Amplification")
	summaries := []model.AttributeSummary{{
		RequestedName: "Amplification",
		Name:          "amplification",
		Type:          model.AttributeUint8,
		Min:           uint8(0),
		Max:           uint8(27),
	}}
	withValues := func() mockNode {
		return newMockNodeWithSummaries(
			summaries,
			model.Point{X: 0, Y: 0, Z: 0, R: 255, G: 0, B: 0, Attributes: packSummaryValues(summaries, map[string]any{"amplification": uint8(12)})},
			model.Point{X: 1, Y: 2, Z: 3, R: 0, G: 255, B: 0, Attributes: packSummaryValues(summaries, map[string]any{"amplification": uint8(27)})},
			model.Point{X: 2, Y: 4, Z: 6, R: 0, G: 0, B: 255, Attributes: packSummaryValues(summaries, map[string]any{"amplification": uint8(5)})},
		)
	}
	// points without any attribute blob: the column must encode as zeros
	withoutValues := func() mockNode {
		return newMockNodeWithSummaries(
			summaries,
			model.Point{X: 0, Y: 0, Z: 0, R: 255, G: 0, B: 0},
			model.Point{X: 1, Y: 2, Z: 3, R: 0, G: 255, B: 0},
		)
	}

	for _, opts := range [][]Option{nil, {WithKHRMeshopt()}} {
		reused := New(attrs, opts...)
		// dirty the reused encoder's pooled buffers with non-zero attribute data
		first := encodeWith(t, reused, withValues())

		if got, want := encodeWith(t, reused, withoutValues()), encodeWith(t, New(attrs, opts...), withoutValues()); !bytes.Equal(got, want) {
			t.Errorf("%s: dirty encoder output differs from fresh encoder output for a no-values tile (len %d vs %d): stale buffer data leaked", reused.meshoptExtension, len(got), len(want))
		}
		if got := encodeWith(t, reused, withValues()); !bytes.Equal(got, first) {
			t.Errorf("%s: re-encoding the same tile with reused buffers produced different bytes (len %d vs %d)", reused.meshoptExtension, len(got), len(first))
		}
	}
}

type discardWriteCloser struct{}

func (discardWriteCloser) Write(p []byte) (int, error) { return len(p), nil }
func (discardWriteCloser) Close() error                { return nil }

func makeBenchNode(n int) mockNode {
	summaries := standardSummaries(model.NewAttributes(model.AttrIntensity, model.AttrClassification))
	blob := packSummaryValues(summaries, map[string]any{
		model.AttrIntensity:      uint16(7),
		model.AttrClassification: uint8(3),
	})
	head := &geom.LinkedPoint{Pt: model.Point{X: 0, Y: 0, Z: 0, R: 10, G: 20, B: 30, Attributes: blob}}
	curr := head
	for i := 1; i < n; i++ {
		next := &geom.LinkedPoint{Pt: model.Point{
			X: float32(i % 100), Y: float32(i % 200), Z: float32(i % 50),
			R: uint8(i), G: uint8(i >> 8), B: uint8(i >> 4), Attributes: blob,
		}}
		curr.Next = next
		curr = next
	}
	return mockNode{
		points:    geom.NewLinkedPointStream(head, n),
		summaries: summaries,
	}
}

func BenchmarkCompressedEncoderWrite(b *testing.B) {
	benchmarkCompressedEncoderWrite(b, New(model.NewAttributes(model.AttrIntensity, model.AttrClassification)))
}

func BenchmarkCompressedEncoderWriteKHR(b *testing.B) {
	benchmarkCompressedEncoderWrite(b, New(model.NewAttributes(model.AttrIntensity, model.AttrClassification), WithKHRMeshopt()))
}

func benchmarkCompressedEncoderWrite(b *testing.B, enc *CompressedGltfEncoder) {
	wp := func(name string) (io.WriteCloser, error) { return discardWriteCloser{}, nil }
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// node streams are single-use: rebuild outside the timed section
		b.StopTimer()
		node := makeBenchNode(50_000)
		b.StartTimer()
		if err := enc.Write(node, wp, ""); err != nil {
			b.Fatal(err)
		}
	}
}
