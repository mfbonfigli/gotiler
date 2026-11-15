// Package glb writes tiles of textured meshes as binary glTF files.
package glb

import (
	"bytes"
	"cmp"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"slices"

	"github.com/qmuntal/gltf"

	"github.com/mfbonfigli/gotiler/v3/internal/mesh"
	"github.com/mfbonfigli/gotiler/v3/internal/mesh/texture"
	"github.com/mfbonfigli/gotiler/v3/plugins/compression"
)

// The glTF extensions a mesh tile uses.
const (
	// ExtensionQuantization lets positions and texture coordinates be
	// integers.
	ExtensionQuantization = "KHR_mesh_quantization"
	// ExtensionMeshopt compresses the vertices and the triangles.
	ExtensionMeshopt = compression.ExtensionMeshopt
	// ExtensionUnlit shows the texture as it is: the textures of
	// photogrammetry hold the light of the scene already.
	ExtensionUnlit = "KHR_materials_unlit"
)

const (
	// positionStride and texCoordStride are the sizes of a quantized
	// position, three 16 bit integers padded to four, and of a quantized
	// texture coordinate.
	positionStride = 8
	texCoordStride = 4
	// gridLimit is the highest coordinate of a quantized position.
	gridLimit = math.MaxUint16
	// minStepExponent bounds the grid of the positions to steps of
	// 2^minStepExponent units, about 0.06 mm for meters.
	minStepExponent = -14
	// mortonBits is how many bits of each axis order the triangles.
	mortonBits = 10
)

// Options configure Write.
type Options struct {
	// Quality is the quality of the JPEG texture, from 1 to 100.
	Quality int
	// Compress compresses the vertices and the triangles with
	// EXT_meshopt_compression.
	Compress bool
	// StepExponent is the least power of two of the step of the grid that
	// positions are stored on. Tiles written with the same exponent, and
	// small enough to need no larger step, store the positions they share
	// identically.
	StepExponent int
}

// Write writes m, textured by atlas, to w as a GLB file. The texture
// coordinates of m must locate its vertices in atlas, growing downward, and
// its positions are in a Z-up frame.
//
// Positions are stored as 16 bit integers on a grid whose step is a power of
// two and whose points are the same for every tile, so that tiles of a
// similar size store a position they share identically.
func Write(w io.Writer, m *mesh.Indexed, atlas *texture.Image, opts Options) error {
	if len(m.Triangles) == 0 {
		return errors.New("no triangles to write")
	}
	indices, vertices := order(m)
	origin, step := grid(m, vertices, opts.StepExponent)

	positions := make([]byte, len(vertices)*positionStride)
	texCoords := make([]byte, len(vertices)*texCoordStride)
	low, high := [3]float64{gridLimit, gridLimit, gridLimit}, [3]float64{}
	for i, index := range vertices {
		v := m.Vertices[index]
		p := m.Positions[v.Position]
		for a := range 3 {
			q := math.Round((float64(p[a]) - origin[a]) / step)
			q = min(max(q, 0), gridLimit)
			low[a], high[a] = min(low[a], q), max(high[a], q)
			binary.LittleEndian.PutUint16(positions[i*positionStride+2*a:], uint16(q))
		}
		for a := range 2 {
			q := math.Round(float64(v.UV[a]) * math.MaxUint16)
			binary.LittleEndian.PutUint16(texCoords[i*texCoordStride+2*a:], uint16(min(max(q, 0), math.MaxUint16)))
		}
	}

	var image bytes.Buffer
	if err := atlas.EncodeJPEG(&image, opts.Quality); err != nil {
		return err
	}

	doc := gltf.NewDocument()
	doc.Asset.Generator = "gotiler"
	doc.ExtensionsUsed = []string{ExtensionQuantization, ExtensionUnlit}
	doc.ExtensionsRequired = []string{ExtensionQuantization}
	var data []byte
	// add appends a segment to the binary chunk, aligned to four bytes, and
	// returns a buffer view of it.
	add := func(segment []byte) *gltf.BufferView {
		for len(data)%4 != 0 {
			data = append(data, 0)
		}
		view := &gltf.BufferView{Buffer: 0, ByteOffset: len(data), ByteLength: len(segment)}
		data = append(data, segment...)
		doc.BufferViews = append(doc.BufferViews, view)
		return view
	}
	// addVertices appends a stream of one value per vertex, compressed if
	// asked: its buffer view then refers to a second buffer that has no
	// data, which only viewers without the extension would need. The
	// fallback offsets stay aligned: the streams are multiples of four
	// bytes long.
	fallback := 0
	addVertices := func(stream []byte, stride int) {
		if !opts.Compress {
			view := add(stream)
			view.ByteStride, view.Target = stride, gltf.TargetArrayBuffer
			return
		}
		compressed := add(compression.EncodeMeshoptAttributes(nil, stream, len(vertices), stride))
		*compressed = gltf.BufferView{
			Buffer:     1,
			ByteOffset: fallback,
			ByteLength: len(stream),
			ByteStride: stride,
			Target:     gltf.TargetArrayBuffer,
			Extensions: gltf.Extensions{ExtensionMeshopt: map[string]any{
				"buffer":     0,
				"byteOffset": compressed.ByteOffset,
				"byteLength": compressed.ByteLength,
				"byteStride": stride,
				"mode":       "ATTRIBUTES",
				"count":      len(vertices),
			}},
		}
		fallback += len(stream)
	}
	addVertices(positions, positionStride)
	addVertices(texCoords, texCoordStride)
	// Triangles whose corners land on the same point of the grid cover
	// nothing: leave them out.
	kept := indices[:0]
	for i := 0; i < len(indices); i += 3 {
		a, b, c := position(positions, indices[i]), position(positions, indices[i+1]), position(positions, indices[i+2])
		if a != b && b != c && a != c {
			kept = append(kept, indices[i], indices[i+1], indices[i+2])
		}
	}
	if indices = kept; len(indices) == 0 {
		return errors.New("no triangles to write")
	}
	indexType, indexSize := gltf.ComponentUshort, 2
	if len(vertices) > math.MaxUint16 {
		indexType, indexSize = gltf.ComponentUint, 4
	}
	if opts.Compress {
		compressed := add(compression.EncodeMeshoptTriangles(nil, indices))
		*compressed = gltf.BufferView{
			Buffer:     1,
			ByteOffset: fallback,
			ByteLength: indexSize * len(indices),
			Target:     gltf.TargetElementArrayBuffer,
			Extensions: gltf.Extensions{ExtensionMeshopt: map[string]any{
				"buffer":     0,
				"byteOffset": compressed.ByteOffset,
				"byteLength": compressed.ByteLength,
				"byteStride": indexSize,
				"mode":       "TRIANGLES",
				"count":      len(indices),
			}},
		}
		fallback += indexSize * len(indices)
	} else {
		indexData := make([]byte, indexSize*len(indices))
		for i, index := range indices {
			if indexSize == 2 {
				binary.LittleEndian.PutUint16(indexData[2*i:], uint16(index))
			} else {
				binary.LittleEndian.PutUint32(indexData[4*i:], index)
			}
		}
		add(indexData).Target = gltf.TargetElementArrayBuffer
	}
	add(image.Bytes())

	doc.Buffers = []*gltf.Buffer{{ByteLength: len(data), Data: data}}
	if opts.Compress {
		doc.ExtensionsUsed = append(doc.ExtensionsUsed, ExtensionMeshopt)
		doc.ExtensionsRequired = append(doc.ExtensionsRequired, ExtensionMeshopt)
		doc.Buffers = append(doc.Buffers, &gltf.Buffer{
			ByteLength: fallback,
			Extensions: gltf.Extensions{ExtensionMeshopt: map[string]any{"fallback": true}},
		})
	}
	doc.Accessors = []*gltf.Accessor{
		{BufferView: gltf.Index(0), ComponentType: gltf.ComponentUshort, Count: len(vertices), Type: gltf.AccessorVec3, Min: low[:], Max: high[:]},
		{BufferView: gltf.Index(1), ComponentType: gltf.ComponentUshort, Count: len(vertices), Type: gltf.AccessorVec2, Normalized: true},
		{BufferView: gltf.Index(2), ComponentType: indexType, Count: len(indices), Type: gltf.AccessorScalar},
	}
	doc.Images = []*gltf.Image{{BufferView: gltf.Index(3), MimeType: "image/jpeg"}}
	doc.Samplers = []*gltf.Sampler{{
		MagFilter: gltf.MagLinear,
		MinFilter: gltf.MinLinearMipMapLinear,
		WrapS:     gltf.WrapClampToEdge,
		WrapT:     gltf.WrapClampToEdge,
	}}
	doc.Textures = []*gltf.Texture{{Sampler: gltf.Index(0), Source: gltf.Index(0)}}
	doc.Materials = []*gltf.Material{{
		PBRMetallicRoughness: &gltf.PBRMetallicRoughness{
			BaseColorTexture: &gltf.TextureInfo{Index: 0},
			MetallicFactor:   gltf.Float(0),
			RoughnessFactor:  gltf.Float(1),
		},
		Extensions: gltf.Extensions{ExtensionUnlit: map[string]any{}},
	}}
	doc.Meshes = []*gltf.Mesh{{Primitives: []*gltf.Primitive{{
		Attributes: gltf.PrimitiveAttributes{gltf.POSITION: 0, gltf.TEXCOORD_0: 1},
		Indices:    gltf.Index(2),
		Material:   gltf.Index(0),
	}}}}
	// The node places the grid of the positions and turns the Z-up frame of
	// the tile into the Y-up axes of glTF, which viewers turn back.
	doc.Nodes = []*gltf.Node{{Mesh: gltf.Index(0), Matrix: [16]float64{
		step, 0, 0, 0,
		0, 0, -step, 0,
		0, step, 0, 0,
		origin[0], origin[2], -origin[1], 1,
	}}}
	doc.Scenes[0].Nodes = []int{0}
	encoder := gltf.NewEncoder(w)
	encoder.AsBinary = true
	return encoder.Encode(doc)
}

// position returns the quantized position of the vertex at index.
func position(positions []byte, index uint32) [3]uint16 {
	p := positions[int(index)*positionStride:]
	return [3]uint16{binary.LittleEndian.Uint16(p), binary.LittleEndian.Uint16(p[2:]), binary.LittleEndian.Uint16(p[4:])}
}

// order returns the corners of the triangles of m as indices into a list of
// the vertices they use, which it returns too, as indices into the vertices
// of m.
//
// Triangles are ordered along a curve that fills their bounds, and vertices
// by their first triangle, so that consecutive ones are close together,
// which the compression relies on.
func order(m *mesh.Indexed) (indices []uint32, vertices []int32) {
	low, high := bounds(m, nil)
	type entry struct {
		code     uint32
		triangle int32
	}
	entries := make([]entry, len(m.Triangles))
	for i, t := range m.Triangles {
		var cell [3]uint32
		for a := range 3 {
			var center float64
			for _, v := range t {
				center += float64(m.Positions[m.Vertices[v].Position][a]) / 3
			}
			if size := high[a] - low[a]; size > 0 {
				cell[a] = uint32(min((center-low[a])/size*(1<<mortonBits), 1<<mortonBits-1))
			}
		}
		entries[i] = entry{code: interleave(cell[0]) | interleave(cell[1])<<1 | interleave(cell[2])<<2, triangle: int32(i)}
	}
	slices.SortFunc(entries, func(a, b entry) int {
		if c := cmp.Compare(a.code, b.code); c != 0 {
			return c
		}
		return cmp.Compare(a.triangle, b.triangle)
	})
	renumbered := make([]int32, len(m.Vertices))
	for i := range renumbered {
		renumbered[i] = -1
	}
	indices = make([]uint32, 0, 3*len(m.Triangles))
	for _, e := range entries {
		for _, v := range m.Triangles[e.triangle] {
			if renumbered[v] < 0 {
				renumbered[v] = int32(len(vertices))
				vertices = append(vertices, v)
			}
			indices = append(indices, uint32(renumbered[v]))
		}
	}
	return indices, vertices
}

// interleave spreads the low 10 bits of v, two zero bits between each.
func interleave(v uint32) uint32 {
	v &= 0x3ff
	v = (v | v<<16) & 0x30000ff
	v = (v | v<<8) & 0x300f00f
	v = (v | v<<4) & 0x30c30c3
	v = (v | v<<2) & 0x9249249
	return v
}

// bounds returns the corners of the box around the positions of the given
// vertices of m, or of all its triangles when vertices is nil.
func bounds(m *mesh.Indexed, vertices []int32) (low, high [3]float64) {
	low = [3]float64{math.Inf(1), math.Inf(1), math.Inf(1)}
	high = [3]float64{math.Inf(-1), math.Inf(-1), math.Inf(-1)}
	include := func(v int32) {
		p := m.Positions[m.Vertices[v].Position]
		for a := range 3 {
			low[a], high[a] = min(low[a], float64(p[a])), max(high[a], float64(p[a]))
		}
	}
	if vertices != nil {
		for _, v := range vertices {
			include(v)
		}
		return low, high
	}
	for _, t := range m.Triangles {
		include(t[0])
		include(t[1])
		include(t[2])
	}
	return low, high
}

// grid returns the grid that the positions of the given vertices of m are
// stored on: its origin, a point of the grid at or below their lowest
// corner, and its step, the smallest power of two, from 2^exponent up, that
// lets 16 bits span them.
func grid(m *mesh.Indexed, vertices []int32, exponent int) (origin [3]float64, step float64) {
	low, high := bounds(m, vertices)
	exponent = max(exponent, minStepExponent)
	for {
		step = math.Ldexp(1, exponent)
		fits := true
		for a := range 3 {
			origin[a] = math.Floor(low[a]/step) * step
			if math.Round((high[a]-origin[a])/step) > gridLimit {
				fits = false
			}
		}
		if fits {
			return origin, step
		}
		exponent++
	}
}

// StepExponent returns the least power of two of a grid step that lets 16
// bits span size along each axis.
func StepExponent(size float64) int {
	exponent := minStepExponent
	for size/math.Ldexp(1, exponent) > gridLimit-1 {
		exponent++
	}
	return exponent
}
