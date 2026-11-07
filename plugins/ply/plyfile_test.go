package ply

import (
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"
)

var allFormats = []format{formatASCII, formatBinaryLittleEndian, formatBinaryBigEndian}

var binaryFormats = []format{formatBinaryLittleEndian, formatBinaryBigEndian}

func formatName(f format) string {
	for name, candidate := range formatsByName {
		if candidate == f {
			return name
		}
	}
	return fmt.Sprintf("format(%d)", f)
}

var typeNames = map[scalarType]string{
	int8Type:    "char",
	uint8Type:   "uchar",
	int16Type:   "short",
	uint16Type:  "ushort",
	int32Type:   "int",
	uint32Type:  "uint",
	float32Type: "float",
	float64Type: "double",
}

func scalarProp(name string, t scalarType) property {
	return property{name: name, valueType: t}
}

func listProp(name string, countType, valueType scalarType) property {
	return property{name: name, valueType: valueType, countType: countType}
}

// testElement describes an element to write into a test PLY file. Each record
// holds the values in property order; a list property contributes its length
// followed by its items.
type testElement struct {
	name       string
	properties []property
	records    [][]float64
}

func buildPLY(t *testing.T, f format, elements ...testElement) []byte {
	t.Helper()
	var head strings.Builder
	fmt.Fprintf(&head, "ply\nformat %s 1.0\ncomment written by the ply package tests\n", formatName(f))
	for _, e := range elements {
		fmt.Fprintf(&head, "element %s %d\n", e.name, len(e.records))
		for _, p := range e.properties {
			if p.isList() {
				fmt.Fprintf(&head, "property list %s %s %s\n", typeNames[p.countType], typeNames[p.valueType], p.name)
			} else {
				fmt.Fprintf(&head, "property %s %s\n", typeNames[p.valueType], p.name)
			}
		}
	}
	head.WriteString("end_header\n")

	data := []byte(head.String())
	for _, e := range elements {
		for _, record := range e.records {
			data = appendRecord(t, data, f, e.properties, record)
		}
	}
	return data
}

func appendRecord(t *testing.T, data []byte, f format, properties []property, record []float64) []byte {
	t.Helper()
	var order binary.AppendByteOrder = binary.LittleEndian
	if f == formatBinaryBigEndian {
		order = binary.BigEndian
	}
	var words []string
	write := func(typ scalarType, v float64) {
		if f == formatASCII {
			words = append(words, strconv.FormatFloat(v, 'g', -1, 64))
			return
		}
		data = appendScalar(data, typ, order, v)
	}
	next := 0
	for _, p := range properties {
		if next >= len(record) {
			t.Fatalf("record %v has too few values for properties %v", record, properties)
		}
		if !p.isList() {
			write(p.valueType, record[next])
			next++
			continue
		}
		// a negative length is written as is, without items, to build malformed files
		length := max(int(record[next]), 0)
		write(p.countType, record[next])
		next++
		for _, v := range record[next : next+length] {
			write(p.valueType, v)
		}
		next += length
	}
	if f == formatASCII {
		data = append(data, strings.Join(words, " ")+"\n"...)
	}
	return data
}

func appendScalar(b []byte, t scalarType, order binary.AppendByteOrder, v float64) []byte {
	switch t {
	case int8Type:
		return append(b, byte(int8(v)))
	case uint8Type:
		return append(b, byte(v))
	case int16Type:
		return order.AppendUint16(b, uint16(int16(v)))
	case uint16Type:
		return order.AppendUint16(b, uint16(v))
	case int32Type:
		return order.AppendUint32(b, uint32(int32(v)))
	case uint32Type:
		return order.AppendUint32(b, uint32(v))
	case float32Type:
		return order.AppendUint32(b, math.Float32bits(float32(v)))
	case float64Type:
		return order.AppendUint64(b, math.Float64bits(v))
	default:
		panic(fmt.Sprintf("unknown scalar type %d", t))
	}
}
