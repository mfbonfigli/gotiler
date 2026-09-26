package utils

import (
	"testing"

	"github.com/mfbonfigli/gotiler/v3/tiler/model"
)

func TestCompareWithTolerance(t *testing.T) {
	diff, err := CompareWithTolerance(1, 2, 3)
	if err != nil {
		t.Errorf("unexpected err %v", err)
	}
	if diff != 1 {
		t.Errorf("expected diff %f, got %f", 1.0, diff)
	}

	diff, err = CompareWithTolerance(1, 2, 0.5)
	if err == nil {
		t.Errorf("expected error but got none")
	}
	if diff != 1 {
		t.Errorf("expected diff %f, got %f", 1.0, diff)
	}
}

func TestCompareCoord(t *testing.T) {
	actual := model.Vector{X: 1, Y: 1, Z: 1}
	reference := model.Vector{X: 2, Y: 3, Z: 4}
	err := CompareCoord(actual, reference, 5)
	if err != nil {
		t.Errorf("unexpected err %v", err)
	}
	err = CompareCoord(actual, reference, 1)
	if err == nil {
		t.Errorf("expected error but got none")
	}
}
