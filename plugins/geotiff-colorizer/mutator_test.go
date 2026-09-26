package geotiffcolorizer

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/mfbonfigli/gotiler/v3/tiler/model"
	"github.com/mfbonfigli/gotiler/v3/tiler/mutator"
)

var _ mutator.Mutator = (*Colorizer)(nil)

func mutateOne(c *Colorizer, pt model.Point, attrs model.AttributeView, localToGlobal model.Transform) (model.Point, bool) {
	points := c.MutateChunk(mutator.PointChunk{Points: []model.Point{pt}}, localToGlobal)
	if len(points) == 0 {
		return model.Point{}, false
	}
	return points[0], true
}

type converterCall struct {
	source string
	target string
	coord  model.Vector
}

type flatConverterCall struct {
	source string
	target string
	coords []float64
}

type fakeConverter struct {
	mu        sync.Mutex
	calls     []converterCall
	flatCalls []flatConverterCall
	out       model.Vector
	flatOut   []model.Vector
	err       error
	flatErr   error
}

func (f *fakeConverter) Transform(sourceCRS string, targetCRS string, coord model.Vector) (model.Vector, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, converterCall{source: sourceCRS, target: targetCRS, coord: coord})
	if f.err != nil {
		return model.Vector{}, f.err
	}
	return f.out, nil
}

func (f *fakeConverter) TransformFlat(sourceCRS string, targetCRS string, flatCoords []float64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.flatCalls = append(f.flatCalls, flatConverterCall{
		source: sourceCRS,
		target: targetCRS,
		coords: append([]float64(nil), flatCoords...),
	})
	if f.flatErr != nil {
		return f.flatErr
	}
	for i := 0; i*3+2 < len(flatCoords); i++ {
		out := f.out
		if i < len(f.flatOut) {
			out = f.flatOut[i]
		}
		offset := i * 3
		flatCoords[offset] = out.X
		flatCoords[offset+1] = out.Y
		flatCoords[offset+2] = out.Z
	}
	return nil
}

func (f *fakeConverter) lastCall(t *testing.T) converterCall {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		t.Fatal("converter was not called")
	}
	return f.calls[len(f.calls)-1]
}

func (f *fakeConverter) lastFlatCall(t *testing.T) flatConverterCall {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.flatCalls) == 0 {
		t.Fatal("converter flat transform was not called")
	}
	return f.flatCalls[len(f.flatCalls)-1]
}

func (f *fakeConverter) pointCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

type pointOnlySequenceConverter struct {
	mu    sync.Mutex
	calls []converterCall
	out   []model.Vector
}

func (c *pointOnlySequenceConverter) Transform(sourceCRS string, targetCRS string, coord model.Vector) (model.Vector, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, converterCall{source: sourceCRS, target: targetCRS, coord: coord})
	if len(c.out) == 0 {
		return model.Vector{}, nil
	}
	i := len(c.calls) - 1
	if i >= len(c.out) {
		i = len(c.out) - 1
	}
	return c.out[i], nil
}

func (c *pointOnlySequenceConverter) pointCallCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.calls)
}

func TestColorizerRequiresNoAttributes(t *testing.T) {
	ortho := testColorizerOrthophoto(t)
	converter := &fakeConverter{out: model.Vector{X: 115, Y: 185}}
	colorizer, err := NewColorizer(ortho, converter)
	if err != nil {
		t.Fatalf("NewColorizer: %v", err)
	}
	if got := colorizer.RequiredAttributes(); len(got) != 0 {
		t.Fatalf("RequiredAttributes = %v, want none", got)
	}
}

func TestColorizerRequiresConverterForDifferentCRS(t *testing.T) {
	_, err := NewColorizer(testColorizerOrthophoto(t), nil)
	if err == nil {
		t.Fatal("NewColorizer succeeded without converter for non-EPSG:4978 orthophoto")
	}
}

func TestColorizerMutateAppliesOrthophotoColor(t *testing.T) {
	ortho := testColorizerOrthophoto(t)
	converter := &fakeConverter{out: model.Vector{X: 115, Y: 185}}
	colorizer, err := NewColorizer(ortho, converter, WithSamplingMode(SampleNearest))
	if err != nil {
		t.Fatalf("NewColorizer: %v", err)
	}

	pt := model.Point{X: 1, Y: 2, Z: 3, R: 7, G: 8, B: 9}
	got, keep := mutateOne(colorizer, pt, model.AttributeView{}, model.IdentityTransform)
	if !keep {
		t.Fatal("Mutate dropped point")
	}
	if got.R != 100 || got.G != 110 || got.B != 120 {
		t.Fatalf("color = (%d,%d,%d), want (100,110,120)", got.R, got.G, got.B)
	}
	if stats := colorizer.Stats(); stats.Colored != 1 || stats.Missing != 0 || stats.ConversionErrors != 0 {
		t.Fatalf("Stats = %+v, want one colored point", stats)
	}

	call := converter.lastFlatCall(t)
	if call.source != epsg4978CRS || call.target != "EPSG:32632" {
		t.Fatalf("converter CRS = %q -> %q, want EPSG:4978 -> EPSG:32632", call.source, call.target)
	}
	if len(call.coords) != 3 || call.coords[0] != 1 || call.coords[1] != 2 || call.coords[2] != 3 {
		t.Fatalf("converter coords = %+v, want local identity point", call.coords)
	}
}

func TestColorizerMutateUsesLocalToGlobalTransform(t *testing.T) {
	ortho := testColorizerOrthophoto(t)
	converter := &fakeConverter{out: model.Vector{X: 115, Y: 185}}
	colorizer, err := NewColorizer(ortho, converter, WithSamplingMode(SampleNearest))
	if err != nil {
		t.Fatalf("NewColorizer: %v", err)
	}
	localToGlobal := model.NewTransform([4][4]float64{
		{1, 0, 0, 10},
		{0, 1, 0, 20},
		{0, 0, 1, 30},
		{0, 0, 0, 1},
	})

	_, keep := mutateOne(colorizer, model.Point{X: 1, Y: 2, Z: 3}, model.AttributeView{}, localToGlobal)
	if !keep {
		t.Fatal("Mutate dropped point")
	}
	call := converter.lastFlatCall(t)
	if len(call.coords) != 3 || call.coords[0] != 11 || call.coords[1] != 22 || call.coords[2] != 33 {
		t.Fatalf("converter coords = %+v, want local-to-global point", call.coords)
	}
}

func TestColorizerKeepsPointWhenSampleTransparent(t *testing.T) {
	ortho := testTransparentColorizerOrthophoto(t)
	converter := &fakeConverter{out: model.Vector{X: 115, Y: 185}}
	colorizer, err := NewColorizer(ortho, converter, WithSamplingMode(SampleNearest))
	if err != nil {
		t.Fatalf("NewColorizer: %v", err)
	}

	pt := model.Point{X: 1, Y: 2, Z: 3, R: 7, G: 8, B: 9}
	got, keep := mutateOne(colorizer, pt, model.AttributeView{}, model.IdentityTransform)
	if !keep {
		t.Fatal("Mutate dropped point")
	}
	if got.R != pt.R || got.G != pt.G || got.B != pt.B {
		t.Fatalf("color = (%d,%d,%d), want original (%d,%d,%d)", got.R, got.G, got.B, pt.R, pt.G, pt.B)
	}
	if stats := colorizer.Stats(); stats.Transparent != 1 || stats.Colored != 0 {
		t.Fatalf("Stats = %+v, want one transparent point", stats)
	}
}

func TestColorizerMutateChunkUsesFlatConverter(t *testing.T) {
	ortho := testColorizerOrthophoto(t)
	converter := &fakeConverter{flatOut: []model.Vector{
		{X: 105, Y: 195},
		{X: 115, Y: 195},
		{X: 115, Y: 185},
	}}
	colorizer, err := NewColorizer(ortho, converter, WithSamplingMode(SampleNearest))
	if err != nil {
		t.Fatalf("NewColorizer: %v", err)
	}
	localToGlobal := model.NewTransform([4][4]float64{
		{1, 0, 0, 10},
		{0, 1, 0, 20},
		{0, 0, 1, 30},
		{0, 0, 0, 1},
	})

	points := []model.Point{
		{X: 1, Y: 2, Z: 3},
		{X: 4, Y: 5, Z: 6},
		{X: 7, Y: 8, Z: 9},
	}
	got := colorizer.MutateChunk(mutator.PointChunk{Points: points}, localToGlobal)
	if len(got) != 3 {
		t.Fatalf("MutateChunk returned %d points, want 3", len(got))
	}
	want := [][3]uint8{
		{10, 20, 30},
		{40, 50, 60},
		{100, 110, 120},
	}
	for i, pt := range got {
		if pt.R != want[i][0] || pt.G != want[i][1] || pt.B != want[i][2] {
			t.Fatalf("point %d color = (%d,%d,%d), want %v", i, pt.R, pt.G, pt.B, want[i])
		}
	}
	if converter.pointCallCount() != 0 {
		t.Fatalf("point Transform calls = %d, want 0", converter.pointCallCount())
	}
	call := converter.lastFlatCall(t)
	if call.source != epsg4978CRS || call.target != "EPSG:32632" {
		t.Fatalf("flat converter CRS = %q -> %q, want EPSG:4978 -> EPSG:32632", call.source, call.target)
	}
	wantFlat := []float64{11, 22, 33, 14, 25, 36, 17, 28, 39}
	for i, want := range wantFlat {
		if call.coords[i] != want {
			t.Fatalf("flat coord %d = %v, want %v", i, call.coords[i], want)
		}
	}
	if stats := colorizer.Stats(); stats.Chunks != 1 || stats.FlatConversions != 1 || stats.Colored != 3 {
		t.Fatalf("Stats = %+v, want one flat converted chunk and three colored points", stats)
	}
}

func TestColorizerMutateChunkFallsBackToPointConverter(t *testing.T) {
	ortho := testColorizerOrthophoto(t)
	converter := &pointOnlySequenceConverter{out: []model.Vector{
		{X: 105, Y: 195},
		{X: 115, Y: 185},
	}}
	colorizer, err := NewColorizer(ortho, converter, WithSamplingMode(SampleNearest))
	if err != nil {
		t.Fatalf("NewColorizer: %v", err)
	}

	got := colorizer.MutateChunk(mutator.PointChunk{Points: []model.Point{{}, {}}}, model.IdentityTransform)
	if len(got) != 2 {
		t.Fatalf("MutateChunk returned %d points, want 2", len(got))
	}
	if got[0].R != 10 || got[0].G != 20 || got[0].B != 30 {
		t.Fatalf("point 0 color = (%d,%d,%d), want (10,20,30)", got[0].R, got[0].G, got[0].B)
	}
	if got[1].R != 100 || got[1].G != 110 || got[1].B != 120 {
		t.Fatalf("point 1 color = (%d,%d,%d), want (100,110,120)", got[1].R, got[1].G, got[1].B)
	}
	if converter.pointCallCount() != 2 {
		t.Fatalf("point Transform calls = %d, want 2", converter.pointCallCount())
	}
	if stats := colorizer.Stats(); stats.Chunks != 1 || stats.FlatConversions != 0 || stats.Colored != 2 {
		t.Fatalf("Stats = %+v, want pointwise converted chunk", stats)
	}
}

func TestColorizerKeepsPointWhenSampleMissing(t *testing.T) {
	ortho := testColorizerOrthophoto(t)
	converter := &fakeConverter{out: model.Vector{X: -100, Y: -100}}
	colorizer, err := NewColorizer(ortho, converter, WithSamplingMode(SampleNearest))
	if err != nil {
		t.Fatalf("NewColorizer: %v", err)
	}

	pt := model.Point{X: 1, Y: 2, Z: 3, R: 7, G: 8, B: 9}
	got, keep := mutateOne(colorizer, pt, model.AttributeView{}, model.IdentityTransform)
	if !keep {
		t.Fatal("Mutate dropped point")
	}
	if got.R != pt.R || got.G != pt.G || got.B != pt.B {
		t.Fatalf("color = (%d,%d,%d), want original (%d,%d,%d)", got.R, got.G, got.B, pt.R, pt.G, pt.B)
	}
	if stats := colorizer.Stats(); stats.Missing != 1 || stats.Colored != 0 {
		t.Fatalf("Stats = %+v, want one missing point", stats)
	}
}

func TestColorizerDropPolicyDropsWhenSampleMissing(t *testing.T) {
	ortho := testColorizerOrthophoto(t)
	converter := &fakeConverter{out: model.Vector{X: -100, Y: -100}}
	colorizer, err := NewColorizer(ortho, converter, WithNoColorPolicy(NoColorDropPoint))
	if err != nil {
		t.Fatalf("NewColorizer: %v", err)
	}

	_, keep := mutateOne(colorizer, model.Point{}, model.AttributeView{}, model.IdentityTransform)
	if keep {
		t.Fatal("Mutate kept point, want drop on missing sample")
	}
}

func TestColorizerRecordsConversionError(t *testing.T) {
	ortho := testColorizerOrthophoto(t)
	conversionErr := errors.New("no projection")
	colorizer, err := NewColorizer(ortho, &fakeConverter{err: conversionErr, flatErr: conversionErr})
	if err != nil {
		t.Fatalf("NewColorizer: %v", err)
	}

	pt := model.Point{R: 7, G: 8, B: 9}
	got, keep := mutateOne(colorizer, pt, model.AttributeView{}, model.IdentityTransform)
	if !keep {
		t.Fatal("Mutate dropped point")
	}
	if got.R != pt.R || got.G != pt.G || got.B != pt.B {
		t.Fatalf("color = (%d,%d,%d), want original", got.R, got.G, got.B)
	}
	if stats := colorizer.Stats(); stats.ConversionErrors != 1 || stats.Colored != 0 {
		t.Fatalf("Stats = %+v, want one conversion error", stats)
	}
	if !errors.Is(colorizer.LastError(), conversionErr) {
		t.Fatalf("LastError = %v, want %v", colorizer.LastError(), conversionErr)
	}
}

func TestColorizerDoesNotRequireConverterWhenSamplingCRSIsGlobal(t *testing.T) {
	ortho := testColorizerOrthophoto4978(t)
	colorizer, err := NewColorizer(ortho, nil, WithSamplingMode(SampleNearest))
	if err != nil {
		t.Fatalf("NewColorizer: %v", err)
	}

	got, keep := mutateOne(colorizer, model.Point{X: 115, Y: 185}, model.AttributeView{}, model.IdentityTransform)
	if !keep {
		t.Fatal("Mutate dropped point")
	}
	if got.R != 100 || got.G != 110 || got.B != 120 {
		t.Fatalf("color = (%d,%d,%d), want (100,110,120)", got.R, got.G, got.B)
	}
}

func TestColorizerFromFile(t *testing.T) {
	data := testColorizerGeoTIFF(t, 32632, 1)
	filename := filepath.Join(t.TempDir(), "ortho.tif")
	if err := os.WriteFile(filename, data, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	converter := &fakeConverter{out: model.Vector{X: 115, Y: 185}}
	colorizer, err := NewColorizerFromFile(filename, converter, WithSamplingMode(SampleNearest))
	if err != nil {
		t.Fatalf("NewColorizerFromFile: %v", err)
	}
	defer colorizer.Close()
	got, keep := mutateOne(colorizer, model.Point{}, model.AttributeView{}, model.IdentityTransform)
	if !keep {
		t.Fatal("Mutate dropped point")
	}
	if got.R != 100 || got.G != 110 || got.B != 120 {
		t.Fatalf("color = (%d,%d,%d), want (100,110,120)", got.R, got.G, got.B)
	}
}

func testColorizerOrthophoto(t *testing.T) *Orthophoto {
	t.Helper()
	ortho, err := OpenOrthophoto("colorizer.tif", testColorizerGeoTIFF(t, 32632, 1))
	if err != nil {
		t.Fatalf("OpenOrthophoto: %v", err)
	}
	return ortho
}

func testTransparentColorizerOrthophoto(t *testing.T) *Orthophoto {
	t.Helper()
	ortho, err := OpenOrthophoto("colorizer-alpha.tif", buildTestGeoTIFF(t, binary.LittleEndian, testTIFFConfig{
		width:      2,
		height:     2,
		samples:    4,
		bits:       []uint16{8, 8, 8, 8},
		photo:      tiffPhotometricRGB,
		compress:   tiffCompressionNone,
		blocks:     [][]byte{{10, 20, 30, 255, 40, 50, 60, 255, 70, 80, 90, 255, 100, 110, 120, 0}},
		rowsPer:    2,
		scale:      []float64{10, 10, 0},
		tiepoint:   []float64{0, 0, 0, 100, 200, 0},
		extra:      []uint16{1},
		modelType:  1,
		rasterType: 1,
		epsg:       32632,
	}))
	if err != nil {
		t.Fatalf("OpenOrthophoto: %v", err)
	}
	return ortho
}

func testColorizerOrthophoto4978(t *testing.T) *Orthophoto {
	t.Helper()
	ortho, err := OpenOrthophoto("colorizer-4978.tif", testColorizerGeoTIFF(t, 4978, 3))
	if err != nil {
		t.Fatalf("OpenOrthophoto: %v", err)
	}
	return ortho
}

func testColorizerGeoTIFF(t *testing.T, epsg uint16, modelType uint16) []byte {
	t.Helper()
	return buildTestGeoTIFF(t, binary.LittleEndian, testTIFFConfig{
		width:      2,
		height:     2,
		samples:    3,
		bits:       []uint16{8, 8, 8},
		photo:      tiffPhotometricRGB,
		compress:   tiffCompressionNone,
		blocks:     [][]byte{{10, 20, 30, 40, 50, 60, 70, 80, 90, 100, 110, 120}},
		rowsPer:    2,
		scale:      []float64{10, 10, 0},
		tiepoint:   []float64{0, 0, 0, 100, 200, 0},
		modelType:  modelType,
		rasterType: 1,
		epsg:       epsg,
	})
}
