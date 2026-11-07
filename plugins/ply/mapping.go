package ply

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/mfbonfigli/gotiler/v3/tiler/geom"
	"github.com/mfbonfigli/gotiler/v3/tiler/model"
)

// scalarFieldPrefix is prepended by CloudCompare to scalar field names, as in
// "scalar_Intensity".
const scalarFieldPrefix = "scalar_"

var colorPropertySets = [][3]string{
	{"red", "green", "blue"},
	{"diffuse_red", "diffuse_green", "diffuse_blue"},
}

var normalAliases = map[string]string{
	"nx":       "normal_x",
	"ny":       "normal_y",
	"nz":       "normal_z",
	"normal_x": "nx",
	"normal_y": "ny",
	"normal_z": "nz",
}

var attributeTypes = map[scalarType]model.AttributeType{
	int8Type:    model.AttributeInt8,
	uint8Type:   model.AttributeUint8,
	int16Type:   model.AttributeInt16,
	uint16Type:  model.AttributeUint16,
	int32Type:   model.AttributeInt32,
	uint32Type:  model.AttributeUint32,
	float32Type: model.AttributeFloat32,
	float64Type: model.AttributeFloat64,
}

type colorChannel struct {
	valueIndex int
	valueType  scalarType
}

type attributeTarget struct {
	valueIndex    int
	attributeType model.AttributeType
	offset        int
}

// pointMapper converts the scalar values of a vertex, as returned by
// vertexDecoder.next, into a point.
type pointMapper struct {
	x, y, z  int
	hasColor bool
	color    [3]colorChannel
	// schema describes the packed attributes of every point, in the order
	// they were requested.
	schema         []model.AttributeDescriptor
	attributes     []attributeTarget
	attributesSize int
}

// newPointMapper resolves the coordinate, color and requested attribute
// properties among the scalar vertex properties.
func newPointMapper(properties []property, requested model.Attributes) (*pointMapper, error) {
	names := make([]string, len(properties))
	for i, p := range properties {
		names[i] = model.CanonicalAttributeName(p.name)
	}
	if err := rejectGaussianSplats(names); err != nil {
		return nil, err
	}

	m := &pointMapper{}
	reserved := make([]bool, len(properties))
	var missing []string
	for _, coordinate := range []struct {
		name  string
		index *int
	}{{"x", &m.x}, {"y", &m.y}, {"z", &m.z}} {
		*coordinate.index = slices.Index(names, coordinate.name)
		if *coordinate.index < 0 {
			missing = append(missing, coordinate.name)
			continue
		}
		reserved[*coordinate.index] = true
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("the PLY vertex element is missing coordinates: %s", strings.Join(missing, ", "))
	}

	for _, set := range colorPropertySets {
		indices := [3]int{slices.Index(names, set[0]), slices.Index(names, set[1]), slices.Index(names, set[2])}
		if slices.Contains(indices[:], -1) {
			continue
		}
		m.hasColor = true
		for channel, index := range indices {
			m.color[channel] = colorChannel{valueIndex: index, valueType: properties[index].valueType}
			reserved[index] = true
		}
		break
	}

	if err := m.resolveAttributes(properties, names, reserved, requested); err != nil {
		return nil, err
	}
	return m, nil
}

func rejectGaussianSplats(names []string) error {
	if slices.Contains(names, "packed_position") {
		return errors.New("the file is a compressed Gaussian splat PLY: tiling Gaussian splats is not supported yet")
	}
	for _, name := range []string{"f_dc_0", "opacity", "scale_0", "rot_0"} {
		if !slices.Contains(names, name) {
			return nil
		}
	}
	return errors.New("the file is a Gaussian splat PLY: tiling Gaussian splats is not supported yet")
}

// resolveAttributes maps each requested attribute to a property, preferring
// an exact name match over an alias. Attributes are emitted under the
// requested name, which is the name the tiler looks them up by.
func (m *pointMapper) resolveAttributes(properties []property, names []string, reserved []bool, requested model.Attributes) error {
	for _, request := range requested {
		request = model.CanonicalAttributeName(request)
		if request == "" || slices.ContainsFunc(m.schema, func(d model.AttributeDescriptor) bool { return d.Name == request }) {
			continue
		}
		index := findProperty(names, reserved, func(name string) bool { return name == request })
		if index < 0 {
			index = findProperty(names, reserved, func(name string) bool { return isAliasOf(name, request) })
		}
		if index < 0 {
			continue
		}
		m.schema = append(m.schema, model.AttributeDescriptor{Name: request, Type: attributeTypes[properties[index].valueType]})
		m.attributes = append(m.attributes, attributeTarget{valueIndex: index})
	}
	layout, size, err := model.AttributeSchemaLayout(m.schema)
	if err != nil {
		return err
	}
	for i, entry := range layout {
		m.attributes[i].attributeType = entry.Type
		m.attributes[i].offset = entry.Offset
	}
	m.attributesSize = size
	return nil
}

func findProperty(names []string, reserved []bool, match func(string) bool) int {
	for i, name := range names {
		if !reserved[i] && match(name) {
			return i
		}
	}
	return -1
}

func isAliasOf(propertyName, request string) bool {
	trimmed, hasPrefix := strings.CutPrefix(propertyName, scalarFieldPrefix)
	return (hasPrefix && trimmed == request) || normalAliases[propertyName] == request
}

// point converts the scalar values of one vertex. It returns false for
// vertices whose coordinates are not finite.
func (m *pointMapper) point(values []float64, arena *model.AttributeValuesArena) (geom.Point64, bool) {
	x, y, z := values[m.x], values[m.y], values[m.z]
	if !isFinite(x) || !isFinite(y) || !isFinite(z) {
		return geom.Point64{}, false
	}
	pt := geom.Point64{Vector: model.Vector{X: x, Y: y, Z: z}}
	if m.hasColor {
		pt.R = m.color[0].toUint8(values)
		pt.G = m.color[1].toUint8(values)
		pt.B = m.color[2].toUint8(values)
	}
	if m.attributesSize > 0 {
		blob := arena.Alloc(m.attributesSize)
		for _, a := range m.attributes {
			putAttribute(blob[a.offset:], a.attributeType, values[a.valueIndex])
		}
		pt.Attributes = blob
	}
	return pt, true
}

func (c colorChannel) toUint8(values []float64) uint8 {
	v := values[c.valueIndex]
	switch c.valueType {
	case uint16Type:
		return uint8(uint16(roundClamped(v, 0, math.MaxUint16)) >> 8)
	case float32Type, float64Type:
		return uint8(math.Round(clamp(v, 0, 1) * math.MaxUint8))
	default:
		return uint8(roundClamped(v, 0, math.MaxUint8))
	}
}

func putAttribute(dst []byte, t model.AttributeType, v float64) {
	switch t {
	case model.AttributeInt8:
		dst[0] = byte(int8(roundClamped(v, math.MinInt8, math.MaxInt8)))
	case model.AttributeUint8:
		dst[0] = uint8(roundClamped(v, 0, math.MaxUint8))
	case model.AttributeInt16:
		binary.LittleEndian.PutUint16(dst, uint16(int16(roundClamped(v, math.MinInt16, math.MaxInt16))))
	case model.AttributeUint16:
		binary.LittleEndian.PutUint16(dst, uint16(roundClamped(v, 0, math.MaxUint16)))
	case model.AttributeInt32:
		binary.LittleEndian.PutUint32(dst, uint32(int32(roundClamped(v, math.MinInt32, math.MaxInt32))))
	case model.AttributeUint32:
		binary.LittleEndian.PutUint32(dst, uint32(roundClamped(v, 0, math.MaxUint32)))
	case model.AttributeFloat32:
		binary.LittleEndian.PutUint32(dst, math.Float32bits(float32(v)))
	case model.AttributeFloat64:
		binary.LittleEndian.PutUint64(dst, math.Float64bits(v))
	}
}

// clamp limits v to [lo, hi]. NaN becomes 0, which every caller's range
// contains.
func clamp(v, lo, hi float64) float64 {
	if math.IsNaN(v) {
		return 0
	}
	return max(lo, min(v, hi))
}

func roundClamped(v, lo, hi float64) float64 {
	return math.Round(clamp(v, lo, hi))
}

func isFinite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}
