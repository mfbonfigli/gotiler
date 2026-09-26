package compression

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"testing"

	"github.com/mfbonfigli/gotiler/v3/tiler/geom"
	"github.com/mfbonfigli/gotiler/v3/tiler/model"
	"github.com/mfbonfigli/gotiler/v3/tiler/plugin"
	"github.com/mfbonfigli/gotiler/v3/tiler/tree"
	"github.com/mfbonfigli/gotiler/v3/version"
)

type captureWriteCloser struct {
	bytes.Buffer
	closeFn func([]byte)
}

func (w *captureWriteCloser) Close() error {
	w.closeFn(w.Bytes())
	return nil
}

type mockNode struct {
	points    geom.PointList
	summaries []model.AttributeSummary
}

func (n mockNode) BoundingBox() geom.BoundingBox {
	return geom.NewBoundingBox(0, 1, 0, 1, 0, 1)
}

func (n mockNode) ChildrenAt(i uint8) tree.Node { return nil }
func (n mockNode) Points() (geom.PointList, error) {
	n.points.Reset()
	return n.points, nil
}
func (n mockNode) TotalNumberOfPoints() int      { return n.points.Len() }
func (n mockNode) NumberOfPoints() int           { return n.points.Len() }
func (n mockNode) IsRoot() bool                  { return true }
func (n mockNode) IsLeaf() bool                  { return true }
func (n mockNode) GeometricError() float64       { return 0 }
func (n mockNode) ToParentCRS() *model.Transform { return nil }
func (n mockNode) RefineMode() model.RefineMode  { return model.RefineAdd }
func (n mockNode) AttributeSummaries() []model.AttributeSummary {
	return n.summaries
}

func newMockNode(points ...model.Point) mockNode {
	return newMockNodeWithSummaries(nil, points...)
}

func newMockNodeWithSummaries(summaries []model.AttributeSummary, points ...model.Point) mockNode {
	var root, prev *geom.LinkedPoint
	for _, pt := range points {
		node := &geom.LinkedPoint{Pt: pt}
		if root == nil {
			root = node
		}
		if prev != nil {
			prev.Next = node
		}
		prev = node
	}
	return mockNode{
		points:    geom.NewLinkedPointStream(root, len(points)),
		summaries: summaries,
	}
}

var testPoints = []model.Point{
	{X: 0, Y: 0, Z: 0, R: 160, G: 166, B: 203},
	{X: 1, Y: 3, Z: 4, R: 186, G: 200, B: 237},
	{X: 2, Y: 6, Z: 8, R: 156, G: 167, B: 204},
}

// testPointValues holds the per-point standard attribute values matching
// testPoints by index, packed by attachStandardAttributes.
var testPointValues = []map[string]any{
	{model.AttrIntensity: uint16(7), model.AttrClassification: uint8(3)},
	{model.AttrIntensity: uint16(100), model.AttrClassification: uint8(5)},
	{model.AttrIntensity: uint16(200), model.AttrClassification: uint8(2)},
}

func makeCompressedGLB(t *testing.T, attrs model.Attributes, points ...model.Point) []byte {
	t.Helper()
	return makeCompressedGLBFromNode(t, attrs, newMockNodeWithSummaries(standardSummaries(attrs), attachStandardAttributes(attrs, points)...))
}

func makeCompressedGLBFromNode(t *testing.T, attrs model.Attributes, node mockNode) []byte {
	t.Helper()

	encoder := New(attrs)
	writes := map[string][]byte{}
	wp := func(name string) (io.WriteCloser, error) {
		return &captureWriteCloser{closeFn: func(data []byte) {
			writes[name] = append([]byte(nil), data...)
		}}, nil
	}

	if err := encoder.Write(node, wp, ""); err != nil {
		t.Fatalf("write compressed GLB: %v", err)
	}

	glb, ok := writes["d.glb"]
	if !ok {
		t.Fatalf("expected d.glb to be written, got files %#v", writes)
	}
	return glb
}

func standardSummaries(attrs model.Attributes) []model.AttributeSummary {
	summaries := make([]model.AttributeSummary, 0, len(attrs))
	for _, name := range attrs {
		switch name {
		case model.AttrIntensity:
			summaries = append(summaries, model.AttributeSummary{RequestedName: name, Name: name, Type: model.AttributeUint16})
		case model.AttrClassification:
			summaries = append(summaries, model.AttributeSummary{RequestedName: name, Name: name, Type: model.AttributeUint8})
		case model.AttrReturnNumber:
			summaries = append(summaries, model.AttributeSummary{RequestedName: name, Name: name, Type: model.AttributeUint8})
		case model.AttrNumberOfReturns:
			summaries = append(summaries, model.AttributeSummary{RequestedName: name, Name: name, Type: model.AttributeUint8})
		}
	}
	return summaries
}

// packSummaryValues packs one point's attribute values (keyed by canonical
// name) into the layout derived from summaries. Missing names stay zero.
func packSummaryValues(summaries []model.AttributeSummary, values map[string]any) model.AttributeValues {
	entries, size := model.AttributeLayout(summaries)
	out := make(model.AttributeValues, size)
	for _, e := range entries {
		v, ok := values[e.Name]
		if !ok {
			continue
		}
		if err := model.EncodeAttributeValue(out[e.Offset:e.Offset+e.Size], e.Type, v); err != nil {
			panic(fmt.Sprintf("pack attribute %q: %v", e.Name, err))
		}
	}
	return out
}

// attachStandardAttributes packs the testPointValues entry matching each
// point's index (zero values past the end of testPointValues).
func attachStandardAttributes(attrs model.Attributes, points []model.Point) []model.Point {
	summaries := standardSummaries(attrs)
	out := make([]model.Point, len(points))
	for i, pt := range points {
		out[i] = pt
		values := map[string]any{}
		if i < len(testPointValues) {
			values = testPointValues[i]
		}
		packed := packSummaryValues(summaries, values)
		if len(packed) > 0 {
			out[i].Attributes = packed
		}
	}
	return out
}

func TestRegistersCompressedEncoder(t *testing.T) {
	factory, ok := plugin.GeometryEncoderFactoryFor(EncoderCompressedGLB)
	if !ok {
		t.Fatal("expected compressed GLB encoder to be registered")
	}
	encoder := factory(model.DefaultAttributes())
	if encoder.TilesetVersion() != version.TilesetVersion_1_1 {
		t.Fatalf("unexpected tileset version %s", encoder.TilesetVersion())
	}
	if encoder.ContentFilename() != filename {
		t.Fatalf("unexpected content filename %q", encoder.ContentFilename())
	}
}

func TestCompressedEncoderWritesGLB(t *testing.T) {
	got := makeCompressedGLB(t,
		model.NewAttributes(model.AttrIntensity, model.AttrClassification),
		model.Point{X: 0, Y: 0, Z: 0, R: 255, G: 0, B: 0},
		model.Point{X: 1, Y: 2, Z: 3, R: 0, G: 255, B: 0},
	)
	if len(got) < 4 {
		t.Fatalf("expected GLB bytes, got %d bytes", len(got))
	}
	if string(got[:4]) != "glTF" {
		t.Fatalf("expected GLB magic glTF, got %q", string(got[:4]))
	}

	doc := parseGLBJSON(t, got)
	assertStringListContains(t, doc["extensionsUsed"], "EXT_structural_metadata")
	assertStringListContains(t, doc["extensionsUsed"], "KHR_mesh_quantization")
	assertStringListContains(t, doc["extensionsUsed"], "EXT_meshopt_compression")
	assertStringListContains(t, doc["extensionsRequired"], "KHR_mesh_quantization")
	assertStringListContains(t, doc["extensionsRequired"], "EXT_meshopt_compression")
	assertStringListDoesNotContain(t, doc["extensionsRequired"], "EXT_structural_metadata")

	docBytes, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal parsed GLB JSON: %v", err)
	}
	for _, want := range []string{"_INTENSITY", "_CLASSIFICATION", "INTENSITY", "CLASSIFICATION"} {
		if !bytes.Contains(docBytes, []byte(want)) {
			t.Fatalf("expected GLB JSON to contain %q", want)
		}
	}
}

func TestCompressedEncoderWritesGenericAttributesFromSummaries(t *testing.T) {
	summaries := []model.AttributeSummary{{
		RequestedName: "Amplification",
		Name:          "amplification",
		Type:          model.AttributeUint8,
		Min:           uint8(12),
		Max:           uint8(27),
	}}
	node := newMockNodeWithSummaries(
		summaries,
		model.Point{X: 0, Y: 0, Z: 0, R: 255, G: 0, B: 0, Attributes: packSummaryValues(summaries, map[string]any{"amplification": uint8(12)})},
		model.Point{X: 1, Y: 2, Z: 3, R: 0, G: 255, B: 0, Attributes: packSummaryValues(summaries, map[string]any{"amplification": uint8(27)})},
	)

	glb := makeCompressedGLBFromNode(t, model.NewAttributes("Amplification"), node)
	doc := parseGLBJSON(t, glb)
	docBytes, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal parsed GLB JSON: %v", err)
	}
	for _, want := range []string{"_AMPLIFICATION", "AMPLIFICATION"} {
		if !bytes.Contains(docBytes, []byte(want)) {
			t.Fatalf("expected GLB JSON to contain %q", want)
		}
	}

	bufferViews := mustList(t, doc, "bufferViews")
	if len(bufferViews) != 3 {
		t.Fatalf("expected 3 buffer views, got %d", len(bufferViews))
	}
	accessors := mustList(t, doc, "accessors")
	if len(accessors) != 3 {
		t.Fatalf("expected 3 accessors, got %d", len(accessors))
	}
	amplification := mustMapFromValue(t, accessors[2], "accessors[2]")
	if got := mustNumber(t, amplification, "componentType"); got != 5121 {
		t.Fatalf("amplification componentType = %v, want 5121", got)
	}
}

func TestCompressedEncoderOmitsStructuralMetadataWithoutOptionalAttributes(t *testing.T) {
	glb := makeCompressedGLB(t, model.NewAttributes(), model.Point{X: 0, Y: 0, Z: 0, R: 255, G: 255, B: 255})
	doc := parseGLBJSON(t, glb)
	assertStringListDoesNotContain(t, doc["extensionsUsed"], "EXT_structural_metadata")
	docBytes, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal parsed GLB JSON: %v", err)
	}
	if bytes.Contains(docBytes, []byte("EXT_structural_metadata")) {
		t.Fatal("did not expect structural metadata extension for default point attributes")
	}
}

func TestCompressedEncoderGLBStructure(t *testing.T) {
	glb := makeCompressedGLB(t, model.NewAttributes(model.AttrIntensity, model.AttrClassification), testPoints...)
	doc := parseGLBJSON(t, glb)

	buffers := mustList(t, doc, "buffers")
	if len(buffers) != 2 {
		t.Fatalf("expected 2 buffers, got %d", len(buffers))
	}
	buffer1 := mustMapFromValue(t, buffers[1], "buffers[1]")
	buffer1Ext := mustMap(t, buffer1, "extensions")
	meshoptFallback := mustMap(t, buffer1Ext, "EXT_meshopt_compression")
	if fallback, ok := meshoptFallback["fallback"].(bool); !ok || !fallback {
		t.Fatalf("expected buffer[1] EXT_meshopt_compression.fallback=true, got %#v", meshoptFallback["fallback"])
	}

	bufferViews := mustList(t, doc, "bufferViews")
	if len(bufferViews) != 4 {
		t.Fatalf("expected 4 buffer views, got %d", len(bufferViews))
	}
	expectedStrides := []float64{8, 4, 4, 4}
	for i, raw := range bufferViews {
		bufferView := mustMapFromValue(t, raw, "bufferViews")
		if got := mustNumber(t, bufferView, "buffer"); got != 1 {
			t.Fatalf("bufferViews[%d].buffer = %v, want 1", i, got)
		}
		if got := mustNumber(t, bufferView, "byteStride"); got != expectedStrides[i] {
			t.Fatalf("bufferViews[%d].byteStride = %v, want %v", i, got, expectedStrides[i])
		}
		ext := mustMap(t, bufferView, "extensions")
		meshopt := mustMap(t, ext, "EXT_meshopt_compression")
		if got := meshopt["mode"]; got != "ATTRIBUTES" {
			t.Fatalf("bufferViews[%d] meshopt mode = %#v, want ATTRIBUTES", i, got)
		}
		if got := mustNumber(t, meshopt, "buffer"); got != 0 {
			t.Fatalf("bufferViews[%d] meshopt buffer = %v, want 0", i, got)
		}
		if got := mustNumber(t, meshopt, "count"); got != float64(len(testPoints)) {
			t.Fatalf("bufferViews[%d] meshopt count = %v, want %d", i, got, len(testPoints))
		}
	}

	accessors := mustList(t, doc, "accessors")
	if len(accessors) != 4 {
		t.Fatalf("expected 4 accessors, got %d", len(accessors))
	}
	position := mustMapFromValue(t, accessors[0], "accessors[0]")
	if got := mustNumber(t, position, "componentType"); got != 5123 {
		t.Fatalf("position componentType = %v, want 5123", got)
	}
	if got := position["type"]; got != "VEC3" {
		t.Fatalf("position type = %#v, want VEC3", got)
	}
	if normalized, ok := position["normalized"].(bool); !ok || !normalized {
		t.Fatalf("position normalized = %#v, want true", position["normalized"])
	}
	if got := mustNumber(t, position, "count"); got != float64(len(testPoints)) {
		t.Fatalf("position count = %v, want %d", got, len(testPoints))
	}
	if minVals := mustList(t, position, "min"); len(minVals) != 3 {
		t.Fatalf("position min should have 3 values, got %d", len(minVals))
	}
	if maxVals := mustList(t, position, "max"); len(maxVals) != 3 {
		t.Fatalf("position max should have 3 values, got %d", len(maxVals))
	}

	color := mustMapFromValue(t, accessors[1], "accessors[1]")
	if got := mustNumber(t, color, "componentType"); got != 5121 {
		t.Fatalf("color componentType = %v, want 5121", got)
	}
	if got := color["type"]; got != "VEC3" {
		t.Fatalf("color type = %#v, want VEC3", got)
	}
	if normalized, ok := color["normalized"].(bool); !ok || !normalized {
		t.Fatalf("color normalized = %#v, want true", color["normalized"])
	}

	intensity := mustMapFromValue(t, accessors[2], "accessors[2]")
	if got := mustNumber(t, intensity, "componentType"); got != 5123 {
		t.Fatalf("intensity componentType = %v, want 5123", got)
	}
	if got := intensity["type"]; got != "SCALAR" {
		t.Fatalf("intensity type = %#v, want SCALAR", got)
	}

	classification := mustMapFromValue(t, accessors[3], "accessors[3]")
	if got := mustNumber(t, classification, "componentType"); got != 5121 {
		t.Fatalf("classification componentType = %v, want 5121", got)
	}
	if got := classification["type"]; got != "SCALAR" {
		t.Fatalf("classification type = %#v, want SCALAR", got)
	}
}

func TestCompressedEncoderNodesAndPrimitive(t *testing.T) {
	glb := makeCompressedGLB(t, model.NewAttributes(model.AttrIntensity, model.AttrClassification), testPoints...)
	doc := parseGLBJSON(t, glb)

	nodes := mustList(t, doc, "nodes")
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(nodes))
	}
	node0 := mustMapFromValue(t, nodes[0], "nodes[0]")
	if _, ok := node0["mesh"]; ok {
		t.Fatal("rotation node should not have a mesh")
	}
	children := mustList(t, node0, "children")
	if len(children) != 1 || children[0].(float64) != 1 {
		t.Fatalf("node 0 children = %#v, want [1]", children)
	}

	node1 := mustMapFromValue(t, nodes[1], "nodes[1]")
	if got := mustNumber(t, node1, "mesh"); got != 0 {
		t.Fatalf("node 1 mesh = %v, want 0", got)
	}
	if _, ok := node1["children"]; ok {
		t.Fatal("dequant node should not have children")
	}
	if matrix := mustList(t, node1, "matrix"); len(matrix) != 16 {
		t.Fatalf("node 1 matrix should have 16 values, got %d", len(matrix))
	}

	meshes := mustList(t, doc, "meshes")
	if len(meshes) != 1 {
		t.Fatalf("expected 1 mesh, got %d", len(meshes))
	}
	mesh := mustMapFromValue(t, meshes[0], "meshes[0]")
	primitives := mustList(t, mesh, "primitives")
	if len(primitives) != 1 {
		t.Fatalf("expected 1 primitive, got %d", len(primitives))
	}
	primitive := mustMapFromValue(t, primitives[0], "primitives[0]")
	if got := mustNumber(t, primitive, "mode"); got != 0 {
		t.Fatalf("primitive mode = %v, want 0 (POINTS)", got)
	}
	attrs := mustMap(t, primitive, "attributes")
	for _, attr := range []string{"POSITION", "COLOR_0", "_INTENSITY", "_CLASSIFICATION"} {
		if _, ok := attrs[attr]; !ok {
			t.Fatalf("primitive attribute %q missing in %#v", attr, attrs)
		}
	}
}

func TestBuildPositionRawByteLayout(t *testing.T) {
	q := QuantizedPositions{
		Data: [][4]uint16{
			{100, 200, 300, 0},
			{400, 500, 600, 0},
		},
	}
	buf := make([]byte, 2*8)
	buildPositionRaw(buf, q, 2)

	if got := binary.LittleEndian.Uint16(buf[0:]); got != 100 {
		t.Fatalf("point 0 x = %d, want 100", got)
	}
	if got := binary.LittleEndian.Uint16(buf[2:]); got != 200 {
		t.Fatalf("point 0 y = %d, want 200", got)
	}
	if got := binary.LittleEndian.Uint16(buf[4:]); got != 300 {
		t.Fatalf("point 0 z = %d, want 300", got)
	}
	if buf[6] != 0 || buf[7] != 0 {
		t.Fatalf("point 0 padding = [%d,%d], want [0,0]", buf[6], buf[7])
	}
	if got := binary.LittleEndian.Uint16(buf[8:]); got != 400 {
		t.Fatalf("point 1 x = %d, want 400", got)
	}
	if got := binary.LittleEndian.Uint16(buf[10:]); got != 500 {
		t.Fatalf("point 1 y = %d, want 500", got)
	}
	if got := binary.LittleEndian.Uint16(buf[12:]); got != 600 {
		t.Fatalf("point 1 z = %d, want 600", got)
	}
	if buf[14] != 0 || buf[15] != 0 {
		t.Fatalf("point 1 padding = [%d,%d], want [0,0]", buf[14], buf[15])
	}
}

func TestBuildColorRawByteLayout(t *testing.T) {
	colors := [][3]uint8{{10, 20, 30}, {40, 50, 60}}
	buf := make([]byte, 2*4)
	buildColorRaw(buf, colors, 2)

	if got := buf[:4]; !slices.Equal(got, []byte{10, 20, 30, 0}) {
		t.Fatalf("point 0 color bytes = %#v, want [10 20 30 0]", got)
	}
	if got := buf[4:8]; !slices.Equal(got, []byte{40, 50, 60, 0}) {
		t.Fatalf("point 1 color bytes = %#v, want [40 50 60 0]", got)
	}
}

func TestQuantizePositionsHandlesEmptyInput(t *testing.T) {
	q := quantizePositions(nil)
	if len(q.Data) != 0 {
		t.Fatalf("expected no quantized data, got %d entries", len(q.Data))
	}
	if q.Scale != [3]float64{1, 1, 1} {
		t.Fatalf("unexpected empty scale: %#v", q.Scale)
	}
	if q.Offset != [3]float64{0, 0, 0} {
		t.Fatalf("unexpected empty offset: %#v", q.Offset)
	}
}

func TestQuantizePositionsMapsBoundsAndDegenerateAxes(t *testing.T) {
	q := quantizePositions([][3]float32{
		{-1, 5, 10},
		{1, 5, 20},
		{0, 5, 15},
	})

	if q.Scale != [3]float64{2, 1, 10} {
		t.Fatalf("unexpected scale: %#v", q.Scale)
	}
	if q.Offset != [3]float64{-1, 5, 10} {
		t.Fatalf("unexpected offset: %#v", q.Offset)
	}
	if got, want := q.Data[0], [4]uint16{0, 0, 0, 0}; got != want {
		t.Fatalf("unexpected first quantized point: got %#v want %#v", got, want)
	}
	if got, want := q.Data[1], [4]uint16{65535, 0, 65535, 0}; got != want {
		t.Fatalf("unexpected second quantized point: got %#v want %#v", got, want)
	}
	if q.Data[2][0] < 32767 || q.Data[2][0] > 32768 {
		t.Fatalf("expected midpoint x to quantize near half range, got %d", q.Data[2][0])
	}
	if q.Data[2][1] != 0 {
		t.Fatalf("expected degenerate y axis to quantize to 0, got %d", q.Data[2][1])
	}

	matrix := dequantNodeMatrix(q)
	if matrix[0] != 2 || matrix[5] != 1 || matrix[10] != 10 {
		t.Fatalf("unexpected dequant matrix scale: %#v", matrix)
	}
	if matrix[12] != -1 || matrix[13] != 5 || matrix[14] != 10 || matrix[15] != 1 {
		t.Fatalf("unexpected dequant matrix offset: %#v", matrix)
	}
}

func TestMeshoptEncodingHandlesEmptyAndZeroStreams(t *testing.T) {
	prefix := []byte{1, 2, 3}
	if got := encodeMeshoptAttributesInto(prefix, nil, 0, 4); !slices.Equal(got, prefix) {
		t.Fatalf("empty stream changed destination: got %#v want %#v", got, prefix)
	}

	encoded := encodeMeshoptAttributesInto(nil, make([]byte, 4), 1, 4)
	if len(encoded) != 37 {
		t.Fatalf("unexpected encoded zero stream length: got %d want 37", len(encoded))
	}
	if encoded[0] != 0xa0 {
		t.Fatalf("unexpected meshopt stream header: %#x", encoded[0])
	}
	for i, b := range encoded[1:] {
		if b != 0 {
			t.Fatalf("expected zero-filled encoding after header, byte %d is %#x", i+1, b)
		}
	}
}

func TestZigzagByte(t *testing.T) {
	cases := map[uint8]uint8{
		0:   0,
		1:   2,
		2:   4,
		255: 1,
		254: 3,
	}
	for in, want := range cases {
		if got := zigzagByte(in); got != want {
			t.Fatalf("zigzagByte(%d) = %d, want %d", in, got, want)
		}
	}
}

func parseGLBJSON(t *testing.T, data []byte) map[string]any {
	t.Helper()
	if len(data) < 20 {
		t.Fatalf("GLB too short: %d bytes", len(data))
	}
	if string(data[:4]) != "glTF" {
		t.Fatalf("expected GLB magic glTF, got %q", string(data[:4]))
	}
	if version := binary.LittleEndian.Uint32(data[4:8]); version != 2 {
		t.Fatalf("expected GLB version 2, got %d", version)
	}
	jsonLen := int(binary.LittleEndian.Uint32(data[12:16]))
	if string(data[16:20]) != "JSON" {
		t.Fatalf("expected first chunk to be JSON, got %q", string(data[16:20]))
	}
	if 20+jsonLen > len(data) {
		t.Fatalf("JSON chunk length %d exceeds GLB length %d", jsonLen, len(data))
	}
	var doc map[string]any
	if err := json.Unmarshal(data[20:20+jsonLen], &doc); err != nil {
		t.Fatalf("parse GLB JSON chunk: %v", err)
	}
	return doc
}

func assertStringListContains(t *testing.T, value any, want string) {
	t.Helper()
	if !stringListContains(value, want) {
		t.Fatalf("expected list to contain %q, got %#v", want, value)
	}
}

func assertStringListDoesNotContain(t *testing.T, value any, want string) {
	t.Helper()
	if stringListContains(value, want) {
		t.Fatalf("expected list not to contain %q, got %#v", want, value)
	}
}

func stringListContains(value any, want string) bool {
	items, ok := value.([]any)
	if !ok {
		return false
	}
	for _, item := range items {
		if got, ok := item.(string); ok && got == want {
			return true
		}
	}
	return false
}

func mustList(t *testing.T, doc map[string]any, key string) []any {
	t.Helper()
	value, ok := doc[key].([]any)
	if !ok {
		t.Fatalf("%s missing or not a list: %#v", key, doc[key])
	}
	return value
}

func mustMap(t *testing.T, doc map[string]any, key string) map[string]any {
	t.Helper()
	value, ok := doc[key].(map[string]any)
	if !ok {
		t.Fatalf("%s missing or not a map: %#v", key, doc[key])
	}
	return value
}

func mustMapFromValue(t *testing.T, value any, label string) map[string]any {
	t.Helper()
	got, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("%s is not a map: %#v", label, value)
	}
	return got
}

func mustNumber(t *testing.T, doc map[string]any, key string) float64 {
	t.Helper()
	value, ok := doc[key].(float64)
	if !ok {
		t.Fatalf("%s missing or not a number: %#v", key, doc[key])
	}
	return value
}
