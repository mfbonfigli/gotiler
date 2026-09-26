package mutator

import (
	"reflect"
	"testing"

	"github.com/mfbonfigli/gotiler/v3/tiler/geom"
	"github.com/mfbonfigli/gotiler/v3/tiler/model"
)

func TestZOffset(t *testing.T) {
	actual, keep := mutateOne(NewZOffset(2), geom.NewPoint(1, 2, 3, 1, 2, 3), testAttributeData{}, model.Transform{})
	expected := geom.NewPoint(1, 2, 5, 1, 2, 3)
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("expected %v, got %v", expected, actual)
	}
	if !keep {
		t.Errorf("expected keep to be true but is false")
	}
}
