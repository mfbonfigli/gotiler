package writer

// PropertyMinMax is one 3D Tiles 1.0 tileset "properties" entry: the global
// minimum and maximum values of a per-point property across the dataset.
// Values hold int64, uint64 or float64 so integer ranges keep full precision
// in the JSON output.
type PropertyMinMax struct {
	Minimum any `json:"minimum"`
	Maximum any `json:"maximum"`
}

// TilesetSchema is the 3D Tiles 1.1 metadata schema of a tileset.
type TilesetSchema struct {
	ID      string                        `json:"id"`
	Classes map[string]TilesetSchemaClass `json:"classes"`
}

// TilesetSchemaClass is one class of a 3D Tiles 1.1 metadata schema.
type TilesetSchemaClass struct {
	Properties map[string]TilesetClassProperty `json:"properties"`
}

// TilesetClassProperty is one property definition of a metadata class.
type TilesetClassProperty struct {
	Type          string `json:"type"`
	ComponentType string `json:"componentType,omitempty"`
}

// TilesetMetadata is the 3D Tiles 1.1 metadata entity of a tileset.
type TilesetMetadata struct {
	Class      string         `json:"class"`
	Properties map[string]any `json:"properties"`
}
