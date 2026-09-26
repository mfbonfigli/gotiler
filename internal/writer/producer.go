package writer

import (
	"context"
	"fmt"
	"runtime/debug"
	"strconv"
	"sync"

	"github.com/mfbonfigli/gotiler/v3/tiler/plugin"
	"github.com/mfbonfigli/gotiler/v3/tiler/tree"
)

type Producer interface {
	Produce(work chan *WorkUnit, errchan chan error, wg *sync.WaitGroup, node tree.Node, ctx context.Context)
}

type StandardProducer struct {
	writerProvider plugin.WriterProvider
	// onTile, when non-nil, is set as the OnDone callback on every WorkUnit produced.
	// The consumer calls it after successfully writing the tile, enabling progress tracking.
	onTile func()
}

func NewStandardProducer(wp plugin.WriterProvider) Producer {
	return &StandardProducer{
		writerProvider: PrefixWriterProvider(wp, dataFolder),
	}
}

// newStandardProducerWithCallback creates a producer that sets an OnDone callback on
// each WorkUnit. The callback is invoked by the consumer after writing each tile.
func newStandardProducerWithCallback(wp plugin.WriterProvider, onTile func()) Producer {
	return &StandardProducer{
		writerProvider: PrefixWriterProvider(wp, dataFolder),
		onTile:         onTile,
	}
}

// Parses a tree node and submits WorkUnits the the provided workchannel. Should be called only on the tree root node.
// Closes the channel when all work is submitted.
func (p *StandardProducer) Produce(work chan *WorkUnit, errchan chan error, wg *sync.WaitGroup, node tree.Node, ctx context.Context) {
	defer close(work)
	defer wg.Done()
	defer func() {
		if r := recover(); r != nil {
			debug.PrintStack()
			errchan <- fmt.Errorf("panic: %v", r)
		}
	}()
	p.produce(errchan, "", node, work, wg, ctx)
}

// Parses a tree node and submits WorkUnits the the provided workchannel.
func (p *StandardProducer) produce(errchan chan error, prefix string, node tree.Node, work chan *WorkUnit, wg *sync.WaitGroup, ctx context.Context) {
	// if node contains points (it should always be the case), then submit work
	if err := ctx.Err(); err != nil {
		errchan <- fmt.Errorf("context closed: %v", err)
		return
	}
	if node.NumberOfPoints() > 0 {
		work <- &WorkUnit{
			Node:           node,
			WriterProvider: p.writerProvider,
			Prefix:         prefix,
			OnDone:         p.onTile,
		}
	} else {
		errchan <- fmt.Errorf("unexpected error: found tile without points: %v", node)
		return
	}

	// iterate all non nil children and recursively submit all work units
	for i := range 8 {
		child := node.ChildrenAt(uint8(i))
		if child != nil {
			p.produce(errchan, prefix+strconv.Itoa(i), child, work, wg, ctx)
		}
	}
}
