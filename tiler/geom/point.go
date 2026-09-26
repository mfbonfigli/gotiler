package geom

import (
	"fmt"

	"github.com/mfbonfigli/gotiler/v3/tiler/model"
)

// Point64 contains point-cloud point data with double-precision coordinates.
// Attributes holds the point's optional attribute values packed according to
// the reader's schema (see pointcloud.Reader.AttributeSchema); nil when the
// reader emits no attributes.
type Point64 struct {
	model.Vector
	R          uint8
	G          uint8
	B          uint8
	Attributes model.AttributeValues
}

// NewPoint builds a model.Point from coordinates and standard attributes.
func NewPoint(X, Y, Z float32, R, G, B uint8) model.Point {
	return model.Point{
		X: X,
		Y: Y,
		Z: Z,
		R: R,
		G: G,
		B: B,
	}
}

// PointList models a list of local model.Point values.
type PointList interface {
	Len() int
	Next() (model.Point, error)
	Reset()
	Close() error
}

// LinkedPoint wraps a model.Point to create a linked list.
type LinkedPoint struct {
	Next *LinkedPoint
	Pt   model.Point
}

// LinkedPointStream allows a LinkedPoint chain to implement PointList.
type LinkedPointStream struct {
	len     int
	current *LinkedPoint
	start   *LinkedPoint
}

// NewLinkedPointStream initializes a linked stream from the given root.
func NewLinkedPointStream(root *LinkedPoint, len int) *LinkedPointStream {
	return &LinkedPointStream{
		len:     len,
		current: root,
		start:   root,
	}
}

func (l *LinkedPointStream) Next() (model.Point, error) {
	if l.current == nil {
		return model.Point{}, fmt.Errorf("no more points")
	}
	pt := l.current.Pt
	l.current = l.current.Next
	return pt, nil
}

func (l *LinkedPointStream) Len() int {
	return l.len
}

func (l *LinkedPointStream) Reset() {
	l.current = l.start
}

func (l *LinkedPointStream) Close() error {
	return nil
}
