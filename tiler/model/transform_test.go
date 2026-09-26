package model

import "testing"

func TestTransformForwardInverse(t *testing.T) {
	// pure translation
	q := NewTransform(
		[4][4]float64{
			{1, 0, 0, 10},
			{0, 1, 0, 20},
			{0, 0, 1, 30},
			{0, 0, 0, 1},
		},
	)
	source := Vector{X: 5, Y: -4, Z: 7}
	actual := q.Forward(source)
	expected := Vector{X: 15, Y: 16, Z: 37}
	compareWithTolerance(expected, actual, t)
	actual = q.Inverse(expected)
	expected = source
	compareWithTolerance(expected, actual, t)

	// pure rotation around z
	q = NewTransform(
		[4][4]float64{
			{0, -1, 0, 0},
			{1, 0, 0, 0},
			{0, 0, 1, 0},
			{0, 0, 0, 1},
		},
	)
	source = Vector{X: 5, Y: -4, Z: 7}
	actual = q.Forward(source)
	expected = Vector{X: 4, Y: 5, Z: 7}
	compareWithTolerance(expected, actual, t)
	actual = q.Inverse(expected)
	expected = source
	compareWithTolerance(expected, actual, t)

	// pure rotation around x
	q = NewTransform(
		[4][4]float64{
			{1, 0, 0, 0},
			{0, 0, -1, 0},
			{0, 1, 0, 0},
			{0, 0, 0, 1},
		},
	)
	source = Vector{X: 5, Y: -4, Z: 7}
	actual = q.Forward(source)
	expected = Vector{X: 5, Y: -7, Z: -4}
	compareWithTolerance(expected, actual, t)
	actual = q.Inverse(expected)
	expected = source
	compareWithTolerance(expected, actual, t)

	// pure rotation around y
	q = NewTransform(
		[4][4]float64{
			{0, 0, 1, 0},
			{0, 1, 0, 0},
			{-1, 0, 0, 0},
			{0, 0, 0, 1},
		},
	)
	source = Vector{X: 5, Y: -4, Z: 7}
	actual = q.Forward(source)
	expected = Vector{X: 7, Y: -4, Z: -5}
	compareWithTolerance(expected, actual, t)
	actual = q.Inverse(expected)
	expected = source
	compareWithTolerance(expected, actual, t)

	// translation and rotation
	q = NewTransform(
		[4][4]float64{
			{0, -1, 0, 10},
			{1, 0, 0, 20},
			{0, 0, 1, 30},
			{0, 0, 0, 1},
		},
	)
	source = Vector{X: 5, Y: -4, Z: 7}
	actual = q.Forward(source)
	expected = Vector{X: 14, Y: 25, Z: 37}
	compareWithTolerance(expected, actual, t)
	actual = q.Inverse(expected)
	expected = source
	compareWithTolerance(expected, actual, t)
}

func TestColumnMajorForwardInverse(t *testing.T) {
	q := NewTransform(
		[4][4]float64{
			{0, -1, 0, 10},
			{1, 0, 0, 20},
			{0, 0, 1, 30},
			{0, 0, 0, 1},
		},
	)

	expectedForward := [16]float64{
		0, 1, 0, 0,
		-1, 0, 0, 0,
		0, 0, 1, 0,
		10, 20, 30, 1,
	}

	expectedInverse := [16]float64{
		0, -1, 0, 0,
		1, 0, 0, 0,
		0, 0, 1, 0,
		-20, 10, -30, 1,
	}

	if actual := q.ForwardColumnMajor(); actual != expectedForward {
		t.Errorf("expected forward column major %v, got %v", expectedForward, actual)
	}

	if actual := q.InverseColumnMajor(); actual != expectedInverse {
		t.Errorf("expected inverse column major %v, got %v", expectedInverse, actual)
	}
}
