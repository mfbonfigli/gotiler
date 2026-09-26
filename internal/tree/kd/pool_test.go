package kd

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/mfbonfigli/gotiler/v3/tiler/model"
)

type mockPointWriter struct {
	name       string
	writeCalls int32
	closeCalls int32
	writeErr   error
	closeErr   error
	mu         sync.Mutex
	writtenPts [][]model.Point // captured batches
}

func (m *mockPointWriter) WriteBatch(points []model.Point) error {
	atomic.AddInt32(&m.writeCalls, 1)
	m.mu.Lock()
	m.writtenPts = append(m.writtenPts, points)
	m.mu.Unlock()
	return m.writeErr
}

func (m *mockPointWriter) Close() error {
	atomic.AddInt32(&m.closeCalls, 1)
	return m.closeErr
}

// writeCount returns the number of WriteBatch calls.
func (m *mockPointWriter) writeCount() int { return int(atomic.LoadInt32(&m.writeCalls)) }

// closeCount returns the number of Close calls.
func (m *mockPointWriter) closeCount() int { return int(atomic.LoadInt32(&m.closeCalls)) }

// newPoolWithMockFactory creates an lruWriterPool backed by mockPointWriter instances.
// The returned mockWriters slice is populated safely by the factory.
func newPoolWithMockFactory(maxOpen int) (*lruWriterPool, *[]*mockPointWriter) {
	var writers []*mockPointWriter
	var mu sync.Mutex
	pool := newLruWriterPool(maxOpen, func(name string) (PointWriter, error) {
		mw := &mockPointWriter{name: name}
		mu.Lock()
		writers = append(writers, mw)
		mu.Unlock()
		return mw, nil
	})
	return pool, &writers
}

// pt is a helper to create a single point batch.
func pt(x, y, z float32) []model.Point {
	return []model.Point{{X: x, Y: y, Z: z}}
}

func TestNewLruWriterPool_Basic(t *testing.T) {
	pool, _ := newPoolWithMockFactory(10)
	if pool == nil {
		t.Fatal("expected non-nil pool")
	}
	if pool.maxOpen != 10 {
		t.Errorf("expected maxOpen 10, got %d", pool.maxOpen)
	}
	if len(pool.entries) != 0 {
		t.Errorf("expected empty entries, got %d", len(pool.entries))
	}
	if pool.lru.Len() != 0 {
		t.Errorf("expected empty lru list, got %d", pool.lru.Len())
	}
}

func TestWriteBatch_EmptyBatch(t *testing.T) {
	pool, _ := newPoolWithMockFactory(5)

	err := pool.WriteBatch("a", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	err = pool.WriteBatch("a", []model.Point{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// No entries should have been created
	if len(pool.entries) != 0 {
		t.Errorf("expected no entries for empty batches, got %d", len(pool.entries))
	}
}

func TestWriteBatch_CacheHit(t *testing.T) {
	pool, writers := newPoolWithMockFactory(5)

	// First write creates writer "a"
	err := pool.WriteBatch("a", pt(1, 0, 0))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Second write to same name should reuse
	err = pool.WriteBatch("a", pt(2, 0, 0))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(*writers) != 1 {
		t.Fatalf("expected 1 writer created, got %d", len(*writers))
	}
	mw := (*writers)[0]
	if mw.writeCount() != 2 {
		t.Errorf("expected 2 WriteBatch calls, got %d", mw.writeCount())
	}
	if mw.closeCount() != 0 {
		t.Errorf("expected no Close calls, got %d", mw.closeCount())
	}
}

func TestWriteBatch_DifferentNames(t *testing.T) {
	pool, writers := newPoolWithMockFactory(10)

	_ = pool.WriteBatch("a", pt(1, 0, 0))
	_ = pool.WriteBatch("b", pt(2, 0, 0))
	_ = pool.WriteBatch("c", pt(3, 0, 0))

	if len(*writers) != 3 {
		t.Fatalf("expected 3 writers, got %d", len(*writers))
	}
	for _, mw := range *writers {
		if mw.writeCount() != 1 {
			t.Errorf("writer %s: expected 1 write, got %d", mw.name, mw.writeCount())
		}
		if mw.closeCount() != 0 {
			t.Errorf("writer %s: expected 0 closes, got %d", mw.name, mw.closeCount())
		}
	}
}

func TestWriteBatch_WithinCapacity_NoEviction(t *testing.T) {
	pool, writers := newPoolWithMockFactory(3)

	_ = pool.WriteBatch("a", pt(1, 0, 0))
	_ = pool.WriteBatch("b", pt(2, 0, 0))
	_ = pool.WriteBatch("c", pt(3, 0, 0))

	if len(*writers) != 3 {
		t.Fatalf("expected 3 writers, got %d", len(*writers))
	}
	for _, mw := range *writers {
		if mw.closeCount() != 0 {
			t.Errorf("writer %s: expected 0 closes, got %d", mw.name, mw.closeCount())
		}
	}
}

func TestWriteBatch_EvictsLRU_WhenAtCapacity(t *testing.T) {
	pool, writers := newPoolWithMockFactory(2)

	_ = pool.WriteBatch("a", pt(1, 0, 0)) // a created
	_ = pool.WriteBatch("b", pt(2, 0, 0)) // b created, at capacity
	_ = pool.WriteBatch("c", pt(3, 0, 0)) // c created, a should be evicted

	if len(*writers) < 3 {
		t.Fatalf("expected at least 3 writers created, got %d", len(*writers))
	}

	find := func(name string) *mockPointWriter {
		for _, w := range *writers {
			if w.name == name {
				return w
			}
		}
		return nil
	}

	a := find("a")
	if a == nil {
		t.Fatal("writer a not found")
	}
	if a.closeCount() != 1 {
		t.Errorf("writer a: expected 1 close (evicted), got %d", a.closeCount())
	}

	b := find("b")
	if b == nil {
		t.Fatal("writer b not found")
	}
	if b.closeCount() != 0 {
		t.Errorf("writer b: expected 0 closes (still open), got %d", b.closeCount())
	}

	c := find("c")
	if c == nil {
		t.Fatal("writer c not found")
	}
	if c.closeCount() != 0 {
		t.Errorf("writer c: expected 0 closes (just created), got %d", c.closeCount())
	}
}

func TestWriteBatch_AccessReordersLRU(t *testing.T) {
	pool, writers := newPoolWithMockFactory(2)

	_ = pool.WriteBatch("a", pt(1, 0, 0)) // a created
	_ = pool.WriteBatch("b", pt(2, 0, 0)) // b created, at capacity
	_ = pool.WriteBatch("a", pt(3, 0, 0)) // a re-accessed (moves to front), b becomes LRU
	_ = pool.WriteBatch("c", pt(4, 0, 0)) // c created, b should be evicted (NOT a)

	find := func(name string) *mockPointWriter {
		for _, w := range *writers {
			if w.name == name {
				return w
			}
		}
		return nil
	}

	a := find("a")
	if a == nil {
		t.Fatal("writer a not found")
	}
	if a.closeCount() != 0 {
		t.Errorf("writer a: expected 0 closes (should still be open after re-access), got %d", a.closeCount())
	}

	b := find("b")
	if b == nil {
		t.Fatal("writer b not found")
	}
	if b.closeCount() != 1 {
		t.Errorf("writer b: expected 1 close (should be evicted as LRU), got %d", b.closeCount())
	}

	if a.writeCount() != 2 {
		t.Errorf("writer a: expected 2 writes, got %d", a.writeCount())
	}
	c := find("c")
	if c == nil || c.writeCount() != 1 {
		t.Errorf("writer c: expected 1 write, got %d", func() int {
			if c == nil {
				return -1
			}
			return c.writeCount()
		}())
	}
}

type latchWriter struct {
	entered chan struct{}
	release chan struct{}
}

func (l *latchWriter) WriteBatch(points []model.Point) error {
	select {
	case l.entered <- struct{}{}:
	default:
	}
	<-l.release
	return nil
}

func (l *latchWriter) Close() error { return nil }

type concurrentRefWriter struct {
	entered chan struct{}
	release chan struct{}
}

func (c *concurrentRefWriter) WriteBatch(points []model.Point) error {
	c.entered <- struct{}{} // Signal that a goroutine has entered WriteBatch
	<-c.release             // Block until allowed to leave
	return nil
}

func (c *concurrentRefWriter) Close() error { return nil }

func TestWriteBatch_RefCountPreventsEviction_Concurrent(t *testing.T) {
	var writers []*mockPointWriter
	var sliceMu sync.Mutex

	firstWriterEntered := make(chan struct{})
	firstWriterRelease := make(chan struct{})
	writerInstance := &concurrentRefWriter{
		entered: firstWriterEntered,
		release: firstWriterRelease,
	}

	// Max open capacity = 1
	pool := newLruWriterPool(1, func(name string) (PointWriter, error) {
		sliceMu.Lock()
		mw := &mockPointWriter{name: name}
		writers = append(writers, mw)
		sliceMu.Unlock()
		return writerInstance, nil
	})

	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(2)

	// Goroutine 1: Pushes first batch, increments refCount, blocks inside WriteBatch
	go func() {
		defer wg.Done()
		errs[0] = pool.WriteBatch("a", pt(1, 0, 0))
	}()

	// Goroutine 2: Waits until G1 is inside WriteBatch, then triggers its own write
	go func() {
		defer wg.Done()
		<-firstWriterEntered // Block until G1 is safely inside WriteBatch holding refCount

		// Spin up a separate helper goroutine to drain the next step.
		// Since G2 will hit the cache and enter the SAME writerInstance.WriteBatch,
		// it will push to c.entered a second time. We must drain it!
		go func() {
			<-firstWriterEntered      // Wait for G2 to enter WriteBatch
			close(firstWriterRelease) // Now unblock BOTH goroutines simultaneously
		}()

		errs[1] = pool.WriteBatch("a", pt(2, 0, 0))
	}()

	wg.Wait()

	for i, e := range errs {
		if e != nil {
			t.Errorf("goroutine %d: unexpected error: %v", i, e)
		}
	}
}

func TestWriteBatch_FactoryError(t *testing.T) {
	pool := newLruWriterPool(5, func(name string) (PointWriter, error) {
		return nil, errors.New("factory boom")
	})

	err := pool.WriteBatch("a", pt(1, 0, 0))
	if err == nil || err.Error() != "factory boom" {
		t.Fatalf("expected 'factory boom' error, got %v", err)
	}

	if len(pool.entries) != 0 {
		t.Errorf("expected no entries after factory error, got %d", len(pool.entries))
	}
}

func TestWriteBatch_WriteError(t *testing.T) {
	var writers []*mockPointWriter
	var sliceMu sync.Mutex

	pool := newLruWriterPool(5, func(name string) (PointWriter, error) {
		sliceMu.Lock()
		mw := &mockPointWriter{name: name, writeErr: errors.New("write boom")}
		writers = append(writers, mw)
		sliceMu.Unlock()
		return mw, nil
	})

	err := pool.WriteBatch("a", pt(1, 0, 0))
	if err == nil || err.Error() != "write boom" {
		t.Fatalf("expected 'write boom' error, got %v", err)
	}

	sliceMu.Lock()
	wLen := len(writers)
	var firstWCount int
	if wLen == 1 {
		firstWCount = writers[0].writeCount()
	}
	sliceMu.Unlock()

	if wLen != 1 {
		t.Fatalf("expected 1 writer, got %d", wLen)
	}
	if firstWCount != 1 {
		t.Errorf("expected 1 write attempt, got %d", firstWCount)
	}
}

func TestWriteBatch_DuplicateCreationGuard(t *testing.T) {
	// Tests the edge case where two threads try to create a writer for the SAME file name simultaneously.
	// One thread will build it, while the other will fall through the map protection.

	factoryEntered := make(chan struct{})
	factoryRelease := make(chan struct{})

	pool := newLruWriterPool(5, func(name string) (PointWriter, error) {
		close(factoryEntered) // Notify that creation logic has started
		<-factoryRelease      // Hold it in the creation phase
		return &mockPointWriter{name: name}, nil
	})

	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(2)

	// Thread 1: Hits a cache miss, enters the factory creation sequence, and blocks
	go func() {
		defer wg.Done()
		errs[0] = pool.WriteBatch("a", pt(1, 0, 0))
	}()

	// Thread 2: Waits for Thread 1 to get inside the factory block, then calls WriteBatch.
	// It will block on the pool's internal `p.mu.Lock()` because Thread 1 holds it
	// until the factory returns.
	go func() {
		defer wg.Done()
		<-factoryEntered

		// Give Thread 2 a brief moment to queue up on p.mu.Lock()
		// Then release the factory block so Thread 1 can finish creation and Thread 2 can follow.
		close(factoryRelease)

		errs[1] = pool.WriteBatch("a", pt(2, 0, 0))
	}()

	wg.Wait()

	for i, e := range errs {
		if e != nil {
			t.Errorf("goroutine %d: unexpected error: %v", i, e)
		}
	}

	// Verify that the duplicate protection worked cleanly: only 1 entry should exist in the pool
	pool.mu.Lock()
	entryCount := len(pool.entries)
	lruLen := pool.lru.Len()
	var refCount int
	if lruLen == 1 {
		refCount = pool.lru.Front().Value.(*poolEntry).refCount
	}
	pool.mu.Unlock()

	if entryCount != 1 {
		t.Errorf("expected 1 entry in pool map, got %d", entryCount)
	}
	if lruLen != 1 {
		t.Errorf("expected 1 entry in pool list, got %d", lruLen)
	}
	if refCount != 0 {
		t.Errorf("expected refCount to return to 0 after completion, got %d", refCount)
	}
}

func TestCloseAll(t *testing.T) {
	pool, writers := newPoolWithMockFactory(10)

	_ = pool.WriteBatch("a", pt(1, 0, 0))
	_ = pool.WriteBatch("b", pt(2, 0, 0))
	_ = pool.WriteBatch("c", pt(3, 0, 0))

	err := pool.CloseAll()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, mw := range *writers {
		if mw.closeCount() != 1 {
			t.Errorf("writer %s: expected 1 close, got %d", mw.name, mw.closeCount())
		}
	}

	if len(pool.entries) != 0 {
		t.Errorf("expected empty entries after CloseAll, got %d", len(pool.entries))
	}
	if pool.lru.Len() != 0 {
		t.Errorf("expected empty lru after CloseAll, got %d", pool.lru.Len())
	}
}

func TestCloseAll_ErrorAggregation(t *testing.T) {
	closeErr := errors.New("close boom")
	var writers []*mockPointWriter
	var sliceMu sync.Mutex

	pool := newLruWriterPool(5, func(name string) (PointWriter, error) {
		sliceMu.Lock()
		mw := &mockPointWriter{name: name}
		writers = append(writers, mw)
		sliceMu.Unlock()
		return mw, nil
	})

	_ = pool.WriteBatch("a", pt(1, 0, 0))
	_ = pool.WriteBatch("b", pt(2, 0, 0))

	sliceMu.Lock()
	writers[0].closeErr = closeErr
	sliceMu.Unlock()

	err := pool.CloseAll()
	if err == nil {
		t.Fatal("expected error from CloseAll, got nil")
	}

	sliceMu.Lock()
	for _, mw := range writers {
		if mw.closeCount() != 1 {
			t.Errorf("writer %s: expected 1 close, got %d", mw.name, mw.closeCount())
		}
	}
	sliceMu.Unlock()

	if len(pool.entries) != 0 {
		t.Errorf("expected empty entries after CloseAll, got %d", len(pool.entries))
	}
}

func TestWriteBatch_EvictionCloseError_SurfacesInCloseAll(t *testing.T) {
	closeErr := errors.New("evicted close boom")
	var writers []*mockPointWriter
	var sliceMu sync.Mutex

	pool := newLruWriterPool(1, func(name string) (PointWriter, error) {
		sliceMu.Lock()
		mw := &mockPointWriter{name: name}
		if name == "a" {
			mw.closeErr = closeErr
		}
		writers = append(writers, mw)
		sliceMu.Unlock()
		return mw, nil
	})

	_ = pool.WriteBatch("a", pt(1, 0, 0)) // a created
	_ = pool.WriteBatch("b", pt(2, 0, 0)) // b created, a evicted, a.Close() fails

	sliceMu.Lock()
	aCloses := writers[0].closeCount()
	sliceMu.Unlock()
	if aCloses != 1 {
		t.Fatalf("expected writer a to be closed on eviction, got %d closes", aCloses)
	}

	err := pool.CloseAll()
	if err == nil {
		t.Fatal("expected CloseAll to surface the eviction close error, got nil")
	}
	if !errors.Is(err, closeErr) {
		t.Errorf("expected CloseAll error to wrap the eviction close error, got %v", err)
	}

	// A second CloseAll must not report the same error again.
	if err := pool.CloseAll(); err != nil {
		t.Errorf("expected nil error from second CloseAll, got %v", err)
	}
}

func TestWriteBatch_MaxOpenZero(t *testing.T) {
	pool, writers := newPoolWithMockFactory(0)

	_ = pool.WriteBatch("a", pt(1, 0, 0))
	_ = pool.WriteBatch("b", pt(2, 0, 0))
	_ = pool.WriteBatch("c", pt(3, 0, 0))

	if len(*writers) < 3 {
		t.Fatalf("expected at least 3 writers, got %d", len(*writers))
	}

	find := func(name string) *mockPointWriter {
		for _, w := range *writers {
			if w.name == name {
				return w
			}
		}
		return nil
	}

	if c := find("a"); c == nil || c.closeCount() != 1 {
		t.Errorf("writer a: expected 1 close, got %v", func() string {
			if c == nil {
				return "nil"
			}
			return fmt.Sprintf("%d", c.closeCount())
		}())
	}
	if c := find("b"); c == nil || c.closeCount() != 1 {
		t.Errorf("writer b: expected 1 close, got %v", func() string {
			if c == nil {
				return "nil"
			}
			return fmt.Sprintf("%d", c.closeCount())
		}())
	}
	if c := find("c"); c == nil || c.closeCount() != 0 {
		t.Errorf("writer c: expected 0 closes (current), got %v", func() string {
			if c == nil {
				return "nil"
			}
			return fmt.Sprintf("%d", c.closeCount())
		}())
	}
}

func TestWriteBatch_RefCountPreventsEviction_DifferentNames(t *testing.T) {
	firstEntered := make(chan struct{}, 1)
	firstUnblock := make(chan struct{})

	pool := newLruWriterPool(1, func(name string) (PointWriter, error) {
		if name == "a" {
			return &latchWriter{release: firstUnblock, entered: firstEntered}, nil
		}
		return &mockPointWriter{name: name}, nil
	})

	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(2)

	go func() {
		defer wg.Done()
		errs[0] = pool.WriteBatch("a", pt(1, 0, 0))
	}()

	go func() {
		defer wg.Done()
		<-firstEntered // wait for G1 to enter WriteBatch
		errs[1] = pool.WriteBatch("b", pt(2, 0, 0))
		close(firstUnblock) // unblock G1
	}()

	wg.Wait()

	for i, e := range errs {
		if e != nil {
			t.Errorf("goroutine %d: unexpected error: %v", i, e)
		}
	}

	if pool.lru.Len() < 1 {
		t.Errorf("expected at least 1 entry, got %d", pool.lru.Len())
	}
}
