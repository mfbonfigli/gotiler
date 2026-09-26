package utils

import "sync"

// SlicePool is a generic type-safe sync.Pool for reusable slices.
type SlicePool[T any] struct {
	pool        sync.Pool
	maxCapacity int
}

// NewSlicePool creates a new SlicePool. Items with capacity <= maxCapacity
// are pooled; larger ones are discarded on Put to let GC wipe oversized slices.
func NewSlicePool[T any](maxCapacity int) *SlicePool[T] {
	return &SlicePool[T]{
		maxCapacity: maxCapacity,
		pool: sync.Pool{
			New: func() any {
				s := make([]T, 0, maxCapacity)
				return &s
			},
		},
	}
}

// Get returns a pooled slice. After being used it must be returned using the Put function on the pool.
func (p *SlicePool[T]) Get() *[]T {
	ptr := p.pool.Get().(*[]T)
	*ptr = (*ptr)[:0]
	return ptr
}

// GetWithMinCapacity returns a pooled slice with at least minCap capacity, length = minCap.
// If the pooled slice is too small, a new larger one is allocated in place of that one.
func (p *SlicePool[T]) GetWithMinCapacity(minCap int) *[]T {
	ptr := p.pool.Get().(*[]T)
	if cap(*ptr) < minCap {
		*ptr = make([]T, minCap)
	} else {
		*ptr = (*ptr)[:minCap]
	}
	return ptr
}

// GetCleared is like Get but also zeroes the underlying memory.
func (p *SlicePool[T]) GetCleared(minCap int) *[]T {
	ptr := p.GetWithMinCapacity(minCap)
	clear(*ptr)
	return ptr
}

// Put returns the slice to the pool if its capacity does not exceed maxCapacity.
func (p *SlicePool[T]) Put(ptr *[]T) {
	if cap(*ptr) <= p.maxCapacity {
		// reset the length to zero
		*ptr = (*ptr)[:0]
		p.pool.Put(ptr)
	}
}
