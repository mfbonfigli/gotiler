package kd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"path"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/mfbonfigli/gotiler/v3/internal/utils"
	coor "github.com/mfbonfigli/gotiler/v3/tiler/coord"
	"github.com/mfbonfigli/gotiler/v3/tiler/geom"
	"github.com/mfbonfigli/gotiler/v3/tiler/model"
	"github.com/mfbonfigli/gotiler/v3/tiler/mutator"
	"github.com/mfbonfigli/gotiler/v3/tiler/pointcloud"
	"github.com/mfbonfigli/gotiler/v3/tiler/tree"
)

// voxelSizeDivideFactor defines how much the size of the voxels for sampling is reduced at every pass
const voxelSizeDivideFactor = 2.0

// reservoirSizeDefault is the size of the sample to use to determine the KD tree shape
const reservoirSizeDefault = 3_000_000

// maxPointsPerLeafDefault is approximate max points per leaf value to use by default
const maxPointsPerLeafDefault = 50_000

// maxLruCacheSize is the max number of open file descriptors during the initialize step
const maxLruCacheSize = 131_072

// lodSelectFraction is the fraction of the root's points that go to each new child LOD node
// (voxel-sampled). The remaining (1 - lodSelectFraction) stay as residuals in the root.
// With 0.75: root keeps 1/4, child gets 3/4 → root.GE is √3 ≈ 1.73× higher per level.
const lodSelectFraction = 0.66

// geometricErrorChildMultiplier keeps emitted parent tile errors slightly above
// child tile errors so validators and viewers see a strictly decreasing LOD
// hierarchy.
const geometricErrorChildMultiplier = 1.01

// voxelKey identifies a voxel cell by its integer grid coordinates.
type voxelKey struct{ x, y, z int }

// bestMatch tracks the best (closest-to-center) candidate point within a voxel cell.
type bestMatch struct {
	pointIndex int
	minDistSq  float64
}

// Config holds shared configuration for all nodes in the tree.
type Config struct {
	reservoirSize     int
	numWorkers        int
	pointsPerTile     int
	refineMode        model.RefineMode
	dataFolder        string
	tmpFolder         string
	ioFactory         IoFactory
	writerPool        *lruWriterPool
	rootTargetGeomErr float64 // 0 means derive from dataset bounds
	attributes        model.Attributes
	outputAttributes  model.Attributes
	attrSummaries     []model.AttributeSummary

	// Slice pools, used to share memory buffers between tree nodes
	pointPool   *utils.SlicePool[model.Point] // large: maxPointsPerLeaf*2, used in processNode for child points + ADD losers
	boolPool    *utils.SlicePool[bool]        // same capacity, used for globalPickedSet
	intPool     *utils.SlicePool[int]         // same capacity, used for remainingIndices
	routingPool *utils.SlicePool[model.Point] // small: pointsPerChunk, used in addBatch routing
}

// Node represents a KD binary tree node to organize points in space according to the axis
// of maximum variance. Every node contains only 2 children since it's a binary tree.
type Node struct {
	config        *Config
	bounds        geom.BoundingBox
	isLeaf        bool
	isRoot        bool
	axis          uint8
	splitValue    float64
	left          *Node
	right         *Node
	filename      string
	numPoints     atomic.Int64
	totalPoints   atomic.Int64
	localToGlobal *model.Transform
	*sync.Mutex
}

// NewTree returns a new tree with default settings.
func NewTree(opts ...func(*Node)) *Node {
	config := &Config{
		reservoirSize: reservoirSizeDefault,
		numWorkers:    max(1, runtime.NumCPU()),
		pointsPerTile: maxPointsPerLeafDefault,
		ioFactory:     NewFileIoFactory(),
	}
	t := &Node{
		Mutex:  &sync.Mutex{},
		bounds: geom.NewBoundingBox(math.MaxFloat64, -math.MaxFloat64, math.MaxFloat64, -math.MaxFloat64, math.MaxFloat64, -math.MaxFloat64),
		config: config,
		isRoot: true,
	}
	for _, optFn := range opts {
		optFn(t)
	}

	// If a data folder was specified, create tmp subfolder
	if config.dataFolder != "" {
		tmpPath := path.Join(config.dataFolder, "tmp")
		if err := os.MkdirAll(tmpPath, 0755); err != nil {
			panic(fmt.Sprintf("kd.NewTree: failed to create tmp folder %s: %v", tmpPath, err))
		}
		config.tmpFolder = tmpPath
	}

	config.writerPool = newLruWriterPool(maxLruCacheSize, config.ioFactory.NewWriter)

	// Size pools from final maxPointsPerLeaf (×2 for 2 children merging)
	bufCap := config.pointsPerTile * 2
	config.pointPool = utils.NewSlicePool[model.Point](bufCap)
	config.boolPool = utils.NewSlicePool[bool](bufCap)
	config.intPool = utils.NewSlicePool[int](bufCap)
	config.routingPool = utils.NewSlicePool[model.Point](pointsPerChunk)

	return t
}

// WithDataFolder defines the base folder where the tree will store its working
// data. A "tmp" subfolder is created inside and deleted on Dispose().
func WithDataFolder(path string) func(*Node) {
	return func(t *Node) {
		t.config.dataFolder = path
	}
}

// WithReservoirSize defines the size of the initial sample used to construct
// the spatial subdivision of the tree. Small samples can result in very uneven
// distributions, large samples consume more memory.
func WithReservoirSize(n int) func(*Node) {
	return func(t *Node) {
		t.config.reservoirSize = n
	}
}

// WithNumWorkers defines how many parallel workers can the tree use
// to process the data.
func WithNumWorkers(n int) func(*Node) {
	return func(t *Node) {
		t.config.numWorkers = n
	}
}

// WithPointsPerTile defines the target number of points for every node in the tree
// The number is not enforced strictly, it's to be intended as an approximate target.
func WithPointsPerTile(n int) func(*Node) {
	return func(t *Node) {
		t.config.pointsPerTile = n
	}
}

// WithRefineMode sets the type of refine mode of the tree between ADD or REPLACE. ADD saves
// bandwidth on the client size as it removes point redundancy across level of details, however
// it can increase the number of network calls as all LOD need to be downloaded and
// it increases the time needed to tile the point cloud. REPLACE is faster to generate
// but less space efficient.
func WithRefineMode(mode model.RefineMode) func(*Node) {
	return func(t *Node) {
		t.config.refineMode = mode
	}
}

// WithIoFactory sets the IoFactory used to create PointReader and PointWriter
// instances for the tree's intermediate point storage.
func WithIoFactory(f IoFactory) func(*Node) {
	return func(t *Node) {
		t.config.ioFactory = f
	}
}

// WithRootTargetGeomErr sets the minimum target geometric error for the root tile.
// LOD levels are added until the root's error first exceeds this threshold, so the
// actual value may be higher. Higher targets produce a coarser root tile visible from
// farther away. Values <= 0 derive the target from the dataset bounds.
func WithRootTargetGeomErr(ge float64) func(*Node) {
	return func(t *Node) {
		t.config.rootTargetGeomErr = ge
	}
}

// WithAttributes sets the ordered generic attribute requests stored by the tree.
func WithAttributes(attrs model.Attributes) func(*Node) {
	return func(t *Node) {
		t.config.attributes = attrs
	}
}

// WithOutputAttributes sets the ordered generic attribute requests eligible
// for tile output. Attributes present only for mutators remain available during
// loading but are marked skipped in the exported summaries.
func WithOutputAttributes(attrs model.Attributes) func(*Node) {
	return func(t *Node) {
		t.config.outputAttributes = attrs
	}
}

// NewNode istantiates a new non-root node from the given config. To create a tree root node use NewTree instead.
func NewNode(t *model.Transform, config *Config) *Node {
	return &Node{
		Mutex:         &sync.Mutex{},
		bounds:        geom.NewBoundingBox(math.MaxFloat64, -math.MaxFloat64, math.MaxFloat64, -math.MaxFloat64, math.MaxFloat64, -math.MaxFloat64),
		localToGlobal: t,
		config:        config,
	}
}

func (n *Node) BoundingBox() geom.BoundingBox {
	return n.bounds
}

func (n *Node) ChildrenAt(i uint8) tree.Node {
	switch i {
	case 0:
		if n.left == nil {
			return nil
		}
		return n.left
	case 1:
		if n.right == nil {
			return nil
		}
		return n.right
	default:
		return nil
	}
}

func (n *Node) Points() (geom.PointList, error) {
	if n.filename == "" {
		return &kdPointList{}, nil
	}
	reader, err := n.config.ioFactory.NewReader(n.filename)
	if err != nil {
		return &kdPointList{}, err
	}
	return &kdPointList{reader: reader, ioFactory: n.config.ioFactory, filename: n.filename}, nil
}

func (n *Node) TotalNumberOfPoints() int {
	return int(n.totalPoints.Load())
}

func (n *Node) NumberOfPoints() int {
	return int(n.numPoints.Load())
}

func (n *Node) IsRoot() bool {
	return n.isRoot
}

func (n *Node) IsLeaf() bool {
	return n.isLeaf
}

// GeometricError provides an estimation of the error for rendering this tile and not the children.
// Given most geographical point clouds are really representations of surfaces, the estimation is done
// with an heuristic based on the computation of the available surface area of the tile per point, with
// an empirical correction factor.
func (n *Node) GeometricError() float64 {
	ge := n.rawGeometricError()
	for _, child := range []*Node{n.left, n.right} {
		if child == nil || child.TotalNumberOfPoints() <= 0 {
			continue
		}
		ge = math.Max(ge, child.GeometricError()*geometricErrorChildMultiplier)
	}
	return ge
}

func (n *Node) rawGeometricError() float64 {
	if n.isLeaf {
		return 0
	}
	dx := n.bounds.Xmax - n.bounds.Xmin
	dy := n.bounds.Ymax - n.bounds.Ymin
	dz := n.bounds.Zmax - n.bounds.Zmin
	surfaceArea := (dx*dy + dx*dz + dy*dz)
	pts := n.numPoints.Load()
	if pts <= 0 {
		return 0
	}
	return math.Sqrt(surfaceArea/float64(pts)) * 4.0
}

func (n *Node) ToParentCRS() *model.Transform {
	return n.localToGlobal
}

func (n *Node) RefineMode() model.RefineMode {
	if n.config != nil {
		return n.config.refineMode
	}
	return model.RefineReplace
}

func (n *Node) AttributeSummaries() []model.AttributeSummary {
	if n.config == nil || len(n.config.attrSummaries) == 0 {
		return nil
	}
	out := make([]model.AttributeSummary, len(n.config.attrSummaries))
	copy(out, n.config.attrSummaries)
	return out
}

func (n *Node) Load(r pointcloud.Reader, cf coor.ConverterFactory, mut mutator.Mutator, ctx context.Context, reporter tree.ProgressReporter) error {
	outputAttributes := n.config.outputAttributes
	if outputAttributes == nil {
		outputAttributes = n.config.attributes
	}
	loader := NewReservoirLoader(cf, mut, n.config.reservoirSize, n.config.numWorkers, n.config.tmpFolder, n.config.ioFactory, n.config.attributes, outputAttributes)
	res, err := loader.Run(r, ctx, reporter)
	if err != nil {
		return err
	}
	n.localToGlobal = &res.localToGlobal
	n.config.attrSummaries = res.attrSummaries
	if f, ok := n.config.ioFactory.(attributeIoFactory); ok {
		f.SetAttributeSummaries(res.attrSummaries)
	}
	return n.initialize(res, ctx, reporter)
}

func (n *Node) Build(ctx context.Context, reporter tree.ProgressReporter) error {
	if err := n.bubbleUp(ctx, reporter); err != nil {
		return err
	}
	return n.insertLODChain(ctx)
}

func (n *Node) Dispose() error {
	var disposeFn func(nd *Node)
	disposeFn = func(nd *Node) {
		if nd == nil {
			return
		}
		disposeFn(nd.left)
		disposeFn(nd.right)
		if nd.filename != "" {
			if err := os.Remove(nd.filename); err != nil && !os.IsNotExist(err) {
				fmt.Fprintf(os.Stderr, "gotiler: warning: failed to remove temp file %s: %v\n", nd.filename, err)
			}
		}
	}
	disposeFn(n)
	if n.config != nil && n.config.tmpFolder != "" {
		os.RemoveAll(n.config.tmpFolder)
	}
	return nil
}

func (n *Node) RootNode() tree.Node {
	return n
}

// Phases returns the processing phases the kd-tree performs in order.
func (n *Node) Phases() []tree.PhaseInfo {
	return []tree.PhaseInfo{
		{Name: "reading", Label: "Reading", Unit: "pts"},
		{Name: "splitting", Label: "Splitting", Unit: "pts"},
		{Name: "building", Label: "Building", Unit: "nodes"},
	}
}

// initialize uses the reservoirResult to create the KD-tree structure from the collected sample
// and loading into the leaf nodes the points read from the temporary binary file created earlier
func (n *Node) initialize(r *reservoirLoaderResult, ctx context.Context, reporter tree.ProgressReporter) error {
	defer os.Remove(r.tempFilePath)

	n.bounds = r.bounds

	// estimate the number of leaves
	numLeaves := (r.totalPoints + n.config.pointsPerTile - 1) / n.config.pointsPerTile
	if numLeaves < 1 {
		numLeaves = 1
	}
	buildKDTreeRecursive(r.sample, numLeaves, n)

	tree.ReportProgress(reporter, tree.ProgressUpdate{
		Phase:       "splitting",
		Percent:     0,
		Message:     fmt.Sprintf("distributing %d points to leaf nodes", r.totalPoints),
		IsMilestone: true,
		ItemCount:   0,
		ItemTotal:   int64(r.totalPoints),
	})

	reader, err := n.config.ioFactory.NewReader(r.tempFilePath)
	if err != nil {
		return err
	}
	defer reader.Close()

	subCtx, cancFn := context.WithCancel(ctx)
	defer cancFn()

	errChan := make(chan error, n.config.numWorkers)
	wg := sync.WaitGroup{}

	var pointsDistributed atomic.Int64
	total := int64(r.totalPoints)

	for range n.config.numWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()

			// Allocate a reusable slice chunk for streaming reads
			readBuf := make([]model.Point, 0, pointsPerChunk)

			for {
				if err := subCtx.Err(); err != nil {
					return
				}

				var readErr error
				readBuf, readErr = reader.NextBatch(readBuf)
				if len(readBuf) == 0 && readErr == io.EOF {
					return
				}
				if readErr != nil && readErr != io.EOF {
					select {
					case errChan <- readErr:
						cancFn()
					default:
					}
					return
				}

				// Check context before entering potentially long recursive addBatch
				if err := subCtx.Err(); err != nil {
					return
				}

				// Distribute batch items into the KD-Tree infrastructure
				if err := n.addBatch(readBuf, subCtx); err != nil {
					select {
					case errChan <- err:
						cancFn()
					default:
					}
					return
				}

				// Throttled progress: emit when crossing a progressReportInterval boundary.
				after := pointsDistributed.Add(int64(len(readBuf)))
				before := after - int64(len(readBuf))
				if after/progressReportInterval > before/progressReportInterval {
					pct := math.Min(99, float64(after)/float64(total)*100)
					tree.ReportProgress(reporter, tree.ProgressUpdate{
						Phase:     "splitting",
						Percent:   pct,
						Message:   fmt.Sprintf("distributed %d points", after),
						ItemCount: after,
						ItemTotal: total,
					})
				}

				if readErr == io.EOF {
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errChan)

	// Flush all leaf writers via the LRU pool. A close failure means leaf data
	// may have been silently truncated, so it must be surfaced as an error.
	closeErr := n.config.writerPool.CloseAll()

	var workerErr error
	if len(errChan) > 0 {
		workerErr = <-errChan
	}
	if err := errors.Join(workerErr, closeErr); err != nil {
		return err
	}

	tree.ReportProgress(reporter, tree.ProgressUpdate{
		Phase:       "splitting",
		Percent:     100,
		Message:     fmt.Sprintf("distributed %d points", r.totalPoints),
		IsMilestone: true,
		ItemCount:   int64(r.totalPoints),
		ItemTotal:   int64(r.totalPoints),
	})

	return nil
}

func (n *Node) bubbleUp(ctx context.Context, reporter tree.ProgressReporter) error {
	totalNodes := countInternalNodes(n)
	var nodesProcessed atomic.Int64

	tree.ReportProgress(reporter, tree.ProgressUpdate{
		Phase:       "building",
		Percent:     0,
		Message:     fmt.Sprintf("building tree (%d internal nodes)", totalNodes),
		IsMilestone: true,
		ItemCount:   0,
		ItemTotal:   totalNodes,
	})

	for n.filename == "" {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("build cancelled: %w", err)
		}

		var readyNodes []*Node
		findReadyNodes(n, &readyNodes)

		if len(readyNodes) == 0 {
			break
		}

		producerWg := &sync.WaitGroup{}
		workerWg := &sync.WaitGroup{}
		nodeChan := make(chan *Node, 2*n.config.numWorkers)

		// Size the error channel to match the number of workers exactly
		// to guarantee no worker blocks when writing an error.
		errCh := make(chan error, n.config.numWorkers)
		subCtx, cancel := context.WithCancel(ctx)

		// launch producer, which enqueues nodes
		producerWg.Add(1)
		go func() {
			defer producerWg.Done()
			defer close(nodeChan)
			for _, node := range readyNodes {
				// Properly block on channel send or context cancellation
				select {
				case <-subCtx.Done():
					return
				case nodeChan <- node:
				}
			}
		}()

		// launch consumers, that bubble up points
		for range n.config.numWorkers {
			workerWg.Add(1)
			go func() {
				defer workerWg.Done()
				for node := range nodeChan {
					if subCtx.Err() != nil {
						return
					}
					if err := node.processNode(ctx); err != nil {
						// Safely report error without blocking
						select {
						case errCh <- fmt.Errorf("build: process node failed: %w", err):
						default:
						}
						cancel()
						return
					}
					processed := nodesProcessed.Add(1)
					pct := math.Min(99, float64(processed)/float64(totalNodes)*100)
					tree.ReportProgress(reporter, tree.ProgressUpdate{
						Phase:     "building",
						Percent:   pct,
						Message:   fmt.Sprintf("built %d/%d nodes", processed, totalNodes),
						ItemCount: processed,
						ItemTotal: totalNodes,
					})
				}
			}()
		}

		producerWg.Wait()
		workerWg.Wait()
		cancel()

		close(errCh)
		for err := range errCh {
			if err != nil {
				return err
			}
		}
	}

	tree.ReportProgress(reporter, tree.ProgressUpdate{
		Phase:       "building",
		Percent:     100,
		Message:     "tree build complete",
		IsMilestone: true,
		ItemCount:   totalNodes,
		ItemTotal:   totalNodes,
	})

	return nil
}

// insertLODChain inserts synthetic LOD levels above the natural tree root until the root's
// geometric error reaches lodTargetGeometricError. Each iteration:
//  1. Randomly samples lodSelectFraction of the root's current points → new child node Rx.
//  2. Rx inherits the root's current children; root's new child becomes Rx.
//  3. Root keeps the residual (1 - lodSelectFraction) points.
//
// In ADD mode the split is disjoint: Rx owns the selected points and the root keeps only
// the residuals (rendering root+Rx together yields the full set). In REPLACE mode Rx must
// stand alone when it replaces the root, so Rx keeps ALL the points and the root holds a
// copy of the residual subset as a sparser preview — no points are lost on replacement.
//
// After k iterations the chain is: root → Rx_k → Rx_(k-1) → … → Rx_1 → original children.
// Root.GE grows by 1/sqrt(1-lodSelectFraction) ≈ 2× per iteration (with the 3/4 fraction).
func (n *Node) insertLODChain(ctx context.Context) error {
	if n.filename == "" {
		return nil // root has no points; nothing to do
	}
	// Ensure the root is not marked as a leaf so GeometricError() is computed from the formula.
	n.isLeaf = false

	targetGE := n.rootTargetGeometricError()
	isAdd := n.config.refineMode == model.RefineAdd

	for i := 0; i < 30 && n.rawGeometricError() < targetGE; i++ {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("insertLODChain cancelled: %w", err)
		}

		pts, err := n.readAllOwnPoints()
		if err != nil {
			return fmt.Errorf("insertLODChain: read root points: %w", err)
		}
		if len(pts) < 4 {
			break // too few points to split meaningfully
		}

		targetSelect := int(float64(len(pts)) * lodSelectFraction)
		if targetSelect == 0 {
			break
		}

		selected, residuals := randomSplit(pts, targetSelect)
		if len(selected) == 0 || len(residuals) == 0 {
			break
		}

		// ADD: Rx owns only the selected points (disjoint split with the root).
		// REPLACE: Rx keeps all the points; the root's residuals are a copy.
		childPts := selected
		if !isAdd {
			childPts = pts
		}

		// Create Rx: inherits root's current children, owns the child points.
		rx := &Node{
			Mutex:         &sync.Mutex{},
			bounds:        n.bounds,
			localToGlobal: n.localToGlobal,
			config:        n.config,
			left:          n.left,
			right:         n.right,
		}
		rxFilename, err := writePointsToTemp(childPts, n.config, "lod_*.bin")
		if err != nil {
			return fmt.Errorf("insertLODChain: write selected: %w", err)
		}
		rx.filename = rxFilename
		rx.numPoints.Store(int64(len(childPts)))
		rx.totalPoints.Store(int64(len(childPts))) // only > 0 check is used by the writer

		// Update root: residuals only, single child = Rx.
		os.Remove(n.filename)
		rFilename, err := writePointsToTemp(residuals, n.config, "lod_*.bin")
		if err != nil {
			return fmt.Errorf("insertLODChain: write residuals: %w", err)
		}
		n.filename = rFilename
		n.numPoints.Store(int64(len(residuals)))
		n.left = rx
		n.right = nil
	}

	return nil
}

func (n *Node) rootTargetGeometricError() float64 {
	if n.config != nil && n.config.rootTargetGeomErr > 0 {
		return n.config.rootTargetGeomErr
	}
	return defaultRootTargetGeometricError(n.bounds)
}

func defaultRootTargetGeometricError(b geom.BoundingBox) float64 {
	dx := math.Max(0, b.Xmax-b.Xmin)
	dy := math.Max(0, b.Ymax-b.Ymin)
	dz := math.Max(0, b.Zmax-b.Zmin)
	side := math.Max(dx, math.Max(dy, dz))
	return side * math.Sqrt(3) / 5
}

// readAllOwnPoints reads all points stored in this node's own file into memory.
func (n *Node) readAllOwnPoints() ([]model.Point, error) {
	if n.filename == "" {
		return nil, nil
	}
	reader, err := n.config.ioFactory.NewReader(n.filename)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	var pts []model.Point
	buf := make([]model.Point, 0, pointsPerChunk)
	for {
		var readErr error
		buf, readErr = reader.NextBatch(buf)
		pts = append(pts, buf...)
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, readErr
		}
	}
	return pts, nil
}

// writePointsToTemp writes pts to a new temp file in cfg.tmpFolder and returns its path.
func writePointsToTemp(pts []model.Point, cfg *Config, pattern string) (string, error) {
	f, err := os.CreateTemp(cfg.tmpFolder, pattern)
	if err != nil {
		return "", err
	}
	name := f.Name()
	f.Close()

	w, err := cfg.ioFactory.NewWriter(name)
	if err != nil {
		os.Remove(name)
		return "", err
	}
	if err := w.WriteBatch(pts); err != nil {
		w.Close()
		os.Remove(name)
		return "", err
	}
	w.Close()
	return name, nil
}

// randomSplit partitions pts into (selected[:targetCount], residuals) via a deterministic
// partial Fisher-Yates shuffle (seed=0). Used by insertLODChain where the input is already
// spatially uniform; voxel resampling of uniform input produces Moiré-pattern stripes.
func randomSplit(pts []model.Point, targetCount int) ([]model.Point, []model.Point) {
	if targetCount <= 0 {
		return nil, append([]model.Point(nil), pts...)
	}
	if targetCount >= len(pts) {
		return append([]model.Point(nil), pts...), nil
	}
	work := make([]model.Point, len(pts))
	copy(work, pts)
	rng := rand.New(rand.NewSource(0))
	for i := 0; i < targetCount; i++ {
		j := i + rng.Intn(len(work)-i)
		work[i], work[j] = work[j], work[i]
	}
	return work[:targetCount], work[targetCount:]
}

// voxelSampleSplit selects approximately targetCount representative points from pts via
// iterative voxel sampling. Returns (selected, residuals, error). The residuals slice is
// only built when collectResiduals is true (ADD mode); otherwise nil is returned to avoid
// copying the unpicked points for nothing (REPLACE mode discards them). Uses cfg pools for
// scratch space — must not be called concurrently on the same Config.
func voxelSampleSplit(pts []model.Point, targetCount int, collectResiduals bool, ctx context.Context, cfg *Config) ([]model.Point, []model.Point, error) {
	if len(pts) == 0 || targetCount <= 0 {
		if !collectResiduals {
			return nil, nil, nil
		}
		cp := make([]model.Point, len(pts))
		copy(cp, pts)
		return nil, cp, nil
	}
	if targetCount >= len(pts) {
		cp := make([]model.Point, len(pts))
		copy(cp, pts)
		return cp, nil, nil
	}

	// 1. Compute local bounding box.
	cMinX, cMinY, cMinZ := float64(pts[0].X), float64(pts[0].Y), float64(pts[0].Z)
	cMaxX, cMaxY, cMaxZ := cMinX, cMinY, cMinZ
	for i := 1; i < len(pts); i++ {
		p := pts[i]
		x, y, z := float64(p.X), float64(p.Y), float64(p.Z)
		if x < cMinX {
			cMinX = x
		}
		if x > cMaxX {
			cMaxX = x
		}
		if y < cMinY {
			cMinY = y
		}
		if y > cMaxY {
			cMaxY = y
		}
		if z < cMinZ {
			cMinZ = z
		}
		if z > cMaxZ {
			cMaxZ = z
		}
	}
	dx := cMaxX - cMinX
	dy := cMaxY - cMinY
	dz := cMaxZ - cMinZ
	if dx <= 0 {
		dx = 1e-5
	}
	if dy <= 0 {
		dy = 1e-5
	}
	if dz <= 0 {
		dz = 1e-5
	}

	// 2. Initial voxel size estimate via volume approach.
	s := math.Pow((dx*dy*dz)/float64(targetCount), 1.0/3.0)
	if s <= 0 {
		s = 1e-5
	}

	globalPickedSetPtr := cfg.boolPool.GetCleared(len(pts))
	globalPickedSet := *globalPickedSetPtr
	defer cfg.boolPool.Put(globalPickedSetPtr)

	remainingIndicesPtr := cfg.intPool.GetWithMinCapacity(len(pts))
	remainingIndices := (*remainingIndicesPtr)[:len(pts)]
	defer cfg.intPool.Put(remainingIndicesPtr)
	for i := range pts {
		remainingIndices[i] = i
	}

	pickedPoints := make([]model.Point, 0, targetCount)
	voxelMap := make(map[voxelKey]bestMatch, targetCount)
	passPicked := make([]int, 0, targetCount)

	// 3. Iterative refinement loop.
	iterCtr := 0
	for len(pickedPoints) < targetCount && len(remainingIndices) > 0 && s > 1e-7 {
		iterCtr++
		if iterCtr%100 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, nil, fmt.Errorf("build cancelled: %w", err)
			}
		}

		clear(voxelMap)

		for _, idx := range remainingIndices {
			p := pts[idx]
			px, py, pz := float64(p.X), float64(p.Y), float64(p.Z)
			vx := int(math.Floor(px / s))
			vy := int(math.Floor(py / s))
			vz := int(math.Floor(pz / s))
			key := voxelKey{vx, vy, vz}
			vCenterX := (float64(vx) + 0.5) * s
			vCenterY := (float64(vy) + 0.5) * s
			vCenterZ := (float64(vz) + 0.5) * s
			diffX := px - vCenterX
			diffY := py - vCenterY
			diffZ := pz - vCenterZ
			distSq := diffX*diffX + diffY*diffY + diffZ*diffZ
			match, exists := voxelMap[key]
			if !exists || distSq < match.minDistSq {
				voxelMap[key] = bestMatch{pointIndex: idx, minDistSq: distSq}
			}
		}

		passPicked = passPicked[:0]
		for _, match := range voxelMap {
			passPicked = append(passPicked, match.pointIndex)
		}
		sort.Ints(passPicked)

		for _, idx := range passPicked {
			if len(pickedPoints) >= targetCount {
				break
			}
			if !globalPickedSet[idx] {
				globalPickedSet[idx] = true
				pickedPoints = append(pickedPoints, pts[idx])
			}
		}
		if len(pickedPoints) >= targetCount {
			break
		}

		writeIdx := 0
		for _, idx := range remainingIndices {
			if !globalPickedSet[idx] {
				remainingIndices[writeIdx] = idx
				writeIdx++
			}
		}
		remainingIndices = remainingIndices[:writeIdx]
		s /= voxelSizeDivideFactor
	}

	// Safety backup: fill quota from remaining unpicked points if the voxel loop exhausted.
	if len(pickedPoints) < targetCount && len(remainingIndices) > 0 {
		for _, idx := range remainingIndices {
			globalPickedSet[idx] = true
			pickedPoints = append(pickedPoints, pts[idx])
			if len(pickedPoints) == targetCount {
				break
			}
		}
	}

	// Build residuals from unpicked points (only needed in ADD mode).
	var residuals []model.Point
	if collectResiduals {
		residuals = make([]model.Point, 0, len(pts)-len(pickedPoints))
		for i, pt := range pts {
			if !globalPickedSet[i] {
				residuals = append(residuals, pt)
			}
		}
	}

	return pickedPoints, residuals, nil
}

// countInternalNodes counts all non-leaf nodes in the tree rooted at n.
func countInternalNodes(n *Node) int64 {
	if n == nil || n.isLeaf {
		return 0
	}
	return 1 + countInternalNodes(n.left) + countInternalNodes(n.right)
}

func findReadyNodes(node *Node, list *[]*Node) {
	if node == nil {
		return
	}
	if node.filename != "" {
		return
	}

	leftReady := node.left == nil || node.left.filename != ""
	rightReady := node.right == nil || node.right.filename != ""

	if leftReady && rightReady {
		*list = append(*list, node)
		return
	}

	findReadyNodes(node.left, list)
	findReadyNodes(node.right, list)
}

func (n *Node) processNode(ctx context.Context) error {
	cfg := n.config
	tmpFile, err := os.CreateTemp(cfg.tmpFolder, "node_*.bin")
	if err != nil {
		return fmt.Errorf("bubbleUp: failed to create temp file: %w", err)
	}
	filename := tmpFile.Name()
	tmpFile.Close()

	w, err := cfg.ioFactory.NewWriter(filename)
	if err != nil {
		return fmt.Errorf("bubbleUp: failed to create writer: %w", err)
	}
	defer w.Close()

	n.filename = filename

	isAdd := cfg.refineMode == model.RefineAdd

	children := []*Node{n.left, n.right}
	for _, child := range children {
		if child == nil || child.filename == "" {
			continue
		}

		pointsPtr := cfg.pointPool.Get()
		reader, err := cfg.ioFactory.NewReader(child.filename)
		if err != nil {
			cfg.pointPool.Put(pointsPtr)
			return fmt.Errorf("bubbleUp: failed to open reader for child: %w", err)
		}

		chunkBuf := make([]model.Point, 0, pointsPerChunk)
		for {
			var readErr error
			chunkBuf, readErr = reader.NextBatch(chunkBuf)
			if len(chunkBuf) > 0 {
				*pointsPtr = append(*pointsPtr, chunkBuf...)
			}
			if readErr == io.EOF {
				break
			}
			if readErr != nil {
				reader.Close()
				cfg.pointPool.Put(pointsPtr)
				return fmt.Errorf("bubbleUp: failed to read child points: %w", readErr)
			}
		}
		reader.Close()

		points := *pointsPtr
		if len(points) == 0 {
			cfg.pointPool.Put(pointsPtr)
			continue
		}

		targetCount := len(points) / 2
		if targetCount == 0 {
			cfg.pointPool.Put(pointsPtr)
			continue
		}

		selected, residuals, err := voxelSampleSplit(points, targetCount, isAdd, ctx, cfg)
		if err != nil {
			cfg.pointPool.Put(pointsPtr)
			return err
		}

		if err := w.WriteBatch(selected); err != nil {
			cfg.pointPool.Put(pointsPtr)
			return fmt.Errorf("bubbleUp: failed to write points to parent node: %w", err)
		}
		n.numPoints.Add(int64(len(selected)))

		// Refine ADD mode: rewrite child file with only loser points.
		if isAdd {
			oldFilename := child.filename
			if len(residuals) > 0 {
				loserName, err := writePointsToTemp(residuals, cfg, "node_*.bin")
				if err != nil {
					cfg.pointPool.Put(pointsPtr)
					return fmt.Errorf("bubbleUp: failed to write ADD child losers: %w", err)
				}
				child.filename = loserName
				child.numPoints.Store(int64(len(residuals)))
			} else {
				child.filename = ""
				child.numPoints.Store(0)
			}
			os.Remove(oldFilename)
		}

		cfg.pointPool.Put(pointsPtr)
	}

	// 4. Update parent bounding box
	pMinX, pMinY, pMinZ := math.MaxFloat64, math.MaxFloat64, math.MaxFloat64
	pMaxX, pMaxY, pMaxZ := -math.MaxFloat64, -math.MaxFloat64, -math.MaxFloat64
	hasValidChild := false

	if n.left != nil {
		hasValidChild = true
		if n.left.bounds.Xmin < pMinX {
			pMinX = n.left.bounds.Xmin
		}
		if n.left.bounds.Xmax > pMaxX {
			pMaxX = n.left.bounds.Xmax
		}
		if n.left.bounds.Ymin < pMinY {
			pMinY = n.left.bounds.Ymin
		}
		if n.left.bounds.Ymax > pMaxY {
			pMaxY = n.left.bounds.Ymax
		}
		if n.left.bounds.Zmin < pMinZ {
			pMinZ = n.left.bounds.Zmin
		}
		if n.left.bounds.Zmax > pMaxZ {
			pMaxZ = n.left.bounds.Zmax
		}
	}

	if n.right != nil {
		hasValidChild = true
		if n.right.bounds.Xmin < pMinX {
			pMinX = n.right.bounds.Xmin
		}
		if n.right.bounds.Xmax > pMaxX {
			pMaxX = n.right.bounds.Xmax
		}
		if n.right.bounds.Ymin < pMinY {
			pMinY = n.right.bounds.Ymin
		}
		if n.right.bounds.Ymax > pMaxY {
			pMaxY = n.right.bounds.Ymax
		}
		if n.right.bounds.Zmin < pMinZ {
			pMinZ = n.right.bounds.Zmin
		}
		if n.right.bounds.Zmax > pMaxZ {
			pMaxZ = n.right.bounds.Zmax
		}
	}

	if hasValidChild {
		n.bounds = geom.NewBoundingBox(pMinX, pMaxX, pMinY, pMaxY, pMinZ, pMaxZ)
	}

	return nil
}

// addBatch divides a collection of elements recursively into tree children branches.
func (n *Node) addBatch(points []model.Point, ctx context.Context) error {
	if len(points) == 0 {
		return nil
	}

	n.totalPoints.Add(int64(len(points)))

	if n.isLeaf {
		if err := n.writeBatch(points, ctx); err != nil {
			return err
		}
		n.numPoints.Add(int64(len(points)))
		return nil
	}

	// Pull reusable slice vectors from sync.Pool instead of allocating heap memory per call.
	rp := n.config.routingPool
	leftPtr := rp.Get()
	rightPtr := rp.Get()

	// returnBuffers helps to wrap the operations to dispose the buffers back in the pool
	returnBuffers := func() {
		rp.Put(leftPtr)
		rp.Put(rightPtr)
	}

	leftBatch := *leftPtr
	rightBatch := *rightPtr

	for _, p := range points {
		if coordAtAxis(p, n.axis) < n.splitValue {
			leftBatch = append(leftBatch, p)
		} else {
			rightBatch = append(rightBatch, p)
		}
	}

	var err error
	if len(leftBatch) > 0 {
		if err = n.left.addBatch(leftBatch, ctx); err != nil {
			*leftPtr = leftBatch
			*rightPtr = rightBatch
			returnBuffers()
			return err
		}
	}
	if len(rightBatch) > 0 {
		if err = n.right.addBatch(rightBatch, ctx); err != nil {
			*leftPtr = leftBatch
			*rightPtr = rightBatch
			returnBuffers()
			return err
		}
	}

	*leftPtr = leftBatch
	*rightPtr = rightBatch
	returnBuffers()
	return nil
}

// writeBatch handles lock orchestration and bulk flushing safely for leaf destinations.
func (n *Node) writeBatch(points []model.Point, ctx context.Context) error {
	// Check context before acquiring the lock to avoid blocking
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("writeBatch cancelled: %w", err)
	}

	n.Lock()
	defer n.Unlock()

	if n.filename == "" {
		tmpFile, err := os.CreateTemp(n.config.tmpFolder, "node_*.bin")
		if err != nil {
			return fmt.Errorf("kd: failed to create temp file: %w", err)
		}
		n.filename = tmpFile.Name()
		tmpFile.Close()
	}

	if err := n.config.writerPool.WriteBatch(n.filename, points); err != nil {
		return err
	}

	for _, p := range points {
		x, y, z := float64(p.X), float64(p.Y), float64(p.Z)
		if x < n.bounds.Xmin {
			n.bounds.Xmin = x
		}
		if x > n.bounds.Xmax {
			n.bounds.Xmax = x
		}
		if y < n.bounds.Ymin {
			n.bounds.Ymin = y
		}
		if y > n.bounds.Ymax {
			n.bounds.Ymax = y
		}
		if z < n.bounds.Zmin {
			n.bounds.Zmin = z
		}
		if z > n.bounds.Zmax {
			n.bounds.Zmax = z
		}
	}

	return nil
}

func coordAtAxis(p model.Point, axis uint8) float64 {
	switch axis {
	case 1:
		return float64(p.Y)
	case 2:
		return float64(p.Z)
	default:
		return float64(p.X)
	}
}

// buildKDTreeRecursive uses the array of model.Point as input, typically a small random
// subsample of the tree, to initialize the tree nodes. The logic it is used to build it
// ensures the leaves have very similar point countss
func buildKDTreeRecursive(points []model.Point, targetLeaves int, node *Node) {
	if targetLeaves <= 1 || len(points) < 2 {
		node.isLeaf = true
		return
	}
	node.isLeaf = false

	// pick the axis of highest variance among all points
	axis := highestVarianceAxis(points)
	node.axis = axis

	// partition the points around the median coordinate on the split axis: a full sort
	// is not needed, only the median element and the two halves are used downstream
	mid := len(points) / 2
	selectKthAtAxis(points, mid, axis)
	node.splitValue = coordAtAxis(points[mid], axis)

	// left target is the number of leaves that should be created on the left branch, which is half of them
	leftTarget := targetLeaves / 2

	// right target is the number of leaves that should be created on the right branch, which is all the remaining
	rightTarget := targetLeaves - leftTarget

	// send the half the points left and half the points right and continue recursively
	node.left = NewNode(node.localToGlobal, node.config)
	buildKDTreeRecursive(points[:mid], leftTarget, node.left)
	node.right = NewNode(node.localToGlobal, node.config)
	buildKDTreeRecursive(points[mid:], rightTarget, node.right)
}

// selectKthAtAxis partially reorders points in place so that points[k] holds the element
// with the k-th smallest coordinate on the given axis, every element in points[:k] has a
// coordinate <= coordAtAxis(points[k], axis) and every element in points[k:] has a
// coordinate >= it. It is a deterministic quickselect with a median-of-three pivot and a
// three-way (Dutch national flag) partition, which keeps duplicate-heavy inputs at O(N)
// instead of degrading quadratically.
func selectKthAtAxis(points []model.Point, k int, axis uint8) {
	lo, hi := 0, len(points)
	for hi-lo > 1 {
		pivot := medianOfThreeAtAxis(points, lo, hi, axis)

		// Three-way partition of [lo, hi): [lo, lt) < pivot, [lt, gt) == pivot, [gt, hi) > pivot.
		lt, gt := lo, hi
		i := lo
		for i < gt {
			v := coordAtAxis(points[i], axis)
			switch {
			case v < pivot:
				points[i], points[lt] = points[lt], points[i]
				lt++
				i++
			case v > pivot:
				gt--
				points[i], points[gt] = points[gt], points[i]
			default:
				i++
			}
		}

		switch {
		case k < lt:
			hi = lt
		case k >= gt:
			lo = gt
		default:
			return // k falls inside the run of elements equal to the pivot
		}
	}
}

// medianOfThreeAtAxis returns the median of the axis coordinates of the first, middle and
// last points in [lo, hi). The result is always the coordinate of an actual element in the
// range, guaranteeing the three-way partition makes progress.
func medianOfThreeAtAxis(points []model.Point, lo, hi int, axis uint8) float64 {
	a := coordAtAxis(points[lo], axis)
	b := coordAtAxis(points[lo+(hi-lo)/2], axis)
	c := coordAtAxis(points[hi-1], axis)
	switch {
	case (a <= b) == (b <= c):
		return b
	case (b <= a) == (a <= c):
		return a
	default:
		return c
	}
}

func highestVarianceAxis(pts []model.Point) uint8 {
	nn := float64(len(pts))
	var sumX, sumY, sumZ float64
	var sumSqX, sumSqY, sumSqZ float64

	for _, p := range pts {
		x := float64(p.X)
		y := float64(p.Y)
		z := float64(p.Z)
		sumX += x
		sumY += y
		sumZ += z
		sumSqX += x * x
		sumSqY += y * y
		sumSqZ += z * z
	}

	varX := (sumSqX / nn) - (sumX/nn)*(sumX/nn)
	varY := (sumSqY / nn) - (sumY/nn)*(sumY/nn)
	varZ := (sumSqZ / nn) - (sumZ/nn)*(sumZ/nn)

	if varY > varX && varY > varZ {
		return 1
	}
	if varZ > varX && varZ > varY {
		return 2
	}
	return 0
}

type kdPointList struct {
	reader    PointReader
	ioFactory IoFactory
	filename  string
	closed    bool
	buf       []model.Point
	idx       int
}

func (p *kdPointList) Len() int {
	if p.reader == nil {
		return 0
	}
	return p.reader.NumPoints()
}

// Next reads single points from an in-memory buffer chunk.
// When the buffer is empty, it requests a new block via NextBatch.
func (p *kdPointList) Next() (model.Point, error) {
	if p.reader == nil || p.closed {
		return model.Point{}, io.EOF
	}

	// If our memory buffer has run dry, pull the next sequential batch from disk
	if p.idx >= len(p.buf) {
		var err error
		p.buf, err = p.reader.NextBatch(p.buf)
		p.idx = 0

		if len(p.buf) == 0 && err == io.EOF {
			return model.Point{}, io.EOF
		}
		if err != nil && err != io.EOF {
			return model.Point{}, err
		}
	}

	pt := p.buf[p.idx]
	p.idx++
	return pt, nil
}

func (p *kdPointList) Reset() {
	if p.reader == nil {
		return
	}
	p.closed = false
	p.idx = 0
	p.buf = p.buf[:0]

	// Close the old reader before creating a new one
	p.reader.Close()

	reader, err := p.ioFactory.NewReader(p.filename)
	if err != nil {
		p.closed = true
		return
	}
	p.reader = reader
}

func (p *kdPointList) Close() error {
	if p.reader == nil || p.closed {
		return nil
	}
	p.closed = true
	p.buf = nil
	return p.reader.Close()
}
