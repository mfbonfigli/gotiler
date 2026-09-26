package mutator

import "github.com/mfbonfigli/gotiler/v3/tiler/model"

// ZOffset is a mutator that shifts the points vertically for the given offset
type ZOffset struct {
	Offset float32
}

func NewZOffset(offset float32) *ZOffset {
	return &ZOffset{
		Offset: offset,
	}
}

func (z *ZOffset) RequiredAttributes() model.Attributes {
	return nil
}

func (z *ZOffset) MutateChunk(chunk PointChunk, localToGlobal model.Transform) []model.Point {
	if z == nil {
		return chunk.Points
	}
	for i := range chunk.Points {
		chunk.Points[i].Z += z.Offset
	}
	return chunk.Points
}

func (z *ZOffset) Close() error {
	return nil
}
