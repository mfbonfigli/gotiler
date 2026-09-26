package geotiffcolorizer

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/mfbonfigli/gotiler/v3/tiler/model"
	"github.com/mfbonfigli/gotiler/v3/tiler/mutator"
)

const epsg4978CRS = "EPSG:4978"

// CoordinateConverter is the subset of the tiler's coord.Converter required
// by the colorizer mutator.
type CoordinateConverter interface {
	Transform(sourceCRS string, targetCRS string, coord model.Vector) (model.Vector, error)
}

// FlatCoordinateConverter is implemented by converters that can transform a
// flat [X,Y,Z,...] coordinate buffer in place. The tiler's PROJ converter
// implements this in chunk-aware builds.
type FlatCoordinateConverter interface {
	TransformFlat(sourceCRS string, targetCRS string, flatCoords []float64) error
}

// NoColorPolicy controls what happens when a point cannot be sampled from the
// orthophoto.
type NoColorPolicy int

const (
	// NoColorKeepPoint keeps the point and leaves its original color unchanged.
	NoColorKeepPoint NoColorPolicy = iota
	// NoColorDropPoint discards points that cannot be colorized.
	NoColorDropPoint
)

// ColorizerStats is a point-in-time snapshot of mutator outcomes.
type ColorizerStats struct {
	Colored          uint64
	Missing          uint64
	Transparent      uint64
	ConversionErrors uint64
	SampleErrors     uint64
	Chunks           uint64
	FlatConversions  uint64
}

// ColorizerOption configures a Colorizer.
type ColorizerOption func(*colorizerConfig)

type colorizerConfig struct {
	sampling      SamplingMode
	targetCRS     string
	noColorPolicy NoColorPolicy
	minAlpha      uint8
}

// WithSamplingMode selects nearest-neighbour or bilinear orthophoto sampling.
func WithSamplingMode(mode SamplingMode) ColorizerOption {
	return func(cfg *colorizerConfig) {
		cfg.sampling = mode
	}
}

// WithTargetCRS overrides the CRS used for orthophoto sampling. By default the
// horizontal EPSG CRS from the GeoTIFF keys is used when available, falling back
// to Orthophoto.CRS.
func WithTargetCRS(crs string) ColorizerOption {
	return func(cfg *colorizerConfig) {
		cfg.targetCRS = strings.TrimSpace(crs)
	}
}

// WithNoColorPolicy selects whether unsampled points are kept or dropped.
func WithNoColorPolicy(policy NoColorPolicy) ColorizerOption {
	return func(cfg *colorizerConfig) {
		cfg.noColorPolicy = policy
	}
}

// WithMinimumAlpha ignores sampled colors whose alpha is below alpha. The
// default is 1, so fully transparent pixels do not overwrite point colors.
func WithMinimumAlpha(alpha uint8) ColorizerOption {
	return func(cfg *colorizerConfig) {
		cfg.minAlpha = alpha
	}
}

// Colorizer is a mutator that colors point-cloud points from an
// RGB/RGBA GeoTIFF orthophoto.
type Colorizer struct {
	orthophoto *Orthophoto
	converter  CoordinateConverter

	sampling      SamplingMode
	targetCRS     string
	noColorPolicy NoColorPolicy
	minAlpha      uint8

	converterMu sync.Mutex
	flatPool    sync.Pool

	colored          atomic.Uint64
	missing          atomic.Uint64
	transparent      atomic.Uint64
	conversionErrors atomic.Uint64
	sampleErrors     atomic.Uint64
	chunks           atomic.Uint64
	flatConversions  atomic.Uint64

	lastErrMu sync.Mutex
	lastErr   error
}

// NewColorizer returns a mutator that samples orthophoto colors for each point.
// The converter may be nil only when the orthophoto sampling CRS is EPSG:4978.
func NewColorizer(orthophoto *Orthophoto, converter CoordinateConverter, opts ...ColorizerOption) (*Colorizer, error) {
	if orthophoto == nil {
		return nil, fmt.Errorf("nil orthophoto")
	}
	cfg := colorizerConfig{
		sampling:      SampleBilinear,
		targetCRS:     defaultColorizerTargetCRS(orthophoto),
		noColorPolicy: NoColorKeepPoint,
		minAlpha:      1,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if cfg.targetCRS == "" {
		return nil, fmt.Errorf("orthophoto %q has no CRS for colorizer sampling", orthophoto.Name)
	}
	switch cfg.sampling {
	case SampleNearest, SampleBilinear:
	default:
		return nil, fmt.Errorf("unsupported sampling mode %d", cfg.sampling)
	}
	switch cfg.noColorPolicy {
	case NoColorKeepPoint, NoColorDropPoint:
	default:
		return nil, fmt.Errorf("unsupported no-color policy %d", cfg.noColorPolicy)
	}
	if converter == nil && !sameCRS(epsg4978CRS, cfg.targetCRS) {
		return nil, fmt.Errorf("coordinate converter is required to transform %s to %s", epsg4978CRS, cfg.targetCRS)
	}
	return &Colorizer{
		orthophoto:    orthophoto,
		converter:     converter,
		sampling:      cfg.sampling,
		targetCRS:     cfg.targetCRS,
		noColorPolicy: cfg.noColorPolicy,
		minAlpha:      cfg.minAlpha,
	}, nil
}

// NewColorizerFromFile opens filename as an RGB/RGBA GeoTIFF orthophoto and
// returns a colorizer mutator for it.
func NewColorizerFromFile(filename string, converter CoordinateConverter, opts ...ColorizerOption) (*Colorizer, error) {
	orthophoto, err := OpenOrthophotoFile(filename)
	if err != nil {
		return nil, err
	}
	colorizer, err := NewColorizer(orthophoto, converter, opts...)
	if err != nil {
		orthophoto.Close()
		return nil, err
	}
	return colorizer, nil
}

// RequiredAttributes reports that the colorizer only needs point position and
// local-to-global transform state supplied by the mutator pipeline.
func (c *Colorizer) RequiredAttributes() model.Attributes {
	return nil
}

// MutateChunk colors a batch of points. When the converter supports
// TransformFlat, this performs one CRS conversion for the whole chunk.
func (c *Colorizer) MutateChunk(chunk mutator.PointChunk, localToGlobal model.Transform) []model.Point {
	if c == nil || c.orthophoto == nil || len(chunk.Points) == 0 {
		return chunk.Points
	}
	c.chunks.Add(1)
	if sameCRS(epsg4978CRS, c.targetCRS) {
		return c.mutateChunkFlat(chunk.Points, localToGlobal, nil)
	}
	if flatConverter, ok := c.converter.(FlatCoordinateConverter); ok {
		return c.mutateChunkFlat(chunk.Points, localToGlobal, flatConverter)
	}
	return c.mutateChunkPointwise(chunk.Points, localToGlobal)
}

// Stats returns a point-in-time snapshot of colorization outcomes.
func (c *Colorizer) Stats() ColorizerStats {
	if c == nil {
		return ColorizerStats{}
	}
	return ColorizerStats{
		Colored:          c.colored.Load(),
		Missing:          c.missing.Load(),
		Transparent:      c.transparent.Load(),
		ConversionErrors: c.conversionErrors.Load(),
		SampleErrors:     c.sampleErrors.Load(),
		Chunks:           c.chunks.Load(),
		FlatConversions:  c.flatConversions.Load(),
	}
}

// LastError returns the most recent coordinate conversion or sampling error.
func (c *Colorizer) LastError() error {
	if c == nil {
		return nil
	}
	c.lastErrMu.Lock()
	defer c.lastErrMu.Unlock()
	return c.lastErr
}

// Close releases resources owned by the colorizer's orthophoto, if any.
func (c *Colorizer) Close() error {
	if c == nil || c.orthophoto == nil {
		return nil
	}
	return c.orthophoto.Close()
}

func (c *Colorizer) toOrthophotoCRS(coord model.Vector) (model.Vector, error) {
	if sameCRS(epsg4978CRS, c.targetCRS) {
		return coord, nil
	}
	c.converterMu.Lock()
	out, err := c.converter.Transform(epsg4978CRS, c.targetCRS, coord)
	c.converterMu.Unlock()
	if err != nil {
		return model.Vector{}, fmt.Errorf("colorizer coordinate conversion %s to %s: %w", epsg4978CRS, c.targetCRS, err)
	}
	return out, nil
}

func (c *Colorizer) mutateChunkFlat(points []model.Point, localToGlobal model.Transform, converter FlatCoordinateConverter) []model.Point {
	flatPtr, flatCoords := c.getFlatCoords(len(points) * 3)
	defer c.putFlatCoords(flatPtr)

	for i, pt := range points {
		global := localToGlobal.Forward(pt.Vector())
		offset := i * 3
		flatCoords[offset] = global.X
		flatCoords[offset+1] = global.Y
		flatCoords[offset+2] = global.Z
	}

	if converter != nil {
		c.converterMu.Lock()
		err := converter.TransformFlat(epsg4978CRS, c.targetCRS, flatCoords)
		c.converterMu.Unlock()
		if err != nil {
			c.conversionErrors.Add(uint64(len(points)))
			c.setLastError(fmt.Errorf("colorizer flat coordinate conversion %s to %s: %w", epsg4978CRS, c.targetCRS, err))
			return c.noColorChunk(points)
		}
		c.flatConversions.Add(1)
	}

	out := points[:0]
	for i, pt := range points {
		offset := i * 3
		mutated, keep := c.colorizeModelPoint(pt, model.Vector{
			X: flatCoords[offset],
			Y: flatCoords[offset+1],
			Z: flatCoords[offset+2],
		})
		if keep {
			out = append(out, mutated)
		}
	}
	return out
}

func (c *Colorizer) mutateChunkPointwise(points []model.Point, localToGlobal model.Transform) []model.Point {
	out := points[:0]
	for _, pt := range points {
		global := localToGlobal.Forward(pt.Vector())
		modelCoord, err := c.toOrthophotoCRS(global)
		if err != nil {
			c.conversionErrors.Add(1)
			c.setLastError(err)
			if c.noColorPolicy == NoColorKeepPoint {
				out = append(out, pt)
			}
			continue
		}
		mutated, keep := c.colorizeModelPoint(pt, modelCoord)
		if keep {
			out = append(out, mutated)
		}
	}
	return out
}

func (c *Colorizer) colorizeModelPoint(pt model.Point, modelCoord model.Vector) (model.Point, bool) {
	color, ok, err := c.orthophoto.SampleModel(modelCoord.X, modelCoord.Y, c.sampling)
	if err != nil {
		c.sampleErrors.Add(1)
		c.setLastError(err)
		return c.noColor(pt)
	}
	if !ok {
		c.missing.Add(1)
		return c.noColor(pt)
	}
	if color.A < c.minAlpha {
		c.transparent.Add(1)
		return c.noColor(pt)
	}
	pt.R = color.R
	pt.G = color.G
	pt.B = color.B
	c.colored.Add(1)
	return pt, true
}

func (c *Colorizer) getFlatCoords(size int) (*[]float64, []float64) {
	if v := c.flatPool.Get(); v != nil {
		buf := v.(*[]float64)
		if cap(*buf) < size {
			*buf = make([]float64, size)
		}
		return buf, (*buf)[:size]
	}
	buf := make([]float64, size)
	return &buf, buf
}

func (c *Colorizer) putFlatCoords(buf *[]float64) {
	*buf = (*buf)[:0]
	c.flatPool.Put(buf)
}

func (c *Colorizer) noColor(pt model.Point) (model.Point, bool) {
	return pt, c.noColorPolicy == NoColorKeepPoint
}

func (c *Colorizer) noColorChunk(points []model.Point) []model.Point {
	if c.noColorPolicy == NoColorDropPoint {
		return points[:0]
	}
	return points
}

func (c *Colorizer) setLastError(err error) {
	c.lastErrMu.Lock()
	c.lastErr = err
	c.lastErrMu.Unlock()
}

func defaultColorizerTargetCRS(orthophoto *Orthophoto) string {
	if orthophoto.GeoKeys != nil {
		if code, ok := orthophoto.GeoKeys.HorizontalEPSG(); ok {
			return fmt.Sprintf("EPSG:%d", code)
		}
	}
	return strings.TrimSpace(orthophoto.CRS)
}

func sameCRS(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}
