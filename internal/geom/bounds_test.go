package geom

import "testing"

func TestBoundsZeroValueIsEmpty(t *testing.T) {
	var b Bounds
	if !b.IsEmpty() {
		t.Error("the zero value is not empty")
	}
	assertVec3(t, "size", b.Size(), Vec3{})
	assertClose(t, "max extent", b.MaxExtent(), 0)
	if !b.Translate(Vec3{1, 1, 1}).IsEmpty() {
		t.Error("translating an empty box made it non-empty")
	}
}

func TestBoundsIncludePoint(t *testing.T) {
	var b Bounds
	b.IncludePoint(Vec3{-1, 2, 3})
	if b.IsEmpty() {
		t.Fatal("the box is empty after adding a point")
	}
	assertVec3(t, "min of one point", b.Min, Vec3{-1, 2, 3})
	assertVec3(t, "max of one point", b.Max, Vec3{-1, 2, 3})

	b.IncludePoint(Vec3{4, -6, 3.5})
	b.IncludePoint(Vec3{0, 0, 0})
	assertVec3(t, "min", b.Min, Vec3{-1, -6, 0})
	assertVec3(t, "max", b.Max, Vec3{4, 2, 3.5})
	assertVec3(t, "center", b.Center(), Vec3{1.5, -2, 1.75})
	assertVec3(t, "size", b.Size(), Vec3{5, 8, 3.5})
	assertClose(t, "max extent", b.MaxExtent(), 8)
}

func TestBoundsInclude(t *testing.T) {
	var a, b Bounds
	a.IncludePoint(Vec3{0, 0, 0})
	a.IncludePoint(Vec3{1, 1, 1})
	b.IncludePoint(Vec3{-2, 0.5, 3})

	a.Include(b)
	assertVec3(t, "min", a.Min, Vec3{-2, 0, 0})
	assertVec3(t, "max", a.Max, Vec3{1, 1, 3})

	before := a
	a.Include(Bounds{})
	if a != before {
		t.Errorf("including an empty box changed %+v to %+v", before, a)
	}

	var empty Bounds
	empty.Include(b)
	if empty != b {
		t.Errorf("including %+v in an empty box gave %+v", b, empty)
	}
}

func TestBoundsTranslate(t *testing.T) {
	var b Bounds
	b.IncludePoint(Vec3{0, 1, 2})
	b.IncludePoint(Vec3{3, 4, 5})

	moved := b.Translate(Vec3{-1, 0, 10})
	assertVec3(t, "min", moved.Min, Vec3{-1, 1, 12})
	assertVec3(t, "max", moved.Max, Vec3{2, 4, 15})
	if moved.IsEmpty() {
		t.Error("translating a box made it empty")
	}
}
