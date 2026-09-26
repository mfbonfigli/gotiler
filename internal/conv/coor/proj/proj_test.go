package proj

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mfbonfigli/gotiler/v3/internal/utils"
	"github.com/mfbonfigli/gotiler/v3/tiler/model"
	"github.com/twpayne/go-proj/v10"
)

var coordTolerance = 0.01

func TestToSrid(t *testing.T) {
	c, err := NewProjCoordinateConverter()
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}

	// 4326 to 4978
	actual, err := c.Transform("EPSG:4326", "EPSG:4978", model.Vector{X: 123.474003, Y: 8.099314, Z: 0})
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}
	expected := model.Vector{X: -3483057.5277292132, Y: 5267517.241803079, Z: 892655.4197953615}
	if err := utils.CompareCoord(actual, expected, coordTolerance); err != nil {
		t.Errorf("expected coordinate %v, got %v. Err: %v", expected, actual, err)
	}

	// 4978 to 4326
	expected, err = c.Transform("EPSG:4978", "EPSG:4326", model.Vector{X: -3483057.5277292132, Y: 5267517.241803079, Z: 892655.4197953615})
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}
	actual = model.Vector{X: 123.474003, Y: 8.099314, Z: 0}
	if err := utils.CompareCoord(actual, expected, coordTolerance); err != nil {
		t.Errorf("expected coordinate %v, got %v. Err: %v", expected, actual, err)
	}

	// 4326 to 3124
	actual, err = c.Transform("EPSG:4326", "EPSG:3124", model.Vector{X: 123.474003, Y: 8.099314, Z: 0})
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}
	expected = model.Vector{X: 552074.5400524682, Y: 895674.6033419219, Z: 0}
	if err := utils.CompareCoord(actual, expected, coordTolerance); err != nil {
		t.Errorf("expected coordinate %v, got %v. Err: %v", expected, actual, err)
	}

	// 4978 to 3124
	actual, err = c.Transform("EPSG:4978", "EPSG:3124", model.Vector{X: -3483057.5277292132, Y: 5267517.241803079, Z: 892655.4197953615})
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}
	expected = model.Vector{X: 552074.5400524682, Y: 895674.6033419219, Z: 0}
	if err := utils.CompareCoord(actual, expected, coordTolerance); err != nil {
		t.Errorf("expected coordinate %v, got %v. Err: %v", expected, actual, err)
	}

	// 3124 to 4978
	actual, err = c.Transform("EPSG:3124", "EPSG:4978", model.Vector{X: 552074.5400524682, Y: 895674.6033419219, Z: 0})
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}
	expected = model.Vector{X: -3483057.5277292132, Y: 5267517.241803079, Z: 892655.4197953615}
	if err := utils.CompareCoord(actual, expected, coordTolerance); err != nil {
		t.Errorf("expected coordinate %v, got %v. Err: %v", expected, actual, err)
	}
	c.Cleanup()
}

func TestToWGS84Cartesian(t *testing.T) {
	c, err := NewProjCoordinateConverter()
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}

	// 4326 to 4978
	actual, err := c.ToWGS84Cartesian("EPSG:4326", model.Vector{X: 123.474003, Y: 8.099314, Z: 0})
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}
	expected := model.Vector{X: -3483057.5277292132, Y: 5267517.241803079, Z: 892655.4197953615}
	if err := utils.CompareCoord(actual, expected, coordTolerance); err != nil {
		t.Errorf("expected coordinate %v, got %v. Err: %v", expected, actual, err)
	}
	c.Cleanup()
}
func TestToWGS84CartesianFlat(t *testing.T) {
	c, err := NewProjCoordinateConverter()
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	defer c.Cleanup()

	// EPSG:4326 → EPSG:4978: batch of 3 points
	flatCoords := []float64{
		123.474003, 8.099314, 0, // point 0
		123.474003, 8.099314, 50, // point 1 (same lat/lon, different Z)
		0, 0, 0, // point 2 (null island)
	}
	err = c.ToWGS84CartesianFlat("EPSG:4326", flatCoords)
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}

	// Point 0: same as single-point ToWGS84Cartesian test
	expected0 := model.Vector{X: -3483057.5277292132, Y: 5267517.241803079, Z: 892655.4197953615}
	if err := utils.CompareCoord(
		model.Vector{X: flatCoords[0], Y: flatCoords[1], Z: flatCoords[2]},
		expected0, coordTolerance,
	); err != nil {
		t.Errorf("point 0: %v", err)
	}

	// Verify in-place transformation (values changed from original input)
	if flatCoords[0] == 123.474003 {
		t.Error("point 0 X was not transformed in-place")
	}

	// Point 2 (null island): should produce valid non-zero ECEF coordinates
	if flatCoords[6] == 0 && flatCoords[7] == 0 && flatCoords[8] == 0 {
		t.Error("null island point was not transformed")
	}
}

func TestToWGS84CartesianFlat_Noop(t *testing.T) {
	c, err := NewProjCoordinateConverter()
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	defer c.Cleanup()

	// Already EPSG:4978: should be a no-op (in-place unchanged)
	input := []float64{1.0, 2.0, 3.0, 4.0, 5.0, 6.0}
	original := make([]float64, len(input))
	copy(original, input)

	err = c.ToWGS84CartesianFlat("EPSG:4978", input)
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	for i, v := range input {
		if v != original[i] {
			t.Errorf("flatCoords[%d] changed from %v to %v on no-op", i, original[i], v)
		}
	}
}

func TestToWGS84CartesianFlat_Empty(t *testing.T) {
	c, err := NewProjCoordinateConverter()
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	defer c.Cleanup()

	// Empty slice should return nil immediately
	if err := c.ToWGS84CartesianFlat("EPSG:4326", nil); err != nil {
		t.Fatalf("unexpected error on nil slice: %v", err)
	}
	if err := c.ToWGS84CartesianFlat("EPSG:4326", []float64{}); err != nil {
		t.Fatalf("unexpected error on empty slice: %v", err)
	}
}

func TestTransformFlat(t *testing.T) {
	c, err := NewProjCoordinateConverter()
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	defer c.Cleanup()

	flatCoords := []float64{
		-3483057.5277292132, 5267517.241803079, 892655.4197953615,
	}
	if err := c.TransformFlat("EPSG:4978", "EPSG:4326", flatCoords); err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	expected := model.Vector{X: 123.474003, Y: 8.099314, Z: 0}
	if err := utils.CompareCoord(
		model.Vector{X: flatCoords[0], Y: flatCoords[1], Z: flatCoords[2]},
		expected, coordTolerance,
	); err != nil {
		t.Errorf("expected coordinate %v, got %v. Err: %v", expected, flatCoords, err)
	}
}

func TestTransformFlatRejectsMisalignedInput(t *testing.T) {
	c, err := NewProjCoordinateConverter()
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	defer c.Cleanup()

	if err := c.TransformFlat("EPSG:4326", "EPSG:4978", []float64{1, 2}); err == nil {
		t.Fatal("expected misaligned flat coordinate slice to be rejected")
	}
}

func TestTest(t *testing.T) {
	context := proj.NewContext()

	// The C function does not return any error hence we can only reasonably
	// validate that executing the SetSearchPaths function call does not panic
	// considering various boundary conditions
	context.SetSearchPaths(nil)
	context.SetSearchPaths([]string{})
	context.SetSearchPaths([]string{"/tmp/data"})
	context.SetSearchPaths([]string{"/tmp/data", "/tmp/data2"})
}

func TestProjSearchPathResolvesSymlinks(t *testing.T) {
	dir := t.TempDir()
	install := filepath.Join(dir, "install")
	if err := os.MkdirAll(filepath.Join(install, "share"), 0o755); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(install, "gotiler")
	if err := os.WriteFile(exe, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	// resolve the expected folder too: the temporary folder itself may sit
	// behind a link, like /var on macOS
	want, err := filepath.EvalSymlinks(filepath.Join(install, "share"))
	if err != nil {
		t.Fatal(err)
	}
	if got := projSearchPath(exe); got != want {
		t.Fatalf("projSearchPath(executable) = %q, want %q", got, want)
	}

	// a link to the executable elsewhere, as package managers install it
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(bin, "gotiler")
	if err := os.Symlink(exe, link); err != nil {
		t.Skipf("symbolic links are not available: %v", err)
	}
	if got := projSearchPath(link); got != want {
		t.Fatalf("projSearchPath(link) = %q, want %q", got, want)
	}
}
