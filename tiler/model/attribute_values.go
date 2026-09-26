package model

import (
	"encoding/binary"
	"fmt"
	"math"
)

// AttributeValues holds one point's optional attribute values packed in
// little-endian order. The layout (which attribute lives at which offset, with
// which type) is defined by the tree's []AttributeSummary via AttributeLayout;
// values are stored in summary order with no padding. A nil/empty slice means
// the point carries no attribute values.
type AttributeValues []byte

// ZeroAttributeValue returns the zero value for an attribute type.
func ZeroAttributeValue(t AttributeType) any {
	switch t {
	case AttributeInt8:
		return int8(0)
	case AttributeUint8:
		return uint8(0)
	case AttributeInt16:
		return int16(0)
	case AttributeUint16:
		return uint16(0)
	case AttributeInt32:
		return int32(0)
	case AttributeUint32:
		return uint32(0)
	case AttributeInt64:
		return int64(0)
	case AttributeUint64:
		return uint64(0)
	case AttributeBool:
		return false
	case AttributeFloat32:
		return float32(0)
	case AttributeFloat64:
		return float64(0)
	default:
		return nil
	}
}

// EncodeAttributeValue writes a scalar attribute value in little-endian form.
func EncodeAttributeValue(dst []byte, t AttributeType, value any) error {
	switch t {
	case AttributeInt8:
		v, err := attributeInt64(value)
		if err != nil {
			return err
		}
		dst[0] = byte(int8(v))
	case AttributeUint8:
		v, err := attributeUint64(value)
		if err != nil {
			return err
		}
		dst[0] = byte(uint8(v))
	case AttributeInt16:
		v, err := attributeInt64(value)
		if err != nil {
			return err
		}
		binary.LittleEndian.PutUint16(dst, uint16(int16(v)))
	case AttributeUint16:
		v, err := attributeUint64(value)
		if err != nil {
			return err
		}
		binary.LittleEndian.PutUint16(dst, uint16(v))
	case AttributeInt32:
		v, err := attributeInt64(value)
		if err != nil {
			return err
		}
		binary.LittleEndian.PutUint32(dst, uint32(int32(v)))
	case AttributeUint32:
		v, err := attributeUint64(value)
		if err != nil {
			return err
		}
		binary.LittleEndian.PutUint32(dst, uint32(v))
	case AttributeInt64:
		v, err := attributeInt64(value)
		if err != nil {
			return err
		}
		binary.LittleEndian.PutUint64(dst, uint64(v))
	case AttributeUint64:
		v, err := attributeUint64(value)
		if err != nil {
			return err
		}
		binary.LittleEndian.PutUint64(dst, v)
	case AttributeBool:
		v, ok := value.(bool)
		if !ok {
			return fmt.Errorf("expected bool attribute value, got %T", value)
		}
		if v {
			dst[0] = 1
		} else {
			dst[0] = 0
		}
	case AttributeFloat32:
		v, err := attributeFloat64(value)
		if err != nil {
			return err
		}
		binary.LittleEndian.PutUint32(dst, math.Float32bits(float32(v)))
	case AttributeFloat64:
		v, err := attributeFloat64(value)
		if err != nil {
			return err
		}
		binary.LittleEndian.PutUint64(dst, math.Float64bits(v))
	default:
		return fmt.Errorf("unsupported attribute type %q", t)
	}
	return nil
}

// DecodeAttributeValue reads a scalar attribute value from little-endian bytes.
func DecodeAttributeValue(src []byte, t AttributeType) (any, error) {
	switch t {
	case AttributeInt8:
		return int8(src[0]), nil
	case AttributeUint8:
		return uint8(src[0]), nil
	case AttributeInt16:
		return int16(binary.LittleEndian.Uint16(src)), nil
	case AttributeUint16:
		return binary.LittleEndian.Uint16(src), nil
	case AttributeInt32:
		return int32(binary.LittleEndian.Uint32(src)), nil
	case AttributeUint32:
		return binary.LittleEndian.Uint32(src), nil
	case AttributeInt64:
		return int64(binary.LittleEndian.Uint64(src)), nil
	case AttributeUint64:
		return binary.LittleEndian.Uint64(src), nil
	case AttributeBool:
		return src[0] != 0, nil
	case AttributeFloat32:
		return math.Float32frombits(binary.LittleEndian.Uint32(src)), nil
	case AttributeFloat64:
		return math.Float64frombits(binary.LittleEndian.Uint64(src)), nil
	default:
		return nil, fmt.Errorf("unsupported attribute type %q", t)
	}
}

// CompareAttributeValues compares two scalar attribute values of the same type.
func CompareAttributeValues(t AttributeType, a, b any) (int, error) {
	switch t {
	case AttributeInt8, AttributeInt16, AttributeInt32, AttributeInt64:
		av, err := attributeInt64(a)
		if err != nil {
			return 0, err
		}
		bv, err := attributeInt64(b)
		if err != nil {
			return 0, err
		}
		if av < bv {
			return -1, nil
		}
		if av > bv {
			return 1, nil
		}
	case AttributeUint8, AttributeUint16, AttributeUint32, AttributeUint64:
		av, err := attributeUint64(a)
		if err != nil {
			return 0, err
		}
		bv, err := attributeUint64(b)
		if err != nil {
			return 0, err
		}
		if av < bv {
			return -1, nil
		}
		if av > bv {
			return 1, nil
		}
	case AttributeBool:
		av, aok := a.(bool)
		bv, bok := b.(bool)
		if !aok || !bok {
			return 0, fmt.Errorf("expected bool values, got %T and %T", a, b)
		}
		if !av && bv {
			return -1, nil
		}
		if av && !bv {
			return 1, nil
		}
	case AttributeFloat32, AttributeFloat64:
		av, err := attributeFloat64(a)
		if err != nil {
			return 0, err
		}
		bv, err := attributeFloat64(b)
		if err != nil {
			return 0, err
		}
		if av < bv {
			return -1, nil
		}
		if av > bv {
			return 1, nil
		}
	default:
		return 0, fmt.Errorf("unsupported attribute type %q", t)
	}
	return 0, nil
}

func attributeInt64(v any) (int64, error) {
	switch n := v.(type) {
	case int8:
		return int64(n), nil
	case int16:
		return int64(n), nil
	case int32:
		return int64(n), nil
	case int64:
		return n, nil
	case int:
		return int64(n), nil
	default:
		return 0, fmt.Errorf("expected signed integer attribute value, got %T", v)
	}
}

func attributeUint64(v any) (uint64, error) {
	switch n := v.(type) {
	case uint8:
		return uint64(n), nil
	case uint16:
		return uint64(n), nil
	case uint32:
		return uint64(n), nil
	case uint64:
		return n, nil
	case uint:
		return uint64(n), nil
	default:
		return 0, fmt.Errorf("expected unsigned integer attribute value, got %T", v)
	}
}

func attributeFloat64(v any) (float64, error) {
	switch n := v.(type) {
	case float32:
		return float64(n), nil
	case float64:
		return n, nil
	default:
		return 0, fmt.Errorf("expected floating-point attribute value, got %T", v)
	}
}
