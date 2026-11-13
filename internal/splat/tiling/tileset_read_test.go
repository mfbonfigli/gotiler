package tiling

import "math"

// closeToStored reports whether a geometric error read from a tileset file
// matches the computed one: the files keep 5 significant digits, so a
// product of two rounded errors can be off by 1e-4 of its value.
func closeToStored(got, want float64) bool {
	return math.Abs(got-want) <= 1e-4*math.Max(math.Abs(got), math.Abs(want))
}

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
