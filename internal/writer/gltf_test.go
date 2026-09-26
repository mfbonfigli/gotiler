package writer

import (
	"io"
	"os"
	"testing"

	"github.com/mfbonfigli/gotiler/v3/internal/testtree"
	"github.com/mfbonfigli/gotiler/v3/tiler/geom"
	"github.com/mfbonfigli/gotiler/v3/tiler/model"
)

// makeGenericAttributeNodeWithoutValues mirrors makeGenericAttributeNode but its
// points carry no attribute blob at all: the encoder must emit zeros for them.
func makeGenericAttributeNodeWithoutValues() *testtree.MockNode {
	node := makeGenericAttributeNode()
	pt1 := &geom.LinkedPoint{Pt: model.Point{X: 0, Y: 0, Z: 0, R: 160, G: 166, B: 203}}
	pt2 := &geom.LinkedPoint{Pt: model.Point{X: 1, Y: 1, Z: 1, R: 186, G: 200, B: 237}}
	pt1.Next = pt2
	node.Pts = geom.NewLinkedPointStream(pt1, 2)
	node.TotalNumPts = 2
	return node
}

// TestGltfEncoderPooledBufferReuseAcrossTiles guards the encoder's buffer reuse:
// data left in pooled buffers by a previous tile must never leak into the next
// one, neither in attribute values nor in the stride padding bytes.
func TestGltfEncoderPooledBufferReuseAcrossTiles(t *testing.T) {
	attrs := model.NewAttributes("Amplification")
	enc := NewGltfEncoder("d.glb", attrs)

	// first write dirties any reused buffers with non-zero attribute values
	first := encodeToFile(t, enc, makeGenericAttributeNode(), "glb")
	firstBytes, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}

	// a tile whose points carry no attribute blob must read back all zeros
	noValues := encodeToFile(t, enc, makeGenericAttributeNodeWithoutValues(), "glb")
	vals := readGlbUint8Accessor(t, noValues, "_AMPLIFICATION")
	if len(vals) != 2 {
		t.Fatalf("_AMPLIFICATION count: got %d want 2", len(vals))
	}
	if !allEqual(vals, 0) {
		t.Errorf("_AMPLIFICATION values leaked from the previous tile: got %v want all 0", vals)
	}

	// re-encoding the first tile with the same encoder must be byte-identical
	third := encodeToFile(t, enc, makeGenericAttributeNode(), "glb")
	thirdBytes, err := os.ReadFile(third)
	if err != nil {
		t.Fatal(err)
	}
	if string(firstBytes) != string(thirdBytes) {
		t.Errorf("re-encoding the same tile with reused buffers produced different bytes (len %d vs %d)", len(firstBytes), len(thirdBytes))
	}
}

type discardWriteCloser struct{}

func (discardWriteCloser) Write(p []byte) (int, error) { return len(p), nil }
func (discardWriteCloser) Close() error                { return nil }

// makeBenchNode builds a node with n points carrying intensity and classification values.
func makeBenchNode(n int) *testtree.MockNode {
	tr := geom.LocalToGlobalTransformFromPoint(2000, 1000, 1000)
	summaries := makeStandardSummaries(model.NewAttributes(model.AttrIntensity, model.AttrClassification))
	values := map[string]any{
		model.AttrIntensity:      uint16(7),
		model.AttrClassification: uint8(3),
	}
	blob := packSummaryValues(summaries, values)
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
	return &testtree.MockNode{
		TotalNumPts: n,
		Pts:         geom.NewLinkedPointStream(head, n),
		Bounds:      geom.NewBoundingBox(0, 100, 0, 200, 0, 50),
		Root:        true,
		Leaf:        true,
		GeomError:   20,
		Transform:   &tr,
		Summaries:   summaries,
	}
}

func BenchmarkGltfEncoderWrite(b *testing.B) {
	enc := NewGltfEncoder("d.glb", model.NewAttributes(model.AttrIntensity, model.AttrClassification))
	wp := func(filename string) (io.WriteCloser, error) { return discardWriteCloser{}, nil }
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
