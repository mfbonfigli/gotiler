package kd

import (
	"context"
	"io"
	"os"
	"sync"
	"testing"

	"github.com/mfbonfigli/gotiler/v3/internal/pc"
	"github.com/mfbonfigli/gotiler/v3/internal/utils"
	"github.com/mfbonfigli/gotiler/v3/internal/utils/test"
	"github.com/mfbonfigli/gotiler/v3/tiler/geom"
	"github.com/mfbonfigli/gotiler/v3/tiler/model"
	"github.com/mfbonfigli/gotiler/v3/tiler/mutator"
)

func TestReservoirLoader_InvalidSize(t *testing.T) {
	conv := test.GetTestCoordinateConverterFactory()
	loader := NewReservoirLoader(conv, nil, 0, 1, t.TempDir(), NewFileIoFactory(), nil)

	reader := &pc.MockLasReader{
		CRS: "EPSG:4978",
		Pts: []geom.Point64{{Vector: model.Vector{X: 4399228.288985, Y: 855784.797006, Z: 0}}},
	}

	_, err := loader.Run(reader, context.Background(), nil)
	if err == nil {
		t.Errorf("expected error for invalid reservoir size, got nil")
	}
}

func TestReservoirLoader_EmptyInput(t *testing.T) {
	conv := test.GetTestCoordinateConverterFactory()
	loader := NewReservoirLoader(conv, nil, 10, 1, t.TempDir(), NewFileIoFactory(), nil)

	reader := &pc.MockLasReader{
		CRS: "EPSG:4978",
		Pts: []geom.Point64{},
	}

	_, err := loader.Run(reader, context.Background(), nil)
	if err == nil {
		t.Errorf("expected error for empty input, got nil")
	}
}

func TestReservoirLoader_BasicPipeline(t *testing.T) {
	conv := test.GetTestCoordinateConverterFactory()
	loader := NewReservoirLoader(conv, nil, 100, 1, t.TempDir(), NewFileIoFactory(), nil)

	reader := &pc.MockLasReader{
		CRS: "EPSG:4978",
		Pts: []geom.Point64{
			{Vector: model.Vector{X: 4399228.288985, Y: 855784.797006, Z: 0}, R: 255, G: 0, B: 0},
			{Vector: model.Vector{X: 4399238.288985, Y: 855784.797006, Z: 0}, R: 0, G: 255, B: 0},
			{Vector: model.Vector{X: 4399228.288985, Y: 855794.797006, Z: 0}, R: 0, G: 0, B: 255},
			{Vector: model.Vector{X: 4399228.288985, Y: 855784.797006, Z: 10}, R: 128, G: 128, B: 128},
			{Vector: model.Vector{X: 4399233.288985, Y: 855789.797006, Z: 5}, R: 64, G: 64, B: 64},
		},
	}

	result, err := loader.Run(reader, context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Ensure file is deleted when test finishes
	t.Cleanup(func() { _ = os.Remove(result.tempFilePath) })

	if result.totalPoints != 5 {
		t.Errorf("expected 5 total points, got %d", result.totalPoints)
	}

	if len(result.sample) != 5 {
		t.Errorf("expected 5 sample points, got %d", len(result.sample))
	}

	if result.tempFilePath == "" {
		t.Errorf("expected non-empty temp file path")
	}

	if diff, err := utils.CompareWithTolerance(result.bounds.Xmin, -10.0, 1e-4); err != nil {
		t.Errorf("Xmin: diff %f above tolerance, got %f expected -10", diff, result.bounds.Xmin)
	}
	if diff, err := utils.CompareWithTolerance(result.bounds.Xmax, 0.0, 1e-4); err != nil {
		t.Errorf("Xmax: diff %f above tolerance, got %f expected 0", diff, result.bounds.Xmax)
	}
	if diff, err := utils.CompareWithTolerance(result.bounds.Ymin, -1.909512, 1e-4); err != nil {
		t.Errorf("Ymin: diff %f above tolerance, got %f expected ~ -1.91", diff, result.bounds.Ymin)
	}
	if diff, err := utils.CompareWithTolerance(result.bounds.Ymax, 9.815995, 1e-4); err != nil {
		t.Errorf("Ymax: diff %f above tolerance, got %f expected ~ 9.82", diff, result.bounds.Ymax)
	}
	if diff, err := utils.CompareWithTolerance(result.bounds.Zmin, 0.0, 1e-4); err != nil {
		t.Errorf("Zmin: diff %f above tolerance, got %f expected 0", diff, result.bounds.Zmin)
	}
	if diff, err := utils.CompareWithTolerance(result.bounds.Zmax, 9.815995, 1e-4); err != nil {
		t.Errorf("Zmax: diff %f above tolerance, got %f expected ~ 9.82", diff, result.bounds.Zmax)
	}

	reader2, err := NewFilePointReader(result.tempFilePath)
	if err != nil {
		t.Fatalf("failed to open temp file: %v", err)
	}
	defer reader2.Close()

	if reader2.NumPoints() != 5 {
		t.Errorf("expected 5 points in temp file, got %d", reader2.NumPoints())
	}

	buf := make([]model.Point, 0, 5)
	buf, readErr := reader2.NextBatch(buf)
	if readErr != nil && readErr != io.EOF {
		t.Fatalf("failed to read points from temp file: %v", readErr)
	}
	if len(buf) != 5 {
		t.Fatalf("expected 5 points, got %d", len(buf))
	}

	if diff, err := utils.CompareWithTolerance(float64(buf[0].X), 0.0, 1e-4); err != nil {
		t.Errorf("point 0 X: diff %f above tolerance, got %f expected 0", diff, buf[0].X)
	}
	if diff, err := utils.CompareWithTolerance(float64(buf[0].Y), 0.0, 1e-4); err != nil {
		t.Errorf("point 0 Y: diff %f above tolerance, got %f expected 0", diff, buf[0].Y)
	}
	if diff, err := utils.CompareWithTolerance(float64(buf[0].Z), 0.0, 1e-4); err != nil {
		t.Errorf("point 0 Z: diff %f above tolerance, got %f expected 0", diff, buf[0].Z)
	}

	expectedPts := reader.Pts
	for i := range 5 {
		if buf[i].R != expectedPts[i].R {
			t.Errorf("point %d R: expected %d got %d", i, expectedPts[i].R, buf[i].R)
		}
		if buf[i].G != expectedPts[i].G {
			t.Errorf("point %d G: expected %d got %d", i, expectedPts[i].G, buf[i].G)
		}
		if buf[i].B != expectedPts[i].B {
			t.Errorf("point %d B: expected %d got %d", i, expectedPts[i].B, buf[i].B)
		}

		ecefPt := result.localToGlobal.Forward(model.Vector{
			X: float64(buf[i].X),
			Y: float64(buf[i].Y),
			Z: float64(buf[i].Z),
		})
		if diffX, errX := utils.CompareWithTolerance(ecefPt.X, expectedPts[i].X, 1e-3); errX != nil || diffX > 1e-2 {
			t.Errorf("point %d X round-trip: diff %f above tolerance, got %f expected %f", i, diffX, ecefPt.X, expectedPts[i].X)
		}
		if diffY, errY := utils.CompareWithTolerance(ecefPt.Y, expectedPts[i].Y, 1e-3); errY != nil || diffY > 1e-2 {
			t.Errorf("point %d Y round-trip: diff %f above tolerance, got %f expected %f", i, diffY, ecefPt.Y, expectedPts[i].Y)
		}
		if diffZ, errZ := utils.CompareWithTolerance(ecefPt.Z, expectedPts[i].Z, 1e-3); errZ != nil || diffZ > 1e-2 {
			t.Errorf("point %d Z round-trip: diff %f above tolerance, got %f expected %f", i, diffZ, ecefPt.Z, expectedPts[i].Z)
		}
	}
}

func TestReservoirLoader_FewerPointsThanReservoir(t *testing.T) {
	conv := test.GetTestCoordinateConverterFactory()
	loader := NewReservoirLoader(conv, nil, 1000, 1, t.TempDir(), NewFileIoFactory(), nil)

	reader := &pc.MockLasReader{
		CRS: "EPSG:4978",
		Pts: []geom.Point64{
			{Vector: model.Vector{X: 4399228.288985, Y: 855784.797006, Z: 0}},
			{Vector: model.Vector{X: 4399238.288985, Y: 855794.797006, Z: 10}},
			{Vector: model.Vector{X: 4399248.288985, Y: 855804.797006, Z: 20}},
		},
	}

	result, err := loader.Run(reader, context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(result.tempFilePath) })

	if result.totalPoints != 3 {
		t.Errorf("expected 3 total points, got %d", result.totalPoints)
	}
	if len(result.sample) != 3 {
		t.Errorf("expected 3 sample points, got %d", len(result.sample))
	}
}

func TestReservoirLoader_WithZOffset(t *testing.T) {
	conv := test.GetTestCoordinateConverterFactory()
	zOffset := mutator.NewZOffset(5.0)
	loader := NewReservoirLoader(conv, zOffset, 10, 1, t.TempDir(), NewFileIoFactory(), nil)

	reader := &pc.MockLasReader{
		CRS: "EPSG:4978",
		Pts: []geom.Point64{
			{Vector: model.Vector{X: 4399228.288985, Y: 855784.797006, Z: 0}, R: 100, G: 100, B: 100},
			{Vector: model.Vector{X: 4399238.288985, Y: 855784.797006, Z: 0}, R: 200, G: 200, B: 200},
		},
	}

	result, err := loader.Run(reader, context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(result.tempFilePath) })

	if result.totalPoints != 2 {
		t.Errorf("expected 2 total points, got %d", result.totalPoints)
	}

	reader2, err := NewFilePointReader(result.tempFilePath)
	if err != nil {
		t.Fatalf("failed to open temp file: %v", err)
	}
	defer reader2.Close()

	if reader2.NumPoints() != 2 {
		t.Errorf("expected 2 points in temp file, got %d", reader2.NumPoints())
	}

	buf := make([]model.Point, 0, 2)
	buf, readErr := reader2.NextBatch(buf)
	if readErr != nil && readErr != io.EOF {
		t.Fatalf("failed to read points: %v", readErr)
	}

	if diff, err := utils.CompareWithTolerance(float64(buf[0].Z), 5.0, 1e-3); err != nil {
		t.Errorf("first point Z: diff %f above tolerance, expected ~5 got %f", diff, buf[0].Z)
	}
}

func TestReservoirLoader_MultipleWorkers(t *testing.T) {
	conv := test.GetTestCoordinateConverterFactory()

	// FIX: Scale dataset to ensure multiple batches fill worker thread slots
	numPoints := (pipelineBatchSize * 4) + 50
	loader := NewReservoirLoader(conv, nil, 100, 4, t.TempDir(), NewFileIoFactory(), nil)

	pts := make([]geom.Point64, numPoints)
	for i := range pts {
		pts[i] = geom.Point64{
			Vector: model.Vector{
				X: 4399228.288985 + float64(i),
				Y: 855784.797006 + float64(i),
				Z: float64(i),
			},
		}
	}
	reader := &pc.MockLasReader{
		CRS: "EPSG:4978",
		Pts: pts,
	}

	result, err := loader.Run(reader, context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(result.tempFilePath) })

	if result.totalPoints != numPoints {
		t.Errorf("expected %d total points, got %d", numPoints, result.totalPoints)
	}

	reader2, err := NewFilePointReader(result.tempFilePath)
	if err != nil {
		t.Fatalf("failed to open temp file: %v", err)
	}
	defer reader2.Close()

	if reader2.NumPoints() != numPoints {
		t.Errorf("expected %d points in temp file, got %d", numPoints, reader2.NumPoints())
	}
}

// TestReservoirLoader_GenericAttributes verifies the end-to-end attribute
// pipeline: reader-provided attributes are packed by the workers, summaries
// accumulate type/min/max/missing information, and packed values survive the
// temp-file round-trip through the io factory.
func TestReservoirLoader_GenericAttributes(t *testing.T) {
	conv := test.GetTestCoordinateConverterFactory()
	ioFactory := NewFileIoFactory()
	requested := model.NewAttributes("Intensity", "amplification", "not_in_source")
	loader := NewReservoirLoader(conv, nil, 100, 2, t.TempDir(), ioFactory, requested)

	schema := []model.AttributeDescriptor{
		{Name: "intensity", Type: model.AttributeUint16},
		{Name: "amplification", Type: model.AttributeFloat32},
	}
	schemaEntries, schemaSize, err := model.AttributeSchemaLayout(schema)
	if err != nil {
		t.Fatalf("schema layout: %v", err)
	}
	mkAttrs := func(intensity uint16, amp float32) model.AttributeValues {
		blob := make(model.AttributeValues, schemaSize)
		v := model.NewAttributeView(schemaEntries, blob)
		if err := v.SetValue(0, intensity); err != nil {
			t.Fatalf("set intensity: %v", err)
		}
		if err := v.SetValue(1, amp); err != nil {
			t.Fatalf("set amplification: %v", err)
		}
		return blob
	}
	type wanted struct {
		intensity uint16
		amp       float32
	}
	wantSet := map[wanted]bool{
		{7, 1.5}:  true,
		{3, -2.5}: true,
		{9, 0.5}:  true,
	}
	reader := &pc.MockLasReader{
		CRS:    "EPSG:4978",
		Schema: schema,
		Pts: []geom.Point64{
			{Vector: model.Vector{X: 4399228.288985, Y: 855784.797006, Z: 0}, Attributes: mkAttrs(7, 1.5)},
			{Vector: model.Vector{X: 4399238.288985, Y: 855784.797006, Z: 0}, Attributes: mkAttrs(3, -2.5)},
			{Vector: model.Vector{X: 4399228.288985, Y: 855794.797006, Z: 0}, Attributes: mkAttrs(9, 0.5)},
		},
	}

	result, err := loader.Run(reader, context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(result.tempFilePath) })

	if len(result.attrSummaries) != 3 {
		t.Fatalf("expected 3 summaries, got %d", len(result.attrSummaries))
	}
	intensitySummary := result.attrSummaries[0]
	if intensitySummary.Name != "intensity" || intensitySummary.Type != model.AttributeUint16 || intensitySummary.SkipIncomplete {
		t.Errorf("unexpected intensity summary: %+v", intensitySummary)
	}
	if intensitySummary.Min != uint16(3) || intensitySummary.Max != uint16(9) {
		t.Errorf("intensity min/max: got %v/%v want 3/9", intensitySummary.Min, intensitySummary.Max)
	}
	ampSummary := result.attrSummaries[1]
	if ampSummary.Type != model.AttributeFloat32 || ampSummary.SkipIncomplete {
		t.Errorf("unexpected amplification summary: %+v", ampSummary)
	}
	if ampSummary.Min != float32(-2.5) || ampSummary.Max != float32(1.5) {
		t.Errorf("amplification min/max: got %v/%v want -2.5/1.5", ampSummary.Min, ampSummary.Max)
	}
	missingSummary := result.attrSummaries[2]
	if !missingSummary.SkipIncomplete || missingSummary.Type != "" {
		t.Errorf("unexpected summary for absent attribute: %+v", missingSummary)
	}

	// Round-trip through the io factory, which was fed the summaries during Run.
	reader2, err := ioFactory.NewReader(result.tempFilePath)
	if err != nil {
		t.Fatalf("failed to open temp file: %v", err)
	}
	defer reader2.Close()
	if reader2.NumPoints() != 3 {
		t.Fatalf("expected 3 points in temp file, got %d", reader2.NumPoints())
	}
	buf, readErr := reader2.NextBatch(make([]model.Point, 0, 3))
	if readErr != nil && readErr != io.EOF {
		t.Fatalf("failed to read points from temp file: %v", readErr)
	}
	if len(buf) != 3 {
		t.Fatalf("expected 3 points, got %d", len(buf))
	}
	entries, size := model.AttributeLayout(result.attrSummaries)
	if size != 6 || len(entries) != 2 {
		t.Fatalf("unexpected layout: %d entries, %d bytes", len(entries), size)
	}
	for i, pt := range buf {
		if len(pt.Attributes) != size {
			t.Fatalf("point %d: attribute values are %d bytes, want %d", i, len(pt.Attributes), size)
		}
		iv, err := model.DecodeAttributeValue(pt.Attributes[entries[0].Offset:], entries[0].Type)
		if err != nil {
			t.Fatalf("decode intensity: %v", err)
		}
		av, err := model.DecodeAttributeValue(pt.Attributes[entries[1].Offset:], entries[1].Type)
		if err != nil {
			t.Fatalf("decode amplification: %v", err)
		}
		got := wanted{intensity: iv.(uint16), amp: av.(float32)}
		if !wantSet[got] {
			t.Errorf("point %d: unexpected attribute values %+v", i, got)
		}
		delete(wantSet, got)
	}
	if len(wantSet) != 0 {
		t.Errorf("missing attribute value combinations: %v", wantSet)
	}
}

// classificationFilter drops points whose "classification" attribute matches
// the discard value, exercising attribute-aware mutators.
type classificationFilter struct {
	discard uint8
}

func (m *classificationFilter) RequiredAttributes() model.Attributes {
	return model.NewAttributes("classification")
}

func (m *classificationFilter) MutateChunk(chunk mutator.PointChunk, t model.Transform) []model.Point {
	out := chunk.Points[:0]
	for i, pt := range chunk.Points {
		attrs := chunk.AttributeView(i)
		if attrIndex := attrs.Index("classification"); attrIndex >= 0 {
			if v, err := attrs.Value(attrIndex); err == nil {
				if c, ok := v.(uint8); ok && c == m.discard {
					continue
				}
			}
		}
		out = append(out, pt)
	}
	return out
}

func (m *classificationFilter) Close() error {
	return nil
}

type recordingMutator struct {
	mu         sync.Mutex
	chunkSizes []int
}

func (m *recordingMutator) RequiredAttributes() model.Attributes {
	return nil
}

func (m *recordingMutator) MutateChunk(chunk mutator.PointChunk, t model.Transform) []model.Point {
	m.mu.Lock()
	m.chunkSizes = append(m.chunkSizes, len(chunk.Points))
	m.mu.Unlock()
	return chunk.Points
}

func (m *recordingMutator) Close() error {
	return nil
}

func (m *recordingMutator) snapshot() []int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]int(nil), m.chunkSizes...)
}

// TestReservoirLoader_AttributeAwareMutator verifies that mutators receive the
// reader-provided attributes and can filter points based on them.
func TestReservoirLoader_AttributeAwareMutator(t *testing.T) {
	conv := test.GetTestCoordinateConverterFactory()
	ioFactory := NewFileIoFactory()
	requested := model.NewAttributes("classification")
	loader := NewReservoirLoader(conv, &classificationFilter{discard: 7}, 100, 2, t.TempDir(), ioFactory, requested)

	mkAttrs := func(class uint8) model.AttributeValues {
		return model.AttributeValues{class}
	}
	reader := &pc.MockLasReader{
		CRS:    "EPSG:4978",
		Schema: []model.AttributeDescriptor{{Name: "classification", Type: model.AttributeUint8}},
		Pts: []geom.Point64{
			{Vector: model.Vector{X: 4399228.288985, Y: 855784.797006, Z: 0}, Attributes: mkAttrs(2)},
			{Vector: model.Vector{X: 4399238.288985, Y: 855784.797006, Z: 0}, Attributes: mkAttrs(7)}, // dropped
			{Vector: model.Vector{X: 4399228.288985, Y: 855794.797006, Z: 0}, Attributes: mkAttrs(5)},
			{Vector: model.Vector{X: 4399230.288985, Y: 855786.797006, Z: 0}, Attributes: mkAttrs(7)}, // dropped
		},
	}

	result, err := loader.Run(reader, context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(result.tempFilePath) })

	if result.totalPoints != 2 {
		t.Fatalf("expected 2 points after filtering, got %d", result.totalPoints)
	}
	summary := result.attrSummaries[0]
	if summary.Min != uint8(2) || summary.Max != uint8(5) {
		t.Errorf("classification min/max should reflect only kept points: got %v/%v want 2/5", summary.Min, summary.Max)
	}
}

func TestReservoirLoader_UsesMutatorForWorkerBatches(t *testing.T) {
	conv := test.GetTestCoordinateConverterFactory()
	ioFactory := NewFileIoFactory()
	recorder := &recordingMutator{}
	loader := NewReservoirLoader(conv, recorder, 100, 1, t.TempDir(), ioFactory, nil)

	reader := &pc.MockLasReader{
		CRS: "EPSG:4978",
		Pts: []geom.Point64{
			{Vector: model.Vector{X: 4399228.288985, Y: 855784.797006, Z: 0}},
			{Vector: model.Vector{X: 4399238.288985, Y: 855784.797006, Z: 0}},
			{Vector: model.Vector{X: 4399248.288985, Y: 855784.797006, Z: 0}},
			{Vector: model.Vector{X: 4399258.288985, Y: 855784.797006, Z: 0}},
			{Vector: model.Vector{X: 4399268.288985, Y: 855784.797006, Z: 0}},
		},
	}

	result, err := loader.Run(reader, context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(result.tempFilePath) })

	if result.totalPoints != 5 {
		t.Fatalf("expected 5 points, got %d", result.totalPoints)
	}
	chunkSizes := recorder.snapshot()
	hasWorkerBatch := false
	for _, size := range chunkSizes {
		if size > 1 {
			hasWorkerBatch = true
			break
		}
	}
	if !hasWorkerBatch {
		t.Fatalf("chunk sizes = %v, want at least one worker batch larger than one point", chunkSizes)
	}
}
