package writer

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/mfbonfigli/gotiler/v3/internal/testtree"
	"github.com/mfbonfigli/gotiler/v3/tiler/geom"
	"github.com/mfbonfigli/gotiler/v3/tiler/model"
	"github.com/mfbonfigli/gotiler/v3/tiler/plugin"
)

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

// makeThreePointNode builds a MockNode with summary-backed standard attributes.
func makeThreePointNode(requested model.Attributes, rn, nr uint8) *testtree.MockNode {
	tr := geom.LocalToGlobalTransformFromPoint(2000, 1000, 1000)
	summaries := makeStandardSummaries(requested)
	values := map[string]any{
		model.AttrIntensity:       uint16(7),
		model.AttrClassification:  uint8(3),
		model.AttrReturnNumber:    rn,
		model.AttrNumberOfReturns: nr,
	}
	pts := []model.Point{
		{X: 0, Y: 0, Z: 0, R: 160, G: 166, B: 203},
		{X: 1, Y: 1, Z: 1, R: 186, G: 200, B: 237},
		{X: 2, Y: 2, Z: 2, R: 156, G: 167, B: 204},
	}
	pt1 := &geom.LinkedPoint{Pt: pts[0]}
	pt2 := &geom.LinkedPoint{Pt: pts[1]}
	pt3 := &geom.LinkedPoint{Pt: pts[2]}
	pt1.Next = pt2
	pt2.Next = pt3
	stream := geom.NewLinkedPointStream(pt1, 3)
	pt1.Pt.Attributes = packSummaryValues(summaries, values)
	pt2.Pt.Attributes = packSummaryValues(summaries, values)
	pt3.Pt.Attributes = packSummaryValues(summaries, values)
	return &testtree.MockNode{
		TotalNumPts: 3,
		Pts:         stream,
		Bounds:      geom.NewBoundingBox(0, 4, 0, 6, 0, 8),
		Root:        true,
		Leaf:        true,
		GeomError:   20,
		Transform:   &tr,
		Summaries:   summaries,
	}
}

func makeStandardSummaries(requested model.Attributes) []model.AttributeSummary {
	summaries := make([]model.AttributeSummary, 0, len(requested))
	for _, name := range requested {
		switch name {
		case model.AttrIntensity:
			summaries = append(summaries, model.AttributeSummary{RequestedName: name, Name: name, Type: model.AttributeUint16, Min: uint16(7), Max: uint16(7)})
		case model.AttrClassification:
			summaries = append(summaries, model.AttributeSummary{RequestedName: name, Name: name, Type: model.AttributeUint8, Min: uint8(3), Max: uint8(3)})
		case model.AttrReturnNumber:
			summaries = append(summaries, model.AttributeSummary{RequestedName: name, Name: name, Type: model.AttributeUint8})
		case model.AttrNumberOfReturns:
			summaries = append(summaries, model.AttributeSummary{RequestedName: name, Name: name, Type: model.AttributeUint8})
		}
	}
	return summaries
}

func makeGenericAttributeNode() *testtree.MockNode {
	tr := geom.LocalToGlobalTransformFromPoint(2000, 1000, 1000)
	summary := model.AttributeSummary{
		RequestedName: "Amplification",
		Name:          model.CanonicalAttributeName("Amplification"),
		Type:          model.AttributeUint8,
		Min:           uint8(4),
		Max:           uint8(4),
	}
	summaries := []model.AttributeSummary{summary}
	values := map[string]any{summary.Name: uint8(4)}
	pts := []model.Point{
		{X: 0, Y: 0, Z: 0, R: 160, G: 166, B: 203, Attributes: packSummaryValues(summaries, values)},
		{X: 1, Y: 1, Z: 1, R: 186, G: 200, B: 237, Attributes: packSummaryValues(summaries, values)},
		{X: 2, Y: 2, Z: 2, R: 156, G: 167, B: 204, Attributes: packSummaryValues(summaries, values)},
	}
	pt1 := &geom.LinkedPoint{Pt: pts[0]}
	pt2 := &geom.LinkedPoint{Pt: pts[1]}
	pt3 := &geom.LinkedPoint{Pt: pts[2]}
	pt1.Next = pt2
	pt2.Next = pt3
	stream := geom.NewLinkedPointStream(pt1, 3)
	return &testtree.MockNode{
		TotalNumPts: 3,
		Pts:         stream,
		Bounds:      geom.NewBoundingBox(0, 4, 0, 6, 0, 8),
		Root:        true,
		Leaf:        true,
		GeomError:   20,
		Transform:   &tr,
		Summaries:   summaries,
	}
}

func encodeToFile(t *testing.T, encoder plugin.GeometryEncoder, node *testtree.MockNode, suffix string) string {
	t.Helper()
	tmp := t.TempDir()
	tmpPath := filepath.Join(tmp, "tst")
	os.Mkdir(tmpPath, 0755)

	c := NewStandardConsumer(WithGeometryEncoder(encoder))
	wc := make(chan *WorkUnit)
	ec := make(chan error, 1)
	wg := &sync.WaitGroup{}
	wg.Add(1)
	go c.Consume(wc, ec, wg)

	wc <- &WorkUnit{Node: node, WriterProvider: NewDiskWriterProvider(tmpPath)}
	close(wc)
	wg.Wait()

	select {
	case err := <-ec:
		t.Fatalf("consumer error: %v", err)
	default:
	}

	outFile := filepath.Join(tmpPath, "d."+suffix)
	if _, err := os.Stat(outFile); err != nil {
		t.Fatalf("output file not written: %v", err)
	}
	return outFile
}

// readGlbUint8Accessor reads a SCALAR UNSIGNED_BYTE accessor from an uncompressed GLB.
// It returns nil if the named attribute is absent.
func readGlbUint8Accessor(t *testing.T, path string, attrName string) []uint8 {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("readGlbUint8Accessor: read file: %v", err)
	}
	if len(raw) < 20 {
		t.Fatalf("GLB too short")
	}

	jsonLen := binary.LittleEndian.Uint32(raw[12:16])
	if int(jsonLen) > len(raw)-20 {
		t.Fatalf("GLB JSON length out of range")
	}
	jsonChunk := raw[20 : 20+jsonLen]

	var doc struct {
		Meshes []struct {
			Primitives []struct {
				Attributes map[string]int `json:"attributes"`
			} `json:"primitives"`
		} `json:"meshes"`
		Accessors []struct {
			BufferView    int    `json:"bufferView"`
			ByteOffset    int    `json:"byteOffset"`
			ComponentType int    `json:"componentType"`
			Count         int    `json:"count"`
			Type          string `json:"type"`
		} `json:"accessors"`
		BufferViews []struct {
			Buffer     int `json:"buffer"`
			ByteOffset int `json:"byteOffset"`
			ByteLength int `json:"byteLength"`
			ByteStride int `json:"byteStride"`
		} `json:"bufferViews"`
	}
	if err := json.Unmarshal(jsonChunk, &doc); err != nil {
		t.Fatalf("GLB JSON parse: %v", err)
	}

	if len(doc.Meshes) == 0 || len(doc.Meshes[0].Primitives) == 0 {
		t.Fatal("no mesh primitives in GLB")
	}
	accIdx, ok := doc.Meshes[0].Primitives[0].Attributes[attrName]
	if !ok {
		return nil
	}

	acc := doc.Accessors[accIdx]
	bv := doc.BufferViews[acc.BufferView]
	stride := bv.ByteStride
	if stride == 0 {
		stride = 1
	}

	binStart := int(20+jsonLen) + 8 // skip BIN chunk header
	base := binStart + bv.ByteOffset + acc.ByteOffset

	vals := make([]uint8, acc.Count)
	for i := range vals {
		vals[i] = raw[base+i*stride]
	}
	return vals
}

// readPntsBatchBodyByte reads n bytes from the pnts batch body at the given
// offset within the body (as declared in the batch table JSON).
func readPntsBatchBodyBytes(t *testing.T, path string, propName string) []uint8 {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read pnts: %v", err)
	}
	// pnts header: magic(4) ver(4) byteLen(4) ftJsonLen(4) ftBinLen(4) btJsonLen(4) btBinLen(4) = 28 bytes
	if len(raw) < 28 {
		t.Fatalf("pnts file too short")
	}
	ftJsonLen := int(binary.LittleEndian.Uint32(raw[12:16]))
	ftBinLen := int(binary.LittleEndian.Uint32(raw[16:20]))
	btJsonLen := int(binary.LittleEndian.Uint32(raw[20:24]))

	btJsonStart := 28 + ftJsonLen + ftBinLen
	btBinStart := btJsonStart + btJsonLen

	if btJsonLen == 0 {
		return nil
	}

	var btJSON map[string]struct {
		ByteOffset    int    `json:"byteOffset"`
		ComponentType string `json:"componentType"`
		Type          string `json:"type"`
	}
	if err := json.Unmarshal(raw[btJsonStart:btJsonStart+btJsonLen], &btJSON); err != nil {
		t.Fatalf("pnts batch table JSON parse: %v", err)
	}

	prop, ok := btJSON[propName]
	if !ok {
		return nil
	}

	// Determine count from feature table
	ftJSON := raw[28 : 28+ftJsonLen]
	var ft struct {
		PointsLength int `json:"POINTS_LENGTH"`
	}
	json.Unmarshal(ftJSON, &ft)
	n := ft.PointsLength

	start := btBinStart + prop.ByteOffset
	return raw[start : start+n]
}

func allEqual(vals []uint8, want uint8) bool {
	for _, v := range vals {
		if v != want {
			return false
		}
	}
	return true
}

// --- Tests ---

func TestGltfEncoderWithReturnAttributes(t *testing.T) {
	attrs := model.NewAttributes(model.AttrIntensity, model.AttrClassification, model.AttrReturnNumber, model.AttrNumberOfReturns)
	path := encodeToFile(t, NewGltfEncoder("d.glb", attrs), makeThreePointNode(attrs, 2, 5), "glb")

	rn := readGlbUint8Accessor(t, path, "_RETURN_NUMBER")
	if len(rn) != 3 {
		t.Fatalf("_RETURN_NUMBER count: got %d want 3", len(rn))
	}
	if !allEqual(rn, 2) {
		t.Errorf("_RETURN_NUMBER values: got %v want all 2", rn)
	}

	nr := readGlbUint8Accessor(t, path, "_NUMBER_OF_RETURNS")
	if len(nr) != 3 {
		t.Fatalf("_NUMBER_OF_RETURNS count: got %d want 3", len(nr))
	}
	if !allEqual(nr, 5) {
		t.Errorf("_NUMBER_OF_RETURNS values: got %v want all 5", nr)
	}
}

func TestPntsEncoderWithReturnAttributes(t *testing.T) {
	attrs := model.NewAttributes(model.AttrIntensity, model.AttrClassification, model.AttrReturnNumber, model.AttrNumberOfReturns)
	path := encodeToFile(t, NewPntsEncoder("d.pnts", attrs), makeThreePointNode(attrs, 2, 5), "pnts")

	rn := readPntsBatchBodyBytes(t, path, "RETURN_NUMBER")
	if len(rn) != 3 {
		t.Fatalf("RETURN_NUMBER count: got %d want 3", len(rn))
	}
	if !allEqual(rn, 2) {
		t.Errorf("RETURN_NUMBER values: got %v want all 2", rn)
	}

	nr := readPntsBatchBodyBytes(t, path, "NUMBER_OF_RETURNS")
	if len(nr) != 3 {
		t.Fatalf("NUMBER_OF_RETURNS count: got %d want 3", len(nr))
	}
	if !allEqual(nr, 5) {
		t.Errorf("NUMBER_OF_RETURNS values: got %v want all 5", nr)
	}
}

func TestPntsEncoderSectionAlignment(t *testing.T) {
	attrs := model.NewAttributes(model.AttrIntensity, model.AttrClassification, model.AttrReturnNumber, model.AttrNumberOfReturns)
	path := encodeToFile(t, NewPntsEncoder("d.pnts", attrs), makeThreePointNode(attrs, 2, 5), "pnts")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read pnts: %v", err)
	}
	if len(raw) < pntsHeaderLen {
		t.Fatalf("pnts too short")
	}
	byteLen := int(binary.LittleEndian.Uint32(raw[8:12]))
	ftJSONLen := int(binary.LittleEndian.Uint32(raw[12:16]))
	ftBinLen := int(binary.LittleEndian.Uint32(raw[16:20]))
	btJSONLen := int(binary.LittleEndian.Uint32(raw[20:24]))
	btBinLen := int(binary.LittleEndian.Uint32(raw[24:28]))

	if byteLen != len(raw) {
		t.Fatalf("header byte length %d does not match file length %d", byteLen, len(raw))
	}
	sectionOffsets := map[string]int{
		"feature table binary": pntsHeaderLen + ftJSONLen,
		"batch table JSON":     pntsHeaderLen + ftJSONLen + ftBinLen,
		"batch table binary":   pntsHeaderLen + ftJSONLen + ftBinLen + btJSONLen,
		"byte length":          byteLen,
	}
	for name, offset := range sectionOffsets {
		if offset%pntsAlignment != 0 {
			t.Fatalf("%s offset/length %d is not %d-byte aligned", name, offset, pntsAlignment)
		}
	}
	if btBinLen%pntsAlignment != 0 {
		t.Fatalf("batch table binary length %d is not %d-byte aligned", btBinLen, pntsAlignment)
	}
}

func TestGltfEncoderReturnNumberOnly(t *testing.T) {
	attrs := model.NewAttributes(model.AttrReturnNumber)
	path := encodeToFile(t, NewGltfEncoder("d.glb", attrs), makeThreePointNode(attrs, 3, 0), "glb")
	rn := readGlbUint8Accessor(t, path, "_RETURN_NUMBER")
	if !allEqual(rn, 3) {
		t.Errorf("_RETURN_NUMBER: got %v want all 3", rn)
	}
	if nr := readGlbUint8Accessor(t, path, "_NUMBER_OF_RETURNS"); nr != nil {
		t.Errorf("_NUMBER_OF_RETURNS should be absent when not requested")
	}
}

func TestGltfEncoderNumberOfReturnsOnly(t *testing.T) {
	attrs := model.NewAttributes(model.AttrNumberOfReturns)
	path := encodeToFile(t, NewGltfEncoder("d.glb", attrs), makeThreePointNode(attrs, 0, 4), "glb")
	nr := readGlbUint8Accessor(t, path, "_NUMBER_OF_RETURNS")
	if !allEqual(nr, 4) {
		t.Errorf("_NUMBER_OF_RETURNS: got %v want all 4", nr)
	}
	if rn := readGlbUint8Accessor(t, path, "_RETURN_NUMBER"); rn != nil {
		t.Errorf("_RETURN_NUMBER should be absent when not requested")
	}
}

func TestPntsEncoderReturnNumberOnly(t *testing.T) {
	attrs := model.NewAttributes(model.AttrReturnNumber)
	path := encodeToFile(t, NewPntsEncoder("d.pnts", attrs), makeThreePointNode(attrs, 3, 0), "pnts")
	rn := readPntsBatchBodyBytes(t, path, "RETURN_NUMBER")
	if !allEqual(rn, 3) {
		t.Errorf("RETURN_NUMBER: got %v want all 3", rn)
	}
}

func TestPntsEncoderNumberOfReturnsOnly(t *testing.T) {
	attrs := model.NewAttributes(model.AttrNumberOfReturns)
	path := encodeToFile(t, NewPntsEncoder("d.pnts", attrs), makeThreePointNode(attrs, 0, 4), "pnts")
	nr := readPntsBatchBodyBytes(t, path, "NUMBER_OF_RETURNS")
	if !allEqual(nr, 4) {
		t.Errorf("NUMBER_OF_RETURNS: got %v want all 4", nr)
	}
}

func TestGltfEncoderWithGenericAttribute(t *testing.T) {
	path := encodeToFile(t, NewGltfEncoder("d.glb", model.NewAttributes("Amplification")), makeGenericAttributeNode(), "glb")
	vals := readGlbUint8Accessor(t, path, "_AMPLIFICATION")
	if len(vals) != 3 {
		t.Fatalf("_AMPLIFICATION count: got %d want 3", len(vals))
	}
	if !allEqual(vals, 4) {
		t.Errorf("_AMPLIFICATION values: got %v want all 4", vals)
	}
}

func TestPntsEncoderWithGenericAttribute(t *testing.T) {
	path := encodeToFile(t, NewPntsEncoder("d.pnts", model.NewAttributes("Amplification")), makeGenericAttributeNode(), "pnts")
	vals := readPntsBatchBodyBytes(t, path, "AMPLIFICATION")
	if len(vals) != 3 {
		t.Fatalf("AMPLIFICATION count: got %d want 3", len(vals))
	}
	if !allEqual(vals, 4) {
		t.Errorf("AMPLIFICATION values: got %v want all 4", vals)
	}
}

// readGlbFloat32Accessor reads a SCALAR FLOAT accessor from an uncompressed GLB.
// It returns nil if the named attribute is absent.
func readGlbFloat32Accessor(t *testing.T, path string, attrName string) []float32 {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("readGlbFloat32Accessor: read file: %v", err)
	}
	jsonLen := binary.LittleEndian.Uint32(raw[12:16])
	jsonChunk := raw[20 : 20+jsonLen]

	var doc struct {
		Meshes []struct {
			Primitives []struct {
				Attributes map[string]int `json:"attributes"`
			} `json:"primitives"`
		} `json:"meshes"`
		Accessors []struct {
			BufferView    int `json:"bufferView"`
			ByteOffset    int `json:"byteOffset"`
			ComponentType int `json:"componentType"`
			Count         int `json:"count"`
		} `json:"accessors"`
		BufferViews []struct {
			ByteOffset int `json:"byteOffset"`
			ByteStride int `json:"byteStride"`
		} `json:"bufferViews"`
	}
	if err := json.Unmarshal(jsonChunk, &doc); err != nil {
		t.Fatalf("GLB JSON parse: %v", err)
	}
	accIdx, ok := doc.Meshes[0].Primitives[0].Attributes[attrName]
	if !ok {
		return nil
	}
	acc := doc.Accessors[accIdx]
	if acc.ComponentType != 5126 {
		t.Fatalf("%s componentType: got %d want 5126 (FLOAT)", attrName, acc.ComponentType)
	}
	bv := doc.BufferViews[acc.BufferView]
	binStart := int(20+jsonLen) + 8 // skip BIN chunk header
	base := binStart + bv.ByteOffset + acc.ByteOffset

	vals := make([]float32, acc.Count)
	for i := range vals {
		bits := binary.LittleEndian.Uint32(raw[base+i*bv.ByteStride:])
		vals[i] = math.Float32frombits(bits)
	}
	return vals
}

// TestGltfEncoderFloat64Attribute verifies that float64 attributes (e.g.
// gps_time, scaled extra bytes) are emitted in GLB as float32 accessors with
// a FLOAT32 metadata declaration, rather than being dropped.
func TestGltfEncoderFloat64Attribute(t *testing.T) {
	tr := geom.LocalToGlobalTransformFromPoint(2000, 1000, 1000)
	summary := model.AttributeSummary{
		RequestedName: "gps_time",
		Name:          "gps_time",
		Type:          model.AttributeFloat64,
	}
	summaries := []model.AttributeSummary{summary}
	// Exactly representable in float32 so the downcast round-trips.
	values := map[string]any{summary.Name: float64(123456.75)}
	pts := []model.Point{
		{X: 0, Y: 0, Z: 0, R: 160, G: 166, B: 203, Attributes: packSummaryValues(summaries, values)},
		{X: 1, Y: 1, Z: 1, R: 186, G: 200, B: 237, Attributes: packSummaryValues(summaries, values)},
		{X: 2, Y: 2, Z: 2, R: 156, G: 167, B: 204, Attributes: packSummaryValues(summaries, values)},
	}
	pt1 := &geom.LinkedPoint{Pt: pts[0]}
	pt2 := &geom.LinkedPoint{Pt: pts[1]}
	pt3 := &geom.LinkedPoint{Pt: pts[2]}
	pt1.Next = pt2
	pt2.Next = pt3
	node := &testtree.MockNode{
		TotalNumPts: 3,
		Pts:         geom.NewLinkedPointStream(pt1, 3),
		Bounds:      geom.NewBoundingBox(0, 4, 0, 6, 0, 8),
		Root:        true,
		Leaf:        true,
		GeomError:   20,
		Transform:   &tr,
		Summaries:   summaries,
	}

	path := encodeToFile(t, NewGltfEncoder("d.glb", model.NewAttributes("gps_time")), node, "glb")
	vals := readGlbFloat32Accessor(t, path, "_GPS_TIME")
	if len(vals) != 3 {
		t.Fatalf("_GPS_TIME count: got %d want 3", len(vals))
	}
	for i, v := range vals {
		if v != 123456.75 {
			t.Errorf("_GPS_TIME[%d]: got %v want 123456.75", i, v)
		}
	}

	// The metadata schema must declare the stored type (FLOAT32), not FLOAT64.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read glb: %v", err)
	}
	jsonLen := binary.LittleEndian.Uint32(raw[12:16])
	jsonChunk := string(raw[20 : 20+jsonLen])
	if !strings.Contains(jsonChunk, `"GPS_TIME":{"type":"SCALAR","componentType":"FLOAT32","required":true}`) {
		t.Errorf("metadata schema does not declare GPS_TIME as FLOAT32:\n%s", jsonChunk)
	}
}
