package writer

import (
	"fmt"
	"io"
	"sync"

	"github.com/mfbonfigli/gotiler/v3/tiler/geom"
	"github.com/mfbonfigli/gotiler/v3/tiler/model"
	"github.com/mfbonfigli/gotiler/v3/tiler/mutator"
	"github.com/mfbonfigli/gotiler/v3/tiler/tree"
)

// writeMutationChunkSize is the number of points batched per MutateChunkOnWrite call.
const writeMutationChunkSize = 4096

// writeMutationBatchPool recycles point batches across tiles. Pooled batches
// are zeroed before being put back so they do not pin the attribute blobs of
// the points they held.
var writeMutationBatchPool = sync.Pool{
	New: func() any {
		buf := make([]model.Point, 0, writeMutationChunkSize)
		return &buf
	},
}

// WrapTreeWithWriteMutation returns a view of t whose nodes stream their
// points through m as tiles are written. The underlying tree is not modified.
func WrapTreeWithWriteMutation(t tree.Tree, m mutator.WriteMutator) tree.Tree {
	if t == nil || m == nil {
		return t
	}
	return &writeMutatedTree{Tree: t, mut: m}
}

type writeMutatedTree struct {
	tree.Tree
	mut mutator.WriteMutator
}

func (t *writeMutatedTree) RootNode() tree.Node {
	return wrapWriteMutatedNode(t.Tree.RootNode(), t.mut)
}

// AttributeSummaries forwards the optional tree.AttributeSummaryProvider
// capability of the wrapped tree.
func (t *writeMutatedTree) AttributeSummaries() []model.AttributeSummary {
	if p, ok := t.Tree.(tree.AttributeSummaryProvider); ok {
		return p.AttributeSummaries()
	}
	return nil
}

func wrapWriteMutatedNode(n tree.Node, m mutator.WriteMutator) tree.Node {
	if n == nil {
		return nil
	}
	return &writeMutatedNode{Node: n, mut: m}
}

type writeMutatedNode struct {
	tree.Node
	mut mutator.WriteMutator
}

func (n *writeMutatedNode) ChildrenAt(i uint8) tree.Node {
	return wrapWriteMutatedNode(n.Node.ChildrenAt(i), n.mut)
}

func (n *writeMutatedNode) Points() (geom.PointList, error) {
	inner, err := n.Node.Points()
	if err != nil {
		return inner, err
	}
	var layout []model.AttributeLayoutEntry
	if p, ok := n.Node.(tree.AttributeSummaryProvider); ok {
		layout, _ = model.AttributeLayout(p.AttributeSummaries())
	}
	transform := model.IdentityTransform
	if t := n.Node.ToParentCRS(); t != nil {
		transform = *t
	}
	return newWriteMutatedPointList(inner, n.mut, layout, transform), nil
}

// AttributeSummaries forwards the optional tree.AttributeSummaryProvider
// capability of the wrapped node: encoders discover attribute columns through
// it, so the wrapper must not hide it.
func (n *writeMutatedNode) AttributeSummaries() []model.AttributeSummary {
	if p, ok := n.Node.(tree.AttributeSummaryProvider); ok {
		return p.AttributeSummaries()
	}
	return nil
}

// writeMutatedPointList streams an inner PointList through a WriteMutator in
// chunks. Write mutators must not add or drop points, so Len is the inner
// length and a size mismatch surfaces as an error from Next.
type writeMutatedPointList struct {
	inner     geom.PointList
	mut       mutator.WriteMutator
	layout    []model.AttributeLayoutEntry
	transform model.Transform
	chunkSize int
	// buf is the pooled backing the inner points are read into; batch is the
	// slice served by Next. They usually alias, but the mutator may return a
	// different slice, so buf is tracked separately to be pooled safely.
	buf       *[]model.Point
	batch     []model.Point
	idx       int
	remaining int
	err       error
}

func newWriteMutatedPointList(inner geom.PointList, m mutator.WriteMutator, layout []model.AttributeLayoutEntry, transform model.Transform) *writeMutatedPointList {
	return &writeMutatedPointList{
		inner:     inner,
		mut:       m,
		layout:    layout,
		transform: transform,
		chunkSize: writeMutationChunkSize,
		remaining: inner.Len(),
	}
}

func (l *writeMutatedPointList) Len() int {
	return l.inner.Len()
}

func (l *writeMutatedPointList) Next() (model.Point, error) {
	if l.err != nil {
		return model.Point{}, l.err
	}
	if l.idx >= len(l.batch) {
		if err := l.fill(); err != nil {
			l.err = err
			return model.Point{}, err
		}
	}
	pt := l.batch[l.idx]
	l.idx++
	return pt, nil
}

// fill pulls the next chunk from the inner list and mutates it. The points
// are safe to hold across inner reads: PointList.Next returns value copies
// and their attribute slices are not reused between batches.
func (l *writeMutatedPointList) fill() error {
	if l.remaining <= 0 {
		return io.EOF
	}
	if l.buf == nil {
		l.buf = writeMutationBatchPool.Get().(*[]model.Point)
	}
	n := min(l.remaining, l.chunkSize)
	points := (*l.buf)[:0]
	for i := 0; i < n; i++ {
		pt, err := l.inner.Next()
		if err != nil {
			return err
		}
		points = append(points, pt)
	}
	*l.buf = points
	l.remaining -= n
	mutated := l.mut.MutateChunkOnWrite(mutator.PointChunk{
		Points:          points,
		AttributeLayout: l.layout,
	}, l.transform)
	if len(mutated) != n {
		return fmt.Errorf("write mutators must not add or drop points: chunk had %d points, mutator returned %d", n, len(mutated))
	}
	l.batch = mutated
	l.idx = 0
	return nil
}

func (l *writeMutatedPointList) Reset() {
	l.inner.Reset()
	l.batch = nil
	l.idx = 0
	l.remaining = l.inner.Len()
	l.err = nil
}

func (l *writeMutatedPointList) Close() error {
	l.releaseBuf()
	return l.inner.Close()
}

// releaseBuf zeroes the pooled backing over its full capacity, so that no
// attribute slice reference survives into the pool, and puts it back.
func (l *writeMutatedPointList) releaseBuf() {
	if l.buf == nil {
		return
	}
	points := (*l.buf)[:cap(*l.buf)]
	clear(points)
	*l.buf = points[:0]
	writeMutationBatchPool.Put(l.buf)
	l.buf = nil
	l.batch = nil
}
