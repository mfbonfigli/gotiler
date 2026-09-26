package utils

import (
	"fmt"
	"math"

	"github.com/mfbonfigli/gotiler/v3/tiler/model"
)

func CompareWithTolerance(actual, expected, tolerance float64) (diff float64, err error) {
	if math.IsNaN(actual) {
		return math.NaN(), fmt.Errorf("number is NaN")
	}
	diff = math.Abs(actual - expected)
	if diff > math.Abs(tolerance) {
		err = fmt.Errorf("expected value to be within %f from %f, but got %f", tolerance, expected, actual)
	}
	return diff, err
}

func CompareCoord(actual model.Vector, expected model.Vector, tolerance float64) error {
	if diff, err := CompareWithTolerance(actual.X, expected.X, tolerance); err != nil {
		return fmt.Errorf("failed tolerance check on X coordinate, expected error less than %f but got %f error", tolerance, diff)
	}
	if diff, err := CompareWithTolerance(actual.Y, expected.Y, tolerance); err != nil {
		return fmt.Errorf("failed tolerance check on Y coordinate, expected error less than %f but got %f error", tolerance, diff)
	}
	if diff, err := CompareWithTolerance(actual.Z, expected.Z, tolerance); err != nil {
		return fmt.Errorf("failed tolerance check on Z coordinate, expected error less than %f but got %f error", tolerance, diff)
	}
	return nil
}
