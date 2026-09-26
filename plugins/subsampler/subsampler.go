package subsampler

import (
	"math/rand"
	"sync/atomic"

	"github.com/mfbonfigli/gotiler/v3/tiler/model"
	"github.com/mfbonfigli/gotiler/v3/tiler/mutator"
)

type Subsampler struct {
	Percentage float64
	first      *atomic.Bool
}

func New(percentage float64) *Subsampler {
	first := atomic.Bool{}
	first.Store(true)
	return &Subsampler{
		Percentage: percentage,
		first:      &first,
	}
}

func NewSubsampler(percentage float64) *Subsampler {
	return New(percentage)
}

func (s *Subsampler) RequiredAttributes() model.Attributes {
	return nil
}

func (s *Subsampler) MutateChunk(chunk mutator.PointChunk, localToGlobal model.Transform) []model.Point {
	out := chunk.Points[:0]
	for _, pt := range chunk.Points {
		if s.keepPoint() {
			out = append(out, pt)
		}
	}
	return out
}

func (s *Subsampler) Close() error {
	return nil
}

func (s *Subsampler) keepPoint() bool {
	if s.first.Load() {
		// always take the first point to ensure the point cloud has at least one point
		s.first.Swap(false)
		return true
	}
	return rand.Float64() < s.Percentage
}
