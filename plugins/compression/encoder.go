package compression

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"

	"github.com/mfbonfigli/gotiler/v3/tiler/encoding"
	"github.com/mfbonfigli/gotiler/v3/tiler/model"
	"github.com/mfbonfigli/gotiler/v3/tiler/plugin"
	"github.com/mfbonfigli/gotiler/v3/tiler/tree"
	"github.com/mfbonfigli/gotiler/v3/version"
	"github.com/qmuntal/gltf"
)

const (
	EncoderCompressedGLB = "glb-compressed"
	// EncoderCompressedGLBKHR selects the compressed GLB encoder in KHR mode,
	// see WithKHRMeshopt.
	EncoderCompressedGLBKHR = "glb-compressed-khr"
	filename                = "d.glb"
)

// glTF extensions for the meshopt compressed buffer views: the EXT variant
// only allows the version 0 attribute codec, the KHR variant, its Khronos
// successor, adds the version 1 codec.
const (
	extMeshoptCompression = "EXT_meshopt_compression"
	khrMeshoptCompression = "KHR_meshopt_compression"
)

// Default pool capacities.
const defaultCompressedBufferCap = 200000

// compressedStream holds the metadata for one optional attribute stream
// (e.g. _INTENSITY or _CLASSIFICATION) in a compressed GLB tile.
type compressedStream struct {
	primitiveName string // glTF primitive attribute name, e.g. "_INTENSITY"
	stride        int    // bytes per point in the raw / fallback buffer
	compOffset    int    // byte offset of the compressed data in compressedBuf
	compLen       int    // compressed byte length
	componentType gltf.ComponentType
	accessorType  gltf.AccessorType
}

// CompressedGltfEncoder writes GLB point cloud files applying both
// KHR_mesh_quantization (POSITION accessor) and EXT_meshopt_compression, or
// KHR_meshopt_compression with WithKHRMeshopt (all buffer views). Implements
// GeometryEncoder.
type CompressedGltfEncoder struct {
	filename  string
	coordPool *SlicePool[[3]float32]
	colorPool *SlicePool[[3]uint8]
	// rawBufPool holds a single reusable byte slice for building the uncompressed
	// attribute buffers before meshopt encoding. Sized for the largest buffer
	// (position: n*8 bytes). All attribute builds share it sequentially.
	rawBufPool *SlicePool[byte]
	// attrColPool holds the stride-4 packed column buffers built for optional
	// attributes; compBufPool holds the compressed output buffer. Both are
	// pooled for the same reason as the other pools: Write runs once per tile
	// and per-tile allocations of this size are measurable GC pressure.
	attrColPool *SlicePool[byte]
	compBufPool *SlicePool[byte]
	// meshoptExtension is the glTF extension of the compressed buffer views,
	// which also determines the attribute codec version.
	meshoptExtension string
}

// Option configures a CompressedGltfEncoder.
type Option func(*CompressedGltfEncoder)

// WithKHRMeshopt makes the encoder write KHR_meshopt_compression, the Khronos
// successor of EXT_meshopt_compression, using its version 1 attribute codec:
// tiles are smaller, but fewer loaders support the extension (CesiumJS added
// it in version 1.143).
func WithKHRMeshopt() Option {
	return func(e *CompressedGltfEncoder) {
		e.meshoptExtension = khrMeshoptCompression
	}
}

func init() {
	plugin.RegisterGeometryEncoder(EncoderCompressedGLB, func(attrs model.Attributes) plugin.GeometryEncoder {
		return New(attrs)
	})
	plugin.RegisterGeometryEncoder(EncoderCompressedGLBKHR, func(attrs model.Attributes) plugin.GeometryEncoder {
		return New(attrs, WithKHRMeshopt())
	})
}

func New(attrs model.Attributes, opts ...Option) *CompressedGltfEncoder {
	e := &CompressedGltfEncoder{
		filename:   filename,
		coordPool:  NewSlicePool[[3]float32](defaultCompressedBufferCap),
		colorPool:  NewSlicePool[[3]uint8](defaultCompressedBufferCap),
		rawBufPool: NewSlicePool[byte](defaultCompressedBufferCap * 8),
		// covers the compressed-buffer worst-case bound for pos+color+2 attrs
		attrColPool:      NewSlicePool[byte](defaultCompressedBufferCap * 4),
		compBufPool:      NewSlicePool[byte](defaultCompressedBufferCap * 24),
		meshoptExtension: extMeshoptCompression,
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

func NewCompressedGltfEncoder(attrs model.Attributes, opts ...Option) *CompressedGltfEncoder {
	return New(attrs, opts...)
}

// encodeStream appends the meshopt ATTRIBUTES encoding of a stream, using the
// codec version of the configured extension.
func (e *CompressedGltfEncoder) encodeStream(dst []byte, data []byte, count, stride int) []byte {
	if e.meshoptExtension == khrMeshoptCompression {
		return encodeMeshoptAttributesV1Into(dst, data, count, stride)
	}
	return encodeMeshoptAttributesInto(dst, data, count, stride)
}

func (e *CompressedGltfEncoder) TilesetVersion() version.TilesetVersion {
	return version.TilesetVersion_1_1
}

func (e *CompressedGltfEncoder) ContentFilename() string {
	return e.filename
}

func (e *CompressedGltfEncoder) Write(node tree.Node, wp plugin.WriterProvider, prefix string) error {
	pts, err := node.Points()
	if err != nil {
		return err
	}
	defer pts.Close()

	n := pts.Len()
	columns := encoding.AttributeColumns(node, encoding.GltfVertexSupportsType)
	extJsonStr, err := encoding.BuildGltfMetadataJSON(columns)
	if err != nil {
		return err
	}

	coordsPtr := e.coordPool.GetWithMinCapacity(n)
	coords := *coordsPtr
	defer e.coordPool.Put(coordsPtr)

	colorsPtr := e.colorPool.GetWithMinCapacity(n)
	colors := *colorsPtr
	defer e.colorPool.Put(colorsPtr)

	// One stride-4 packed column per attribute, filled by copying the packed
	// point bytes; the buffers are drawn zeroed from the pool so values
	// narrower than 4 bytes keep their zero padding. These double as the raw
	// meshopt input streams.
	columnData := make([][]byte, len(columns))
	for i := range columns {
		colPtr := e.attrColPool.GetCleared(n * 4)
		defer e.attrColPool.Put(colPtr)
		columnData[i] = *colPtr
	}

	for i := 0; i < n; i++ {
		pt, err := pts.Next()
		if err != nil {
			return err
		}
		coords[i][0] = pt.X
		coords[i][1] = pt.Y
		coords[i][2] = pt.Z

		// LAS colors are typically in the sRGB space, however GLTF specs require
		// COLOR_0 for meshes to be in the linear RGB space, hence we need to convert.
		colors[i][0] = srgbToLinear[pt.R]
		colors[i][1] = srgbToLinear[pt.G]
		colors[i][2] = srgbToLinear[pt.B]
		for j, col := range columns {
			b := encoding.ColumnBytes(pt, col)
			if b == nil {
				continue
			}
			if col.Summary.Type == model.AttributeFloat64 {
				// glTF has no float64 accessor component type, so float64
				// attributes are downcast to float32 here (lossy: ~7
				// significant digits) rather than dropped from the output.
				v := math.Float64frombits(binary.LittleEndian.Uint64(b))
				binary.LittleEndian.PutUint32(columnData[j][i*4:], math.Float32bits(float32(v)))
				continue
			}
			copy(columnData[j][i*4:], b)
		}
	}

	qPos := quantizePositions(coords[:n])

	// One pooled raw buffer reused for all attribute builds (largest = n*8 for positions).
	rawBufPtr := e.rawBufPool.GetWithMinCapacity(n * 8)
	rawBuf := *rawBufPtr
	defer e.rawBufPool.Put(rawBufPtr)

	// Reserve the compressed output buffer from the pool. Worst-case bound:
	//   pos:   n*8  + n/8  + 33
	//   col:   n*4  + n/16 + 33
	//   per optional attr: n*4 + n/16 + 33
	// The compressed streams are always fully overwritten, so no clearing is needed.
	compCap := n*8 + n/8 + 33 + n*4 + n/16 + 33 + len(columns)*(n*4+n/16+33)
	compBufPtr := e.compBufPool.GetWithMinCapacity(compCap)
	defer e.compBufPool.Put(compBufPtr)
	compressedBuf := (*compBufPtr)[:0]

	// Encode position (always present)
	buildPositionRaw(rawBuf[:n*8], qPos, n)
	off0 := len(compressedBuf)
	compressedBuf = e.encodeStream(compressedBuf, rawBuf[:n*8], n, 8)
	cLen0 := len(compressedBuf) - off0

	// Encode color (always present)
	buildColorRaw(rawBuf[:n*4], colors[:n], n)
	off1 := len(compressedBuf)
	compressedBuf = e.encodeStream(compressedBuf, rawBuf[:n*4], n, 4)
	cLen1 := len(compressedBuf) - off1

	// Encode optional attribute streams.
	var optStreams []compressedStream

	for i, col := range columns {
		componentType, err := compressedAttributeComponentType(col.Summary.Type)
		if err != nil {
			return err
		}
		off := len(compressedBuf)
		compressedBuf = e.encodeStream(compressedBuf, columnData[i], n, 4)
		optStreams = append(optStreams, compressedStream{
			primitiveName: encoding.AttributePrimitiveName(col.Summary.Name),
			stride:        4,
			compOffset:    off,
			compLen:       len(compressedBuf) - off,
			componentType: componentType,
			accessorType:  gltf.AccessorScalar,
		})
	}

	w, err := wp(prefix + e.filename)
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			_ = w.Close()
		}
	}()

	if err := buildAndSaveCompressedGltf(
		w,
		n, qPos, compressedBuf,
		off0, cLen0, off1, cLen1,
		optStreams,
		extJsonStr,
		e.meshoptExtension,
	); err != nil {
		return err
	}
	closed = true
	return w.Close()
}

// buildPositionRaw writes stride=8 bytes per point: [uint16 x, uint16 y, uint16 z, uint16 pad]
func buildPositionRaw(buf []byte, q QuantizedPositions, n int) {
	for i := 0; i < n; i++ {
		off := i * 8
		binary.LittleEndian.PutUint16(buf[off:], q.Data[i][0])
		binary.LittleEndian.PutUint16(buf[off+2:], q.Data[i][1])
		binary.LittleEndian.PutUint16(buf[off+4:], q.Data[i][2])
		// bytes 6-7 stay zero (padding)
		buf[off+6] = 0
		buf[off+7] = 0
	}
}

// buildColorRaw writes stride=4 bytes per point: [uint8 r, uint8 g, uint8 b, uint8 pad]
func buildColorRaw(buf []byte, colors [][3]uint8, n int) {
	for i := 0; i < n; i++ {
		off := i * 4
		buf[off] = colors[i][0]
		buf[off+1] = colors[i][1]
		buf[off+2] = colors[i][2]
		buf[off+3] = 0
	}
}

// compressedAttributeComponentType maps an attribute type to the glTF accessor
// componentType used for its vertex data stream.
func compressedAttributeComponentType(t model.AttributeType) (gltf.ComponentType, error) {
	switch encoding.GltfEffectiveType(t) {
	case model.AttributeInt8:
		return gltf.ComponentByte, nil
	case model.AttributeUint8, model.AttributeBool:
		return gltf.ComponentUbyte, nil
	case model.AttributeInt16:
		return gltf.ComponentShort, nil
	case model.AttributeUint16:
		return gltf.ComponentUshort, nil
	case model.AttributeFloat32:
		// Covers float64 source attributes too: they are downcast to float32
		// when the column data is filled (glTF has no float64 component type).
		return gltf.ComponentFloat, nil
	default:
		return 0, fmt.Errorf("attribute type %q is not supported by compressed GLB vertex accessors", t)
	}
}

// buildAndSaveCompressedGltf constructs the full glTF document with compression
// extensions and writes it as GLB.
//
// compressedBuf holds all attribute streams concatenated.
// off0/cLen0 = position stream, off1/cLen1 = color stream.
// optStreams holds any optional attribute streams (intensity, classification, …).
// extJsonStr is the pre-built EXT_structural_metadata JSON (empty string = extension omitted).
// meshoptExt is the meshopt extension name, EXT_ or KHR_meshopt_compression.
func buildAndSaveCompressedGltf(
	w io.Writer,
	n int,
	qPos QuantizedPositions,
	compressedBuf []byte,
	off0, cLen0 int,
	off1, cLen1 int,
	optStreams []compressedStream,
	extJsonStr string,
	meshoptExt string,
) error {
	doc := gltf.NewDocument()
	doc.Asset = gltf.Asset{
		Generator: "gotiler",
		Version:   "2.0",
	}

	// Fallback buffer (Buffer 1) layout: pos stride 8, color stride 4, each optional stride 4.
	fallbackSize := n*8 + n*4 + len(optStreams)*n*4

	doc.Buffers = []*gltf.Buffer{
		// Buffer 0: compressed data → GLB binary chunk
		{
			ByteLength: len(compressedBuf),
			Data:       compressedBuf,
		},
		// Buffer 1: fallback placeholder required by the meshopt extension spec
		{
			ByteLength: fallbackSize,
			Extensions: gltf.Extensions{
				meshoptExt: map[string]interface{}{
					"fallback": true,
				},
			},
		},
	}

	// Buffer views: BV0=position, BV1=color, BV2..=optional streams
	bvs := []*gltf.BufferView{
		{
			Buffer:     1,
			ByteOffset: 0,
			ByteLength: n * 8,
			ByteStride: 8,
			Target:     gltf.TargetArrayBuffer,
			Extensions: gltf.Extensions{
				meshoptExt: map[string]interface{}{
					"buffer":     uint32(0),
					"byteOffset": off0,
					"byteLength": cLen0,
					"byteStride": uint32(8),
					"mode":       "ATTRIBUTES",
					"count":      n,
				},
			},
		},
		{
			Buffer:     1,
			ByteOffset: n * 8,
			ByteLength: n * 4,
			ByteStride: 4,
			Target:     gltf.TargetArrayBuffer,
			Extensions: gltf.Extensions{
				meshoptExt: map[string]interface{}{
					"buffer":     uint32(0),
					"byteOffset": off1,
					"byteLength": cLen1,
					"byteStride": uint32(4),
					"mode":       "ATTRIBUTES",
					"count":      n,
				},
			},
		},
	}
	for i, s := range optStreams {
		bvs = append(bvs, &gltf.BufferView{
			Buffer:     1,
			ByteOffset: n*12 + i*n*4,
			ByteLength: n * 4,
			ByteStride: s.stride,
			Target:     gltf.TargetArrayBuffer,
			Extensions: gltf.Extensions{
				meshoptExt: map[string]interface{}{
					"buffer":     uint32(0),
					"byteOffset": s.compOffset,
					"byteLength": s.compLen,
					"byteStride": uint32(s.stride),
					"mode":       "ATTRIBUTES",
					"count":      n,
				},
			},
		})
	}
	doc.BufferViews = bvs

	// Compute quantized accessor bounds.
	posMin := [3]float64{65535, 65535, 65535}
	posMax := [3]float64{0, 0, 0}
	for _, d := range qPos.Data {
		for axis := 0; axis < 3; axis++ {
			v := float64(d[axis])
			if v < posMin[axis] {
				posMin[axis] = v
			}
			if v > posMax[axis] {
				posMax[axis] = v
			}
		}
	}

	// Accessors: ACC0=position, ACC1=color, ACC2..=optional streams
	accs := []*gltf.Accessor{
		{
			BufferView:    gltf.Index(0),
			ComponentType: gltf.ComponentUshort,
			Type:          gltf.AccessorVec3,
			Normalized:    true,
			Count:         n,
			Min:           []float64{posMin[0], posMin[1], posMin[2]},
			Max:           []float64{posMax[0], posMax[1], posMax[2]},
		},
		{
			BufferView:    gltf.Index(1),
			ComponentType: gltf.ComponentUbyte,
			Type:          gltf.AccessorVec3,
			Normalized:    true,
			Count:         n,
		},
	}
	for i, s := range optStreams {
		accs = append(accs, &gltf.Accessor{
			BufferView:    gltf.Index(2 + i),
			ComponentType: s.componentType,
			Type:          s.accessorType,
			Count:         n,
		})
	}
	doc.Accessors = accs

	// Primitive attributes map
	primAttrs := gltf.PrimitiveAttributes{
		gltf.POSITION: 0,
		gltf.COLOR_0:  1,
	}
	for i, s := range optStreams {
		primAttrs[s.primitiveName] = 2 + i
	}

	var primExts gltf.Extensions
	if extJsonStr != "" {
		primExts = gltf.Extensions{
			"EXT_structural_metadata": json.RawMessage(`{"propertyAttributes": [0]}`),
		}
	}

	doc.Meshes = []*gltf.Mesh{{
		Name: "PointCloud",
		Primitives: []*gltf.Primitive{{
			Mode:       gltf.PrimitivePoints,
			Attributes: primAttrs,
			Extensions: primExts,
		}},
	}}

	// Node 0: Y-up → Z-up rotation, no mesh, parent of Node 1.
	// Node 1: dequantization scale+offset matrix, holds the mesh.
	doc.Nodes = []*gltf.Node{
		{
			Name:     "PointCloud",
			Children: []int{1},
			Matrix:   [16]float64{1, 0, 0, 0, 0, 0, -1, 0, 0, 1, 0, 0, 0, 0, 0, 1},
		},
		{
			Name:   "PointCloudDequant",
			Mesh:   gltf.Index(0),
			Matrix: dequantNodeMatrix(qPos),
		},
	}

	doc.Scenes[0].Nodes = append(doc.Scenes[0].Nodes, 0)

	doc.ExtensionsUsed = []string{
		"KHR_mesh_quantization",
		meshoptExt,
	}
	doc.ExtensionsRequired = []string{
		"KHR_mesh_quantization",
		meshoptExt,
	}
	if extJsonStr != "" {
		doc.Extensions = gltf.Extensions{
			"EXT_structural_metadata": json.RawMessage(extJsonStr),
		}
		doc.ExtensionsUsed = append([]string{"EXT_structural_metadata"}, doc.ExtensionsUsed...)
	}

	enc := gltf.NewEncoder(w)
	enc.AsBinary = true
	return enc.Encode(doc)
}
