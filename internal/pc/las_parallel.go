package pc

import (
	"errors"
	"io"
	"sync"

	"github.com/mfbonfigli/golaz"
	"github.com/mfbonfigli/gotiler/v3/tiler/geom"
)

// Parallel LAS/LAZ decoding.
//
// A LAZ file is a sequence of chunks (50 000 points by default) that are
// compressed independently, and decompression is where nearly all the time of
// reading one goes. golaz can decode chunks on their own (Reader.Chunks,
// Reader.NewChunkDecoder), so a file is read by a few goroutines, each
// decoding whole chunks into batches of points, while GetNext hands the
// batches out in file order. The stream of points is exactly the one
// sequential reading produces, which matters because the tiler's sampling
// depends on the order of its input.

const (
	// lasMaxDecodeWorkers caps the decoding goroutines of one file. Reading
	// alone scales to about 6x with 8 workers and little beyond, because
	// GetNext is a single consumer; a tiling run stops gaining at 4, where
	// the rest of the pipeline becomes the limit.
	lasMaxDecodeWorkers = 8

	// lasMaxBufferedPoints bounds the points decoded but not yet handed out
	// (about 56 bytes each plus attributes). Files with unusually large
	// chunks get fewer workers, or are read sequentially.
	lasMaxBufferedPoints = 1_000_000
)

// lasReadAheadChunks is how many chunks may be decoded, or being decoded, and
// not yet fully handed out: one per worker, plus two so that a worker is not
// idle while GetNext drains a batch.
func lasReadAheadChunks(workers int) int { return workers + 2 }

// lasDecodeWorkers returns how many goroutines should decode a file with the
// given chunks, or 0 when it should be read sequentially: when there is
// nothing to run in parallel, or when the chunks are so large that holding a
// few of them in memory would exceed lasMaxBufferedPoints.
func lasDecodeWorkers(chunks []golaz.Chunk, maxWorkers int) int {
	if len(chunks) < 2 || maxWorkers < 2 {
		return 0
	}
	largest := 0
	for _, c := range chunks {
		largest = max(largest, int(c.NumPoints))
	}
	// lasReadAheadChunks(workers) chunks must fit in the budget.
	byMemory := lasMaxBufferedPoints/largest - 2
	workers := min(maxWorkers, len(chunks), byMemory)
	if workers < 2 {
		return 0
	}
	return workers
}

// lasChunkDecoder decodes chunk number chunk of a file, appends its points to
// dst and returns the extended slice. On error it returns the points decoded
// before the failure. A decoder is used by one goroutine only.
type lasChunkDecoder func(chunk int, dst []geom.Point64) ([]geom.Point64, error)

// errLasReaderClosed is returned when reading from a closed reader.
var errLasReaderClosed = errors.New("las reader is closed")

// lasChunkBatch holds the points of one chunk on their way to next.
type lasChunkBatch struct {
	pts []geom.Point64
	err error // decoding failed after pts
}

// lasChunkPipeline decodes the chunks of a file on several goroutines and
// returns their points one by one, in chunk order. Workers start on the first
// call to next and never run more than lasReadAheadChunks ahead of it.
//
// It is not safe for concurrent use: the owner serialises next, reset and
// close.
type lasChunkPipeline struct {
	numChunks  int
	workers    int
	newDecoder func() (lasChunkDecoder, error) // called once by each worker

	run     *lasPipelineRun // the running workers, nil when there are none
	cur     *lasChunkBatch  // batch being handed out
	pos     int             // next point of cur
	nextOut int             // next chunk to hand out
	err     error           // sticky: io.EOF, a decode error, or errLasReaderClosed
}

// lasPipelineRun is the state shared with one generation of workers. reset
// and close retire it as a whole, so a stale worker can never touch the next
// generation's channels.
type lasPipelineRun struct {
	// results is a ring: chunk i is delivered on results[i%len(results)].
	// At most len(results) chunks are outstanding at any time, and they are
	// consecutive, so every one has a slot of its own.
	results []chan *lasChunkBatch
	// tokens limits the outstanding chunks: a worker takes one before it
	// picks a chunk, next gives it back once that chunk's batch is used up.
	tokens chan struct{}
	free   chan *lasChunkBatch // used-up batches, for reuse
	stop   chan struct{}       // closed to make the workers quit
	wg     sync.WaitGroup

	mu     sync.Mutex
	nextIn int // next chunk to decode
}

func newLasChunkPipeline(numChunks, workers int, newDecoder func() (lasChunkDecoder, error)) *lasChunkPipeline {
	return &lasChunkPipeline{numChunks: numChunks, workers: max(1, min(workers, numChunks)), newDecoder: newDecoder}
}

// next returns the next point in file order, or io.EOF after the last one.
// Once it has returned an error it keeps returning that error until reset.
func (p *lasChunkPipeline) next() (geom.Point64, error) {
	if p.err != nil {
		return geom.Point64{}, p.err
	}
	for p.cur == nil || p.pos >= len(p.cur.pts) {
		if p.cur != nil {
			// The batch is used up. If its chunk failed part-way, this is
			// where the failure sits in the stream.
			err := p.cur.err
			p.recycle(p.cur)
			p.cur = nil
			if err != nil {
				return geom.Point64{}, p.fail(err)
			}
		}
		if p.nextOut >= p.numChunks {
			return geom.Point64{}, p.fail(io.EOF)
		}
		if p.run == nil {
			p.start()
		}
		p.cur = <-p.run.results[p.nextOut%len(p.run.results)]
		p.nextOut++
		p.pos = 0
	}
	pt := p.cur.pts[p.pos]
	p.pos++
	return pt, nil
}

// fail records the error that ends the stream and stops the workers.
func (p *lasChunkPipeline) fail(err error) error {
	p.stopWorkers()
	p.err = err
	return err
}

// reset rewinds to the first point. Workers start again on the next read.
// A closed pipeline stays closed.
func (p *lasChunkPipeline) reset() {
	p.stopWorkers()
	if p.err != errLasReaderClosed {
		p.err = nil
	}
	p.nextOut = 0
}

// close stops the workers and waits for them. Reads fail afterwards.
func (p *lasChunkPipeline) close() {
	p.stopWorkers()
	p.err = errLasReaderClosed
}

func (p *lasChunkPipeline) start() {
	window := lasReadAheadChunks(p.workers)
	run := &lasPipelineRun{
		results: make([]chan *lasChunkBatch, window),
		tokens:  make(chan struct{}, window),
		free:    make(chan *lasChunkBatch, window),
		stop:    make(chan struct{}),
	}
	for i := range run.results {
		run.results[i] = make(chan *lasChunkBatch, 1)
	}
	run.wg.Add(p.workers)
	for i := 0; i < p.workers; i++ {
		go p.work(run)
	}
	p.run = run
}

// stopWorkers retires the current workers, if any, and drops what they had
// decoded ahead.
func (p *lasChunkPipeline) stopWorkers() {
	p.cur, p.pos = nil, 0
	if p.run == nil {
		return
	}
	close(p.run.stop)
	p.run.wg.Wait()
	p.run = nil
}

// recycle returns a used-up batch to the workers and lets one more chunk be
// decoded.
func (p *lasChunkPipeline) recycle(b *lasChunkBatch) {
	b.err = nil
	select {
	case p.run.free <- b:
	default:
	}
	<-p.run.tokens
}

func (p *lasChunkPipeline) work(run *lasPipelineRun) {
	defer run.wg.Done()
	var decode lasChunkDecoder
	var decoderErr error
	for {
		// A token first: it is what keeps the workers from running ahead.
		select {
		case <-run.stop:
			return
		default:
		}
		select {
		case run.tokens <- struct{}{}:
		case <-run.stop:
			return
		}
		run.mu.Lock()
		chunk := run.nextIn
		run.nextIn++
		run.mu.Unlock()
		if chunk >= p.numChunks {
			return
		}

		var b *lasChunkBatch
		select {
		case b = <-run.free:
		default:
			b = &lasChunkBatch{}
		}
		if decode == nil && decoderErr == nil {
			decode, decoderErr = p.newDecoder()
		}
		if decoderErr != nil {
			b.pts, b.err = b.pts[:0], decoderErr
		} else {
			b.pts, b.err = decode(chunk, b.pts[:0])
		}
		// Never blocks: the slot is free until this chunk is delivered.
		run.results[chunk%len(run.results)] <- b
	}
}
