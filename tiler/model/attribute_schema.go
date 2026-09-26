package model

import (
	"encoding/binary"
	"fmt"
	"math"
)

// AttributeDescriptor describes one per-point attribute emitted by a reader:
// its canonical name and scalar type. A reader's schema (ordered descriptor
// list) defines the packed layout of the AttributeValues it attaches to every
// point: values are stored contiguously, little-endian, in schema order.
type AttributeDescriptor struct {
	Name string
	Type AttributeType
}

// AttributeSchemaLayout computes the packed value layout of a reader schema.
// All descriptors must have a sized type. The returned total is the byte size
// of one point's packed AttributeValues.
func AttributeSchemaLayout(schema []AttributeDescriptor) (entries []AttributeLayoutEntry, total int, err error) {
	for i, desc := range schema {
		size, ok := AttributeTypeSize(desc.Type)
		if !ok {
			return nil, 0, fmt.Errorf("attribute %q: type %q has no defined size", desc.Name, desc.Type)
		}
		entries = append(entries, AttributeLayoutEntry{
			Name:        desc.Name,
			Type:        desc.Type,
			Offset:      total,
			Size:        size,
			SourceIndex: i,
		})
		total += size
	}
	return entries, total, nil
}

// AttributeValuesArena hands out per-point AttributeValues buffers carved from
// large shared blocks, amortizing one heap allocation over thousands of
// points. Returned buffers are zero-initialized. Not safe for concurrent use:
// callers must synchronize externally (readers typically already hold a mutex
// in GetNext).
type AttributeValuesArena struct {
	block []byte
	off   int
}

// arenaBlockSize balances allocation amortization against how long a block
// stays pinned by in-flight points that reference sub-slices of it.
const arenaBlockSize = 64 * 1024

// Alloc returns a zeroed n-byte AttributeValues buffer.
func (a *AttributeValuesArena) Alloc(n int) AttributeValues {
	if n <= 0 {
		return nil
	}
	if a.off+n > len(a.block) {
		size := arenaBlockSize
		if n > size {
			size = n
		}
		a.block = make([]byte, size)
		a.off = 0
	}
	out := a.block[a.off : a.off+n : a.off+n]
	a.off += n
	return out
}

// AttributeView is a typed window over one point's packed attribute values.
// It is a cheap value type (two slice headers): construct it per point without
// allocation. Setters write through to the underlying buffer.
type AttributeView struct {
	entries []AttributeLayoutEntry
	data    AttributeValues
}

// NewAttributeView builds a view over data using the given layout entries
// (see AttributeLayout / AttributeSchemaLayout).
func NewAttributeView(entries []AttributeLayoutEntry, data AttributeValues) AttributeView {
	return AttributeView{entries: entries, data: data}
}

// Len returns the number of attributes in the view.
func (v AttributeView) Len() int { return len(v.entries) }

// Name returns the canonical name of the i-th attribute.
func (v AttributeView) Name(i int) string { return v.entries[i].Name }

// Type returns the type of the i-th attribute.
func (v AttributeView) Type(i int) AttributeType { return v.entries[i].Type }

// Index returns the index of the named attribute (canonical name), or -1.
func (v AttributeView) Index(name string) int {
	for i := range v.entries {
		if v.entries[i].Name == name {
			return i
		}
	}
	return -1
}

// Value decodes and returns the i-th attribute value. The scalar is boxed into
// an any: convenient, but do not use it in per-point hot paths that care about
// allocations.
func (v AttributeView) Value(i int) (any, error) {
	e := &v.entries[i]
	if len(v.data) < e.Offset+e.Size {
		return ZeroAttributeValue(e.Type), nil
	}
	return DecodeAttributeValue(v.data[e.Offset:e.Offset+e.Size], e.Type)
}

func (v AttributeView) raw(i int, typ AttributeType) ([]byte, bool, error) {
	e := &v.entries[i]
	if e.Type != typ {
		return nil, false, fmt.Errorf("attribute %q has type %q, not %q", e.Name, e.Type, typ)
	}
	if len(v.data) < e.Offset+e.Size {
		return nil, false, nil
	}
	return v.data[e.Offset : e.Offset+e.Size], true, nil
}

// Bool decodes the i-th attribute as bool without boxing.
func (v AttributeView) Bool(i int) (bool, error) {
	raw, ok, err := v.raw(i, AttributeBool)
	if err != nil || !ok {
		return false, err
	}
	return raw[0] != 0, nil
}

// Int8 decodes the i-th attribute as int8 without boxing.
func (v AttributeView) Int8(i int) (int8, error) {
	raw, ok, err := v.raw(i, AttributeInt8)
	if err != nil || !ok {
		return 0, err
	}
	return int8(raw[0]), nil
}

// Uint8 decodes the i-th attribute as uint8 without boxing.
func (v AttributeView) Uint8(i int) (uint8, error) {
	raw, ok, err := v.raw(i, AttributeUint8)
	if err != nil || !ok {
		return 0, err
	}
	return raw[0], nil
}

// Int16 decodes the i-th attribute as int16 without boxing.
func (v AttributeView) Int16(i int) (int16, error) {
	raw, ok, err := v.raw(i, AttributeInt16)
	if err != nil || !ok {
		return 0, err
	}
	return int16(binary.LittleEndian.Uint16(raw)), nil
}

// Uint16 decodes the i-th attribute as uint16 without boxing.
func (v AttributeView) Uint16(i int) (uint16, error) {
	raw, ok, err := v.raw(i, AttributeUint16)
	if err != nil || !ok {
		return 0, err
	}
	return binary.LittleEndian.Uint16(raw), nil
}

// Int32 decodes the i-th attribute as int32 without boxing.
func (v AttributeView) Int32(i int) (int32, error) {
	raw, ok, err := v.raw(i, AttributeInt32)
	if err != nil || !ok {
		return 0, err
	}
	return int32(binary.LittleEndian.Uint32(raw)), nil
}

// Uint32 decodes the i-th attribute as uint32 without boxing.
func (v AttributeView) Uint32(i int) (uint32, error) {
	raw, ok, err := v.raw(i, AttributeUint32)
	if err != nil || !ok {
		return 0, err
	}
	return binary.LittleEndian.Uint32(raw), nil
}

// Int64 decodes the i-th attribute as int64 without boxing.
func (v AttributeView) Int64(i int) (int64, error) {
	raw, ok, err := v.raw(i, AttributeInt64)
	if err != nil || !ok {
		return 0, err
	}
	return int64(binary.LittleEndian.Uint64(raw)), nil
}

// Uint64 decodes the i-th attribute as uint64 without boxing.
func (v AttributeView) Uint64(i int) (uint64, error) {
	raw, ok, err := v.raw(i, AttributeUint64)
	if err != nil || !ok {
		return 0, err
	}
	return binary.LittleEndian.Uint64(raw), nil
}

// Float32 decodes the i-th attribute as float32 without boxing.
func (v AttributeView) Float32(i int) (float32, error) {
	raw, ok, err := v.raw(i, AttributeFloat32)
	if err != nil || !ok {
		return 0, err
	}
	return math.Float32frombits(binary.LittleEndian.Uint32(raw)), nil
}

// Float64 decodes the i-th attribute as float64 without boxing.
func (v AttributeView) Float64(i int) (float64, error) {
	raw, ok, err := v.raw(i, AttributeFloat64)
	if err != nil || !ok {
		return 0, err
	}
	return math.Float64frombits(binary.LittleEndian.Uint64(raw)), nil
}

// SetValue encodes val as the i-th attribute value, writing through to the
// underlying buffer. The value must be convertible to the attribute's type.
func (v AttributeView) SetValue(i int, val any) error {
	e := &v.entries[i]
	if len(v.data) < e.Offset+e.Size {
		return fmt.Errorf("attribute %q: packed values buffer too small", e.Name)
	}
	return EncodeAttributeValue(v.data[e.Offset:e.Offset+e.Size], e.Type, val)
}
