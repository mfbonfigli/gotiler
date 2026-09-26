package mutator

import (
	"fmt"
	"math"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/mfbonfigli/gotiler/v3/tiler/model"
)

// Color is an RGB color used by ColorGradientScale.
type Color struct {
	R uint8
	G uint8
	B uint8
}

// GradientInterpolationMode controls how colors are selected between gradient stops.
type GradientInterpolationMode int

const (
	// GradientInterpolationFlat selects the color of the lower stop for the interval.
	GradientInterpolationFlat GradientInterpolationMode = iota
	// GradientInterpolationLinear linearly interpolates colors between stops.
	GradientInterpolationLinear
)

// ColorGradientScale maps normalized values in [0, 1] to RGB colors.
// Nums must be sorted, strictly increasing, start at 0, and end at 1.
type ColorGradientScale struct {
	Nums              []float64
	Colors            []Color
	InterpolationMode GradientInterpolationMode
	// FixedRange pins the gradient to the absolute value range
	// [RangeMin, RangeMax]: the Colorizer uses it instead of the range
	// observed in the data. This is meant for gradients whose stops encode
	// absolute values, like categorical class palettes, where scaling to the
	// data would remap the categories.
	FixedRange bool
	RangeMin   float64
	RangeMax   float64
}

var (
	colorGradientRegistryMu sync.RWMutex
	colorGradientRegistry   = map[string]ColorGradientScale{
		"grayscale": {
			Nums:              []float64{0, 1},
			Colors:            []Color{{R: 0, G: 0, B: 0}, {R: 255, G: 255, B: 255}},
			InterpolationMode: GradientInterpolationLinear,
		},
		"heat": {
			Nums: []float64{0, 0.33, 0.66, 1},
			Colors: []Color{
				{R: 0, G: 0, B: 0},
				{R: 220, G: 0, B: 0},
				{R: 255, G: 220, B: 0},
				{R: 255, G: 255, B: 255},
			},
			InterpolationMode: GradientInterpolationLinear,
		},
		// viridis, magma, inferno, plasma, cividis and turbo are registered
		// from their canonical 256-entry lookup tables in gradients.go.
		"las-classification": {
			Nums: []float64{
				0,
				1.0 / 255.0,
				2.0 / 255.0,
				3.0 / 255.0,
				4.0 / 255.0,
				5.0 / 255.0,
				6.0 / 255.0,
				7.0 / 255.0,
				8.0 / 255.0,
				9.0 / 255.0,
				10.0 / 255.0,
				11.0 / 255.0,
				12.0 / 255.0,
				13.0 / 255.0,
				14.0 / 255.0,
				15.0 / 255.0,
				16.0 / 255.0,
				17.0 / 255.0,
				18.0 / 255.0,
				19.0 / 255.0,
				1,
			},
			Colors: []Color{
				{R: 180, G: 180, B: 180}, // 0: Created, never classified
				{R: 220, G: 220, B: 220}, // 1: Unclassified
				{R: 139, G: 100, B: 60},  // 2: Ground
				{R: 170, G: 220, B: 120}, // 3: Low vegetation
				{R: 80, G: 180, B: 80},   // 4: Medium vegetation
				{R: 20, G: 120, B: 40},   // 5: High vegetation
				{R: 210, G: 70, B: 60},   // 6: Building
				{R: 220, G: 0, B: 220},   // 7: Low point / noise
				{R: 0, G: 200, B: 200},   // 8: Model key-point
				{R: 40, G: 120, B: 230},  // 9: Water
				{R: 90, G: 90, B: 90},    // 10: Rail
				{R: 55, G: 55, B: 55},    // 11: Road surface
				{R: 255, G: 210, B: 0},   // 12: Overlap
				{R: 255, G: 150, B: 60},  // 13: Wire guard
				{R: 255, G: 235, B: 80},  // 14: Wire conductor
				{R: 150, G: 80, B: 200},  // 15: Transmission tower
				{R: 255, G: 180, B: 120}, // 16: Wire connector
				{R: 180, G: 140, B: 90},  // 17: Bridge deck
				{R: 255, G: 60, B: 180},  // 18: High noise
				{R: 120, G: 120, B: 120}, // 19-255: Unknown / reserved
				{R: 120, G: 120, B: 120},
			},
			InterpolationMode: GradientInterpolationFlat,
			// class codes are absolute: never rescale them to the data
			FixedRange: true,
			RangeMin:   0,
			RangeMax:   255,
		},
	}
)

// RegisterColorGradient registers or replaces a global gradient alias.
func RegisterColorGradient(alias string, gradient ColorGradientScale) error {
	if alias == "" {
		return fmt.Errorf("gradient alias cannot be empty")
	}
	if err := validateColorGradient(gradient); err != nil {
		return err
	}
	colorGradientRegistryMu.Lock()
	colorGradientRegistry[alias] = cloneColorGradient(gradient)
	colorGradientRegistryMu.Unlock()
	return nil
}

// RegisteredColorGradient returns a copy of the gradient registered for alias.
func RegisteredColorGradient(alias string) (ColorGradientScale, bool) {
	colorGradientRegistryMu.RLock()
	gradient, ok := colorGradientRegistry[alias]
	colorGradientRegistryMu.RUnlock()
	if !ok {
		return ColorGradientScale{}, false
	}
	return cloneColorGradient(gradient), true
}

// colorizerOptions holds the rendering settings configured through
// ColorizerOption values.
type colorizerOptions struct {
	stretchLow  float64
	stretchHigh float64
	reverse     bool
	steps       int
	blend       float64
}

func defaultColorizerOptions() colorizerOptions {
	return colorizerOptions{
		stretchLow:  2,
		stretchHigh: 98,
		blend:       1,
	}
}

// ColorizerOption configures how a Colorizer renders the gradient.
type ColorizerOption func(*colorizerOptions) error

// WithStretch sets the percentile stretch used to derive the color range from
// the data: the gradient is scaled between the pLow-th and pHigh-th
// percentiles of the observed values, and values outside saturate to the end
// colors. The default is (2, 98), which keeps outliers from washing out the
// ramp. Use WithStretch(0, 100) for a plain min/max scale. Ignored by
// gradients that declare a FixedRange.
func WithStretch(pLow, pHigh float64) ColorizerOption {
	return func(o *colorizerOptions) error {
		if !isFinite(pLow) || !isFinite(pHigh) || pLow < 0 || pHigh > 100 || pLow >= pHigh {
			return fmt.Errorf("stretch percentiles must satisfy 0 <= low < high <= 100, got (%v, %v)", pLow, pHigh)
		}
		o.stretchLow = pLow
		o.stretchHigh = pHigh
		return nil
	}
}

// WithReverse reverses the gradient's color order.
func WithReverse() ColorizerOption {
	return func(o *colorizerOptions) error {
		o.reverse = true
		return nil
	}
}

// WithSteps quantizes the gradient into n discrete color bands.
func WithSteps(n int) ColorizerOption {
	return func(o *colorizerOptions) error {
		if n < 2 {
			return fmt.Errorf("steps must be at least 2, got %d", n)
		}
		o.steps = n
		return nil
	}
}

// WithBlend blends the gradient color with the point's original color:
// 1 (the default) fully replaces the original color, 0.5 mixes them evenly.
func WithBlend(alpha float64) ColorizerOption {
	return func(o *colorizerOptions) error {
		if !isFinite(alpha) || alpha <= 0 || alpha > 1 {
			return fmt.Errorf("blend must be in (0, 1], got %v", alpha)
		}
		o.blend = alpha
		return nil
	}
}

// Colorizer colors points using a numeric per-point attribute or a local
// point coordinate (x, y or z). The value range is derived from the data:
// during loading it observes the attribute's value distribution without
// touching the points, then colors the points as tiles are written, scaling
// the gradient between the observed 2nd and 98th percentiles (configurable
// through WithStretch) so that outliers do not wash out the ramp. Gradients
// that declare a FixedRange (like las-classification, whose stops encode
// absolute class codes) are never rescaled to the data: their declared range
// is used as is. Because colors are applied only at write time, the RGB
// values stored in the tree keep their source values.
type Colorizer struct {
	attribute string
	gradient  ColorGradientScale
	blend     float64
	pLow      float64
	pHigh     float64

	// fixed is the mapping built upfront when the gradient declares a
	// FixedRange; observation and run resets are skipped entirely then.
	fixed *colorMapping

	// mu guards the observed range, the quantile estimators and the phase
	// transitions; frozen holds the write-time mapping so MutateChunkOnWrite
	// can run lock-free once built.
	mu     sync.Mutex
	min    float64
	max    float64
	seen   bool
	qLow   *quantileEstimator // nil when pLow is 0
	qHigh  *quantileEstimator // nil when pHigh is 100
	frozen atomic.Pointer[frozenMapping]
}

// frozenMapping is the immutable write-phase mapping. A nil colorMapping
// means no finite attribute value was observed, so points pass through
// unchanged.
type frozenMapping struct {
	m *colorMapping
}

// NewColorizer creates a Colorizer from a registered gradient alias.
func NewColorizer(attributeName, gradientAlias string, opts ...ColorizerOption) (*Colorizer, error) {
	gradient, ok := RegisteredColorGradient(gradientAlias)
	if !ok {
		return nil, fmt.Errorf("unknown color gradient %q", gradientAlias)
	}
	return NewColorizerWithGradient(attributeName, gradient, opts...)
}

// NewColorizerWithGradient creates a Colorizer using an explicit gradient
// definition.
func NewColorizerWithGradient(attributeName string, gradient ColorGradientScale, opts ...ColorizerOption) (*Colorizer, error) {
	attribute := model.CanonicalAttributeName(attributeName)
	if attribute == "" {
		return nil, fmt.Errorf("attribute name cannot be empty")
	}
	if err := validateColorGradient(gradient); err != nil {
		return nil, err
	}
	options := defaultColorizerOptions()
	for _, opt := range opts {
		if err := opt(&options); err != nil {
			return nil, err
		}
	}
	gradient = cloneColorGradient(gradient)
	if options.reverse {
		gradient = reversedGradient(gradient)
	}
	if options.steps > 0 {
		gradient = steppedGradient(gradient, options.steps)
	}
	c := &Colorizer{
		attribute: attribute,
		gradient:  gradient,
		blend:     options.blend,
		pLow:      options.stretchLow,
		pHigh:     options.stretchHigh,
	}
	if gradient.FixedRange {
		fixed, err := newColorMapping(attribute, gradient.RangeMin, gradient.RangeMax, gradient, c.blend)
		if err != nil {
			return nil, err
		}
		c.fixed = fixed
		return c, nil
	}
	if c.pLow > 0 {
		c.qLow = newQuantileEstimator(c.pLow / 100)
	}
	if c.pHigh < 100 {
		c.qHigh = newQuantileEstimator(c.pHigh / 100)
	}
	return c, nil
}

// reversedGradient returns the gradient with the color order reversed.
func reversedGradient(g ColorGradientScale) ColorGradientScale {
	n := len(g.Nums)
	nums := make([]float64, n)
	colors := make([]Color, n)
	for i := 0; i < n; i++ {
		nums[i] = 1 - g.Nums[n-1-i]
		colors[i] = g.Colors[n-1-i]
	}
	out := g
	out.Nums = nums
	out.Colors = colors
	return out
}

// steppedGradient quantizes the gradient into n discrete bands, sampling each
// band's color at its center.
func steppedGradient(g ColorGradientScale, n int) ColorGradientScale {
	// sample the gradient in normalized [0, 1] space
	sampler, err := newColorMapping("", 0, 1, g, 1)
	if err != nil {
		// gradient is already validated, so this cannot happen
		return g
	}
	nums := make([]float64, n+1)
	colors := make([]Color, n+1)
	for k := 0; k < n; k++ {
		nums[k] = float64(k) / float64(n)
		colors[k] = sampler.color((float64(k) + 0.5) / float64(n))
	}
	// flat interpolation requires a final stop at 1; it repeats the last band
	nums[n] = 1
	colors[n] = colors[n-1]
	out := g
	out.Nums = nums
	out.Colors = colors
	out.InterpolationMode = GradientInterpolationFlat
	return out
}

func (c *Colorizer) RequiredAttributes() model.Attributes {
	if c == nil {
		return nil
	}
	if colorizerPointField(c.attribute) {
		return nil
	}
	return model.NewAttributes(c.attribute)
}

// colorizerValuePool recycles the chunk-local value buffers used to feed the
// quantile estimators without holding the mutex during attribute extraction.
var colorizerValuePool = sync.Pool{
	New: func() any {
		buf := make([]float64, 0, 4096)
		return &buf
	},
}

// MutateChunk observes the attribute's value distribution and returns the
// points unchanged. Gradients with a fixed range need no observation, so it
// is a no-op for them.
func (c *Colorizer) MutateChunk(chunk PointChunk, localToGlobal model.Transform) []model.Point {
	if c == nil || c.fixed != nil {
		return chunk.Points
	}
	// extract the values chunk-locally, without holding the lock; the values
	// are buffered only when quantile estimators need them
	needValues := c.qLow != nil || c.qHigh != nil
	var bufPtr *[]float64
	var values []float64
	if needValues {
		bufPtr = colorizerValuePool.Get().(*[]float64)
		values = (*bufPtr)[:0]
	}
	mn, mx, ok := math.Inf(1), math.Inf(-1), false
	forEachChunkValue(c.attribute, chunk, func(v float64) {
		if v < mn {
			mn = v
		}
		if v > mx {
			mx = v
		}
		ok = true
		if needValues {
			values = append(values, v)
		}
	})

	c.mu.Lock()
	// A read call after write calls means a new tiling run started: discard
	// the previous run's range, estimators and mapping before observing.
	if c.frozen.Load() != nil {
		c.frozen.Store(nil)
		c.seen = false
		if c.qLow != nil {
			c.qLow.reset()
		}
		if c.qHigh != nil {
			c.qHigh.reset()
		}
	}
	if ok {
		if !c.seen {
			c.min, c.max = mn, mx
			c.seen = true
		} else {
			if mn < c.min {
				c.min = mn
			}
			if mx > c.max {
				c.max = mx
			}
		}
		for _, v := range values {
			if c.qLow != nil {
				c.qLow.add(v)
			}
			if c.qHigh != nil {
				c.qHigh.add(v)
			}
		}
	}
	c.mu.Unlock()

	if bufPtr != nil {
		*bufPtr = values[:0]
		colorizerValuePool.Put(bufPtr)
	}
	return chunk.Points
}

// MutateChunkOnWrite colors the points using the gradient scaled over the
// fixed range, when the gradient declares one, or over the range observed
// during loading. If the range is not fixed and the attribute was never
// observed with a finite value, points pass through unchanged.
func (c *Colorizer) MutateChunkOnWrite(chunk PointChunk, localToGlobal model.Transform) []model.Point {
	if c == nil {
		return chunk.Points
	}
	if c.fixed != nil {
		return c.fixed.mutateChunk(chunk)
	}
	f := c.frozen.Load()
	if f == nil {
		f = c.freeze()
	}
	if f.m == nil {
		return chunk.Points
	}
	return f.m.mutateChunk(chunk)
}

func (c *Colorizer) Close() error {
	return nil
}

// freeze builds the write-phase mapping from the observed distribution
// exactly once per run; concurrent callers get the same mapping.
func (c *Colorizer) freeze() *frozenMapping {
	c.mu.Lock()
	defer c.mu.Unlock()
	if f := c.frozen.Load(); f != nil {
		return f
	}
	f := &frozenMapping{}
	if c.seen {
		mn, mx := c.min, c.max
		// stretch to the estimated percentiles; the estimates only exist
		// after enough observations, otherwise min/max is used as is
		if c.qLow != nil {
			if v, ok := c.qLow.estimate(); ok && v > mn {
				mn = v
			}
		}
		if c.qHigh != nil {
			if v, ok := c.qHigh.estimate(); ok && v < mx {
				mx = v
			}
		}
		if mn >= mx {
			// degenerate stretch (e.g. near-constant data): fall back to the
			// full observed range
			mn, mx = c.min, c.max
		}
		if mn == mx {
			// a single-valued attribute has no meaningful range: widen it so
			// every point maps to the gradient midpoint.
			mn -= 0.5
			mx += 0.5
		}
		if m, err := newColorMapping(c.attribute, mn, mx, c.gradient, c.blend); err == nil {
			f.m = m
		}
	}
	c.frozen.Store(f)
	return f
}

// forEachChunkValue calls yield with every finite value of the attribute in
// the chunk.
func forEachChunkValue(attribute string, chunk PointChunk, yield func(float64)) {
	if fieldValue := colorizerPointFieldGetter(attribute); fieldValue != nil {
		for i := range chunk.Points {
			v := fieldValue(chunk.Points[i])
			if !isFinite(v) {
				continue
			}
			yield(v)
		}
		return
	}
	layout := model.NewAttributeView(chunk.AttributeLayout, nil)
	attrIndex := layout.Index(attribute)
	if attrIndex < 0 {
		return
	}
	extract := attributeFloat64Extractor(layout.Type(attrIndex))
	if extract == nil {
		return
	}
	for i := range chunk.Points {
		v, vok := extract(chunk.AttributeView(i), attrIndex)
		if !vok {
			continue
		}
		yield(v)
	}
}

// colorMapping colors points by mapping a numeric per-point attribute over an
// absolute value range. It is the immutable per-point machinery behind
// Colorizer.
type colorMapping struct {
	attribute string
	nums      []float64
	colors    []Color
	mode      GradientInterpolationMode
	blend     float64
}

// newColorMapping builds a mapping scaling the gradient stops so normalized 0
// maps to minRange and 1 maps to maxRange. attribute must be canonical.
func newColorMapping(attribute string, minRange, maxRange float64, gradient ColorGradientScale, blend float64) (*colorMapping, error) {
	if !isFinite(minRange) || !isFinite(maxRange) {
		return nil, fmt.Errorf("colorizer range must be finite")
	}
	if minRange >= maxRange {
		return nil, fmt.Errorf("colorizer min range %v must be less than max range %v", minRange, maxRange)
	}
	nums := make([]float64, len(gradient.Nums))
	scale := maxRange - minRange
	for i, n := range gradient.Nums {
		nums[i] = minRange + n*scale
	}
	colors := append([]Color(nil), gradient.Colors...)
	return &colorMapping{
		attribute: attribute,
		nums:      nums,
		colors:    colors,
		mode:      gradient.InterpolationMode,
		blend:     blend,
	}, nil
}

func (c *colorMapping) mutateChunk(chunk PointChunk) []model.Point {
	if len(chunk.Points) == 0 {
		return chunk.Points
	}
	// the field-vs-attribute decision, the attribute lookup and the type
	// dispatch are invariant across a chunk: resolve them once up front
	// instead of re-evaluating them for every point.
	if fieldValue := colorizerPointFieldGetter(c.attribute); fieldValue != nil {
		for i := range chunk.Points {
			value := fieldValue(chunk.Points[i])
			if !isFinite(value) {
				continue
			}
			c.colorizePoint(&chunk.Points[i], value)
		}
		return chunk.Points
	}
	layout := model.NewAttributeView(chunk.AttributeLayout, nil)
	attrIndex := layout.Index(c.attribute)
	if attrIndex < 0 {
		return chunk.Points
	}
	extract := attributeFloat64Extractor(layout.Type(attrIndex))
	if extract == nil {
		return chunk.Points
	}
	for i := range chunk.Points {
		value, ok := extract(chunk.AttributeView(i), attrIndex)
		if !ok {
			continue
		}
		c.colorizePoint(&chunk.Points[i], value)
	}
	return chunk.Points
}

func (c *colorMapping) colorizePoint(pt *model.Point, value float64) {
	color := c.color(value)
	if c.blend >= 1 {
		pt.R = color.R
		pt.G = color.G
		pt.B = color.B
		return
	}
	pt.R = blendChannel(pt.R, color.R, c.blend)
	pt.G = blendChannel(pt.G, color.G, c.blend)
	pt.B = blendChannel(pt.B, color.B, c.blend)
}

// blendChannel mixes the original channel value with the gradient one:
// alpha 1 is gradient only, alpha 0 would be the original color.
func blendChannel(orig, grad uint8, alpha float64) uint8 {
	v := float64(orig)*(1-alpha) + float64(grad)*alpha
	if v <= 0 {
		return 0
	}
	if v >= 255 {
		return 255
	}
	return uint8(math.Round(v))
}

func colorizerPointField(attribute string) bool {
	return colorizerPointFieldGetter(attribute) != nil
}

// colorizerPointFieldGetter returns a getter for the point field named by
// attribute, or nil if the attribute does not name a point field.
func colorizerPointFieldGetter(attribute string) func(model.Point) float64 {
	switch attribute {
	case "x":
		return func(pt model.Point) float64 { return float64(pt.X) }
	case "y":
		return func(pt model.Point) float64 { return float64(pt.Y) }
	case "z":
		return func(pt model.Point) float64 { return float64(pt.Z) }
	default:
		return nil
	}
}

func (c *colorMapping) color(value float64) Color {
	if value <= c.nums[0] {
		return c.colors[0]
	}
	last := len(c.nums) - 1
	if value >= c.nums[last] {
		return c.colors[last]
	}
	hi := sort.Search(len(c.nums), func(i int) bool {
		return c.nums[i] > value
	})
	lo := hi - 1
	if c.mode == GradientInterpolationFlat {
		return c.colors[lo]
	}
	den := c.nums[hi] - c.nums[lo]
	if den <= 0 {
		return c.colors[lo]
	}
	t := (value - c.nums[lo]) / den
	return interpolateColor(c.colors[lo], c.colors[hi], t)
}

// attributeFloat64Extractor returns a function that reads the i-th attribute
// of a view as a float64, or nil if the type is unsupported. The type dispatch
// happens once here so the returned extractor can run in per-point hot loops.
func attributeFloat64Extractor(typ model.AttributeType) func(attrs model.AttributeView, i int) (float64, bool) {
	switch typ {
	case model.AttributeInt8:
		return func(attrs model.AttributeView, i int) (float64, bool) {
			v, err := attrs.Int8(i)
			return float64(v), err == nil
		}
	case model.AttributeUint8:
		return func(attrs model.AttributeView, i int) (float64, bool) {
			v, err := attrs.Uint8(i)
			return float64(v), err == nil
		}
	case model.AttributeInt16:
		return func(attrs model.AttributeView, i int) (float64, bool) {
			v, err := attrs.Int16(i)
			return float64(v), err == nil
		}
	case model.AttributeUint16:
		return func(attrs model.AttributeView, i int) (float64, bool) {
			v, err := attrs.Uint16(i)
			return float64(v), err == nil
		}
	case model.AttributeInt32:
		return func(attrs model.AttributeView, i int) (float64, bool) {
			v, err := attrs.Int32(i)
			return float64(v), err == nil
		}
	case model.AttributeUint32:
		return func(attrs model.AttributeView, i int) (float64, bool) {
			v, err := attrs.Uint32(i)
			return float64(v), err == nil
		}
	case model.AttributeInt64:
		return func(attrs model.AttributeView, i int) (float64, bool) {
			v, err := attrs.Int64(i)
			return float64(v), err == nil
		}
	case model.AttributeUint64:
		return func(attrs model.AttributeView, i int) (float64, bool) {
			v, err := attrs.Uint64(i)
			return float64(v), err == nil
		}
	case model.AttributeBool:
		return func(attrs model.AttributeView, i int) (float64, bool) {
			v, err := attrs.Bool(i)
			if v {
				return 1, err == nil
			}
			return 0, err == nil
		}
	case model.AttributeFloat32:
		return func(attrs model.AttributeView, i int) (float64, bool) {
			v, err := attrs.Float32(i)
			return float64(v), err == nil && isFinite(float64(v))
		}
	case model.AttributeFloat64:
		return func(attrs model.AttributeView, i int) (float64, bool) {
			v, err := attrs.Float64(i)
			return v, err == nil && isFinite(v)
		}
	default:
		return nil
	}
}

func interpolateColor(a, b Color, t float64) Color {
	return Color{
		R: interpolateChannel(a.R, b.R, t),
		G: interpolateChannel(a.G, b.G, t),
		B: interpolateChannel(a.B, b.B, t),
	}
}

func interpolateChannel(a, b uint8, t float64) uint8 {
	v := float64(a) + (float64(b)-float64(a))*t
	if v <= 0 {
		return 0
	}
	if v >= 255 {
		return 255
	}
	return uint8(math.Round(v))
}

func validateColorGradient(gradient ColorGradientScale) error {
	if len(gradient.Nums) != len(gradient.Colors) {
		return fmt.Errorf("gradient has %d numeric stops and %d colors", len(gradient.Nums), len(gradient.Colors))
	}
	if len(gradient.Nums) < 2 {
		return fmt.Errorf("gradient must contain at least two stops")
	}
	switch gradient.InterpolationMode {
	case GradientInterpolationFlat, GradientInterpolationLinear:
	default:
		return fmt.Errorf("unsupported gradient interpolation mode %d", gradient.InterpolationMode)
	}
	for i, n := range gradient.Nums {
		if !isFinite(n) || n < 0 || n > 1 {
			return fmt.Errorf("gradient stop %d is %v, expected a finite value in [0, 1]", i, n)
		}
		if i > 0 && n <= gradient.Nums[i-1] {
			return fmt.Errorf("gradient stops must be strictly increasing")
		}
	}
	if gradient.Nums[0] != 0 {
		return fmt.Errorf("gradient first stop must be 0")
	}
	if gradient.Nums[len(gradient.Nums)-1] != 1 {
		return fmt.Errorf("gradient last stop must be 1")
	}
	if gradient.FixedRange {
		if !isFinite(gradient.RangeMin) || !isFinite(gradient.RangeMax) {
			return fmt.Errorf("gradient fixed range must be finite")
		}
		if gradient.RangeMin >= gradient.RangeMax {
			return fmt.Errorf("gradient fixed range min %v must be less than max %v", gradient.RangeMin, gradient.RangeMax)
		}
	}
	return nil
}

func cloneColorGradient(gradient ColorGradientScale) ColorGradientScale {
	return ColorGradientScale{
		Nums:              append([]float64(nil), gradient.Nums...),
		Colors:            append([]Color(nil), gradient.Colors...),
		InterpolationMode: gradient.InterpolationMode,
		FixedRange:        gradient.FixedRange,
		RangeMin:          gradient.RangeMin,
		RangeMax:          gradient.RangeMax,
	}
}

func isFinite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}
