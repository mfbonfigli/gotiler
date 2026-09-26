package mutator

import (
	"math"
	"reflect"
	"testing"

	"github.com/mfbonfigli/gotiler/v3/tiler/geom"
	"github.com/mfbonfigli/gotiler/v3/tiler/model"
)

func mutateOneOnWrite(m WriteMutator, pt model.Point, attrs testAttributeData, t model.Transform) (model.Point, bool) {
	pt.Attributes = attrs.values
	points := m.MutateChunkOnWrite(PointChunk{
		Points:          []model.Point{pt},
		AttributeLayout: attrs.layout,
	}, t)
	if len(points) == 0 {
		return model.Point{}, false
	}
	return points[0], true
}

func observeOne(t *testing.T, c *Colorizer, attrs testAttributeData) {
	t.Helper()
	pt := geom.NewPoint(0, 0, 0, 1, 2, 3)
	got, keep := mutateOne(c, pt, attrs, model.IdentityTransform)
	if !keep {
		t.Fatal("expected observed point to be kept")
	}
	if got.R != 1 || got.G != 2 || got.B != 3 {
		t.Fatalf("expected MutateChunk to leave colors unchanged, got %+v", got)
	}
}

// fixedRangeGradient returns a linear black-to-white gradient pinned to the
// given absolute range.
func fixedRangeGradient(min, max float64) ColorGradientScale {
	gradient := testLinearGradient()
	gradient.FixedRange = true
	gradient.RangeMin = min
	gradient.RangeMax = max
	return gradient
}

func TestRegisterColorGradientUsesExactAliasAndCopiesDefinition(t *testing.T) {
	gradient := ColorGradientScale{
		Nums:              []float64{0, 1},
		Colors:            []Color{{R: 1}, {R: 2}},
		InterpolationMode: GradientInterpolationFlat,
	}
	if err := RegisterColorGradient(" Test Scale ", gradient); err != nil {
		t.Fatalf("RegisterColorGradient: %v", err)
	}
	gradient.Colors[0].R = 99

	got, ok := RegisteredColorGradient(" Test Scale ")
	if !ok {
		t.Fatal("expected registered gradient")
	}
	if got.Colors[0].R != 1 {
		t.Fatalf("expected registry to copy gradient, got %v", got.Colors[0].R)
	}
	got.Colors[0].R = 42
	again, _ := RegisteredColorGradient(" Test Scale ")
	if again.Colors[0].R != 1 {
		t.Fatalf("expected lookup to return a copy, got %v", again.Colors[0].R)
	}
	if _, ok := RegisteredColorGradient("testscale"); ok {
		t.Fatal("expected aliases to match exactly")
	}
}

func TestRegisterColorGradientRejectsInvalidDefinitions(t *testing.T) {
	cases := []ColorGradientScale{
		{Nums: []float64{0}, Colors: []Color{{}}, InterpolationMode: GradientInterpolationFlat},
		{Nums: []float64{0, 1}, Colors: []Color{{}}, InterpolationMode: GradientInterpolationFlat},
		{Nums: []float64{0.1, 1}, Colors: []Color{{}, {}}, InterpolationMode: GradientInterpolationFlat},
		{Nums: []float64{0, 0.5}, Colors: []Color{{}, {}}, InterpolationMode: GradientInterpolationFlat},
		{Nums: []float64{0, 0.5, 0.5, 1}, Colors: []Color{{}, {}, {}, {}}, InterpolationMode: GradientInterpolationFlat},
		{Nums: []float64{0, 1}, Colors: []Color{{}, {}}, InterpolationMode: GradientInterpolationMode(99)},
		{Nums: []float64{0, 1}, Colors: []Color{{}, {}}, FixedRange: true, RangeMin: 10, RangeMax: 10},
		{Nums: []float64{0, 1}, Colors: []Color{{}, {}}, FixedRange: true, RangeMin: 20, RangeMax: 10},
		{Nums: []float64{0, 1}, Colors: []Color{{}, {}}, FixedRange: true, RangeMin: math.Inf(-1), RangeMax: 10},
	}
	for i, c := range cases {
		if err := RegisterColorGradient("bad-gradient", c); err == nil {
			t.Fatalf("case %d: expected invalid gradient to be rejected", i)
		}
	}
}

func TestRegisteredGradientKeepsFixedRange(t *testing.T) {
	if err := RegisterColorGradient("fixed-range-test", fixedRangeGradient(-5, 5)); err != nil {
		t.Fatalf("RegisterColorGradient: %v", err)
	}
	got, ok := RegisteredColorGradient("fixed-range-test")
	if !ok {
		t.Fatal("expected registered gradient")
	}
	if !got.FixedRange || got.RangeMin != -5 || got.RangeMax != 5 {
		t.Fatalf("expected fixed range to survive registration, got %+v", got)
	}
}

func TestNewColorizerRequiredAttributes(t *testing.T) {
	c, err := NewColorizerWithGradient(" IntensitY ", testLinearGradient())
	if err != nil {
		t.Fatalf("NewColorizerWithGradient: %v", err)
	}
	got := c.RequiredAttributes()
	if len(got) != 1 || !got.Has(model.AttrIntensity) {
		t.Fatalf("expected intensity required attribute, got %v", got)
	}
}

func TestNewColorizerPointCoordinateRequiresNoAttributes(t *testing.T) {
	c, err := NewColorizerWithGradient(" X ", testLinearGradient())
	if err != nil {
		t.Fatalf("NewColorizerWithGradient: %v", err)
	}
	if got := c.RequiredAttributes(); len(got) != 0 {
		t.Fatalf("expected no required attributes for point coordinate colorizer, got %v", got)
	}
}

func TestNewColorizerRejectsInvalidInputs(t *testing.T) {
	if _, err := NewColorizerWithGradient("  ", testLinearGradient()); err == nil {
		t.Fatal("expected empty attribute name to be rejected")
	}
	if _, err := NewColorizerWithGradient("intensity", ColorGradientScale{}); err == nil {
		t.Fatal("expected invalid gradient to be rejected")
	}
	if _, err := NewColorizer("intensity", "no-such-gradient"); err == nil {
		t.Fatal("expected unknown gradient alias to be rejected")
	}
}

func TestColorizerFlatInterpolation(t *testing.T) {
	gradient := ColorGradientScale{
		Nums: []float64{0, 0.3, 0.6, 1},
		Colors: []Color{
			{R: 255, G: 0, B: 0},
			{R: 0, G: 0, B: 255},
			{R: 255, G: 255, B: 0},
			{R: 0, G: 255, B: 0},
		},
		InterpolationMode: GradientInterpolationFlat,
		FixedRange:        true,
		RangeMin:          10,
		RangeMax:          20,
	}
	c, err := NewColorizerWithGradient(model.AttrIntensity, gradient)
	if err != nil {
		t.Fatalf("NewColorizerWithGradient: %v", err)
	}

	cases := []struct {
		value uint16
		want  Color
	}{
		{9, Color{R: 255}},
		{10, Color{R: 255}},
		{12, Color{R: 255}},
		{13, Color{B: 255}},
		{15, Color{B: 255}},
		{16, Color{R: 255, G: 255}},
		{20, Color{G: 255}},
		{21, Color{G: 255}},
	}

	for _, tc := range cases {
		pt, keep := mutateOneOnWrite(c, geom.NewPoint(0, 0, 0, 1, 2, 3), colorizerView(model.AttrIntensity, model.AttributeUint16, tc.value), model.IdentityTransform)
		if !keep {
			t.Fatalf("value %d: expected point to be kept", tc.value)
		}
		got := Color{R: pt.R, G: pt.G, B: pt.B}
		if got != tc.want {
			t.Fatalf("value %d: got color %+v want %+v", tc.value, got, tc.want)
		}
	}
}

func TestColorizerLinearInterpolation(t *testing.T) {
	c, err := NewColorizerWithGradient(model.AttrIntensity, fixedRangeGradient(0, 10))
	if err != nil {
		t.Fatalf("NewColorizerWithGradient: %v", err)
	}
	pt, keep := mutateOneOnWrite(c, geom.NewPoint(0, 0, 0, 1, 2, 3), colorizerView(model.AttrIntensity, model.AttributeFloat32, float32(5)), model.IdentityTransform)
	if !keep {
		t.Fatal("expected point to be kept")
	}
	got := Color{R: pt.R, G: pt.G, B: pt.B}
	want := Color{R: 128, G: 128, B: 128}
	if got != want {
		t.Fatalf("got color %+v want %+v", got, want)
	}
}

func TestColorizerFixedRangeUsesPointCoordinates(t *testing.T) {
	cases := []struct {
		attribute string
		point     model.Point
		want      Color
	}{
		{
			attribute: "x",
			point:     geom.NewPoint(0, 99, 99, 1, 2, 3),
			want:      Color{},
		},
		{
			attribute: "y",
			point:     geom.NewPoint(99, 5, 99, 1, 2, 3),
			want:      Color{R: 128, G: 128, B: 128},
		},
		{
			attribute: "z",
			point:     geom.NewPoint(99, 99, 10, 1, 2, 3),
			want:      Color{R: 255, G: 255, B: 255},
		},
	}
	for _, tc := range cases {
		c, err := NewColorizerWithGradient(tc.attribute, fixedRangeGradient(0, 10))
		if err != nil {
			t.Fatalf("%s: NewColorizerWithGradient: %v", tc.attribute, err)
		}
		pt, keep := mutateOneOnWrite(c, tc.point, testAttributeData{}, model.IdentityTransform)
		if !keep {
			t.Fatalf("%s: expected point to be kept", tc.attribute)
		}
		got := Color{R: pt.R, G: pt.G, B: pt.B}
		if got != tc.want {
			t.Fatalf("%s: got color %+v want %+v", tc.attribute, got, tc.want)
		}
	}
}

func TestColorizerObservesPointCoordinates(t *testing.T) {
	c, err := NewColorizerWithGradient("z", testLinearGradient())
	if err != nil {
		t.Fatalf("NewColorizerWithGradient: %v", err)
	}
	for _, z := range []float32{0, 10} {
		pt := geom.NewPoint(0, 0, z, 1, 2, 3)
		if _, keep := mutateOne(c, pt, testAttributeData{}, model.IdentityTransform); !keep {
			t.Fatal("expected observed point to be kept")
		}
	}
	pt, keep := mutateOneOnWrite(c, geom.NewPoint(0, 0, 10, 1, 2, 3), testAttributeData{}, model.IdentityTransform)
	if !keep {
		t.Fatal("expected point to be kept")
	}
	got := Color{R: pt.R, G: pt.G, B: pt.B}
	want := Color{R: 255, G: 255, B: 255}
	if got != want {
		t.Fatalf("got color %+v want %+v", got, want)
	}
}

func TestColorizerColorsWithObservedRange(t *testing.T) {
	c, err := NewColorizerWithGradient(model.AttrIntensity, testLinearGradient())
	if err != nil {
		t.Fatalf("NewColorizerWithGradient: %v", err)
	}
	observeOne(t, c, colorizerView(model.AttrIntensity, model.AttributeUint16, uint16(10)))
	observeOne(t, c, colorizerView(model.AttrIntensity, model.AttributeUint16, uint16(20)))

	cases := []struct {
		value uint16
		want  Color
	}{
		{10, Color{R: 0, G: 0, B: 0}},
		{15, Color{R: 128, G: 128, B: 128}},
		{20, Color{R: 255, G: 255, B: 255}},
	}
	for _, tc := range cases {
		pt, keep := mutateOneOnWrite(c, geom.NewPoint(0, 0, 0, 1, 2, 3), colorizerView(model.AttrIntensity, model.AttributeUint16, tc.value), model.IdentityTransform)
		if !keep {
			t.Fatalf("value %d: expected point to be kept", tc.value)
		}
		got := Color{R: pt.R, G: pt.G, B: pt.B}
		if got != tc.want {
			t.Fatalf("value %d: got color %+v want %+v", tc.value, got, tc.want)
		}
	}
}

func TestColorizerUsesRegisteredGradientAlias(t *testing.T) {
	if err := RegisterColorGradient("Alias Gradient", fixedRangeGradient(0, 10)); err != nil {
		t.Fatalf("RegisterColorGradient: %v", err)
	}
	c, err := NewColorizer(model.AttrIntensity, "Alias Gradient")
	if err != nil {
		t.Fatalf("NewColorizer: %v", err)
	}
	pt, keep := mutateOneOnWrite(c, geom.NewPoint(0, 0, 0, 1, 2, 3), colorizerView(model.AttrIntensity, model.AttributeUint8, uint8(10)), model.IdentityTransform)
	if !keep {
		t.Fatal("expected point to be kept")
	}
	got := Color{R: pt.R, G: pt.G, B: pt.B}
	want := Color{R: 255, G: 255, B: 255}
	if got != want {
		t.Fatalf("got color %+v want %+v", got, want)
	}
}

func TestColorizerLASClassificationGradient(t *testing.T) {
	// las-classification declares a fixed 0-255 range: even when the observed
	// data covers a narrow class range, class codes must keep their absolute
	// colors instead of being rescaled over the observed range.
	c, err := NewColorizer(model.AttrClassification, "las-classification")
	if err != nil {
		t.Fatalf("NewColorizer: %v", err)
	}
	observeOne(t, c, colorizerView(model.AttrClassification, model.AttributeUint8, uint8(2)))
	observeOne(t, c, colorizerView(model.AttrClassification, model.AttributeUint8, uint8(6)))

	cases := []struct {
		class uint8
		want  Color
	}{
		{0, Color{R: 180, G: 180, B: 180}},
		{2, Color{R: 139, G: 100, B: 60}},
		{5, Color{R: 20, G: 120, B: 40}},
		{6, Color{R: 210, G: 70, B: 60}},
		{9, Color{R: 40, G: 120, B: 230}},
		{18, Color{R: 255, G: 60, B: 180}},
		{19, Color{R: 120, G: 120, B: 120}},
		{255, Color{R: 120, G: 120, B: 120}},
	}
	for _, tc := range cases {
		pt, keep := mutateOneOnWrite(c, geom.NewPoint(0, 0, 0, 1, 2, 3), colorizerView(model.AttrClassification, model.AttributeUint8, tc.class), model.IdentityTransform)
		if !keep {
			t.Fatalf("class %d: expected point to be kept", tc.class)
		}
		got := Color{R: pt.R, G: pt.G, B: pt.B}
		if got != tc.want {
			t.Fatalf("class %d: got color %+v want %+v", tc.class, got, tc.want)
		}
	}
}

func TestColorizerLeavesPointUnchangedWhenAttributeMissing(t *testing.T) {
	c, err := NewColorizerWithGradient(model.AttrIntensity, fixedRangeGradient(0, 10))
	if err != nil {
		t.Fatalf("NewColorizerWithGradient: %v", err)
	}
	input := geom.NewPoint(0, 0, 0, 10, 20, 30)
	attrs := colorizerView(model.AttrClassification, model.AttributeUint8, uint8(5))
	got, keep := mutateOneOnWrite(c, input, attrs, model.IdentityTransform)
	if !keep {
		t.Fatal("expected point to be kept")
	}
	want := input
	want.Attributes = attrs.values
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected unchanged point, got %+v want %+v", got, want)
	}
}

func TestColorizerPassesThroughWithoutObservations(t *testing.T) {
	c, err := NewColorizerWithGradient(model.AttrIntensity, testLinearGradient())
	if err != nil {
		t.Fatalf("NewColorizerWithGradient: %v", err)
	}
	// observed chunks carried a different attribute, so no value was seen
	observeOne(t, c, colorizerView(model.AttrClassification, model.AttributeUint8, uint8(2)))
	pt, keep := mutateOneOnWrite(c, geom.NewPoint(0, 0, 0, 10, 20, 30), colorizerView(model.AttrClassification, model.AttributeUint8, uint8(2)), model.IdentityTransform)
	if !keep {
		t.Fatal("expected point to be kept")
	}
	got := Color{R: pt.R, G: pt.G, B: pt.B}
	want := Color{R: 10, G: 20, B: 30}
	if got != want {
		t.Fatalf("expected unchanged colors, got %+v want %+v", got, want)
	}
}

func TestColorizerSingleValueMapsToMidpoint(t *testing.T) {
	c, err := NewColorizerWithGradient(model.AttrIntensity, testLinearGradient())
	if err != nil {
		t.Fatalf("NewColorizerWithGradient: %v", err)
	}
	observeOne(t, c, colorizerView(model.AttrIntensity, model.AttributeUint16, uint16(7)))
	pt, keep := mutateOneOnWrite(c, geom.NewPoint(0, 0, 0, 1, 2, 3), colorizerView(model.AttrIntensity, model.AttributeUint16, uint16(7)), model.IdentityTransform)
	if !keep {
		t.Fatal("expected point to be kept")
	}
	got := Color{R: pt.R, G: pt.G, B: pt.B}
	want := Color{R: 128, G: 128, B: 128}
	if got != want {
		t.Fatalf("got color %+v want %+v", got, want)
	}
}

func TestColorizerResetsOnNewRun(t *testing.T) {
	c, err := NewColorizerWithGradient(model.AttrIntensity, testLinearGradient())
	if err != nil {
		t.Fatalf("NewColorizerWithGradient: %v", err)
	}
	// run 1: range [0, 10]
	observeOne(t, c, colorizerView(model.AttrIntensity, model.AttributeUint16, uint16(0)))
	observeOne(t, c, colorizerView(model.AttrIntensity, model.AttributeUint16, uint16(10)))
	pt, _ := mutateOneOnWrite(c, geom.NewPoint(0, 0, 0, 1, 2, 3), colorizerView(model.AttrIntensity, model.AttributeUint16, uint16(10)), model.IdentityTransform)
	if (Color{R: pt.R, G: pt.G, B: pt.B}) != (Color{R: 255, G: 255, B: 255}) {
		t.Fatalf("run 1: got color %+v want white", pt)
	}

	// run 2: a read call after write calls resets the range to [100, 110]
	observeOne(t, c, colorizerView(model.AttrIntensity, model.AttributeUint16, uint16(100)))
	observeOne(t, c, colorizerView(model.AttrIntensity, model.AttributeUint16, uint16(110)))
	pt, _ = mutateOneOnWrite(c, geom.NewPoint(0, 0, 0, 1, 2, 3), colorizerView(model.AttrIntensity, model.AttributeUint16, uint16(100)), model.IdentityTransform)
	if (Color{R: pt.R, G: pt.G, B: pt.B}) != (Color{R: 0, G: 0, B: 0}) {
		t.Fatalf("run 2: got color %+v want black; stale range from run 1?", pt)
	}
	pt, _ = mutateOneOnWrite(c, geom.NewPoint(0, 0, 0, 1, 2, 3), colorizerView(model.AttrIntensity, model.AttributeUint16, uint16(110)), model.IdentityTransform)
	if (Color{R: pt.R, G: pt.G, B: pt.B}) != (Color{R: 255, G: 255, B: 255}) {
		t.Fatalf("run 2: got color %+v want white", pt)
	}
}

func TestColorizerFixedRangeNeedsNoObservation(t *testing.T) {
	c, err := NewColorizerWithGradient(model.AttrIntensity, fixedRangeGradient(0, 10))
	if err != nil {
		t.Fatalf("NewColorizerWithGradient: %v", err)
	}
	// no MutateChunk calls at all: the fixed range must still apply
	pt, keep := mutateOneOnWrite(c, geom.NewPoint(0, 0, 0, 1, 2, 3), colorizerView(model.AttrIntensity, model.AttributeUint16, uint16(5)), model.IdentityTransform)
	if !keep {
		t.Fatal("expected point to be kept")
	}
	got := Color{R: pt.R, G: pt.G, B: pt.B}
	want := Color{R: 128, G: 128, B: 128}
	if got != want {
		t.Fatalf("got color %+v want %+v", got, want)
	}
	// a later read call (new run) must not disturb the fixed mapping
	observeOne(t, c, colorizerView(model.AttrIntensity, model.AttributeUint16, uint16(1000)))
	pt, _ = mutateOneOnWrite(c, geom.NewPoint(0, 0, 0, 1, 2, 3), colorizerView(model.AttrIntensity, model.AttributeUint16, uint16(10)), model.IdentityTransform)
	if (Color{R: pt.R, G: pt.G, B: pt.B}) != (Color{R: 255, G: 255, B: 255}) {
		t.Fatalf("expected fixed range to persist across runs, got %+v", pt)
	}
}

func TestColorizerSupportsAllAttributeTypes(t *testing.T) {
	c, err := NewColorizerWithGradient("value", fixedRangeGradient(0, 1))
	if err != nil {
		t.Fatalf("NewColorizerWithGradient: %v", err)
	}
	cases := []struct {
		typ   model.AttributeType
		value any
	}{
		{model.AttributeInt8, int8(1)},
		{model.AttributeUint8, uint8(1)},
		{model.AttributeInt16, int16(1)},
		{model.AttributeUint16, uint16(1)},
		{model.AttributeInt32, int32(1)},
		{model.AttributeUint32, uint32(1)},
		{model.AttributeInt64, int64(1)},
		{model.AttributeUint64, uint64(1)},
		{model.AttributeBool, true},
		{model.AttributeFloat32, float32(1)},
		{model.AttributeFloat64, float64(1)},
	}
	for _, tc := range cases {
		pt, keep := mutateOneOnWrite(c, geom.NewPoint(0, 0, 0, 1, 2, 3), colorizerView("value", tc.typ, tc.value), model.IdentityTransform)
		if !keep {
			t.Fatalf("type %s: expected point to be kept", tc.typ)
		}
		got := Color{R: pt.R, G: pt.G, B: pt.B}
		want := Color{R: 255, G: 255, B: 255}
		if got != want {
			t.Fatalf("type %s: got color %+v want %+v", tc.typ, got, want)
		}
	}
}

func TestPipelineHasWriteMutators(t *testing.T) {
	if NewPipeline(NewZOffset(1)).HasWriteMutators() {
		t.Fatal("expected pipeline without write mutators to report false")
	}
	c, err := NewColorizerWithGradient(model.AttrIntensity, testLinearGradient())
	if err != nil {
		t.Fatalf("NewColorizerWithGradient: %v", err)
	}
	if !NewPipeline(NewZOffset(1), c).HasWriteMutators() {
		t.Fatal("expected pipeline with write mutators to report true")
	}
	if NewPipeline().HasWriteMutators() {
		t.Fatal("expected empty pipeline to report false")
	}
}

func TestPipelineMutateChunkOnWriteSkipsReadOnlyMutators(t *testing.T) {
	c, err := NewColorizerWithGradient(model.AttrIntensity, testLinearGradient())
	if err != nil {
		t.Fatalf("NewColorizerWithGradient: %v", err)
	}
	p := NewPipeline(NewZOffset(5), c)

	// load phase: ZOffset applies and the colorizer observes
	attrs := colorizerView(model.AttrIntensity, model.AttributeUint16, uint16(0))
	pt, keep := mutateOne(p, geom.NewPoint(0, 0, 1, 1, 2, 3), attrs, model.IdentityTransform)
	if !keep || pt.Z != 6 {
		t.Fatalf("expected read-time ZOffset to apply, got %+v", pt)
	}
	observeOne(t, c, colorizerView(model.AttrIntensity, model.AttributeUint16, uint16(10)))

	// write phase: only the colorizer applies, Z must stay unchanged
	got, keep := mutateOneOnWrite(p, geom.NewPoint(0, 0, 1, 1, 2, 3), colorizerView(model.AttrIntensity, model.AttributeUint16, uint16(10)), model.IdentityTransform)
	if !keep {
		t.Fatal("expected point to be kept")
	}
	if got.Z != 1 {
		t.Fatalf("expected write-time pipeline to skip ZOffset, got Z=%v", got.Z)
	}
	if (Color{R: got.R, G: got.G, B: got.B}) != (Color{R: 255, G: 255, B: 255}) {
		t.Fatalf("expected write-time colorization, got %+v", got)
	}
}

func BenchmarkColorizerMutateChunkOnWriteAttribute(b *testing.B) {
	c, err := NewColorizerWithGradient(model.AttrIntensity, fixedRangeGradient(0, 100))
	if err != nil {
		b.Fatalf("NewColorizerWithGradient: %v", err)
	}
	attrs := colorizerView(model.AttrIntensity, model.AttributeUint16, uint16(50))
	points := make([]model.Point, 1024)
	for i := range points {
		points[i] = geom.NewPoint(0, 0, 0, 1, 2, 3)
		points[i].Attributes = attrs.values
	}
	chunk := PointChunk{Points: points, AttributeLayout: attrs.layout}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.MutateChunkOnWrite(chunk, model.IdentityTransform)
	}
}

func testLinearGradient() ColorGradientScale {
	return ColorGradientScale{
		Nums:              []float64{0, 1},
		Colors:            []Color{{R: 0, G: 0, B: 0}, {R: 255, G: 255, B: 255}},
		InterpolationMode: GradientInterpolationLinear,
	}
}

func colorizerView(name string, typ model.AttributeType, value any) testAttributeData {
	summaries := []model.AttributeSummary{{
		Name: model.CanonicalAttributeName(name),
		Type: typ,
	}}
	entries, size := model.AttributeLayout(summaries)
	values := make(model.AttributeValues, size)
	if err := model.EncodeAttributeValue(values[entries[0].Offset:entries[0].Offset+entries[0].Size], typ, value); err != nil {
		panic(err)
	}
	return testAttributeData{layout: entries, values: values}
}
