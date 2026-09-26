package kd

import (
	"math"
	"testing"

	"github.com/mfbonfigli/gotiler/v3/tiler/geom"
)

func TestRootTargetGeometricErrorDefaultsToOneFifthCubicBoundingBoxDiagonal(t *testing.T) {
	n := NewTree()
	n.bounds = geom.NewBoundingBox(-1, 2, 10, 14, 100, 112)

	expected := math.Sqrt(3) * 12 / 5
	if got := n.rootTargetGeometricError(); math.Abs(got-expected) > 1e-9 {
		t.Fatalf("expected root target GE %v, got %v", expected, got)
	}
}

func TestRootTargetGeometricErrorOverride(t *testing.T) {
	n := NewTree(WithRootTargetGeomErr(128))
	n.bounds = geom.NewBoundingBox(-1, 2, 10, 14, 100, 112)

	if got := n.rootTargetGeometricError(); got != 128 {
		t.Fatalf("expected overridden root target GE 128, got %v", got)
	}
}

func TestDefaultRootTargetGeometricErrorHandlesDegenerateBounds(t *testing.T) {
	bounds := geom.NewBoundingBox(5, 5, -2, -2, 7, 7)

	if got := defaultRootTargetGeometricError(bounds); got != 0 {
		t.Fatalf("expected zero target GE for degenerate bounds, got %v", got)
	}
}

func TestGeometricErrorClampsAboveChildren(t *testing.T) {
	parent := NewTree()
	parent.bounds = geom.NewBoundingBox(0, 10, 0, 10, 0, 10)
	parent.numPoints.Store(1000)
	parent.totalPoints.Store(1001)

	child := &Node{
		Mutex:  parent.Mutex,
		bounds: parent.bounds,
		config: parent.config,
	}
	child.numPoints.Store(1)
	child.totalPoints.Store(1)
	parent.left = child

	rawParent := parent.rawGeometricError()
	childGE := child.GeometricError()
	got := parent.GeometricError()
	want := childGE * geometricErrorChildMultiplier

	if rawParent >= want {
		t.Fatalf("test setup invalid: raw parent GE %v should be less than clamped GE %v", rawParent, want)
	}
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("expected parent GE to clamp to child*%.2f (%v), got %v", geometricErrorChildMultiplier, want, got)
	}
}
