package geom

import "math"

// Bounds is an axis-aligned bounding box. The zero value is an empty box,
// which grows to contain the points and boxes added to it.
type Bounds struct {
	Min, Max Vec3
	nonEmpty bool
}

// IsEmpty reports whether nothing was added to the box.
func (b Bounds) IsEmpty() bool { return !b.nonEmpty }

// IncludePoint grows the box to contain p.
func (b *Bounds) IncludePoint(p Vec3) {
	if !b.nonEmpty {
		b.Min, b.Max, b.nonEmpty = p, p, true
		return
	}
	b.Min = Vec3{math.Min(b.Min.X, p.X), math.Min(b.Min.Y, p.Y), math.Min(b.Min.Z, p.Z)}
	b.Max = Vec3{math.Max(b.Max.X, p.X), math.Max(b.Max.Y, p.Y), math.Max(b.Max.Z, p.Z)}
}

// Include grows the box to contain o.
func (b *Bounds) Include(o Bounds) {
	if !o.nonEmpty {
		return
	}
	b.IncludePoint(o.Min)
	b.IncludePoint(o.Max)
}

// Translate returns the box moved by offset.
func (b Bounds) Translate(offset Vec3) Bounds {
	if !b.nonEmpty {
		return b
	}
	return Bounds{Min: b.Min.Add(offset), Max: b.Max.Add(offset), nonEmpty: true}
}

// Center returns the center of the box, the origin when the box is empty.
func (b Bounds) Center() Vec3 {
	return Vec3{(b.Min.X + b.Max.X) * 0.5, (b.Min.Y + b.Max.Y) * 0.5, (b.Min.Z + b.Max.Z) * 0.5}
}

// Size returns the extent of the box along each axis, zero when the box is
// empty.
func (b Bounds) Size() Vec3 {
	if !b.nonEmpty {
		return Vec3{}
	}
	return b.Max.Sub(b.Min)
}

// MaxExtent returns the largest extent of the box.
func (b Bounds) MaxExtent() float64 { return b.Size().MaxComponent() }
