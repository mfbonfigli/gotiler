package mutator

import "github.com/mfbonfigli/gotiler/v3/tiler/model"

// WithheldFilter discards points whose withheld attribute is true.
type WithheldFilter struct{}

func NewWithheldFilter() *WithheldFilter {
	return &WithheldFilter{}
}

func (f *WithheldFilter) RequiredAttributes() model.Attributes {
	return model.NewAttributes(model.AttrWithheld)
}

func (f *WithheldFilter) MutateChunk(chunk PointChunk, localToGlobal model.Transform) []model.Point {
	i := model.NewAttributeView(chunk.AttributeLayout, nil).Index(model.AttrWithheld)
	if i < 0 {
		return chunk.Points
	}
	out := chunk.Points[:0]
	for idx, pt := range chunk.Points {
		withheld, err := chunk.AttributeView(idx).Bool(i)
		if err != nil || !withheld {
			out = append(out, pt)
		}
	}
	return out
}

func (f *WithheldFilter) Close() error {
	return nil
}
