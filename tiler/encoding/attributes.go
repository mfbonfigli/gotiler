// Package encoding provides shared helpers for tile encoders (built-in and
// plugin-provided): resolution of a node's attribute summaries into packed
// value columns, output naming, and per-format attribute type mappings for
// PNTS (3D Tiles 1.0) and glTF (3D Tiles 1.1).
package encoding

import (
	"fmt"
	"strings"

	"github.com/mfbonfigli/gotiler/v3/tiler/model"
	"github.com/mfbonfigli/gotiler/v3/tiler/tree"
)

var standardAttributeOutputNames = map[string]string{
	model.AttrIntensity:       "INTENSITY",
	model.AttrClassification:  "CLASSIFICATION",
	model.AttrReturnNumber:    "RETURN_NUMBER",
	model.AttrNumberOfReturns: "NUMBER_OF_RETURNS",
}

// AttributeColumn is one attribute an encoder emits, resolved to its location
// inside each point's packed AttributeValues.
type AttributeColumn struct {
	Summary model.AttributeSummary
	Offset  int // byte offset of the value inside model.Point.Attributes
	Size    int
}

// AttributeColumns resolves the node's attribute summaries into packed-value
// columns. Incomplete attributes and types the encoder cannot represent
// (per the supports predicate) are dropped; offsets are always computed
// against the full layout so kept columns still address the right bytes.
func AttributeColumns(node tree.Node, supports func(model.AttributeType) bool) []AttributeColumn {
	provider, ok := node.(tree.AttributeSummaryProvider)
	if !ok {
		return nil
	}
	source := provider.AttributeSummaries()
	entries, _ := model.AttributeLayout(source)
	var out []AttributeColumn
	for _, e := range entries {
		summary := source[e.SourceIndex]
		if summary.SkipIncomplete || !supports(summary.Type) {
			continue
		}
		out = append(out, AttributeColumn{Summary: summary, Offset: e.Offset, Size: e.Size})
	}
	return out
}

// ColumnBytes returns the little-endian packed bytes of one column value for a
// point, or nil when the point carries no attribute values.
func ColumnBytes(pt model.Point, col AttributeColumn) []byte {
	if len(pt.Attributes) >= col.Offset+col.Size {
		return pt.Attributes[col.Offset : col.Offset+col.Size]
	}
	return nil
}

// AttributeOutputName returns the name under which an attribute appears in
// output tiles: the standard spelling for well-known attributes, the
// uppercased canonical name otherwise.
func AttributeOutputName(canonical string) string {
	canonical = model.CanonicalAttributeName(canonical)
	if name, ok := standardAttributeOutputNames[canonical]; ok {
		return name
	}
	return strings.ToUpper(canonical)
}

// AttributePrimitiveName returns the glTF mesh primitive attribute name for an
// attribute (the output name with the custom-attribute underscore prefix).
func AttributePrimitiveName(canonical string) string {
	return "_" + AttributeOutputName(canonical)
}

// MetadataPropertyID transforms a name into a valid 3D Metadata property or
// schema identifier (matching ^[a-zA-Z_][a-zA-Z0-9_]*$): invalid characters
// become underscores and a leading digit gets an underscore prefix.
func MetadataPropertyID(name string) string {
	var b strings.Builder
	b.Grow(len(name) + 1)
	for i, r := range name {
		valid := r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if !valid {
			b.WriteByte('_')
			continue
		}
		if i == 0 && r >= '0' && r <= '9' {
			b.WriteByte('_')
		}
		b.WriteRune(r)
	}
	if b.Len() == 0 {
		return "_"
	}
	return b.String()
}

// PntsSupportsType reports whether the type can be stored in a PNTS batch
// table binary. 64-bit integers have no PNTS component type.
func PntsSupportsType(t model.AttributeType) bool {
	_, err := PntsComponentType(t)
	return err == nil
}

// PntsComponentType maps an attribute type to its PNTS batch table componentType.
func PntsComponentType(t model.AttributeType) (string, error) {
	switch t {
	case model.AttributeInt8:
		return "BYTE", nil
	case model.AttributeUint8, model.AttributeBool:
		return "UNSIGNED_BYTE", nil
	case model.AttributeInt16:
		return "SHORT", nil
	case model.AttributeUint16:
		return "UNSIGNED_SHORT", nil
	case model.AttributeInt32:
		return "INT", nil
	case model.AttributeUint32:
		return "UNSIGNED_INT", nil
	case model.AttributeFloat32:
		return "FLOAT", nil
	case model.AttributeFloat64:
		return "DOUBLE", nil
	default:
		return "", fmt.Errorf("attribute type %q cannot be represented in PNTS batch table binary", t)
	}
}

// GltfVertexSupportsType reports whether the type can be stored in a glTF
// vertex attribute accessor. glTF 2.0 restricts vertex attributes to 8/16-bit
// integers and float32: UNSIGNED_INT is reserved for indices and there is no
// 32/64-bit integer component type, so such attributes are omitted from GLB
// output. float64 is supported by downcasting to float32 (see GltfEffectiveType).
func GltfVertexSupportsType(t model.AttributeType) bool {
	switch t {
	case model.AttributeInt8, model.AttributeUint8, model.AttributeBool,
		model.AttributeInt16, model.AttributeUint16,
		model.AttributeFloat32, model.AttributeFloat64:
		return true
	default:
		return false
	}
}

// GltfEffectiveType returns the type actually stored in the GLB vertex data
// for an attribute. glTF has no float64 accessor component type, so float64
// attributes are downcast to float32 (lossy: ~7 significant digits) rather
// than being dropped from the output. All other supported types are stored
// as-is.
func GltfEffectiveType(t model.AttributeType) model.AttributeType {
	if t == model.AttributeFloat64 {
		return model.AttributeFloat32
	}
	return t
}

// GltfMetadataComponentType maps an attribute type to its EXT_structural_metadata
// class property type and componentType. Booleans are exposed as UINT8 because
// property attributes (unlike property tables) cannot encode BOOLEAN values.
func GltfMetadataComponentType(t model.AttributeType) (string, string, error) {
	switch t {
	case model.AttributeBool:
		return "SCALAR", "UINT8", nil
	case model.AttributeInt8:
		return "SCALAR", "INT8", nil
	case model.AttributeUint8:
		return "SCALAR", "UINT8", nil
	case model.AttributeInt16:
		return "SCALAR", "INT16", nil
	case model.AttributeUint16:
		return "SCALAR", "UINT16", nil
	case model.AttributeInt32:
		return "SCALAR", "INT32", nil
	case model.AttributeUint32:
		return "SCALAR", "UINT32", nil
	case model.AttributeInt64:
		return "SCALAR", "INT64", nil
	case model.AttributeUint64:
		return "SCALAR", "UINT64", nil
	case model.AttributeFloat32:
		return "SCALAR", "FLOAT32", nil
	case model.AttributeFloat64:
		return "SCALAR", "FLOAT64", nil
	default:
		return "", "", fmt.Errorf("unsupported attribute type %q", t)
	}
}
