package tiling

import (
	"cmp"
	"container/list"
	"context"
	"errors"
	"io"
	"path/filepath"
	"slices"
	"sync"

	"github.com/mfbonfigli/gotiler/v3/internal/geom"
	"github.com/mfbonfigli/gotiler/v3/internal/splat"
	"github.com/mfbonfigli/gotiler/v3/internal/splat/store"
)

// maxOpenWriters is how many leaf stores routing keeps open at once. The
// others are closed and reopened to append to them when needed.
const maxOpenWriters = 1024

// route moves the splats of the input store to stores of the leaves of plan,
// in dir, making their centers relative to the input origin. Workers read
// parts of the input in parallel.
//
// It sets the path, count and bounds of each leaf, and creates no store for a
// leaf that gets no splats. Within a leaf store, the splats of each batch keep
// their input order, but the batches of different workers interleave.
func route(ctx context.Context, in *input, plan *node, dir string, workers int) error {
	leaves := plan.leaves(nil)
	for i, leaf := range leaves {
		leaf.ordinal = i
		leaf.path = filepath.Join(dir, "leaf_"+leaf.id+".bin")
		leaf.count, leaf.bounds = 0, geom.Bounds{}
	}
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	pool := newWriterPool(maxOpenWriters, in.shCount)
	workers = max(1, min(workers, in.count))
	var wg sync.WaitGroup
	for w := range workers {
		start, end := in.count*w/workers, in.count*(w+1)/workers
		wg.Go(func() {
			if err := routeRange(ctx, in, plan, len(leaves), pool, start, end-start); err != nil {
				cancel(err)
			}
		})
	}
	wg.Wait()
	closeErr := pool.closeAll()
	if err := context.Cause(ctx); err != nil {
		return err
	}
	return closeErr
}

// routeRange routes count splats of the input store from index start.
func routeRange(ctx context.Context, in *input, plan *node, leafCount int, pool *writerPool, start, count int) error {
	reader, err := store.OpenRange(in.path, in.shCount, start, count)
	if err != nil {
		return err
	}
	defer reader.Close() //nolint:errcheck
	var batch, grouped []splat.Splat
	var targets []*node
	var order []int32
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		batch, err = reader.Next(batch)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		// Group the batch by leaf, keeping the order of the splats in each.
		targets, order = targets[:0], order[:0]
		for i := range batch {
			batch[i].Center = batch[i].Center.Sub(in.origin)
			targets = append(targets, plan.leafOf(batch[i].Center))
			order = append(order, int32(i))
		}
		slices.SortFunc(order, func(a, b int32) int {
			if c := cmp.Compare(targets[a].ordinal, targets[b].ordinal); c != 0 {
				return c
			}
			return cmp.Compare(a, b)
		})
		grouped = grouped[:0]
		for _, i := range order {
			grouped = append(grouped, batch[i])
		}
		for first := 0; first < len(order); {
			leaf := targets[order[first]]
			last := first + 1
			for last < len(order) && targets[order[last]] == leaf {
				last++
			}
			if err := routeGroup(leaf, grouped[first:last], pool); err != nil {
				return err
			}
			first = last
		}
	}
}

// routeGroup appends splats to the store of leaf.
func routeGroup(leaf *node, splats []splat.Splat, pool *writerPool) error {
	var bounds geom.Bounds
	for _, s := range splats {
		bounds.Include(s.Bounds(boundsSigmas))
	}
	if err := pool.write(leaf, splats); err != nil {
		return err
	}
	leaf.mu.Lock()
	leaf.count += len(splats)
	leaf.bounds.Include(bounds)
	leaf.mu.Unlock()
	return nil
}

// writerPool keeps the stores of up to maxOpen leaves open for writing,
// closing the least recently used one to open another. It is safe for
// concurrent use: writes to different leaves run in parallel.
type writerPool struct {
	maxOpen int
	shCount int

	mu   sync.Mutex
	open map[*node]*list.Element
	// lru orders the open writers, most recently used first.
	lru *list.List
}

type pooledWriter struct {
	leaf   *node
	writer *store.Writer
	// users counts the writes in progress, which keep the writer open.
	users int
	// mu serializes the writes.
	mu sync.Mutex
}

func newWriterPool(maxOpen, shCount int) *writerPool {
	return &writerPool{maxOpen: maxOpen, shCount: shCount, open: map[*node]*list.Element{}, lru: list.New()}
}

// write appends splats to the store of leaf.
func (p *writerPool) write(leaf *node, splats []splat.Splat) error {
	w, err := p.acquire(leaf)
	if err != nil {
		return err
	}
	w.mu.Lock()
	err = w.writer.Write(splats)
	w.mu.Unlock()
	p.mu.Lock()
	w.users--
	p.mu.Unlock()
	return err
}

// acquire returns the open writer of leaf, opening it if needed.
func (p *writerPool) acquire(leaf *node) (*pooledWriter, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if e, ok := p.open[leaf]; ok {
		p.lru.MoveToFront(e)
		w := e.Value.(*pooledWriter)
		w.users++
		return w, nil
	}
	if p.lru.Len() >= p.maxOpen {
		if err := p.closeLeastRecentlyUsed(); err != nil {
			return nil, err
		}
	}
	writer, err := store.Append(leaf.path, p.shCount)
	if err != nil {
		return nil, err
	}
	w := &pooledWriter{leaf: leaf, writer: writer, users: 1}
	p.open[leaf] = p.lru.PushFront(w)
	return w, nil
}

// closeLeastRecentlyUsed closes the least recently used writer that no write
// is using. When all are in use, it closes none, letting the pool grow.
func (p *writerPool) closeLeastRecentlyUsed() error {
	for e := p.lru.Back(); e != nil; e = e.Prev() {
		w := e.Value.(*pooledWriter)
		if w.users == 0 {
			delete(p.open, w.leaf)
			p.lru.Remove(e)
			return w.writer.Close()
		}
	}
	return nil
}

// closeAll closes every open writer and returns the first error.
func (p *writerPool) closeAll() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	var first error
	for e := p.lru.Front(); e != nil; e = e.Next() {
		if err := e.Value.(*pooledWriter).writer.Close(); err != nil && first == nil {
			first = err
		}
	}
	clear(p.open)
	p.lru.Init()
	return first
}
