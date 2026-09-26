package writer

import (
	"fmt"
	"runtime/debug"
	"sync"

	"github.com/mfbonfigli/gotiler/v3/tiler/model"
	"github.com/mfbonfigli/gotiler/v3/tiler/plugin"
)

type Consumer interface {
	Consume(workchan chan *WorkUnit, errchan chan error, waitGroup *sync.WaitGroup)
}

type StandardConsumer struct {
	encoder plugin.GeometryEncoder
}

func NewStandardConsumer(optFn ...func(*StandardConsumer)) Consumer {
	c := &StandardConsumer{
		encoder: NewPntsEncoder("d.pnts", model.DefaultAttributes()),
	}
	for _, fn := range optFn {
		fn(c)
	}
	return c
}

// WithGeometryEncoder sets the consumer geometry encoder to the given one
func WithGeometryEncoder(e plugin.GeometryEncoder) func(*StandardConsumer) {
	return func(c *StandardConsumer) {
		c.encoder = e
	}
}

// Continually consumes WorkUnits submitted to a work channel producing corresponding gometry .pnts/.glb files and tileset.json files
// continues working until work channel is closed. If an error is raised (or a panic occurs) while processing a work unit,
// the consumer submits it to the error channel and keeps receiving from the work channel, discarding the remaining work
// units without processing them, until the channel is closed: this guarantees that the producer can always submit all its
// work units and terminate even when the work channel buffer is full. Only the first error is reported.
func (c *StandardConsumer) Consume(workchan chan *WorkUnit, errchan chan error, waitGroup *sync.WaitGroup) {
	// signal waitgroup finished work. Registered first so that, by LIFO deferral order, it runs
	// last: any error submitted by the recover handler below must reach the error channel before
	// Done() allows the writer to pass waitGroup.Wait() and close that channel.
	defer waitGroup.Done()
	// safety net for panics raised outside doWork: report them and drain the work channel so
	// that the producer never blocks on a full channel
	defer func() {
		if r := recover(); r != nil {
			debug.PrintStack()
			errchan <- fmt.Errorf("panic: %v", r)
			for range workchan {
			}
		}
	}()
	errored := false
	for {
		// get work from channel
		work, ok := <-workchan
		if !ok {
			// channel was closed by producer, quit infinite loop
			break
		}

		if errored {
			// an error was already reported: discard the remaining work without processing it
			continue
		}

		// do work, converting panics into errors
		err := c.doWorkSafe(work)

		// if there were errors during work send in error channel and switch to draining mode
		if err != nil {
			errchan <- err
			errored = true
		}
	}

}

// doWorkSafe invokes doWork converting panics into errors, so that a panicking work unit
// is handled exactly like a failing one and the consumer keeps draining the work channel.
func (c *StandardConsumer) doWorkSafe(workUnit *WorkUnit) (err error) {
	defer func() {
		if r := recover(); r != nil {
			debug.PrintStack()
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return c.doWork(workUnit)
}

// Takes a workunit and writes the corresponding content.glb/.pnts and tileset.json files
func (c *StandardConsumer) doWork(workUnit *WorkUnit) error {
	node := workUnit.Node

	// encodes and writes the geometries to the disk as a .pnts/.glb file
	err := c.encoder.Write(node, workUnit.WriterProvider, workUnit.Prefix)
	if err != nil {
		return err
	}
	if workUnit.OnDone != nil {
		workUnit.OnDone()
	}
	return nil
}
