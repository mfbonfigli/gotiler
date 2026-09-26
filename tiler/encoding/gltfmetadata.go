package encoding

import (
	"bytes"
	"encoding/json"
)

// orderedJSONMap marshals as a JSON object with keys in insertion order.
type orderedJSONMap[V any] struct {
	keys []string
	data map[string]V
}

func newOrderedJSONMap[V any]() orderedJSONMap[V] {
	return orderedJSONMap[V]{data: make(map[string]V)}
}

func (m *orderedJSONMap[V]) add(key string, val V) {
	m.keys = append(m.keys, key)
	m.data[key] = val
}

func (m orderedJSONMap[V]) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range m.keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		kb, _ := json.Marshal(k)
		vb, err := json.Marshal(m.data[k])
		if err != nil {
			return nil, err
		}
		buf.Write(kb)
		buf.WriteByte(':')
		buf.Write(vb)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// JSON structs for EXT_structural_metadata.
type extMetaPropDef struct {
	Description   string `json:"description,omitempty"`
	Type          string `json:"type"`
	ComponentType string `json:"componentType,omitempty"`
	Required      bool   `json:"required"`
}

type extMetaClass struct {
	Name        string                         `json:"name"`
	Description string                         `json:"description"`
	Properties  orderedJSONMap[extMetaPropDef] `json:"properties"`
}

type extMetaSchema struct {
	ID          string                  `json:"id"`
	Name        string                  `json:"name"`
	Description string                  `json:"description"`
	Version     string                  `json:"version"`
	Classes     map[string]extMetaClass `json:"classes"`
}

type extMetaPropAttrEntry struct {
	Attribute string `json:"attribute"`
}

type extMetaPropAttr struct {
	Class      string                               `json:"class"`
	Properties orderedJSONMap[extMetaPropAttrEntry] `json:"properties"`
}

type extMetadata struct {
	Schema             extMetaSchema     `json:"schema"`
	PropertyAttributes []extMetaPropAttr `json:"propertyAttributes"`
}

// BuildGltfMetadataJSON returns the serialized EXT_structural_metadata
// extension JSON declaring the given attribute columns as vertex property
// attributes of a single "point" class, or the empty string when there are no
// columns. Property component types are declared as stored in the vertex data
// (see GltfEffectiveType), not as the source type.
func BuildGltfMetadataJSON(columns []AttributeColumn) (string, error) {
	props := newOrderedJSONMap[extMetaPropDef]()
	propAttrs := newOrderedJSONMap[extMetaPropAttrEntry]()

	for _, col := range columns {
		metaType, componentType, err := GltfMetadataComponentType(GltfEffectiveType(col.Summary.Type))
		if err != nil {
			return "", err
		}
		name := AttributeOutputName(col.Summary.Name)
		props.add(name, extMetaPropDef{
			Type:          metaType,
			ComponentType: componentType,
			Required:      true,
		})
		propAttrs.add(name, extMetaPropAttrEntry{Attribute: AttributePrimitiveName(col.Summary.Name)})
	}

	if len(props.keys) == 0 {
		return "", nil
	}

	meta := extMetadata{
		Schema: extMetaSchema{
			ID:          "pts_schema",
			Name:        "pts_schema",
			Description: "point cloud point attribute schema",
			Version:     "1.0.0",
			Classes: map[string]extMetaClass{
				"point": {
					Name:        "point",
					Description: "Properties of point cloud points",
					Properties:  props,
				},
			},
		},
		PropertyAttributes: []extMetaPropAttr{
			{Class: "point", Properties: propAttrs},
		},
	}

	b, err := json.Marshal(meta)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
