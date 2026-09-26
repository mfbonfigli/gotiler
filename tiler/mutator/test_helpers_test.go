package mutator

import "github.com/mfbonfigli/gotiler/v3/tiler/model"

type testAttributeData struct {
	layout []model.AttributeLayoutEntry
	values model.AttributeValues
}

func mutateOne(m Mutator, pt model.Point, attrs testAttributeData, t model.Transform) (model.Point, bool) {
	pt.Attributes = attrs.values
	points := m.MutateChunk(PointChunk{
		Points:          []model.Point{pt},
		AttributeLayout: attrs.layout,
	}, t)
	if len(points) == 0 {
		return model.Point{}, false
	}
	return points[0], true
}
