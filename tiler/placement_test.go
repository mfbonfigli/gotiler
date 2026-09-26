package tiler

import (
	"math"
	"testing"

	"github.com/mfbonfigli/gotiler/v3/tiler/model"
)

func vecClose(t *testing.T, got, want model.Vector, tol float64, msg string) {
	t.Helper()
	if math.Abs(got.X-want.X) > tol || math.Abs(got.Y-want.Y) > tol || math.Abs(got.Z-want.Z) > tol {
		t.Fatalf("%s: got %+v, want %+v", msg, got, want)
	}
}

func TestPlacementDefaultAnchorsOnEllipsoidAtZeroZero(t *testing.T) {
	tr, err := Placement{}.Transform()
	if err != nil {
		t.Fatalf("Transform: %v", err)
	}
	// local origin lands on the ellipsoid surface at lat 0, lon 0
	vecClose(t, tr.Forward(model.Vector{}), model.Vector{X: wgs84SemiMajor}, 1e-6, "origin")
	// local +Z is up (outward along +X ECEF at lat 0, lon 0)
	vecClose(t, tr.Forward(model.Vector{Z: 1}), model.Vector{X: wgs84SemiMajor + 1}, 1e-6, "up")
	// local +X is east (+Y ECEF), local +Y is north (+Z ECEF)
	vecClose(t, tr.Forward(model.Vector{X: 1}), model.Vector{X: wgs84SemiMajor, Y: 1}, 1e-6, "east")
	vecClose(t, tr.Forward(model.Vector{Y: 1}), model.Vector{X: wgs84SemiMajor, Z: 1}, 1e-6, "north")
}

func TestPlacementPositionHeightAndLongitude(t *testing.T) {
	tr, err := Placement{Longitude: 90, Height: 100}.Transform()
	if err != nil {
		t.Fatalf("Transform: %v", err)
	}
	// at lon 90 the origin sits along +Y ECEF, 100m above the ellipsoid
	vecClose(t, tr.Forward(model.Vector{}), model.Vector{Y: wgs84SemiMajor + 100}, 1e-6, "origin")
	// up now points along +Y ECEF
	vecClose(t, tr.Forward(model.Vector{Z: 1}), model.Vector{Y: wgs84SemiMajor + 101}, 1e-6, "up")
}

func TestPlacementAtPole(t *testing.T) {
	tr, err := Placement{Latitude: 90}.Transform()
	if err != nil {
		t.Fatalf("Transform: %v", err)
	}
	// the polar radius (semi-minor axis) of WGS84
	b := wgs84SemiMajor * (1 - wgs84Flattening)
	vecClose(t, tr.Forward(model.Vector{}), model.Vector{Z: b}, 1e-5, "origin")
	vecClose(t, tr.Forward(model.Vector{Z: 1}), model.Vector{Z: b + 1}, 1e-5, "up")
}

func TestPlacementHeading(t *testing.T) {
	// heading 90: the model's north (+Y) turns eastward (+Y ECEF at lat/lon 0)
	tr, err := Placement{Heading: 90}.Transform()
	if err != nil {
		t.Fatalf("Transform: %v", err)
	}
	vecClose(t, tr.Forward(model.Vector{Y: 1}), model.Vector{X: wgs84SemiMajor, Y: 1}, 1e-6, "north turned east")
	// east (+X) turns south (-Z ECEF)
	vecClose(t, tr.Forward(model.Vector{X: 1}), model.Vector{X: wgs84SemiMajor, Z: -1}, 1e-6, "east turned south")
	// up unchanged
	vecClose(t, tr.Forward(model.Vector{Z: 1}), model.Vector{X: wgs84SemiMajor + 1}, 1e-6, "up unchanged")
}

func TestPlacementPitch(t *testing.T) {
	// positive pitch lifts the model's east axis above the east-north plane
	tr, err := Placement{Pitch: 90}.Transform()
	if err != nil {
		t.Fatalf("Transform: %v", err)
	}
	vecClose(t, tr.Forward(model.Vector{X: 1}), model.Vector{X: wgs84SemiMajor + 1}, 1e-6, "east lifted up")
}

func TestPlacementRoll(t *testing.T) {
	// roll rotates about the local east axis: the model's north tips up
	tr, err := Placement{Roll: 90}.Transform()
	if err != nil {
		t.Fatalf("Transform: %v", err)
	}
	vecClose(t, tr.Forward(model.Vector{Y: 1}), model.Vector{X: wgs84SemiMajor + 1}, 1e-6, "north tipped up")
	vecClose(t, tr.Forward(model.Vector{X: 1}), model.Vector{X: wgs84SemiMajor, Y: 1}, 1e-6, "east unchanged")
}

func TestPlacementScale(t *testing.T) {
	tr, err := Placement{Scale: 2}.Transform()
	if err != nil {
		t.Fatalf("Transform: %v", err)
	}
	vecClose(t, tr.Forward(model.Vector{Z: 3}), model.Vector{X: wgs84SemiMajor + 6}, 1e-6, "scaled up")
}

func TestPlacementUpAxis(t *testing.T) {
	// Y-up input: the model's +Y must map to up
	tr, err := Placement{UpAxis: AxisY}.Transform()
	if err != nil {
		t.Fatalf("Transform: %v", err)
	}
	vecClose(t, tr.Forward(model.Vector{Y: 1}), model.Vector{X: wgs84SemiMajor + 1}, 1e-6, "y-up")
	// X-up input: the model's +X must map to up
	tr, err = Placement{UpAxis: AxisX}.Transform()
	if err != nil {
		t.Fatalf("Transform: %v", err)
	}
	vecClose(t, tr.Forward(model.Vector{X: 1}), model.Vector{X: wgs84SemiMajor + 1}, 1e-6, "x-up")
}

func TestPlacementInverseRoundTrip(t *testing.T) {
	p := Placement{
		Longitude: 12.5, Latitude: 41.9, Height: 76,
		Heading: 33, Pitch: -12, Roll: 5,
		Scale: 2.5, UpAxis: AxisY,
	}
	tr, err := p.Transform()
	if err != nil {
		t.Fatalf("Transform: %v", err)
	}
	for _, v := range []model.Vector{{}, {X: 10, Y: -20, Z: 5}, {X: -1234.5, Y: 0.25, Z: 999}} {
		back := tr.Inverse(tr.Forward(v))
		vecClose(t, back, v, 1e-6, "roundtrip")
	}
}

func TestPlacementValidation(t *testing.T) {
	invalid := []Placement{
		{Latitude: 91},
		{Latitude: -91},
		{Longitude: 181},
		{Longitude: -181},
		{Scale: -1},
		{UpAxis: "w"},
		{Height: math.NaN()},
		{Heading: math.Inf(1)},
	}
	for i, p := range invalid {
		if _, err := p.Transform(); err == nil {
			t.Fatalf("case %d: expected placement %+v to be rejected", i, p)
		}
	}
}
