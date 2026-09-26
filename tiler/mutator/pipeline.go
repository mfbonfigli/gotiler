package mutator

import (
	"errors"

	"github.com/mfbonfigli/gotiler/v3/tiler/model"
)

// Pipeline is a mutator that applies all registered mutators sequentially
// and returns the result as output. Like any Mutator, its MutateChunk may be
// invoked concurrently from multiple goroutines, so the registered mutators
// must be safe for concurrent use themselves. Closing the pipeline closes
// all registered mutators.
type Pipeline struct {
	mutators []Mutator
}

func NewPipeline(m ...Mutator) *Pipeline {
	return &Pipeline{
		mutators: m,
	}
}

func (p *Pipeline) RequiredAttributes() model.Attributes {
	if p == nil || len(p.mutators) == 0 {
		return nil
	}
	var names []string
	for _, m := range p.mutators {
		if m == nil {
			continue
		}
		names = append(names, m.RequiredAttributes().Names()...)
	}
	return model.NewAttributes(names...)
}

func (p *Pipeline) MutateChunk(chunk PointChunk, localToGlobal model.Transform) []model.Point {
	if p == nil || len(p.mutators) == 0 {
		return chunk.Points
	}
	points := chunk.Points
	for _, m := range p.mutators {
		if m == nil {
			continue
		}
		points = MutateChunk(m, PointChunk{
			Points:          points,
			AttributeLayout: chunk.AttributeLayout,
		}, localToGlobal)
		if len(points) == 0 {
			return points
		}
	}
	return points
}

// HasWriteMutators reports whether at least one registered mutator also acts
// at write time. Callers can use it to skip write-time mutation entirely when
// no registered mutator needs it.
func (p *Pipeline) HasWriteMutators() bool {
	if p == nil {
		return false
	}
	for _, m := range p.mutators {
		if m == nil {
			continue
		}
		if _, ok := m.(WriteMutator); ok {
			return true
		}
	}
	return false
}

// MutateChunkOnWrite applies, in registration order, the registered mutators
// that implement WriteMutator; the others are skipped.
func (p *Pipeline) MutateChunkOnWrite(chunk PointChunk, localToGlobal model.Transform) []model.Point {
	if p == nil || len(p.mutators) == 0 {
		return chunk.Points
	}
	points := chunk.Points
	for _, m := range p.mutators {
		wm, ok := m.(WriteMutator)
		if !ok {
			continue
		}
		points = wm.MutateChunkOnWrite(PointChunk{
			Points:          points,
			AttributeLayout: chunk.AttributeLayout,
		}, localToGlobal)
	}
	return points
}

func (p *Pipeline) Close() error {
	if p == nil {
		return nil
	}
	var errs []error
	for _, m := range p.mutators {
		if m == nil {
			continue
		}
		if err := m.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
