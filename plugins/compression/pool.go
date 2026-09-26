package compression

import "sync"

// SlicePool is a generic type-safe sync.Pool for reusable slices.
type SlicePool[T any] struct {
	pool        sync.Pool
	maxCapacity int
}

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

func (p *SlicePool[T]) GetWithMinCapacity(minCap int) *[]T {
	ptr := p.pool.Get().(*[]T)
	if cap(*ptr) < minCap {
		*ptr = make([]T, minCap)
	} else {
		*ptr = (*ptr)[:minCap]
	}
	return ptr
}

// GetCleared is like GetWithMinCapacity but also zeroes the returned memory.
func (p *SlicePool[T]) GetCleared(minCap int) *[]T {
	ptr := p.GetWithMinCapacity(minCap)
	clear(*ptr)
	return ptr
}

func (p *SlicePool[T]) Put(ptr *[]T) {
	if cap(*ptr) <= p.maxCapacity {
		*ptr = (*ptr)[:0]
		p.pool.Put(ptr)
	}
}
