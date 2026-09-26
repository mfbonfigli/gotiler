package writer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/mfbonfigli/gotiler/v3/internal/testtree"
	"github.com/mfbonfigli/gotiler/v3/tiler/geom"
	"github.com/mfbonfigli/gotiler/v3/tiler/tree"
)

func TestProduce(t *testing.T) {
	pt1 := &geom.LinkedPoint{
		Pt: geom.NewPoint(1, 2, 3, 4, 5, 6),
	}
	pt2 := &geom.LinkedPoint{
		Pt: geom.NewPoint(9, 10, 11, 12, 13, 14),
	}
	pt3 := &geom.LinkedPoint{
		Pt: geom.NewPoint(17, 18, 19, 20, 21, 22),
	}
	pt1.Next = pt2
	pt2.Next = pt3

	stream := geom.NewLinkedPointStream(pt1, 3)
	stream2 := geom.NewLinkedPointStream(pt2, 2)

	tmp := t.TempDir()
	p := NewStandardProducer(NewDiskWriterProvider(tmp))
	child := &testtree.MockNode{
		TotalNumPts: 2,
		Pts:         stream2,
	}
	root := &testtree.MockNode{
		TotalNumPts: 5,
		Pts:         stream,
		ChildNodes: [8]tree.Node{
			nil,
			child,
		},
	}
	c := make(chan *WorkUnit, 10)
	ec := make(chan error, 1)
	wg := &sync.WaitGroup{}
	wg.Add(1)
	p.Produce(c, ec, wg, root, context.TODO())
	wg.Wait()
	rootSeen := false
	childSeen := false
	for wu := range c {
		if wu.Node != root && wu.Node != child {
			t.Errorf("unexpected unit")
		}
		if wu.WriterProvider == nil {
			t.Errorf("expected writer provider")
			continue
		}
		if wu.Node == root {
			rootSeen = true
			out, err := wu.WriterProvider(wu.Prefix + "test.bin")
			if err != nil {
				t.Errorf("unexpected provider error: %v", err)
			} else if err := out.Close(); err != nil {
				t.Errorf("unexpected close error: %v", err)
			}
			if _, err := os.Stat(filepath.Join(tmp, "data", "test.bin")); err != nil {
				t.Errorf("expected root provider to write under data/: %v", err)
			}
		}
		if wu.Node == child {
			childSeen = true
			out, err := wu.WriterProvider(wu.Prefix + "test.bin")
			if err != nil {
				t.Errorf("unexpected provider error: %v", err)
			} else if err := out.Close(); err != nil {
				t.Errorf("unexpected close error: %v", err)
			}
			if _, err := os.Stat(filepath.Join(tmp, "data", "1test.bin")); err != nil {
				t.Errorf("expected child provider to write prefixed file under data/: %v", err)
			}
		}
	}
	if !rootSeen || !childSeen {
		t.Errorf("not all nodes were seen")
	}
	if len(ec) != 0 {
		t.Errorf("unexpected errors in the channel")
	}
}

func TestProduceWithCancelOk(t *testing.T) {

	pt1 := &geom.LinkedPoint{
		Pt: geom.NewPoint(1, 2, 3, 4, 5, 6),
	}
	pt2 := &geom.LinkedPoint{
		Pt: geom.NewPoint(9, 10, 11, 12, 13, 14),
	}
	pt3 := &geom.LinkedPoint{
		Pt: geom.NewPoint(17, 18, 19, 20, 21, 22),
	}
	pt1.Next = pt2
	pt2.Next = pt3

	stream := geom.NewLinkedPointStream(pt1, 3)
	stream2 := geom.NewLinkedPointStream(pt2, 2)

	p := NewStandardProducer(NewDiskWriterProvider(t.TempDir()))
	child := &testtree.MockNode{
		TotalNumPts: 2,
		Pts:         stream2,
	}
	root := &testtree.MockNode{
		TotalNumPts: 5,
		Pts:         stream,
		ChildNodes: [8]tree.Node{
			nil,
			child,
		},
	}
	c := make(chan *WorkUnit)
	ec := make(chan error, 10)
	wg := &sync.WaitGroup{}
	wg.Add(1)
	mockErr := fmt.Errorf("mock error")
	ctx, cancel := context.WithDeadlineCause(context.Background(), time.Now().Add(500*time.Millisecond), mockErr)
	defer cancel()
	time.Sleep(600 * time.Millisecond)
	p.Produce(c, ec, wg, root, ctx)
	wg.Wait()
	if len(c) > 0 {
		t.Errorf("unexpected work units in the channel")
	}
	if len(ec) == 0 {
		t.Errorf("expected errors in the channel")
	}
}
