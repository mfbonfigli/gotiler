// Package glb writes tiles of Gaussian splats as binary glTF files, with the
// splats compressed by the KHR_gaussian_splatting_compression_spz_2
// extension, the layout CesiumJS renders.
package glb

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/qmuntal/gltf"

	"github.com/mfbonfigli/gotiler/v3/internal/geom"
	"github.com/mfbonfigli/gotiler/v3/internal/splat"
	"github.com/mfbonfigli/gotiler/v3/internal/splat/spz"
	"github.com/mfbonfigli/gotiler/v3/internal/utils"
)

// The glTF extensions a splat tile uses. A tileset of splat tiles must list
// both as required, or CesiumJS loads the tiles as plain points.
const (
	ExtensionGaussianSplatting = "KHR_gaussian_splatting"
	ExtensionSPZ               = "KHR_gaussian_splatting_compression_spz_2"
)

// splattingExtension is the KHR_gaussian_splatting object of the primitive.
// The colors are in sRGB, like the images that splats are trained on, and the
// compressed payload is buffer view 0.
var splattingExtension = json.RawMessage(`{"kernel":"ellipse","colorSpace":"srgb_rec709_display","extensions":{"` + ExtensionSPZ + `":{"bufferView":0}}}`)

// zUpToYUp is the node matrix, in column-major order, that turns the Z-up tile
// frame into glTF's Y-up axes, like the point cloud tiles do. Viewers turn
// glTF content back to Z-up, so splats are stored in the tile frame as is.
var zUpToYUp = [16]float64{1, 0, 0, 0, 0, 0, -1, 0, 0, 1, 0, 0, 0, 0, 0, 1}

// bodyPool holds the buffers the SPZ payloads are compressed into. Its
// capacity fits 20,000 splats of degree 3, the tile size the SPZ encoder
// pools for, with room to spare for data that compresses poorly.
var bodyPool = utils.NewSlicePool[byte](2 << 20)

// Write writes the splats of one tile to w as a GLB file, with the splats in
// the tile's Z-up frame. Nothing is written when the splats cannot be
// encoded. Write is safe for concurrent use.
func Write(w io.Writer, splats []splat.Splat, version spz.Version) error {
	bodyPtr := bodyPool.Get()
	defer bodyPool.Put(bodyPtr)
	body := bytes.NewBuffer(*bodyPtr)
	err := spz.Encode(body, splats, version)
	// Keep the buffer even if the payload outgrew it, so that it is reused.
	*bodyPtr = body.Bytes()[:0]
	if err != nil {
		return err
	}
	encoder := gltf.NewEncoder(w)
	encoder.AsBinary = true
	return encoder.Encode(document(splats, body.Bytes()))
}

// document returns the glTF document of a tile. Its accessors have no buffer
// views: the SPZ decoder fills them, in the types listed here.
func document(splats []splat.Splat, payload []byte) *gltf.Document {
	doc := gltf.NewDocument()
	doc.Asset.Generator = "gotiler"
	doc.ExtensionsUsed = []string{ExtensionGaussianSplatting, ExtensionSPZ}
	doc.ExtensionsRequired = []string{ExtensionGaussianSplatting, ExtensionSPZ}
	doc.Buffers = []*gltf.Buffer{{ByteLength: len(payload), Data: payload}}
	doc.BufferViews = []*gltf.BufferView{{Buffer: 0, ByteLength: len(payload)}}

	attributes := gltf.PrimitiveAttributes{}
	addAccessor := func(semantic string, accessorType gltf.AccessorType, componentType gltf.ComponentType) *gltf.Accessor {
		accessor := &gltf.Accessor{ComponentType: componentType, Count: len(splats), Type: accessorType}
		attributes[semantic] = len(doc.Accessors)
		doc.Accessors = append(doc.Accessors, accessor)
		return accessor
	}

	var bounds geom.Bounds
	for _, s := range splats {
		bounds.IncludePoint(s.Center)
	}
	position := addAccessor(gltf.POSITION, gltf.AccessorVec3, gltf.ComponentFloat)
	position.Min = []float64{bounds.Min.X, bounds.Min.Y, bounds.Min.Z}
	position.Max = []float64{bounds.Max.X, bounds.Max.Y, bounds.Max.Z}
	// The SPZ extension carries opacity in the alpha of COLOR_0, instead of the
	// OPACITY attribute of uncompressed splats.
	addAccessor(gltf.COLOR_0, gltf.AccessorVec4, gltf.ComponentUbyte).Normalized = true
	addAccessor(ExtensionGaussianSplatting+":SCALE", gltf.AccessorVec3, gltf.ComponentFloat)
	addAccessor(ExtensionGaussianSplatting+":ROTATION", gltf.AccessorVec4, gltf.ComponentFloat)
	for j := range splats[0].SHRest {
		addAccessor(shSemantic(j), gltf.AccessorVec3, gltf.ComponentFloat)
	}

	doc.Meshes = []*gltf.Mesh{{
		Primitives: []*gltf.Primitive{{
			Mode:       gltf.PrimitivePoints,
			Attributes: attributes,
			Extensions: gltf.Extensions{ExtensionGaussianSplatting: splattingExtension},
		}},
	}}
	doc.Nodes = []*gltf.Node{{Mesh: gltf.Index(0), Matrix: zUpToYUp}}
	doc.Scenes[0].Nodes = []int{0}
	return doc
}

// shSemantic returns the attribute name of the higher-degree spherical
// harmonic coefficient at index j of splat.Splat.SHRest.
func shSemantic(j int) string {
	degree, coefficient := 1, j
	switch {
	case j >= 8:
		degree, coefficient = 3, j-8
	case j >= 3:
		degree, coefficient = 2, j-3
	}
	return fmt.Sprintf("%s:SH_DEGREE_%d_COEF_%d", ExtensionGaussianSplatting, degree, coefficient)
}
