package writer

import (
	"io"
	"strings"
	"testing"

	"github.com/mfbonfigli/gotiler/v3/internal/testtree"
	"github.com/mfbonfigli/gotiler/v3/tiler/geom"
	"github.com/mfbonfigli/gotiler/v3/tiler/model"
	"github.com/mfbonfigli/gotiler/v3/tiler/mutator"
	"github.com/mfbonfigli/gotiler/v3/tiler/tree"
)

// paintWriteMutator marks every point it sees at write time and records the
// chunk sizes and layouts it received.
type paintWriteMutator struct {
	chunkSizes []int
	layouts    [][]model.AttributeLayoutEntry
	transforms []model.Transform
}

func (m *paintWriteMutator) RequiredAttributes() model.Attributes { return nil }

func (m *paintWriteMutator) MutateChunk(chunk mutator.PointChunk, t model.Transform) []model.Point {
	return chunk.Points
}

func (m *paintWriteMutator) MutateChunkOnWrite(chunk mutator.PointChunk, t model.Transform) []model.Point {
	m.chunkSizes = append(m.chunkSizes, len(chunk.Points))
	m.layouts = append(m.layouts, chunk.AttributeLayout)
	m.transforms = append(m.transforms, t)
	for i := range chunk.Points {
		chunk.Points[i].R = 200
	}
	return chunk.Points
}

func (m *paintWriteMutator) Close() error { return nil }

// droppingWriteMutator violates the write-time contract by dropping a point.
type droppingWriteMutator struct{}

func (m *droppingWriteMutator) RequiredAttributes() model.Attributes { return nil }

func (m *droppingWriteMutator) MutateChunk(chunk mutator.PointChunk, t model.Transform) []model.Point {
	return chunk.Points
}

func (m *droppingWriteMutator) MutateChunkOnWrite(chunk mutator.PointChunk, t model.Transform) []model.Point {
	return chunk.Points[:len(chunk.Points)-1]
}

func (m *droppingWriteMutator) Close() error { return nil }

func linkedStream(pts ...model.Point) *geom.LinkedPointStream {
	var root *geom.LinkedPoint
	for i := len(pts) - 1; i >= 0; i-- {
		root = &geom.LinkedPoint{Pt: pts[i], Next: root}
	}
	return geom.NewLinkedPointStream(root, len(pts))
}

func TestWriteMutatedPointListBatchesAndPreservesOrder(t *testing.T) {
	pts := []model.Point{
		geom.NewPoint(1, 0, 0, 1, 2, 3),
		geom.NewPoint(2, 0, 0, 1, 2, 3),
		geom.NewPoint(3, 0, 0, 1, 2, 3),
		geom.NewPoint(4, 0, 0, 1, 2, 3),
		geom.NewPoint(5, 0, 0, 1, 2, 3),
	}
	mut := &paintWriteMutator{}
	l := newWriteMutatedPointList(linkedStream(pts...), mut, nil, model.IdentityTransform)
	l.chunkSize = 2

	if l.Len() != 5 {
		t.Fatalf("Len = %d, want 5", l.Len())
	}
	for i := 0; i < 5; i++ {
		pt, err := l.Next()
		if err != nil {
			t.Fatalf("point %d: %v", i, err)
		}
		if pt.X != float32(i+1) {
			t.Fatalf("point %d: X = %v, want %v", i, pt.X, i+1)
		}
		if pt.R != 200 {
			t.Fatalf("point %d: expected mutated color, got R=%d", i, pt.R)
		}
	}
	if _, err := l.Next(); err != io.EOF {
		t.Fatalf("expected io.EOF after all points, got %v", err)
	}
	wantSizes := []int{2, 2, 1}
	if len(mut.chunkSizes) != len(wantSizes) {
		t.Fatalf("chunk sizes = %v, want %v", mut.chunkSizes, wantSizes)
	}
	for i, s := range wantSizes {
		if mut.chunkSizes[i] != s {
			t.Fatalf("chunk sizes = %v, want %v", mut.chunkSizes, wantSizes)
		}
	}
	for _, tr := range mut.transforms {
		if tr != model.IdentityTransform {
			t.Fatalf("expected identity transform, got %+v", tr)
		}
	}
}

func TestWriteMutatedPointListReset(t *testing.T) {
	pts := []model.Point{
		geom.NewPoint(1, 0, 0, 1, 2, 3),
		geom.NewPoint(2, 0, 0, 1, 2, 3),
		geom.NewPoint(3, 0, 0, 1, 2, 3),
	}
	l := newWriteMutatedPointList(linkedStream(pts...), &paintWriteMutator{}, nil, model.IdentityTransform)
	l.chunkSize = 2

	for i := 0; i < 2; i++ {
		if _, err := l.Next(); err != nil {
			t.Fatalf("first pass point %d: %v", i, err)
		}
	}
	l.Reset()
	for i := 0; i < 3; i++ {
		pt, err := l.Next()
		if err != nil {
			t.Fatalf("second pass point %d: %v", i, err)
		}
		if pt.X != float32(i+1) {
			t.Fatalf("second pass point %d: X = %v, want %v", i, pt.X, i+1)
		}
	}
	if _, err := l.Next(); err != io.EOF {
		t.Fatalf("expected io.EOF after reset pass, got %v", err)
	}
}

func TestWriteMutatedPointListReleasesZeroedBufferOnClose(t *testing.T) {
	pt := geom.NewPoint(1, 0, 0, 1, 2, 3)
	pt.Attributes = model.AttributeValues{7, 7}
	l := newWriteMutatedPointList(linkedStream(pt), &paintWriteMutator{}, nil, model.IdentityTransform)
	if _, err := l.Next(); err != nil {
		t.Fatalf("Next: %v", err)
	}
	backing := (*l.buf)[:cap(*l.buf)]
	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if l.buf != nil {
		t.Fatal("expected pooled buffer to be released on Close")
	}
	for i := range backing {
		if backing[i].Attributes != nil || backing[i].X != 0 {
			t.Fatalf("expected pooled buffer to be zeroed, got %+v at %d", backing[i], i)
		}
	}
	// a second Close must be a no-op
	if err := l.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestWriteMutatedPointListRejectsDroppedPoints(t *testing.T) {
	pts := []model.Point{
		geom.NewPoint(1, 0, 0, 1, 2, 3),
		geom.NewPoint(2, 0, 0, 1, 2, 3),
	}
	l := newWriteMutatedPointList(linkedStream(pts...), &droppingWriteMutator{}, nil, model.IdentityTransform)
	_, err := l.Next()
	if err == nil || !strings.Contains(err.Error(), "must not add or drop points") {
		t.Fatalf("expected drop-contract error, got %v", err)
	}
	// the error must be sticky
	if _, err := l.Next(); err == nil {
		t.Fatal("expected subsequent Next calls to keep failing")
	}
}

func TestWrapTreeWithWriteMutation(t *testing.T) {
	summaries := []model.AttributeSummary{{
		Name: model.AttrIntensity,
		Type: model.AttributeUint16,
	}}
	child := &testtree.MockNode{
		Pts:       linkedStream(geom.NewPoint(9, 0, 0, 1, 2, 3)),
		Summaries: summaries,
	}
	root := &testtree.MockNode{
		Pts:       linkedStream(geom.NewPoint(1, 0, 0, 1, 2, 3), geom.NewPoint(2, 0, 0, 1, 2, 3)),
		Summaries: summaries,
		Root:      true,
		ChildNodes: [8]tree.Node{
			child,
		},
	}
	mut := &paintWriteMutator{}
	wrapped := WrapTreeWithWriteMutation(root, mut)
	if wrapped == root {
		t.Fatal("expected tree to be wrapped")
	}

	wrappedRoot := wrapped.RootNode()
	pts, err := wrappedRoot.Points()
	if err != nil {
		t.Fatalf("Points: %v", err)
	}
	for i := 0; i < 2; i++ {
		pt, err := pts.Next()
		if err != nil {
			t.Fatalf("root point %d: %v", i, err)
		}
		if pt.R != 200 {
			t.Fatalf("root point %d: expected mutated color, got R=%d", i, pt.R)
		}
	}

	// children must be wrapped recursively
	wrappedChild := wrappedRoot.ChildrenAt(0)
	if wrappedChild == nil {
		t.Fatal("expected wrapped child")
	}
	childPts, err := wrappedChild.Points()
	if err != nil {
		t.Fatalf("child Points: %v", err)
	}
	pt, err := childPts.Next()
	if err != nil {
		t.Fatalf("child point: %v", err)
	}
	if pt.R != 200 {
		t.Fatalf("child point: expected mutated color, got R=%d", pt.R)
	}
	if wrappedRoot.ChildrenAt(1) != nil {
		t.Fatal("expected nil child to stay nil")
	}

	// the attribute layout derived from the node summaries must reach the mutator
	if len(mut.layouts) == 0 || len(mut.layouts[0]) != 1 || mut.layouts[0][0].Name != model.AttrIntensity {
		t.Fatalf("expected intensity attribute layout, got %+v", mut.layouts)
	}

	// encoders discover attribute columns via AttributeSummaryProvider: the
	// wrappers must keep forwarding it
	if p, ok := wrappedRoot.(tree.AttributeSummaryProvider); !ok || len(p.AttributeSummaries()) != 1 {
		t.Fatal("expected wrapped node to forward AttributeSummaries")
	}
	if p, ok := wrapped.(tree.AttributeSummaryProvider); !ok || len(p.AttributeSummaries()) != 1 {
		t.Fatal("expected wrapped tree to forward AttributeSummaries")
	}
}

func TestWrapTreeWithWriteMutationNilInputs(t *testing.T) {
	if WrapTreeWithWriteMutation(nil, &paintWriteMutator{}) != nil {
		t.Fatal("expected nil tree to stay nil")
	}
	root := &testtree.MockNode{}
	if WrapTreeWithWriteMutation(root, nil) != tree.Tree(root) {
		t.Fatal("expected nil mutator to return the tree unchanged")
	}
}
