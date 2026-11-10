package ply

import (
	"math"
	"reflect"
	"testing"

	"github.com/mfbonfigli/gotiler/v3/tiler/geom"
	"github.com/mfbonfigli/gotiler/v3/tiler/model"
)

func floatProps(names ...string) []property {
	props := make([]property, len(names))
	for i, name := range names {
		props[i] = scalarProp(name, float32Type)
	}
	return props
}

func newTestMapper(t *testing.T, properties []property, requested ...string) *pointMapper {
	t.Helper()
	m, err := newPointMapper(properties, model.NewAttributes(requested...))
	if err != nil {
		t.Fatalf("newPointMapper: %v", err)
	}
	return m
}

func mapPoint(t *testing.T, m *pointMapper, values ...float64) geom.Point64 {
	t.Helper()
	var arena model.AttributeValuesArena
	pt, ok := m.point(values, &arena)
	if !ok {
		t.Fatalf("point(%v) skipped the vertex", values)
	}
	return pt
}

func attributeValues(t *testing.T, m *pointMapper, pt geom.Point64) map[string]any {
	t.Helper()
	layout, size, err := model.AttributeSchemaLayout(m.schema)
	if err != nil {
		t.Fatalf("schema layout: %v", err)
	}
	if len(pt.Attributes) != size {
		t.Fatalf("point carries %d attribute bytes, schema layout expects %d", len(pt.Attributes), size)
	}
	values := map[string]any{}
	for _, e := range layout {
		v, err := model.DecodeAttributeValue(pt.Attributes[e.Offset:e.Offset+e.Size], e.Type)
		if err != nil {
			t.Fatalf("decode %q: %v", e.Name, err)
		}
		values[e.Name] = v
	}
	return values
}

func TestPointMapperMapsCoordinatesCaseInsensitively(t *testing.T) {
	m := newTestMapper(t, floatProps("intensity", "Z", "y", "X"))
	pt := mapPoint(t, m, 7, 3, 2, 1)
	want := geom.Point64{Vector: model.Vector{X: 1, Y: 2, Z: 3}}
	if !reflect.DeepEqual(pt, want) {
		t.Errorf("point = %+v, want %+v", pt, want)
	}
	if m.schema != nil {
		t.Errorf("schema = %v, want none when no attribute is requested", m.schema)
	}
}

func TestPointMapperRequiresCoordinates(t *testing.T) {
	tests := []struct {
		properties []property
		wantErr    string
	}{
		{floatProps("x", "y"), "the PLY vertex element is missing coordinates: z"},
		{floatProps("X", "z"), "the PLY vertex element is missing coordinates: y"},
		{floatProps("y"), "the PLY vertex element is missing coordinates: x, z"},
		{floatProps("intensity"), "the PLY vertex element is missing coordinates: x, y, z"},
		{nil, "the PLY vertex element is missing coordinates: x, y, z"},
	}
	for _, tc := range tests {
		t.Run(tc.wantErr, func(t *testing.T) {
			_, err := newPointMapper(tc.properties, nil)
			if err == nil || err.Error() != tc.wantErr {
				t.Errorf("error = %v, want %q", err, tc.wantErr)
			}
		})
	}
}

func TestPointMapperRejectsGaussianSplats(t *testing.T) {
	tests := []struct {
		name       string
		properties []property
		wantErr    string
	}{
		{
			"standard layout",
			floatProps("x", "y", "z", "nx", "ny", "nz", "f_dc_0", "f_dc_1", "f_dc_2", "f_rest_0", "opacity", "scale_0", "scale_1", "scale_2", "rot_0", "rot_1", "rot_2", "rot_3"),
			"the file is a Gaussian splat PLY: tile it with gotiler splat",
		},
		{
			"compressed layout",
			[]property{scalarProp("packed_position", uint32Type), scalarProp("packed_rotation", uint32Type), scalarProp("packed_scale", uint32Type), scalarProp("packed_color", uint32Type)},
			"the file is a compressed Gaussian splat PLY: export it as a standard splat PLY and tile it with gotiler splat",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := newPointMapper(tc.properties, nil)
			if err == nil || err.Error() != tc.wantErr {
				t.Errorf("error = %v, want %q", err, tc.wantErr)
			}
		})
	}

	t.Run("point cloud with some splat-like names", func(t *testing.T) {
		newTestMapper(t, floatProps("x", "y", "z", "opacity", "scale_0", "f_dc_0"))
	})
}

func TestPointMapperConvertsColorsByType(t *testing.T) {
	nan := math.NaN()
	tests := []struct {
		name      string
		colorType scalarType
		rgb       [3]float64
		want      [3]uint8
	}{
		{"uchar", uint8Type, [3]float64{0, 128, 255}, [3]uint8{0, 128, 255}},
		{"ushort", uint16Type, [3]float64{0, 32768, 65535}, [3]uint8{0, 128, 255}},
		{"ushort low byte dropped", uint16Type, [3]float64{255, 256, 511}, [3]uint8{0, 1, 1}},
		{"float", float32Type, [3]float64{0, 0.5, 1}, [3]uint8{0, 128, 255}},
		{"float out of range", float32Type, [3]float64{-0.5, 1.5, nan}, [3]uint8{0, 255, 0}},
		{"double", float64Type, [3]float64{0.25, 0.75, 0.1}, [3]uint8{64, 191, 26}},
		{"char", int8Type, [3]float64{-5, 100, 127}, [3]uint8{0, 100, 127}},
		{"short", int16Type, [3]float64{-1, 300, 255}, [3]uint8{0, 255, 255}},
		{"int", int32Type, [3]float64{-1, 70000, 12}, [3]uint8{0, 255, 12}},
		{"uint", uint32Type, [3]float64{0, 4294967295, 200}, [3]uint8{0, 255, 200}},
		{"uchar from lenient ascii", uint8Type, [3]float64{300, -2, 1.6}, [3]uint8{255, 0, 2}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			props := append(floatProps("x", "y", "z"),
				scalarProp("red", tc.colorType), scalarProp("green", tc.colorType), scalarProp("blue", tc.colorType))
			m := newTestMapper(t, props)
			pt := mapPoint(t, m, 0, 0, 0, tc.rgb[0], tc.rgb[1], tc.rgb[2])
			if got := [3]uint8{pt.R, pt.G, pt.B}; got != tc.want {
				t.Errorf("rgb = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestPointMapperColorPropertyNames(t *testing.T) {
	uchar := func(names ...string) []property {
		props := floatProps("x", "y", "z")
		for _, name := range names {
			props = append(props, scalarProp(name, uint8Type))
		}
		return props
	}
	tests := []struct {
		name       string
		properties []property
		values     []float64
		want       [3]uint8
	}{
		{"red green blue", uchar("Red", "GREEN", "blue"), []float64{0, 0, 0, 1, 2, 3}, [3]uint8{1, 2, 3}},
		{"diffuse colors", uchar("diffuse_red", "diffuse_green", "diffuse_blue"), []float64{0, 0, 0, 4, 5, 6}, [3]uint8{4, 5, 6}},
		{"red green blue preferred over diffuse", uchar("diffuse_red", "diffuse_green", "diffuse_blue", "red", "green", "blue"), []float64{0, 0, 0, 4, 5, 6, 1, 2, 3}, [3]uint8{1, 2, 3}},
		{"incomplete channels are not a color", uchar("red", "green", "diffuse_blue"), []float64{0, 0, 0, 1, 2, 3}, [3]uint8{0, 0, 0}},
		{"alpha is ignored", uchar("red", "green", "blue", "alpha"), []float64{0, 0, 0, 1, 2, 3, 4}, [3]uint8{1, 2, 3}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pt := mapPoint(t, newTestMapper(t, tc.properties), tc.values...)
			if got := [3]uint8{pt.R, pt.G, pt.B}; got != tc.want {
				t.Errorf("rgb = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestPointMapperSkipsVerticesWithNonFiniteCoordinates(t *testing.T) {
	m := newTestMapper(t, floatProps("x", "y", "z"))
	var arena model.AttributeValuesArena
	for _, bad := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		for axis := range 3 {
			values := []float64{1, 2, 3}
			values[axis] = bad
			if _, ok := m.point(values, &arena); ok {
				t.Errorf("point(%v) was kept, want it skipped", values)
			}
		}
	}
	if _, ok := m.point([]float64{-1e300, 0, 1e300}, &arena); !ok {
		t.Error("a vertex with large finite coordinates was skipped")
	}
}

func TestPointMapperEmitsAttributesWithTheirPLYType(t *testing.T) {
	props := append(floatProps("x", "y", "z"),
		scalarProp("a_char", int8Type),
		scalarProp("a_uchar", uint8Type),
		scalarProp("a_short", int16Type),
		scalarProp("a_ushort", uint16Type),
		scalarProp("a_int", int32Type),
		scalarProp("a_uint", uint32Type),
		scalarProp("a_float", float32Type),
		scalarProp("a_double", float64Type),
	)
	m := newTestMapper(t, props, "a_char", "a_uchar", "a_short", "a_ushort", "a_int", "a_uint", "a_float", "a_double")
	wantSchema := []model.AttributeDescriptor{
		{Name: "a_char", Type: model.AttributeInt8},
		{Name: "a_uchar", Type: model.AttributeUint8},
		{Name: "a_short", Type: model.AttributeInt16},
		{Name: "a_ushort", Type: model.AttributeUint16},
		{Name: "a_int", Type: model.AttributeInt32},
		{Name: "a_uint", Type: model.AttributeUint32},
		{Name: "a_float", Type: model.AttributeFloat32},
		{Name: "a_double", Type: model.AttributeFloat64},
	}
	if !reflect.DeepEqual(m.schema, wantSchema) {
		t.Fatalf("schema = %v, want %v", m.schema, wantSchema)
	}
	pt := mapPoint(t, m, 0, 0, 0, math.MinInt8, math.MaxUint8, math.MinInt16, math.MaxUint16, math.MinInt32, math.MaxUint32, 0.1, 0.1)
	want := map[string]any{
		"a_char":   int8(math.MinInt8),
		"a_uchar":  uint8(math.MaxUint8),
		"a_short":  int16(math.MinInt16),
		"a_ushort": uint16(math.MaxUint16),
		"a_int":    int32(math.MinInt32),
		"a_uint":   uint32(math.MaxUint32),
		"a_float":  float32(0.1),
		"a_double": 0.1,
	}
	if got := attributeValues(t, m, pt); !reflect.DeepEqual(got, want) {
		t.Errorf("attributes = %v, want %v", got, want)
	}
}

func TestPointMapperClampsIntegerAttributesFromLenientASCII(t *testing.T) {
	props := append(floatProps("x", "y", "z"), scalarProp("u8", uint8Type), scalarProp("i8", int8Type), scalarProp("u32", uint32Type))
	m := newTestMapper(t, props, "u8", "i8", "u32")
	tests := []struct {
		values []float64
		want   map[string]any
	}{
		{[]float64{300, -200, 5e9}, map[string]any{"u8": uint8(255), "i8": int8(-128), "u32": uint32(math.MaxUint32)}},
		{[]float64{-3, 200, -1}, map[string]any{"u8": uint8(0), "i8": int8(127), "u32": uint32(0)}},
		{[]float64{1.6, -1.6, 2.5}, map[string]any{"u8": uint8(2), "i8": int8(-2), "u32": uint32(3)}},
		{[]float64{math.NaN(), math.NaN(), math.Inf(1)}, map[string]any{"u8": uint8(0), "i8": int8(0), "u32": uint32(math.MaxUint32)}},
	}
	for _, tc := range tests {
		pt := mapPoint(t, m, append([]float64{0, 0, 0}, tc.values...)...)
		if got := attributeValues(t, m, pt); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("attributes for %v = %v, want %v", tc.values, got, tc.want)
		}
	}
}

func TestPointMapperAttributeSchemaFollowsTheRequestOrder(t *testing.T) {
	m := newTestMapper(t, floatProps("x", "y", "z", "a", "b", "c"), "c", "missing", "a", "c")
	wantSchema := []model.AttributeDescriptor{{Name: "c", Type: model.AttributeFloat32}, {Name: "a", Type: model.AttributeFloat32}}
	if !reflect.DeepEqual(m.schema, wantSchema) {
		t.Fatalf("schema = %v, want %v", m.schema, wantSchema)
	}
	pt := mapPoint(t, m, 0, 0, 0, 1, 2, 3)
	if got, want := attributeValues(t, m, pt), map[string]any{"a": float32(1), "c": float32(3)}; !reflect.DeepEqual(got, want) {
		t.Errorf("attributes = %v, want %v", got, want)
	}
}

func TestPointMapperAttributeNameMatching(t *testing.T) {
	tests := []struct {
		name       string
		properties []property
		request    string
		want       []model.AttributeDescriptor
		wantValue  any
	}{
		{"CloudCompare prefix", floatProps("scalar_Intensity"), "intensity", []model.AttributeDescriptor{{Name: "intensity", Type: model.AttributeFloat32}}, float32(9)},
		{"CloudCompare full name", floatProps("scalar_Intensity"), "scalar_intensity", []model.AttributeDescriptor{{Name: "scalar_intensity", Type: model.AttributeFloat32}}, float32(9)},
		{"exact name preferred over prefix", []property{scalarProp("scalar_intensity", float32Type), scalarProp("intensity", uint16Type)}, "intensity", []model.AttributeDescriptor{{Name: "intensity", Type: model.AttributeUint16}}, uint16(10)},
		{"normal requested by long name", floatProps("nx"), "normal_x", []model.AttributeDescriptor{{Name: "normal_x", Type: model.AttributeFloat32}}, float32(9)},
		{"normal requested by short name", floatProps("nx"), "nx", []model.AttributeDescriptor{{Name: "nx", Type: model.AttributeFloat32}}, float32(9)},
		{"long normal property requested by short name", floatProps("Normal_Y"), "ny", []model.AttributeDescriptor{{Name: "ny", Type: model.AttributeFloat32}}, float32(9)},
		{"request is canonicalized", floatProps("intensity"), " Intensity ", []model.AttributeDescriptor{{Name: "intensity", Type: model.AttributeFloat32}}, float32(9)},
		{"coordinates are not attributes", floatProps("intensity"), "x", nil, nil},
		{"colors are not attributes", []property{scalarProp("red", uint8Type), scalarProp("green", uint8Type), scalarProp("blue", uint8Type)}, "red", nil, nil},
		{"unknown request", floatProps("intensity"), "nx", nil, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			props := append(floatProps("x", "y", "z"), tc.properties...)
			m := newTestMapper(t, props, tc.request)
			if !reflect.DeepEqual(m.schema, tc.want) {
				t.Fatalf("schema = %v, want %v", m.schema, tc.want)
			}
			values := []float64{0, 0, 0}
			for i := range tc.properties {
				values = append(values, float64(9+i))
			}
			got := attributeValues(t, m, mapPoint(t, m, values...))
			if tc.want == nil {
				if len(got) != 0 {
					t.Errorf("attributes = %v, want none", got)
				}
				return
			}
			if got[tc.want[0].Name] != tc.wantValue {
				t.Errorf("attribute %q = %v, want %v", tc.want[0].Name, got[tc.want[0].Name], tc.wantValue)
			}
		})
	}
}

func TestPointMapperOnDecodedVertices(t *testing.T) {
	vertex := testElement{
		name: "vertex",
		properties: []property{
			scalarProp("x", float64Type), scalarProp("y", float64Type), scalarProp("z", float64Type),
			listProp("weights", uint8Type, float32Type),
			scalarProp("red", uint8Type), scalarProp("green", uint8Type), scalarProp("blue", uint8Type),
			scalarProp("scalar_Classification", uint8Type),
		},
		records: [][]float64{{500000.25, 4500000.5, 120.75, 1, 0.5, 10, 20, 30, 6}},
	}
	for _, f := range allFormats {
		t.Run(formatName(f), func(t *testing.T) {
			d, vertices := decodeVertices(t, buildPLY(t, f, vertex))
			m, err := newPointMapper(d.scalars, model.NewAttributes("classification"))
			if err != nil {
				t.Fatalf("newPointMapper: %v", err)
			}
			pt := mapPoint(t, m, vertices[0]...)
			if pt.Vector != (model.Vector{X: 500000.25, Y: 4500000.5, Z: 120.75}) || [3]uint8{pt.R, pt.G, pt.B} != [3]uint8{10, 20, 30} {
				t.Errorf("point = %+v", pt)
			}
			if got := attributeValues(t, m, pt); !reflect.DeepEqual(got, map[string]any{"classification": uint8(6)}) {
				t.Errorf("attributes = %v", got)
			}
		})
	}
}
