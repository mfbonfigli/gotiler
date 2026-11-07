package ply

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"math"
	"reflect"
	"strings"
	"testing"
	"testing/iotest"
)

func newTestDecoder(data []byte, bufferSize int) (*vertexDecoder, error) {
	return newTestDecoderFrom(bytes.NewReader(data), bufferSize)
}

func newTestDecoderFrom(src io.Reader, bufferSize int) (*vertexDecoder, error) {
	r := bufio.NewReaderSize(src, bufferSize)
	h, err := readHeader(r)
	if err != nil {
		return nil, err
	}
	return newVertexDecoder(r, h)
}

func readAllVertices(d *vertexDecoder) ([][]float64, error) {
	var out [][]float64
	for {
		values, err := d.next()
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return out, err
		}
		out = append(out, append([]float64(nil), values...))
	}
}

func decodeVertices(t *testing.T, data []byte) (*vertexDecoder, [][]float64) {
	t.Helper()
	d, err := newTestDecoder(data, 64*1024)
	if err != nil {
		t.Fatalf("newVertexDecoder: %v", err)
	}
	vertices, err := readAllVertices(d)
	if err != nil {
		t.Fatalf("reading vertices: %v", err)
	}
	return d, vertices
}

func propertyNames(properties []property) []string {
	names := make([]string, len(properties))
	for i, p := range properties {
		names[i] = p.name
	}
	return names
}

var xyzProperties = []property{
	scalarProp("x", float32Type),
	scalarProp("y", float32Type),
	scalarProp("z", float32Type),
}

func TestVertexDecoderDecodesEveryScalarType(t *testing.T) {
	vertex := testElement{
		name: "vertex",
		properties: []property{
			scalarProp("c", int8Type),
			scalarProp("uc", uint8Type),
			scalarProp("s", int16Type),
			scalarProp("us", uint16Type),
			scalarProp("i", int32Type),
			scalarProp("ui", uint32Type),
			scalarProp("f", float32Type),
			scalarProp("d", float64Type),
		},
		records: [][]float64{
			{math.MinInt8, math.MaxUint8, math.MinInt16, math.MaxUint16, math.MinInt32, math.MaxUint32, -1.5, 1234567.891234567},
			{math.MaxInt8, 0, math.MaxInt16, 0, math.MaxInt32, 0, 3.25, -1e-300},
			{-1, 1, -1, 1, -1, 1, 0, 0},
		},
	}
	for _, f := range allFormats {
		t.Run(formatName(f), func(t *testing.T) {
			d, got := decodeVertices(t, buildPLY(t, f, vertex))
			if !reflect.DeepEqual(got, vertex.records) {
				t.Errorf("vertices = %v, want %v", got, vertex.records)
			}
			if names := propertyNames(d.scalars); !reflect.DeepEqual(names, []string{"c", "uc", "s", "us", "i", "ui", "f", "d"}) {
				t.Errorf("scalars = %v", names)
			}
		})
	}
}

func TestVertexDecoderSkipsElementsDeclaredBeforeVertex(t *testing.T) {
	material := testElement{
		name:       "material",
		properties: []property{scalarProp("red", uint8Type), scalarProp("shininess", float32Type)},
		records:    [][]float64{{10, 0.5}, {20, 0.25}},
	}
	face := testElement{
		name:       "face",
		properties: []property{listProp("vertex_indices", uint8Type, int32Type), scalarProp("flags", uint8Type)},
		records:    [][]float64{{3, 0, 1, 2, 7}, {0, 9}, {4, 0, 1, 2, 3, 1}},
	}
	vertex := testElement{
		name:       "vertex",
		properties: xyzProperties,
		records:    [][]float64{{1, 2, 3}, {-4, 5.5, 6}},
	}
	edge := testElement{
		name:       "edge",
		properties: []property{scalarProp("vertex1", int32Type), scalarProp("vertex2", int32Type)},
		records:    [][]float64{{0, 1}},
	}
	for _, f := range allFormats {
		t.Run(formatName(f), func(t *testing.T) {
			data := buildPLY(t, f, material, face, vertex, edge)
			d, got := decodeVertices(t, data)
			if !reflect.DeepEqual(got, vertex.records) {
				t.Fatalf("vertices = %v, want %v", got, vertex.records)
			}

			d.restart(bufio.NewReader(bytes.NewReader(data[d.vertexDataOffset:])))
			again, err := readAllVertices(d)
			if err != nil {
				t.Fatalf("reading after restart: %v", err)
			}
			if !reflect.DeepEqual(again, vertex.records) {
				t.Errorf("vertices after restart at offset %d = %v, want %v", d.vertexDataOffset, again, vertex.records)
			}
		})
	}
}

func TestVertexDecoderSkipsListPropertiesOnVertex(t *testing.T) {
	vertex := testElement{
		name: "vertex",
		properties: []property{
			scalarProp("x", float32Type),
			listProp("weights", uint8Type, float32Type),
			scalarProp("y", float64Type),
		},
		records: [][]float64{{1, 2, 0.5, 0.25, 2}, {3, 0, 4}},
	}
	for _, f := range allFormats {
		t.Run(formatName(f), func(t *testing.T) {
			d, got := decodeVertices(t, buildPLY(t, f, vertex))
			want := [][]float64{{1, 2}, {3, 4}}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("vertices = %v, want %v", got, want)
			}
			if names := propertyNames(d.scalars); !reflect.DeepEqual(names, []string{"x", "y"}) {
				t.Errorf("scalars = %v, want [x y]", names)
			}
		})
	}
}

func TestVertexDecoderStopsAtTheDeclaredVertexCount(t *testing.T) {
	vertex := testElement{name: "vertex", properties: xyzProperties, records: [][]float64{{1, 2, 3}, {4, 5, 6}}}
	edge := testElement{name: "edge", properties: []property{scalarProp("a", int32Type)}, records: [][]float64{{7}, {8}}}
	for _, f := range allFormats {
		t.Run(formatName(f), func(t *testing.T) {
			data := append(buildPLY(t, f, vertex, edge), "trailing bytes\n"...)
			d, got := decodeVertices(t, data)
			if len(got) != 2 {
				t.Fatalf("read %d vertices, want 2", len(got))
			}
			if _, err := d.next(); !errors.Is(err, io.EOF) {
				t.Errorf("next after the last vertex = %v, want io.EOF", err)
			}
		})
	}
}

func TestVertexDecoderEmptyVertexElement(t *testing.T) {
	for _, f := range allFormats {
		t.Run(formatName(f), func(t *testing.T) {
			_, got := decodeVertices(t, buildPLY(t, f, testElement{name: "vertex", properties: xyzProperties}))
			if len(got) != 0 {
				t.Errorf("read %d vertices, want 0", len(got))
			}
		})
	}
}

func TestVertexDecoderASCIIToleratesWhitespaceVariations(t *testing.T) {
	text := "ply\nformat ascii 1.0\n" +
		"element face 1\nproperty list uchar int vertex_indices\n" +
		"element vertex 3\nproperty float x\nproperty float y\n" +
		"end_header\n" +
		"\n3 0 1 2\r\n" +
		"\n 1\t2 \r\n" +
		"\n\t\n3   4\n" +
		"5 6"
	_, got := decodeVertices(t, []byte(text))
	want := [][]float64{{1, 2}, {3, 4}, {5, 6}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("vertices = %v, want %v", got, want)
	}
}

func TestVertexDecoderASCIISkipsElementLinesLongerThanTheBuffer(t *testing.T) {
	longFace := "100" + strings.Repeat(" 1", 100) + "\n"
	text := "ply\nformat ascii 1.0\n" +
		"element face 2\nproperty list uchar int idx\n" +
		"element vertex 1\nproperty float x\n" +
		"end_header\n" +
		longFace + longFace + "42\n"
	d, err := newTestDecoder([]byte(text), 64)
	if err != nil {
		t.Fatalf("newVertexDecoder: %v", err)
	}
	got, err := readAllVertices(d)
	if err != nil {
		t.Fatalf("reading vertices: %v", err)
	}
	if !reflect.DeepEqual(got, [][]float64{{42}}) {
		t.Errorf("vertices = %v, want [[42]]", got)
	}
}

func TestVertexDecoderErrors(t *testing.T) {
	const asciiXYZ = "ply\nformat ascii 1.0\nelement vertex 3\nproperty float x\nproperty float y\nproperty float z\nend_header\n"
	const asciiFaceThenXYZ = "ply\nformat ascii 1.0\nelement face 2\nproperty list uchar int idx\nelement vertex 1\nproperty float x\nproperty float y\nproperty float z\nend_header\n"
	const asciiList = "ply\nformat ascii 1.0\nelement vertex 1\nproperty list uchar float w\nproperty float y\nend_header\n"
	vertex := testElement{name: "vertex", properties: xyzProperties, records: [][]float64{{1, 2, 3}, {4, 5, 6}, {7, 8, 9}}}
	face := testElement{name: "face", properties: []property{listProp("idx", uint8Type, int32Type)}, records: [][]float64{{3, 0, 1, 2}, {3, 2, 1, 0}}}
	signedList := []property{listProp("idx", int8Type, int32Type)}

	type testCase struct {
		name    string
		data    []byte
		wantErr string
	}
	tests := []testCase{
		{"no vertex element", []byte("ply\nformat ascii 1.0\nelement face 0\nend_header\n"), "PLY file has no vertex element"},
		{"ascii missing vertex lines", []byte(asciiXYZ + "1 2 3\n4 5 6\n"), "PLY vertex 2: data ends early, the header declares 3 vertices"},
		{"ascii missing element lines", []byte(asciiFaceThenXYZ + "3 0 1 2\n"), `PLY element "face": data ends early`},
		{"ascii too few values", []byte(asciiXYZ + "1 2\n"), "PLY vertex 0: found 2 values, expected 3"},
		{"ascii too many values", []byte(asciiXYZ + "1 2 3\n1 2 3 4\n"), "PLY vertex 1: found 4 values, expected 3"},
		{"ascii invalid number", []byte(asciiXYZ + "1 2 3\n1 abc 3\n"), `PLY vertex 1: property "y": invalid number "abc"`},
		{"ascii non integer list length", []byte(asciiList + "1.5 2 3\n"), `property "w": invalid list length "1.5"`},
		{"ascii negative list length", []byte(asciiList + "-1 2\n"), `property "w": invalid list length "-1"`},
		{"ascii list longer than the line", []byte(asciiList + "5 1 2\n"), `property "w": list length 5 exceeds the values on the line`},
		{"ascii missing value after list", []byte(asciiList + "2 1 2\n"), "found 3 values, too few for the declared properties"},
		{"ascii extra value after list", []byte(asciiList + "1 1 2 3\n"), "found 4 values, expected 3"},
		{"binary element too large", []byte("ply\nformat binary_little_endian 1.0\nelement face 9223372036854775807\nproperty double a\nelement vertex 0\nend_header\n"), `PLY element "face": element data is too large`},
	}
	for _, f := range binaryFormats {
		name := formatName(f) + " "
		full := buildPLY(t, f, vertex)
		withFace := buildPLY(t, f, face, vertex)
		withList := buildPLY(t, f, testElement{
			name:       "vertex",
			properties: []property{scalarProp("x", float32Type), listProp("w", uint8Type, float32Type), scalarProp("y", float32Type)},
			records:    [][]float64{{1, 1, 2, 3}, {4, 2, 5, 6, 7}},
		})
		headerLength := bytes.Index(full, []byte("end_header\n")) + len("end_header\n")
		faceHeaderLength := bytes.Index(withFace, []byte("end_header\n")) + len("end_header\n")
		tests = append(tests,
			testCase{name + "truncated vertex", full[:len(full)-2], "PLY vertex 2: data ends early, the header declares 3 vertices"},
			testCase{name + "no vertex data", full[:headerLength], "PLY vertex 0: data ends early"},
			testCase{name + "truncated element before vertex", withFace[:faceHeaderLength+5], `PLY element "face": data ends early`},
			testCase{name + "truncated list on vertex", withList[:len(withList)-6], "PLY vertex 1: data ends early, the header declares 2 vertices"},
			testCase{name + "truncated scalar after list on vertex", withList[:len(withList)-2], "PLY vertex 1: data ends early, the header declares 2 vertices"},
			testCase{name + "negative list length before vertex", buildPLY(t, f, testElement{name: "face", properties: signedList, records: [][]float64{{-1}}}, vertex), `PLY element "face": property "idx": negative list length -1`},
			testCase{name + "negative list length on vertex", buildPLY(t, f, testElement{name: "vertex", properties: signedList, records: [][]float64{{-1}}}), `PLY vertex 0: property "idx": negative list length -1`},
		)
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d, err := newTestDecoder(tc.data, 64*1024)
			if err == nil {
				_, err = readAllVertices(d)
			}
			if err == nil {
				t.Fatalf("decoding succeeded, want error containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}

func TestVertexDecoderReportsReadErrorsAsIs(t *testing.T) {
	errDisk := errors.New("disk failure")
	face := testElement{name: "face", properties: []property{listProp("idx", uint8Type, int32Type)}, records: [][]float64{{3, 0, 1, 2}}}
	fixedVertex := testElement{name: "vertex", properties: xyzProperties, records: [][]float64{{1, 2, 3}}}
	listVertex := testElement{
		name:       "vertex",
		properties: []property{listProp("w", uint8Type, float32Type), scalarProp("x", float32Type)},
		records:    [][]float64{{1, 2, 3}},
	}
	for _, f := range allFormats {
		for _, tc := range []struct {
			name     string
			elements []testElement
		}{
			{"while skipping elements", []testElement{face, fixedVertex}},
			{"while reading fixed vertices", []testElement{fixedVertex}},
			{"while reading vertices with lists", []testElement{listVertex}},
		} {
			t.Run(formatName(f)+" "+tc.name, func(t *testing.T) {
				data := buildPLY(t, f, tc.elements...)
				dataStart := bytes.Index(data, []byte("end_header\n")) + len("end_header\n")
				src := io.MultiReader(bytes.NewReader(data[:dataStart+1]), iotest.ErrReader(errDisk))
				d, err := newTestDecoderFrom(src, 64*1024)
				if err == nil {
					_, err = readAllVertices(d)
				}
				if !errors.Is(err, errDisk) {
					t.Errorf("error = %v, want it to wrap the read error", err)
				}
				if err != nil && strings.Contains(err.Error(), "data ends early") {
					t.Errorf("error = %q, a read failure must not be reported as truncated data", err)
				}
			})
		}
	}
}

func TestVertexDecoderRejectsASCIIVertexLinesLongerThanTheBuffer(t *testing.T) {
	text := "ply\nformat ascii 1.0\nelement vertex 1\nproperty float x\nend_header\n" + strings.Repeat(" ", 100) + "1\n"
	d, err := newTestDecoder([]byte(text), 64)
	if err != nil {
		t.Fatalf("newVertexDecoder: %v", err)
	}
	if _, err := d.next(); err == nil || !strings.Contains(err.Error(), "PLY vertex 0: line too long") {
		t.Errorf("error = %v, want a line too long error", err)
	}
}

func FuzzVertexDecoder(f *testing.F) {
	face := testElement{name: "face", properties: []property{listProp("idx", int8Type, int32Type)}, records: [][]float64{{3, 0, 1, 2}}}
	vertex := testElement{
		name:       "vertex",
		properties: []property{scalarProp("x", float32Type), listProp("w", uint8Type, uint16Type), scalarProp("y", float64Type)},
		records:    [][]float64{{1, 2, 7, 8, 2}, {3, 0, 4}},
	}
	t := &testing.T{}
	for _, format := range allFormats {
		f.Add(buildPLY(t, format, face, vertex))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		d, err := newTestDecoder(data, 4096)
		if err != nil {
			return
		}
		for range 10_000 {
			values, err := d.next()
			if err != nil {
				return
			}
			if len(values) != len(d.scalars) {
				t.Fatalf("got %d values for %d scalar properties", len(values), len(d.scalars))
			}
		}
	})
}
