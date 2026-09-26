package mutator

import (
	"reflect"
	"testing"

	"github.com/mfbonfigli/gotiler/v3/tiler/geom"
	"github.com/mfbonfigli/gotiler/v3/tiler/model"
)

type discardMutator struct{}

func (p *discardMutator) RequiredAttributes() model.Attributes {
	return model.NewAttributes("classification", " intensity ")
}

func (p *discardMutator) MutateChunk(chunk PointChunk, t model.Transform) []model.Point {
	return chunk.Points[:0]
}

func (p *discardMutator) Close() error {
	return nil
}

type chunkRecordingMutator struct {
	chunkCalls int
}

func (m *chunkRecordingMutator) RequiredAttributes() model.Attributes {
	return nil
}

func (m *chunkRecordingMutator) MutateChunk(chunk PointChunk, t model.Transform) []model.Point {
	m.chunkCalls++
	out := chunk.Points[:0]
	for _, pt := range chunk.Points {
		if pt.X == 2 {
			continue
		}
		pt.Z += 10
		out = append(out, pt)
	}
	return out
}

func (m *chunkRecordingMutator) Close() error {
	return nil
}

func TestPipeline(t *testing.T) {
	p := NewPipeline(
		NewZOffset(1.5),
		NewZOffset(2.5),
	)
	actual, keep := mutateOne(p, geom.NewPoint(1, 2, 3, 1, 2, 3), testAttributeData{}, model.Transform{})
	expected := geom.NewPoint(1, 2, 7, 1, 2, 3)
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("expected %v, got %v", expected, actual)
	}
	if !keep {
		t.Errorf("expected keep to be true but is false")
	}
}

func TestPipelineRequiredAttributes(t *testing.T) {
	p := NewPipeline(
		NewZOffset(1.5),
		&discardMutator{},
	)
	got := p.RequiredAttributes()
	if !got.Has("classification") || !got.Has("intensity") {
		t.Fatalf("expected required attributes to include classification and intensity, got %v", got)
	}
	if len(got) != 2 {
		t.Fatalf("expected de-duplicated required attributes, got %v", got)
	}
}

func TestPipelineDiscard(t *testing.T) {
	p := NewPipeline(
		NewZOffset(1.5),
		&discardMutator{},
		NewZOffset(2.5),
	)
	_, keep := mutateOne(p, geom.NewPoint(1, 2, 3, 1, 2, 3), testAttributeData{}, model.Transform{})
	if keep {
		t.Errorf("expected point to be discarded but was not")
	}
}

func TestMutateChunkAppliesMutator(t *testing.T) {
	points := []model.Point{
		geom.NewPoint(1, 2, 3, 1, 2, 3),
		geom.NewPoint(4, 5, 6, 4, 5, 6),
	}
	got := MutateChunk(NewZOffset(2), PointChunk{Points: points}, model.IdentityTransform)
	want := []model.Point{
		geom.NewPoint(1, 2, 5, 1, 2, 3),
		geom.NewPoint(4, 5, 8, 4, 5, 6),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("MutateChunk = %v, want %v", got, want)
	}
}

func TestPipelineMutateChunk(t *testing.T) {
	chunked := &chunkRecordingMutator{}
	p := NewPipeline(
		chunked,
		NewZOffset(1),
	)
	points := []model.Point{
		geom.NewPoint(1, 0, 0, 1, 2, 3),
		geom.NewPoint(2, 0, 0, 1, 2, 3),
		geom.NewPoint(3, 0, 0, 1, 2, 3),
	}

	got := p.MutateChunk(PointChunk{Points: points}, model.IdentityTransform)
	want := []model.Point{
		geom.NewPoint(1, 0, 11, 1, 2, 3),
		geom.NewPoint(3, 0, 11, 1, 2, 3),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("MutateChunk = %v, want %v", got, want)
	}
	if chunked.chunkCalls != 1 {
		t.Fatalf("chunkCalls = %d, want 1", chunked.chunkCalls)
	}
}
