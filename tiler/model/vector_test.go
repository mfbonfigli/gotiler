package model

import (
	"math"
	"testing"
)

const tolerance = 1e-7

func compareWithTolerance(u Vector, v Vector, t *testing.T) {
	if math.Abs(u.X-v.X) > tolerance {
		t.Errorf("expected coordinate X %f, got %f", u.X, v.X)
	}
	if math.Abs(u.Y-v.Y) > tolerance {
		t.Errorf("expected coordinate Y %f, got %f", u.Y, v.Y)
	}
	if math.Abs(u.Z-v.Z) > tolerance {
		t.Errorf("expected coordinate Z %f, got %f", u.Z, v.Z)
	}
}

func TestVectorUnit(t *testing.T) {
	v := Vector{
		X: 2.0,
		Y: 2.0,
		Z: 0.0,
	}
	expected := Vector{X: math.Sqrt(2) / 2, Y: math.Sqrt(2) / 2, Z: 0}
	compareWithTolerance(v.Unit(), expected, t)

	v = Vector{
		X: 2.0,
		Y: 0.0,
		Z: 2.0,
	}
	expected = Vector{X: math.Sqrt(2) / 2, Y: 0, Z: math.Sqrt(2) / 2}
	compareWithTolerance(v.Unit(), expected, t)

	v = Vector{
		X: 0.0,
		Y: 2.0,
		Z: 2.0,
	}
	expected = Vector{X: 0, Y: math.Sqrt(2) / 2, Z: math.Sqrt(2) / 2}
	compareWithTolerance(v.Unit(), expected, t)

	v = Vector{
		X: 2.0,
		Y: 2.0,
		Z: 2.0,
	}
	expected = Vector{X: math.Sqrt(3) / 3, Y: math.Sqrt(3) / 3, Z: math.Sqrt(3) / 3}
	compareWithTolerance(v.Unit(), expected, t)
}

func TestVectorNorm(t *testing.T) {
	v := Vector{
		X: 6.0,
		Y: 8.0,
		Z: 0.0,
	}
	expected := 10.0
	if actual := v.Norm(); actual != expected {
		t.Errorf("expected norm %f, got %f", expected, actual)
	}

	v = Vector{
		X: 0.0,
		Y: 8.0,
		Z: 6.0,
	}
	expected = 10.0
	if actual := v.Norm(); actual != expected {
		t.Errorf("expected norm %f, got %f", expected, actual)
	}

	v = Vector{
		X: -6.0,
		Y: -7.0,
		Z: 6.0,
	}
	expected = 11.0
	if actual := v.Norm(); actual != expected {
		t.Errorf("expected norm %f, got %f", expected, actual)
	}
}

func TestVectorCross(t *testing.T) {
	u := Vector{X: 2, Y: 0, Z: 0}
	v := Vector{X: 0, Y: 2, Z: 0}
	expected := Vector{X: 0, Y: 0, Z: 4}
	compareWithTolerance(expected, u.Cross(v), t)

	u = Vector{X: 1, Y: 1, Z: 0}
	v = Vector{X: 0, Y: 1, Z: 0}
	expected = Vector{X: 0, Y: 0, Z: 1}
	compareWithTolerance(expected, u.Cross(v), t)

	u = Vector{X: 0, Y: 1, Z: 0}
	v = Vector{X: 0, Y: 0, Z: 1}
	expected = Vector{X: 1, Y: 0, Z: 0}
	compareWithTolerance(expected, u.Cross(v), t)
}
