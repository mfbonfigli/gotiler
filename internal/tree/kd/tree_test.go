package kd

import (
	"context"
	"errors"
	"math/rand"
	"sort"
	"testing"

	"github.com/mfbonfigli/gotiler/v3/tiler/geom"
	"github.com/mfbonfigli/gotiler/v3/tiler/model"
)

// closeFailIoFactory delegates reads to a real file-based factory but returns
// writers that accept every batch and fail on Close, simulating a flush
// failure (e.g. ENOSPC) when the writer pool closes the leaf files.
type closeFailIoFactory struct {
	inner    IoFactory
	closeErr error
}

func (f *closeFailIoFactory) NewReader(filePath string) (PointReader, error) {
	return f.inner.NewReader(filePath)
}

func (f *closeFailIoFactory) NewWriter(name string) (PointWriter, error) {
	return &mockPointWriter{name: name, closeErr: f.closeErr}, nil
}

// testPoints returns n points with unique, float32-exact X coordinates spread
// inside [0, 10) on every axis.
func testPoints(n int) []model.Point {
	pts := make([]model.Point, n)
	for i := range pts {
		v := float32(i) * 10.0 / float32(n)
		pts[i] = model.Point{X: v, Y: v, Z: v}
	}
	return pts
}

func TestInitialize_ReturnsWriterPoolCloseError(t *testing.T) {
	closeErr := errors.New("flush boom")
	factory := &closeFailIoFactory{inner: NewFileIoFactory(), closeErr: closeErr}
	n := NewTree(WithDataFolder(t.TempDir()), WithIoFactory(factory), WithPointsPerTile(5), WithNumWorkers(1))

	pts := testPoints(10)
	tempFilePath, err := writePointsToTemp(pts, &Config{tmpFolder: n.config.tmpFolder, ioFactory: NewFileIoFactory()}, "init_*.bin")
	if err != nil {
		t.Fatalf("failed to write temp points file: %v", err)
	}

	res := &reservoirLoaderResult{
		sample:       testPoints(10),
		bounds:       geom.NewBoundingBox(0, 10, 0, 10, 0, 10),
		totalPoints:  len(pts),
		tempFilePath: tempFilePath,
	}

	err = n.initialize(res, context.Background(), nil)
	if err == nil {
		t.Fatal("expected initialize to surface the writer pool close error, got nil")
	}
	if !errors.Is(err, closeErr) {
		t.Errorf("expected initialize error to wrap the close error, got %v", err)
	}
}

// newLODChainTestRoot builds a root node holding numPts points with unique X
// coordinates and a target geometric error calibrated so that insertLODChain
// performs exactly one iteration: with bounds [0,10]^3 and 1000 points the
// root GE is ~2.19 before and ~3.76 after one iteration, so a target of 3.0
// stops the loop after the first pass.
func newLODChainTestRoot(t *testing.T, mode model.RefineMode, numPts int) (*Node, []model.Point) {
	t.Helper()
	n := NewTree(WithDataFolder(t.TempDir()), WithRefineMode(mode), WithRootTargetGeomErr(3.0))
	pts := testPoints(numPts)
	filename, err := writePointsToTemp(pts, n.config, "lod_*.bin")
	if err != nil {
		t.Fatalf("failed to write root points: %v", err)
	}
	n.filename = filename
	n.numPoints.Store(int64(numPts))
	n.totalPoints.Store(int64(numPts))
	n.bounds = geom.NewBoundingBox(0, 10, 0, 10, 0, 10)
	return n, pts
}

// pointSetByX indexes points by their (unique) X coordinate.
func pointSetByX(t *testing.T, pts []model.Point) map[float32]bool {
	t.Helper()
	set := make(map[float32]bool, len(pts))
	for _, p := range pts {
		if set[p.X] {
			t.Fatalf("duplicate point X=%v in node contents", p.X)
		}
		set[p.X] = true
	}
	return set
}

func TestInsertLODChain_AddModeSplitsDisjoint(t *testing.T) {
	n, original := newLODChainTestRoot(t, model.RefineAdd, 1000)

	if err := n.insertLODChain(context.Background()); err != nil {
		t.Fatalf("insertLODChain: %v", err)
	}

	rx := n.left
	if rx == nil || n.right != nil {
		t.Fatalf("expected a single synthetic child, got left=%v right=%v", n.left, n.right)
	}
	if rx.left != nil {
		t.Fatalf("expected exactly one LOD iteration, but the chain has more levels")
	}

	rootPts, err := n.readAllOwnPoints()
	if err != nil {
		t.Fatalf("read root points: %v", err)
	}
	childPts, err := rx.readAllOwnPoints()
	if err != nil {
		t.Fatalf("read child points: %v", err)
	}

	wantChild := int(float64(len(original)) * lodSelectFraction)
	if len(childPts) != wantChild {
		t.Errorf("expected child to hold %d points, got %d", wantChild, len(childPts))
	}
	if len(rootPts)+len(childPts) != len(original) {
		t.Errorf("expected root+child == total (%d), got %d + %d", len(original), len(rootPts), len(childPts))
	}

	rootSet := pointSetByX(t, rootPts)
	childSet := pointSetByX(t, childPts)
	for x := range rootSet {
		if childSet[x] {
			t.Fatalf("ADD mode must be disjoint: point X=%v is in both root and child", x)
		}
	}
	originalSet := pointSetByX(t, original)
	for x := range originalSet {
		if !rootSet[x] && !childSet[x] {
			t.Fatalf("point X=%v lost: present in neither root nor child", x)
		}
	}
}

func TestInsertLODChain_ReplaceModeChildKeepsAllPoints(t *testing.T) {
	n, original := newLODChainTestRoot(t, model.RefineReplace, 1000)

	if err := n.insertLODChain(context.Background()); err != nil {
		t.Fatalf("insertLODChain: %v", err)
	}

	rx := n.left
	if rx == nil || n.right != nil {
		t.Fatalf("expected a single synthetic child, got left=%v right=%v", n.left, n.right)
	}
	if rx.left != nil {
		t.Fatalf("expected exactly one LOD iteration, but the chain has more levels")
	}

	rootPts, err := n.readAllOwnPoints()
	if err != nil {
		t.Fatalf("read root points: %v", err)
	}
	childPts, err := rx.readAllOwnPoints()
	if err != nil {
		t.Fatalf("read child points: %v", err)
	}

	// REPLACE: the child replaces the parent, so it must keep ALL the points.
	if len(childPts) != len(original) {
		t.Errorf("expected child to hold all %d points, got %d", len(original), len(childPts))
	}
	childSet := pointSetByX(t, childPts)
	for _, p := range original {
		if !childSet[p.X] {
			t.Fatalf("point X=%v lost from child in REPLACE mode", p.X)
		}
	}

	// The root holds the sparser preview subset, copied from the child's points.
	wantRoot := len(original) - int(float64(len(original))*lodSelectFraction)
	if len(rootPts) != wantRoot {
		t.Errorf("expected root to hold %d preview points, got %d", wantRoot, len(rootPts))
	}
	rootSet := pointSetByX(t, rootPts)
	for x := range rootSet {
		if !childSet[x] {
			t.Fatalf("root preview point X=%v is not a copy of a child point", x)
		}
	}
}

func TestSelectKthAtAxis(t *testing.T) {
	rng := rand.New(rand.NewSource(42))

	makeRandom := func(n int) []model.Point {
		pts := make([]model.Point, n)
		for i := range pts {
			pts[i] = model.Point{X: rng.Float32() * 100, Y: rng.Float32() * 100, Z: rng.Float32() * 100}
		}
		return pts
	}
	makeDuplicateHeavy := func(n int) []model.Point {
		pts := make([]model.Point, n)
		for i := range pts {
			pts[i] = model.Point{X: float32(rng.Intn(3)), Y: float32(rng.Intn(2)), Z: float32(rng.Intn(5))}
		}
		return pts
	}
	makeAllEqual := func(n int) []model.Point {
		pts := make([]model.Point, n)
		for i := range pts {
			pts[i] = model.Point{X: 7, Y: 7, Z: 7}
		}
		return pts
	}
	makeSorted := func(n int) []model.Point {
		return testPoints(n)
	}
	makeReversed := func(n int) []model.Point {
		pts := testPoints(n)
		for i, j := 0, len(pts)-1; i < j; i, j = i+1, j-1 {
			pts[i], pts[j] = pts[j], pts[i]
		}
		return pts
	}

	cases := []struct {
		name string
		gen  func(n int) []model.Point
	}{
		{"random", makeRandom},
		{"duplicateHeavy", makeDuplicateHeavy},
		{"allEqual", makeAllEqual},
		{"sorted", makeSorted},
		{"reversed", makeReversed},
	}

	for _, tc := range cases {
		for _, n := range []int{1, 2, 3, 4, 10, 101, 1000} {
			for axis := uint8(0); axis < 3; axis++ {
				pts := tc.gen(n)

				// Reference: the true median value from a fully sorted copy.
				wantCoords := make([]float64, len(pts))
				for i, p := range pts {
					wantCoords[i] = coordAtAxis(p, axis)
				}
				sort.Float64s(wantCoords)

				mid := len(pts) / 2
				selectKthAtAxis(pts, mid, axis)

				gotMedian := coordAtAxis(pts[mid], axis)
				if gotMedian != wantCoords[mid] {
					t.Fatalf("%s n=%d axis=%d: element at mid is %v, true median is %v", tc.name, n, axis, gotMedian, wantCoords[mid])
				}

				// Partition invariant: points[:mid] <= points[mid] <= points[mid:].
				for i := 0; i < mid; i++ {
					if coordAtAxis(pts[i], axis) > gotMedian {
						t.Fatalf("%s n=%d axis=%d: points[%d]=%v > median %v", tc.name, n, axis, i, coordAtAxis(pts[i], axis), gotMedian)
					}
				}
				for i := mid; i < len(pts); i++ {
					if coordAtAxis(pts[i], axis) < gotMedian {
						t.Fatalf("%s n=%d axis=%d: points[%d]=%v < median %v", tc.name, n, axis, i, coordAtAxis(pts[i], axis), gotMedian)
					}
				}

				// The selection must be a permutation: same coordinate multiset.
				gotCoords := make([]float64, len(pts))
				for i, p := range pts {
					gotCoords[i] = coordAtAxis(p, axis)
				}
				sort.Float64s(gotCoords)
				for i := range gotCoords {
					if gotCoords[i] != wantCoords[i] {
						t.Fatalf("%s n=%d axis=%d: coordinate multiset changed at index %d", tc.name, n, axis, i)
					}
				}
			}
		}
	}
}

func TestVoxelSampleSplit_ResidualCollection(t *testing.T) {
	cfg := NewTree(WithPointsPerTile(100)).config
	pts := testPoints(100)
	targetCount := len(pts) / 2

	// With residual collection, selected+residuals must partition the input.
	selected, residuals, err := voxelSampleSplit(pts, targetCount, true, context.Background(), cfg)
	if err != nil {
		t.Fatalf("voxelSampleSplit: %v", err)
	}
	if len(selected) != targetCount {
		t.Errorf("expected %d selected points, got %d", targetCount, len(selected))
	}
	if len(selected)+len(residuals) != len(pts) {
		t.Errorf("expected selected+residuals == %d, got %d + %d", len(pts), len(selected), len(residuals))
	}
	all := pointSetByX(t, append(append([]model.Point{}, selected...), residuals...))
	for _, p := range pts {
		if !all[p.X] {
			t.Fatalf("point X=%v lost by voxelSampleSplit", p.X)
		}
	}

	// Without residual collection (REPLACE mode), the same points must be
	// selected but no residual slice must be built.
	selectedNoRes, residualsNoRes, err := voxelSampleSplit(pts, targetCount, false, context.Background(), cfg)
	if err != nil {
		t.Fatalf("voxelSampleSplit without residuals: %v", err)
	}
	if residualsNoRes != nil {
		t.Errorf("expected nil residuals when collection is disabled, got %d points", len(residualsNoRes))
	}
	if len(selectedNoRes) != len(selected) {
		t.Fatalf("expected identical selection regardless of residual collection, got %d vs %d", len(selectedNoRes), len(selected))
	}
	for i := range selected {
		if selected[i].X != selectedNoRes[i].X {
			t.Fatalf("selection differs at index %d: X=%v vs X=%v", i, selected[i].X, selectedNoRes[i].X)
		}
	}
}
