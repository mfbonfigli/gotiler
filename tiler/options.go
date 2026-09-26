package tiler

import (
	"fmt"
	"runtime"

	"github.com/mfbonfigli/gotiler/v3/tiler/model"
	"github.com/mfbonfigli/gotiler/v3/tiler/mutator"
	"github.com/mfbonfigli/gotiler/v3/tiler/plugin"
)

type TilerOptions struct {
	mutators              []mutator.Mutator
	placement             *Placement
	refineMode            model.RefineMode
	eightBitColors        bool
	numWorkers            int
	PointsPerTile         int
	progressCallback      ProgressCallback
	encoderID             string
	initialGeometricError float64
	geCorrection          float64
	attributes            model.Attributes
	writerProvider        plugin.WriterProvider
	writerMiddleware      []plugin.WriterMiddleware
	writerFinalizers      []plugin.WriterFinalizer
	treeProvider          TreeProvider
}

// Apply applies the given option functions to the TilerOptions in-place.
func (opts *TilerOptions) Apply(fns ...tilerOptionsFn) {
	for _, fn := range fns {
		fn(opts)
	}
}

type tilerOptionsFn func(*TilerOptions)

// NewDefaultTilerOptions returns sensible defaults for tiling options
func NewDefaultTilerOptions() *TilerOptions {
	return &TilerOptions{
		numWorkers:            runtime.NumCPU(),
		PointsPerTile:         50000,
		eightBitColors:        false,
		encoderID:             plugin.EncoderGLB,
		refineMode:            model.RefineAdd,
		initialGeometricError: 0,
		geCorrection:          1.0,
		attributes:            model.DefaultAttributes(),
	}
}

// NewTilerOptions returns default tiler options modified using the
// provided manipulating functions
func NewTilerOptions(optFn ...tilerOptionsFn) *TilerOptions {
	opts := NewDefaultTilerOptions()
	for _, fn := range optFn {
		fn(opts)
	}
	return opts
}

// validate checks that the effective option values are usable before any point
// cloud data is read. Some options (e.g. PointsPerTile) are exported fields that
// can be set directly, so they are validated at process time rather than only
// when applying option functions.
func (opts *TilerOptions) validate() error {
	if err := opts.validateEncoder(); err != nil {
		return err
	}
	if opts.placement != nil {
		if _, err := opts.placement.Transform(); err != nil {
			return err
		}
	}
	if opts.PointsPerTile <= 0 {
		return fmt.Errorf("points per tile must be greater than zero, got %d", opts.PointsPerTile)
	}
	if opts.numWorkers <= 0 {
		return fmt.Errorf("number of workers must be greater than zero, got %d", opts.numWorkers)
	}
	switch opts.refineMode {
	case model.RefineAdd, model.RefineReplace:
	default:
		return fmt.Errorf("unsupported refine mode %q; supported modes: %q, %q", opts.refineMode, model.RefineAdd, model.RefineReplace)
	}
	return nil
}

func (opts *TilerOptions) validateEncoder() error {
	factory, ok := plugin.GeometryEncoderFactoryFor(opts.encoderID)
	if !ok {
		return fmt.Errorf("unsupported geometry encoder %q; supported encoders: %v", opts.encoderID, plugin.SupportedGeometryEncoders())
	}
	if encoder := factory(opts.attributes); encoder == nil {
		return fmt.Errorf("geometry encoder %q returned nil", opts.encoderID)
	}
	return nil
}

// WithPlacement enables ungeoreferenced input mode: no CRS is required, the
// input coordinates are treated as a local cartesian system in meters and
// placed on the globe according to the given Placement. Mutually exclusive
// with providing a source CRS.
func WithPlacement(p Placement) tilerOptionsFn {
	return func(opt *TilerOptions) {
		opt.placement = &p
	}
}

// WithMutators adds the specified list of mutators to the processing step of the cloud
func WithMutators(m []mutator.Mutator) tilerOptionsFn {
	return func(opt *TilerOptions) {
		opt.mutators = m
	}
}

// WithWorkerNumber sets the number of workers to use to read the las files or to
// run the export jobs
func WithWorkerNumber(numWorkers int) tilerOptionsFn {
	return func(opt *TilerOptions) {
		opt.numWorkers = numWorkers
	}
}

// WithPointsPerTile sets the maximum number of points a tile can contain.
// If a tile contains more points, a subsampling strategy is applied.
func WithPointsPerTile(pointsPerTile int) tilerOptionsFn {
	return func(opt *TilerOptions) {
		opt.PointsPerTile = pointsPerTile
	}
}

// WithProgressCallback registers a callback that receives ProgressEvents as the
// tiler runs. Milestone events (phase start/end, errors) are always delivered.
// Detail events (per-batch point counts, per-tile export updates) are throttled
// internally so the callback is never flooded.
// Pass nil to disable progress reporting.
func WithProgressCallback(cb ProgressCallback) tilerOptionsFn {
	return func(opt *TilerOptions) {
		opt.progressCallback = cb
	}
}

// WithEightBitColors true forces the tiler to interpret the color info on the file as eight bit colors
func WithEightBitColors(eightBit bool) tilerOptionsFn {
	return func(opt *TilerOptions) {
		opt.eightBitColors = eightBit
	}
}

// WithEncoder selects the geometry encoder by registry ID. The encoder controls
// both tile content format and tileset version.
func WithEncoder(id string) tilerOptionsFn {
	return func(opt *TilerOptions) {
		opt.encoderID = id
	}
}

// WithRefineMode sets the refine mode of the tiler
func WithRefineMode(r model.RefineMode) tilerOptionsFn {
	return func(opt *TilerOptions) {
		opt.refineMode = r
	}
}

// WithGECorrection sets a multiplier applied to every geometric error value in the output
// tileset.json. Helps to control at tile generation time at what distance the viewer
// switches between LOD levels. Values > 1 make tiles appear at greater distances,
// values < 1 only up close. Default is 1.0 (no correction).
func WithGECorrection(c float64) tilerOptionsFn {
	return func(o *TilerOptions) { o.geCorrection = c }
}

// WithAttributes sets which optional per-point attributes are written to output tiles.
// Use model.NewAttributes("intensity", "classification") or model.DefaultAttributes().
// An empty set omits all optional attributes. Default is all attributes enabled.
func WithAttributes(attrs model.Attributes) tilerOptionsFn {
	return func(o *TilerOptions) { o.attributes = attrs }
}

// WithWriterProvider sets the provider used for final tileset output. Use this
// to write to object storage or disk. When unset, output is written to disk
// under the output folder.
func WithWriterProvider(wp plugin.WriterProvider) tilerOptionsFn {
	return func(o *TilerOptions) { o.writerProvider = wp }
}

// WithWriterMiddleware wraps the writer provider for all generated tile content
// and tileset.json files. Use this to inject encryption or similar transforms.
func WithWriterMiddleware(middlewares ...plugin.WriterMiddleware) tilerOptionsFn {
	return func(o *TilerOptions) {
		o.writerMiddleware = append(o.writerMiddleware, middlewares...)
	}
}

// WithWriterFinalizer registers hooks that run after all generated content and
// tileset.json files have been written. Use this for archive formats that need
// to append indexes or close a container.
func WithWriterFinalizer(finalizers ...plugin.WriterFinalizer) tilerOptionsFn {
	return func(o *TilerOptions) {
		for _, finalizer := range finalizers {
			if finalizer != nil {
				o.writerFinalizers = append(o.writerFinalizers, finalizer)
			}
		}
	}
}

// WithTreeProvider sets the tree implementation used to organize points before export.
// This is intended for alternate spatial hierarchies or private tree implementations.
func WithTreeProvider(provider TreeProvider) tilerOptionsFn {
	return func(o *TilerOptions) { o.treeProvider = provider }
}

// WithInitialGeometricError sets the minimum target geometric error in meters for the root tile.
// LOD levels are added until the root's error first exceeds this threshold, so the
// actual value may be higher. Higher targets produce a coarser root tile visible from
// farther away. Values <= 0 use a dataset-size-dependent default.
func WithInitialGeometricError(ge float64) tilerOptionsFn {
	return func(o *TilerOptions) { o.initialGeometricError = ge }
}
