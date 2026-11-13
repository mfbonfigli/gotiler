package tiling

// The JSON of a splat tileset file, as the tests read it back.

type tilesetJSON struct {
	Asset              assetJSON           `json:"asset"`
	ExtensionsUsed     []string            `json:"extensionsUsed"`
	ExtensionsRequired []string            `json:"extensionsRequired"`
	Extensions         map[string]gltfJSON `json:"extensions"`
	GeometricError     float64             `json:"geometricError"`
	Root               tileJSON            `json:"root"`
}

type assetJSON struct {
	Version string `json:"version"`
}

type gltfJSON struct {
	ExtensionsUsed     []string `json:"extensionsUsed"`
	ExtensionsRequired []string `json:"extensionsRequired"`
}

type tileJSON struct {
	BoundingVolume boundingVolumeJSON `json:"boundingVolume"`
	GeometricError float64            `json:"geometricError"`
	Refine         string             `json:"refine"`
	Transform      *[16]float64       `json:"transform,omitempty"`
	Content        contentJSON        `json:"content"`
	Children       []tileJSON         `json:"children,omitempty"`
}

type boundingVolumeJSON struct {
	Box [12]float64 `json:"box"`
}

type contentJSON struct {
	URI string `json:"uri"`
}
