package kd

import (
	"container/list"
	"errors"
	"sync"

	"github.com/mfbonfigli/gotiler/v3/tiler/model"
)

type lruWriterPool struct {
	maxOpen   int
	mu        sync.Mutex
	newWriter func(name string) (PointWriter, error)
	entries   map[string]*list.Element
	lru       *list.List
	// evictErr accumulates errors from writers closed on LRU eviction so that
	// no flush/close failure is silently dropped; CloseAll surfaces (and resets) it.
	evictErr error
}

type poolEntry struct {
	name   string
	writer PointWriter
	// refCount tracks how many goroutines are actively writing to this writer.
	// This prevents the entry from being evicted and closed mid-write.
	refCount int
}

func newLruWriterPool(maxOpen int, newWriter func(name string) (PointWriter, error)) *lruWriterPool {
	return &lruWriterPool{
		maxOpen:   maxOpen,
		newWriter: newWriter,
		entries:   make(map[string]*list.Element),
		lru:       list.New(),
	}
}

func (p *lruWriterPool) WriteBatch(name string, points []model.Point) error {
	if len(points) == 0 {
		return nil
	}

	p.mu.Lock()

	// 1. Handle Cache Hit
	if elem, ok := p.entries[name]; ok {
		p.lru.MoveToFront(elem)
		entry := elem.Value.(*poolEntry)
		entry.refCount++ // Increment so eviction bypasses this entry
		p.mu.Unlock()

		// Perform I/O completely unlocked from the pool
		err := entry.writer.WriteBatch(points)

		// Safely decrement refCount back under pool lock
		p.mu.Lock()
		entry.refCount--
		p.mu.Unlock()
		return err
	}

	// 2. Handle Eviction if at capacity
	// We must find an item that is NOT currently being written to (refCount == 0)
	if p.lru.Len() >= p.maxOpen {
		var evictedW PointWriter

		// Walk backwards from least-recently-used to find an evictable target
		for elem := p.lru.Back(); elem != nil; elem = elem.Prev() {
			entry := elem.Value.(*poolEntry)
			if entry.refCount == 0 {
				delete(p.entries, entry.name)
				p.lru.Remove(elem)
				evictedW = entry.writer
				break
			}
		}

		// If an eligible item was found, close it outside the lock.
		// A close failure means the flush may have been lost: record it so
		// CloseAll surfaces it instead of silently dropping it.
		if evictedW != nil {
			p.mu.Unlock()
			closeErr := evictedW.Close()
			p.mu.Lock()
			if closeErr != nil {
				p.evictErr = errors.Join(p.evictErr, closeErr)
			}
		}
	}

	// 3. Create New Writer (Safely protected against duplicate concurrent creation)
	// Check again just in case another goroutine created it while we were evicting
	if _, exists := p.entries[name]; !exists {
		newW, err := p.newWriter(name)
		if err != nil {
			p.mu.Unlock()
			return err
		}

		entry := &poolEntry{
			name:     name,
			writer:   newW,
			refCount: 1, // Start at 1 because this goroutine is about to use it
		}
		p.entries[name] = p.lru.PushFront(entry)
	} else {
		// Edge-case fallback: Someone else created it during our eviction dance
		p.entries[name].Value.(*poolEntry).refCount++
		p.lru.MoveToFront(p.entries[name])
	}

	w := p.entries[name].Value.(*poolEntry).writer
	p.mu.Unlock()

	// Perform the actual I/O
	err := w.WriteBatch(points)

	// Clean up references
	p.mu.Lock()
	if elem, ok := p.entries[name]; ok {
		elem.Value.(*poolEntry).refCount--
	}
	p.mu.Unlock()

	return err
}

func (p *lruWriterPool) CloseAll() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	var errs []error
	if p.evictErr != nil {
		errs = append(errs, p.evictErr)
		p.evictErr = nil
	}
	for _, elem := range p.entries {
		entry := elem.Value.(*poolEntry)
		if err := entry.writer.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	p.entries = make(map[string]*list.Element)
	p.lru.Init()

	return errors.Join(errs...) // Go 1.20+ clean error aggregation
}
