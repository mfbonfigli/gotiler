package mutator

import (
	"math"
	"testing"

	"github.com/mfbonfigli/gotiler/v3/tiler/geom"
	"github.com/mfbonfigli/gotiler/v3/tiler/model"
)

func TestPerceptualGradientsAreCanonicalLUTs(t *testing.T) {
	aliases := []string{"viridis", "magma", "inferno", "plasma", "cividis", "turbo"}
	for _, alias := range aliases {
		g, ok := RegisteredColorGradient(alias)
		if !ok {
			t.Fatalf("%s: expected registered gradient", alias)
		}
		if len(g.Nums) != 256 || len(g.Colors) != 256 {
			t.Fatalf("%s: expected 256-entry LUT, got %d stops", alias, len(g.Nums))
		}
		if g.InterpolationMode != GradientInterpolationLinear {
			t.Fatalf("%s: expected linear interpolation", alias)
		}
		for i, n := range g.Nums {
			want := float64(i) / 255
			if math.Abs(n-want) > 1e-12 {
				t.Fatalf("%s: stop %d is %v, expected uniform spacing %v", alias, i, n, want)
			}
		}
	}
	// spot-check canonical endpoints
	viridis, _ := RegisteredColorGradient("viridis")
	if viridis.Colors[0] != (Color{R: 68, G: 1, B: 84}) || viridis.Colors[255] != (Color{R: 253, G: 231, B: 37}) {
		t.Fatalf("viridis endpoints do not match the canonical LUT: %+v ... %+v", viridis.Colors[0], viridis.Colors[255])
	}
	turbo, _ := RegisteredColorGradient("turbo")
	if turbo.Colors[0] != (Color{R: 48, G: 18, B: 59}) || turbo.Colors[255] != (Color{R: 122, G: 4, B: 3}) {
		t.Fatalf("turbo endpoints do not match the canonical LUT: %+v ... %+v", turbo.Colors[0], turbo.Colors[255])
	}
}

func TestQuantileEstimatorAccuracy(t *testing.T) {
	// feed a deterministic permutation of 0..9999 (uniform distribution)
	for _, tc := range []struct {
		q    float64
		want float64
	}{
		{0.02, 200},
		{0.5, 5000},
		{0.98, 9800},
	} {
		e := newQuantileEstimator(tc.q)
		n := 10000
		for i := 0; i < n; i++ {
			e.add(float64((i * 7919) % n))
		}
		got, ok := e.estimate()
		if !ok {
			t.Fatalf("q=%v: expected an estimate", tc.q)
		}
		if math.Abs(got-tc.want) > float64(n)*0.01 {
			t.Fatalf("q=%v: estimate %v too far from %v", tc.q, got, tc.want)
		}
	}
}

func TestQuantileEstimatorNeedsFiveValues(t *testing.T) {
	e := newQuantileEstimator(0.5)
	for i := 0; i < 4; i++ {
		e.add(float64(i))
		if _, ok := e.estimate(); ok {
			t.Fatal("expected no estimate before five observations")
		}
	}
	e.add(4)
	if v, ok := e.estimate(); !ok || v != 2 {
		t.Fatalf("expected median estimate 2, got %v ok=%v", v, ok)
	}
	e.reset()
	if _, ok := e.estimate(); ok {
		t.Fatal("expected reset to discard observations")
	}
}

// observeValues feeds intensity values through MutateChunk in one chunk.
func observeValues(t *testing.T, c *Colorizer, values []float64) {
	t.Helper()
	points := make([]model.Point, len(values))
	var layout []model.AttributeLayoutEntry
	for i, v := range values {
		data := colorizerView(model.AttrIntensity, model.AttributeFloat64, v)
		points[i] = geom.NewPoint(0, 0, 0, 1, 2, 3)
		points[i].Attributes = data.values
		layout = data.layout
	}
	got := c.MutateChunk(PointChunk{Points: points, AttributeLayout: layout}, model.IdentityTransform)
	if len(got) != len(values) {
		t.Fatalf("expected observation to keep all points, got %d", len(got))
	}
}

func writeValue(t *testing.T, c *Colorizer, value float64) Color {
	t.Helper()
	pt, keep := mutateOneOnWrite(c, geom.NewPoint(0, 0, 0, 1, 2, 3), colorizerView(model.AttrIntensity, model.AttributeFloat64, value), model.IdentityTransform)
	if !keep {
		t.Fatal("expected point to be kept")
	}
	return Color{R: pt.R, G: pt.G, B: pt.B}
}

func TestColorizerDefaultStretchIgnoresOutliers(t *testing.T) {
	c, err := NewColorizerWithGradient(model.AttrIntensity, testLinearGradient())
	if err != nil {
		t.Fatalf("NewColorizerWithGradient: %v", err)
	}
	// uniform values 0..999 plus one massive outlier
	values := make([]float64, 0, 1001)
	for i := 0; i < 1000; i++ {
		values = append(values, float64((i*7919)%1000))
	}
	values = append(values, 1e6)
	observeValues(t, c, values)

	// with a plain min/max scale the outlier would push every regular value
	// into the bottom 0.1% of the ramp (all black); with the default p2-p98
	// stretch the ramp spans the bulk of the data instead
	if got := writeValue(t, c, 999); got.R < 250 {
		t.Fatalf("value 999 should saturate near the top of the ramp, got %+v", got)
	}
	mid := writeValue(t, c, 500)
	if mid.R < 100 || mid.R > 155 {
		t.Fatalf("value 500 should map near the middle of the ramp, got %+v", mid)
	}
	if got := writeValue(t, c, 0); got.R != 0 {
		t.Fatalf("value 0 should map to the bottom of the ramp, got %+v", got)
	}
}

func TestColorizerMinMaxStretchKeepsOldBehavior(t *testing.T) {
	c, err := NewColorizerWithGradient(model.AttrIntensity, testLinearGradient(), WithStretch(0, 100))
	if err != nil {
		t.Fatalf("NewColorizerWithGradient: %v", err)
	}
	values := make([]float64, 0, 1000)
	for i := 0; i < 1000; i++ {
		values = append(values, float64((i*7919)%1000)) // 0..999
	}
	observeValues(t, c, values)
	// with p0-p100 the range is exactly [0, 999]
	mid := writeValue(t, c, 499.5)
	if mid.R < 126 || mid.R > 130 {
		t.Fatalf("expected min/max scaling midpoint, got %+v", mid)
	}
}

func TestColorizerStretchRejectsInvalidPercentiles(t *testing.T) {
	for _, tc := range [][2]float64{{-1, 98}, {2, 101}, {98, 2}, {50, 50}, {math.NaN(), 98}} {
		if _, err := NewColorizerWithGradient(model.AttrIntensity, testLinearGradient(), WithStretch(tc[0], tc[1])); err == nil {
			t.Fatalf("expected stretch (%v, %v) to be rejected", tc[0], tc[1])
		}
	}
}

func TestColorizerReverse(t *testing.T) {
	c, err := NewColorizerWithGradient(model.AttrIntensity, fixedRangeGradient(0, 10), WithReverse())
	if err != nil {
		t.Fatalf("NewColorizerWithGradient: %v", err)
	}
	if got := writeValue(t, c, 0); got != (Color{R: 255, G: 255, B: 255}) {
		t.Fatalf("reversed gradient at min should be white, got %+v", got)
	}
	if got := writeValue(t, c, 10); got != (Color{R: 0, G: 0, B: 0}) {
		t.Fatalf("reversed gradient at max should be black, got %+v", got)
	}
	if got := writeValue(t, c, 5); got != (Color{R: 128, G: 128, B: 128}) {
		t.Fatalf("reversed gradient at midpoint should stay gray, got %+v", got)
	}
}

func TestColorizerReverseAsymmetricStops(t *testing.T) {
	gradient := ColorGradientScale{
		Nums:              []float64{0, 0.9, 1},
		Colors:            []Color{{R: 10}, {R: 20}, {R: 30}},
		InterpolationMode: GradientInterpolationFlat,
		FixedRange:        true,
		RangeMin:          0,
		RangeMax:          10,
	}
	c, err := NewColorizerWithGradient(model.AttrIntensity, gradient, WithReverse())
	if err != nil {
		t.Fatalf("NewColorizerWithGradient: %v", err)
	}
	// reversed stops are [0, 0.1, 1] with colors [30, 20, 10]
	if got := writeValue(t, c, 0.5); got.R != 30 {
		t.Fatalf("expected first reversed band, got %+v", got)
	}
	if got := writeValue(t, c, 5); got.R != 20 {
		t.Fatalf("expected middle reversed band, got %+v", got)
	}
}

func TestColorizerSteps(t *testing.T) {
	c, err := NewColorizerWithGradient(model.AttrIntensity, fixedRangeGradient(0, 10), WithSteps(2))
	if err != nil {
		t.Fatalf("NewColorizerWithGradient: %v", err)
	}
	// two bands sampled at normalized 0.25 and 0.75
	low := Color{R: 64, G: 64, B: 64}
	high := Color{R: 191, G: 191, B: 191}
	for _, tc := range []struct {
		value float64
		want  Color
	}{
		{0, low}, {2, low}, {4.9, low},
		{5, high}, {7, high}, {10, high},
	} {
		if got := writeValue(t, c, tc.value); got != tc.want {
			t.Fatalf("value %v: got %+v want %+v", tc.value, got, tc.want)
		}
	}
}

func TestColorizerStepsRejectsInvalidCount(t *testing.T) {
	for _, n := range []int{-1, 0, 1} {
		if _, err := NewColorizerWithGradient(model.AttrIntensity, testLinearGradient(), WithSteps(n)); err == nil {
			t.Fatalf("expected steps %d to be rejected", n)
		}
	}
}

func TestColorizerBlend(t *testing.T) {
	c, err := NewColorizerWithGradient(model.AttrIntensity, fixedRangeGradient(0, 10), WithBlend(0.5))
	if err != nil {
		t.Fatalf("NewColorizerWithGradient: %v", err)
	}
	// original color (0, 0, 0), gradient at max is white: an even blend is gray
	pt, keep := mutateOneOnWrite(c, geom.NewPoint(0, 0, 0, 0, 0, 0), colorizerView(model.AttrIntensity, model.AttributeFloat64, 10.0), model.IdentityTransform)
	if !keep {
		t.Fatal("expected point to be kept")
	}
	if got := (Color{R: pt.R, G: pt.G, B: pt.B}); got != (Color{R: 128, G: 128, B: 128}) {
		t.Fatalf("expected even blend of black and white, got %+v", got)
	}
	// original color (100, 100, 100), gradient at min is black: blend halves it
	pt, _ = mutateOneOnWrite(c, geom.NewPoint(0, 0, 0, 100, 100, 100), colorizerView(model.AttrIntensity, model.AttributeFloat64, 0.0), model.IdentityTransform)
	if got := (Color{R: pt.R, G: pt.G, B: pt.B}); got != (Color{R: 50, G: 50, B: 50}) {
		t.Fatalf("expected blend to halve the original color, got %+v", got)
	}
}

func TestColorizerBlendRejectsInvalidAlpha(t *testing.T) {
	for _, alpha := range []float64{0, -0.5, 1.5, math.NaN()} {
		if _, err := NewColorizerWithGradient(model.AttrIntensity, testLinearGradient(), WithBlend(alpha)); err == nil {
			t.Fatalf("expected blend %v to be rejected", alpha)
		}
	}
}

func TestColorizerCombinedOptions(t *testing.T) {
	// reverse + steps compose: two bands of the reversed grayscale ramp
	c, err := NewColorizerWithGradient(model.AttrIntensity, fixedRangeGradient(0, 10), WithReverse(), WithSteps(2))
	if err != nil {
		t.Fatalf("NewColorizerWithGradient: %v", err)
	}
	if got := writeValue(t, c, 2); got != (Color{R: 191, G: 191, B: 191}) {
		t.Fatalf("expected bright low band on reversed ramp, got %+v", got)
	}
	if got := writeValue(t, c, 8); got != (Color{R: 64, G: 64, B: 64}) {
		t.Fatalf("expected dark high band on reversed ramp, got %+v", got)
	}
}

func TestColorizerStretchResetsOnNewRun(t *testing.T) {
	c, err := NewColorizerWithGradient(model.AttrIntensity, testLinearGradient())
	if err != nil {
		t.Fatalf("NewColorizerWithGradient: %v", err)
	}
	// run 1: values 0..999
	values := make([]float64, 1000)
	for i := range values {
		values[i] = float64((i * 7919) % 1000)
	}
	observeValues(t, c, values)
	if got := writeValue(t, c, 999); got.R < 250 {
		t.Fatalf("run 1: expected top of ramp, got %+v", got)
	}

	// run 2: values 10000..10999; the estimators must restart
	for i := range values {
		values[i] = 10000 + float64((i*7919)%1000)
	}
	observeValues(t, c, values)
	if got := writeValue(t, c, 10000); got.R > 5 {
		t.Fatalf("run 2: expected bottom of ramp for new minimum, got %+v", got)
	}
	if got := writeValue(t, c, 10999); got.R < 250 {
		t.Fatalf("run 2: expected top of ramp for new maximum, got %+v", got)
	}
}
