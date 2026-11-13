package writer

import (
	"context"
	"errors"
	"fmt"
	"math"
	"path"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/mfbonfigli/gotiler/v3/internal/tileset"
	"github.com/mfbonfigli/gotiler/v3/tiler/encoding"
	"github.com/mfbonfigli/gotiler/v3/tiler/model"
	"github.com/mfbonfigli/gotiler/v3/tiler/plugin"
	"github.com/mfbonfigli/gotiler/v3/tiler/tree"
	"github.com/mfbonfigli/gotiler/v3/version"
)

const pntsFilename string = "d.pnts"
const glbFilename string = "d.glb"
const dataFolder string = "data"

// Writer writes a tree as a 3D Cesium Point cloud to the given output folder
type Writer interface {
	// Write converts the tree into 3D tiles in folderName under the configured base path.
	// reporter receives progress updates for the "exporting" phase; pass nil to suppress.
	Write(t tree.Tree, folderName string, ctx context.Context, reporter tree.ProgressReporter) error
}

type StandardWriter struct {
	numWorkers       int
	bufferRatio      int
	basePath         string
	encoderID        string
	encoderFactory   plugin.GeometryEncoderFactory
	tilesetVersion   version.TilesetVersion
	contentFilename  string
	producerFunc     func(plugin.WriterProvider) Producer
	consumerFunc     func() Consumer
	writerProvider   plugin.WriterProvider
	writerMiddleware []plugin.WriterMiddleware
	writerFinalizers []plugin.WriterFinalizer
	geCorrection     float64
	attributes       model.Attributes
	// maxTilesPerTileset is the most tiles a tileset JSON file holds before
	// subtrees move into external tilesets, 0 for a single tileset.json.
	maxTilesPerTileset int
}

func init() {
	plugin.RegisterGeometryEncoder(plugin.EncoderPNTS, func(attrs model.Attributes) plugin.GeometryEncoder {
		return NewPntsEncoder(pntsFilename, attrs)
	})
	plugin.RegisterGeometryEncoder(plugin.EncoderGLB, func(attrs model.Attributes) plugin.GeometryEncoder {
		return NewGltfEncoder(glbFilename, attrs)
	})
}

func NewWriter(basePath string, options ...func(*StandardWriter)) (*StandardWriter, error) {
	w := &StandardWriter{
		basePath:     basePath,
		numWorkers:   1,
		bufferRatio:  5,
		encoderID:    plugin.EncoderGLB,
		geCorrection: 1.0,
		attributes:   model.DefaultAttributes(),
		producerFunc: NewStandardProducer,
	}
	w.writerProvider = NewDiskWriterProvider(basePath)
	for _, optFn := range options {
		optFn(w)
	}
	factory, ok := plugin.GeometryEncoderFactoryFor(w.encoderID)
	if !ok {
		return nil, fmt.Errorf("unsupported geometry encoder %q; supported encoders: %v", w.encoderID, plugin.SupportedGeometryEncoders())
	}
	encoder := factory(w.attributes)
	if encoder == nil {
		return nil, fmt.Errorf("geometry encoder %q returned nil", w.encoderID)
	}
	w.encoderFactory = factory
	w.tilesetVersion = encoder.TilesetVersion()
	w.contentFilename = encoder.ContentFilename()

	w.consumerFunc = func() Consumer {
		return NewStandardConsumer(WithGeometryEncoder(w.encoderFactory(w.attributes)))
	}

	return w, nil
}

// WithNumWorkers defines how many writer goroutines to launch when writing the tiles.
func WithNumWorkers(n int) func(*StandardWriter) {
	return func(w *StandardWriter) {
		w.numWorkers = n
	}
}

// WithBufferRation defines how many jobs per writer worker to allow enqueuing.
func WithBufferRatio(n int) func(*StandardWriter) {
	return func(w *StandardWriter) {
		w.bufferRatio = int(math.Max(1, float64(n)))
	}
}

// WithEncoder selects the geometry encoder by registry ID. The encoder controls
// both tile content format and tileset version.
func WithEncoder(id string) func(*StandardWriter) {
	return func(w *StandardWriter) {
		w.encoderID = id
	}
}

// WithGECorrection sets a multiplier applied to every geometric error value written
// to tileset.json. Use values > 1 to make tiles appear at greater distances,
// < 1 to make them appear only up close. Default is 1.0 (no correction).
func WithGECorrection(c float64) func(*StandardWriter) {
	return func(w *StandardWriter) { w.geCorrection = c }
}

// WithAttributes sets which optional per-point attributes are written to output tiles.
// Default is all attributes (intensity and classification) enabled.
func WithAttributes(attrs model.Attributes) func(*StandardWriter) {
	return func(w *StandardWriter) { w.attributes = attrs }
}

// WithMaxTilesPerTileset sets the most tiles a tileset JSON file holds: the
// subtrees of larger tilesets move into external tilesets next to their
// tiles, so viewers read the top of the tree first. Default is 0, a single
// tileset.json.
func WithMaxTilesPerTileset(n int) func(*StandardWriter) {
	return func(w *StandardWriter) { w.maxTilesPerTileset = n }
}

// WithWriterProvider sets the destination provider used for tile content and
// tileset.json. The provider receives output names relative to the tileset root,
// such as "data/d.glb" and "tileset.json".
func WithWriterProvider(wp plugin.WriterProvider) func(*StandardWriter) {
	return func(w *StandardWriter) {
		if wp != nil {
			w.writerProvider = wp
		}
	}
}

// WithWriterMiddleware wraps the output provider used for tile content and
// tileset.json. Middlewares are applied after folder scoping.
func WithWriterMiddleware(middlewares ...plugin.WriterMiddleware) func(*StandardWriter) {
	return func(w *StandardWriter) {
		w.writerMiddleware = append(w.writerMiddleware, middlewares...)
	}
}

// WithWriterFinalizer registers finalizers to run after all tile content and
// tileset.json have been written.
func WithWriterFinalizer(finalizers ...plugin.WriterFinalizer) func(*StandardWriter) {
	return func(w *StandardWriter) {
		for _, finalizer := range finalizers {
			if finalizer != nil {
				w.writerFinalizers = append(w.writerFinalizers, finalizer)
			}
		}
	}
}

func (w *StandardWriter) Write(t tree.Tree, folderName string, ctx context.Context, reporter tree.ProgressReporter) error {
	// Count total tiles and set up progress tracking when a reporter is provided.
	var (
		tilesTotal   int64
		tilesWritten atomic.Int64
	)
	if reporter != nil {
		tilesTotal = int64(countTreeNodes(t.RootNode()))
		tree.ReportProgress(reporter, tree.ProgressUpdate{
			Phase:       "exporting",
			Percent:     0,
			Message:     fmt.Sprintf("exporting %d tiles", tilesTotal),
			IsMilestone: true,
			ItemCount:   0,
			ItemTotal:   tilesTotal,
		})
	}

	// afterTile is called by the consumer after each tile is successfully written.
	afterTile := func() {
		if reporter == nil {
			return
		}
		n := tilesWritten.Add(1)
		pct := math.Min(99, float64(n)/float64(tilesTotal)*100)
		tree.ReportProgress(reporter, tree.ProgressUpdate{
			Phase:     "exporting",
			Percent:   pct,
			Message:   fmt.Sprintf("exported %d/%d tiles", n, tilesTotal),
			ItemCount: n,
			ItemTotal: tilesTotal,
		})
	}

	rootWriterProvider := plugin.ChainWriterMiddleware(PrefixWriterProvider(w.writerProvider, folderName), w.writerMiddleware...)

	// Build a producer that sets OnDone on every WorkUnit it produces.
	producerFn := w.producerFunc
	if reporter != nil {
		producerFn = func(wp plugin.WriterProvider) Producer {
			return newStandardProducerWithCallback(wp, afterTile)
		}
	}

	// init channel where consumers can eventually submit errors that prevented them to finish the job
	errorChannel := make(chan error)

	// launch error listener
	var errorWaitGroup sync.WaitGroup
	errs := []error{}
	errorWaitGroup.Add(1)
	go func() {
		defer errorWaitGroup.Done()
		for err := range errorChannel {
			errs = append(errs, err)
		}
	}()

	// init channel where to submit work with a buffer N times greater than the number of consumer
	workChannel := make(chan *WorkUnit, w.numWorkers*w.bufferRatio)

	var waitGroup sync.WaitGroup

	// producing is easy, only 1 producer
	producer := producerFn(rootWriterProvider)
	waitGroup.Add(1)
	go producer.Produce(workChannel, errorChannel, &waitGroup, t.RootNode(), ctx)

	// add consumers to waitgroup and launch them
	for i := 0; i < w.numWorkers; i++ {
		waitGroup.Add(1)
		// instantiate a new converter per each goroutine for thread safety
		consumer := w.consumerFunc()
		go consumer.Consume(workChannel, errorChannel, &waitGroup)
	}

	// wait for producers and consumers to finish
	waitGroup.Wait()

	// close error chan
	close(errorChannel)
	errorWaitGroup.Wait()

	// runFinalizers runs all registered finalizers joining their errors. Finalizers must run
	// even when tile writing failed, e.g. to release resources held by output plugins.
	runFinalizers := func() error {
		var err error
		for _, finalizer := range w.writerFinalizers {
			err = errors.Join(err, finalizer.Finalize())
		}
		return err
	}

	if len(errs) != 0 {
		// tile writing failed: do not write the tileset.json for a failed export but still
		// run the finalizers, joining all collected errors with theirs
		return errors.Join(errors.Join(errs...), runFinalizers())
	}

	if val := ctx.Value("IS_TEST"); val != nil && val.(bool) {
		// if we are executing tests, do not write the tileset
		// TODO: allow to mock or inject the writer
		return nil
	}

	if err := w.writeTileset(t, rootWriterProvider); err != nil {
		return errors.Join(err, runFinalizers())
	}
	if err := runFinalizers(); err != nil {
		return err
	}

	tree.ReportProgress(reporter, tree.ProgressUpdate{
		Phase:       "exporting",
		Percent:     100,
		Message:     fmt.Sprintf("exported %d tiles", tilesTotal),
		IsMilestone: true,
		ItemCount:   tilesTotal,
		ItemTotal:   tilesTotal,
	})

	return nil
}

// writeTileset writes tileset.json, and the external tilesets of a large
// tree, after the tiles.
func (w *StandardWriter) writeTileset(t tree.Tree, wp plugin.WriterProvider) error {
	root := w.buildTileTree(t.RootNode(), "")
	header := tileset.Header{Version: string(w.tilesetVersion)}
	opts := tileset.Options{MaxTiles: w.maxTilesPerTileset}
	if _, err := tileset.Write(tileset.Create(wp), root, header, w.attributeRangeMetadata(t), opts); err != nil {
		return fmt.Errorf("failed to write the tileset: %w", err)
	}
	return nil
}

// attributeRangeMetadata returns the dataset-global minimum and maximum of
// every exported per-point attribute, for the root tileset.json, so clients
// can build shaders and gradients without scanning the tiles. 3D Tiles 1.0
// uses the top-level "properties" dictionary keyed by the per-point property
// name; 3D Tiles 1.1 uses the core metadata mechanism: a schema plus a
// tileset metadata entity with MIN_/MAX_-prefixed properties. Ranges are
// emitted for every selected attribute with observed values, even when the
// tile format cannot carry the per-point data (e.g. 64-bit integers): the
// metadata is then the only surviving trace of the attribute.
func (w *StandardWriter) attributeRangeMetadata(t tree.Tree) tileset.RootOnly {
	provider, ok := t.(tree.AttributeSummaryProvider)
	if !ok {
		return tileset.RootOnly{}
	}
	if w.tilesetVersion == version.TilesetVersion_1_0 {
		props := map[string]PropertyMinMax{}
		for _, s := range provider.AttributeSummaries() {
			mn, mx, ok := summaryRange(s)
			if !ok {
				continue
			}
			props[encoding.AttributeOutputName(s.Name)] = PropertyMinMax{Minimum: mn, Maximum: mx}
		}
		if len(props) == 0 {
			return tileset.RootOnly{}
		}
		return tileset.RootOnly{Properties: props}
	}

	classProps := map[string]TilesetClassProperty{}
	values := map[string]any{}
	for _, s := range provider.AttributeSummaries() {
		mn, mx, ok := summaryRange(s)
		if !ok {
			continue
		}
		metaType, componentType, err := encoding.GltfMetadataComponentType(s.Type)
		if err != nil {
			continue
		}
		id := encoding.MetadataPropertyID(encoding.AttributeOutputName(s.Name))
		property := TilesetClassProperty{Type: metaType, ComponentType: componentType}
		classProps["MIN_"+id] = property
		classProps["MAX_"+id] = property
		values["MIN_"+id] = mn
		values["MAX_"+id] = mx
	}
	if len(values) == 0 {
		return tileset.RootOnly{}
	}
	return tileset.RootOnly{
		Schema: &TilesetSchema{
			ID: "gotiler_dataset",
			Classes: map[string]TilesetSchemaClass{
				"dataset": {Properties: classProps},
			},
		},
		Metadata: &TilesetMetadata{
			Class:      "dataset",
			Properties: values,
		},
	}
}

// summaryRange unboxes the global min/max of an exported attribute summary
// into JSON-encodable values. Booleans are exposed as 0/1, matching their
// per-point representation in the tiles. Incomplete attributes and
// attributes with no observed values are skipped.
func summaryRange(s model.AttributeSummary) (mn, mx any, ok bool) {
	if s.SkipIncomplete || s.Min == nil || s.Max == nil {
		return nil, nil, false
	}
	unbox := func(v any) (any, bool) {
		switch x := v.(type) {
		case int64, uint64, float64:
			return x, true
		case bool:
			if x {
				return 1, true
			}
			return 0, true
		default:
			return nil, false
		}
	}
	mn, okMin := unbox(s.Min)
	mx, okMax := unbox(s.Max)
	if !okMin || !okMax {
		return nil, nil, false
	}
	return mn, mx, true
}

// buildTileTree builds the tile hierarchy of the tree under node, the root.
func (w *StandardWriter) buildTileTree(node tree.Node, parentPath string) *tileset.Tile {
	root := w.buildTile(node, "")
	if trans := node.ToParentCRS(); trans != nil && *trans != model.IdentityTransform {
		cMajor := trans.ForwardColumnMajor()
		root.Transform = &cMajor
	}
	if root.Content != "" && parentPath != "" {
		root.Content = path.Join(parentPath, root.Content)
	}
	return root
}

// buildTile builds the tile of node and of its descendants, prefix naming
// the node's content as its path from the root, one digit per level.
func (w *StandardWriter) buildTile(node tree.Node, prefix string) *tileset.Tile {
	tile := &tileset.Tile{
		Box:            node.BoundingBox().AsCesiumBox(),
		GeometricError: node.GeometricError() * w.geCorrection,
		Refine:         string(node.RefineMode()),
	}
	if node.TotalNumberOfPoints() > 0 {
		tile.Content = path.Join(dataFolder, prefix+w.contentFilename)
	}
	for i := range 8 {
		child := node.ChildrenAt(uint8(i))
		if child != nil && child.TotalNumberOfPoints() > 0 {
			tile.Children = append(tile.Children, w.buildTile(child, prefix+strconv.Itoa(i)))
		}
	}
	return tile
}

// countTreeNodes returns the number of nodes in the tree that have points.
func countTreeNodes(node tree.Node) int {
	if node == nil {
		return 0
	}
	count := 0
	if node.NumberOfPoints() > 0 {
		count = 1
	}
	for i := range 8 {
		child := node.ChildrenAt(uint8(i))
		if child != nil {
			count += countTreeNodes(child)
		}
	}
	return count
}
