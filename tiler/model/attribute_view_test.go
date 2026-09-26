package model

import (
	"testing"
)

func TestAttributeViewTypedGetters(t *testing.T) {
	summaries := []AttributeSummary{
		{Name: "bool", Type: AttributeBool},
		{Name: "int8", Type: AttributeInt8},
		{Name: "uint8", Type: AttributeUint8},
		{Name: "int16", Type: AttributeInt16},
		{Name: "uint16", Type: AttributeUint16},
		{Name: "int32", Type: AttributeInt32},
		{Name: "uint32", Type: AttributeUint32},
		{Name: "int64", Type: AttributeInt64},
		{Name: "uint64", Type: AttributeUint64},
		{Name: "float32", Type: AttributeFloat32},
		{Name: "float64", Type: AttributeFloat64},
	}
	entries, size := AttributeLayout(summaries)
	data := make(AttributeValues, size)
	values := []any{
		true,
		int8(-8),
		uint8(8),
		int16(-1600),
		uint16(1600),
		int32(-320000),
		uint32(320000),
		int64(-6400000000),
		uint64(6400000000),
		float32(32.5),
		float64(64.5),
	}
	for i, entry := range entries {
		if err := EncodeAttributeValue(data[entry.Offset:entry.Offset+entry.Size], entry.Type, values[i]); err != nil {
			t.Fatalf("encode %s: %v", entry.Name, err)
		}
	}

	view := NewAttributeView(entries, data)

	if got, err := view.Bool(0); err != nil || got != true {
		t.Fatalf("Bool = %v, %v", got, err)
	}
	if got, err := view.Int8(1); err != nil || got != -8 {
		t.Fatalf("Int8 = %v, %v", got, err)
	}
	if got, err := view.Uint8(2); err != nil || got != 8 {
		t.Fatalf("Uint8 = %v, %v", got, err)
	}
	if got, err := view.Int16(3); err != nil || got != -1600 {
		t.Fatalf("Int16 = %v, %v", got, err)
	}
	if got, err := view.Uint16(4); err != nil || got != 1600 {
		t.Fatalf("Uint16 = %v, %v", got, err)
	}
	if got, err := view.Int32(5); err != nil || got != -320000 {
		t.Fatalf("Int32 = %v, %v", got, err)
	}
	if got, err := view.Uint32(6); err != nil || got != 320000 {
		t.Fatalf("Uint32 = %v, %v", got, err)
	}
	if got, err := view.Int64(7); err != nil || got != -6400000000 {
		t.Fatalf("Int64 = %v, %v", got, err)
	}
	if got, err := view.Uint64(8); err != nil || got != 6400000000 {
		t.Fatalf("Uint64 = %v, %v", got, err)
	}
	if got, err := view.Float32(9); err != nil || got != 32.5 {
		t.Fatalf("Float32 = %v, %v", got, err)
	}
	if got, err := view.Float64(10); err != nil || got != 64.5 {
		t.Fatalf("Float64 = %v, %v", got, err)
	}
}

func TestAttributeViewTypedGetterWrongType(t *testing.T) {
	entries, size := AttributeLayout([]AttributeSummary{{Name: "flag", Type: AttributeBool}})
	view := NewAttributeView(entries, make(AttributeValues, size))

	if _, err := view.Uint8(0); err == nil {
		t.Fatal("expected wrong-type error")
	}
}

func TestAttributeViewTypedGetterShortBufferReturnsZero(t *testing.T) {
	entries, _ := AttributeLayout([]AttributeSummary{{Name: "value", Type: AttributeUint32}})
	view := NewAttributeView(entries, nil)

	got, err := view.Uint32(0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 0 {
		t.Fatalf("expected zero value for short buffer, got %d", got)
	}
}
