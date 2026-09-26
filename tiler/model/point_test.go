package model

import "testing"

func TestPointVector(t *testing.T) {
	p := Point{
		X: 123.5,
		Y: 121.5,
		Z: -4986.5,
	}
	expected := Vector{
		X: 123.5,
		Y: 121.5,
		Z: -4986.5,
	}
	if actual := p.Vector(); actual != expected {
		t.Errorf("expected vector %v, got %v", expected, actual)
	}
}
