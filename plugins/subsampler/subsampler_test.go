package subsampler

import (
	"testing"

	"github.com/mfbonfigli/gotiler/v3/tiler/model"
	"github.com/mfbonfigli/gotiler/v3/tiler/mutator"
)

var _ mutator.Mutator = (*Subsampler)(nil)

func TestSubsamplerRequiresNoAttributes(t *testing.T) {
	if got := New(0.5).RequiredAttributes(); len(got) != 0 {
		t.Fatalf("expected no required attributes, got %v", got)
	}
}

func TestSubsamplerAlwaysKeepsFirstPoint(t *testing.T) {
	s := New(0)
	keep := mutateOne(s)
	if !keep {
		t.Fatal("expected first point to be kept")
	}
	keep = mutateOne(s)
	if keep {
		t.Fatal("expected second point to be dropped with zero percentage")
	}
}

func TestSubsamplerKeepsAllAtOne(t *testing.T) {
	s := New(1)
	for i := 0; i < 10; i++ {
		keep := mutateOne(s)
		if !keep {
			t.Fatalf("expected point %d to be kept", i)
		}
	}
}

func TestNewSubsamplerAlias(t *testing.T) {
	s := NewSubsampler(0.25)
	if s.Percentage != 0.25 {
		t.Fatalf("unexpected percentage: %f", s.Percentage)
	}
}

func TestSubsamplerDropsAfterFirstWhenPercentageIsNegative(t *testing.T) {
	s := New(-1)
	keep := mutateOne(s)
	if !keep {
		t.Fatal("expected first point to be kept")
	}
	for i := 0; i < 10; i++ {
		keep = mutateOne(s)
		if keep {
			t.Fatalf("expected point %d after first to be dropped", i)
		}
	}
}

func TestSubsamplerKeepsAfterFirstWhenPercentageExceedsOne(t *testing.T) {
	s := New(2)
	for i := 0; i < 10; i++ {
		keep := mutateOne(s)
		if !keep {
			t.Fatalf("expected point %d to be kept", i)
		}
	}
}

func mutateOne(s *Subsampler) bool {
	points := s.MutateChunk(mutator.PointChunk{Points: []model.Point{{}}}, model.IdentityTransform)
	return len(points) > 0
}
