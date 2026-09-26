package tiler

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mfbonfigli/gotiler/v3/internal/pc"
	"github.com/mfbonfigli/gotiler/v3/internal/testtree"
	"github.com/mfbonfigli/gotiler/v3/internal/tree/kd"
	"github.com/mfbonfigli/gotiler/v3/internal/utils"
	"github.com/mfbonfigli/gotiler/v3/internal/writer"
	coor "github.com/mfbonfigli/gotiler/v3/tiler/coord"
	"github.com/mfbonfigli/gotiler/v3/tiler/geom"
	"github.com/mfbonfigli/gotiler/v3/tiler/model"
	"github.com/mfbonfigli/gotiler/v3/tiler/mutator"
	"github.com/mfbonfigli/gotiler/v3/tiler/pointcloud"
	"github.com/mfbonfigli/gotiler/v3/tiler/tree"
)

type requestingMutator struct {
	attrs model.Attributes
}

func (m requestingMutator) RequiredAttributes() model.Attributes {
	return m.attrs
}

func (m requestingMutator) MutateChunk(chunk mutator.PointChunk, localToGlobal model.Transform) []model.Point {
	return chunk.Points
}

func (m requestingMutator) Close() error {
	return nil
}

// countingMutator counts MutateChunk and Close invocations and records
// whether it was invoked again after having been closed.
type countingMutator struct {
	mutateChunkCalls int
	closeCalls       int
	mutateAfterClose bool
}

func (m *countingMutator) RequiredAttributes() model.Attributes {
	return nil
}

func (m *countingMutator) MutateChunk(chunk mutator.PointChunk, localToGlobal model.Transform) []model.Point {
	m.mutateChunkCalls++
	if m.closeCalls > 0 {
		m.mutateAfterClose = true
	}
	return chunk.Points
}

func (m *countingMutator) Close() error {
	m.closeCalls++
	return nil
}

// mutatingMockNode is a MockNode whose Load invokes the mutator once,
// mimicking what a real tree does while loading points.
type mutatingMockNode struct {
	testtree.MockNode
}

func (n *mutatingMockNode) Load(l pointcloud.Reader, c coor.ConverterFactory, m mutator.Mutator, ctx context.Context, reporter tree.ProgressReporter) error {
	if err := n.MockNode.Load(l, c, m, ctx, reporter); err != nil {
		return err
	}
	m.MutateChunk(mutator.PointChunk{Points: []model.Point{{}}}, model.IdentityTransform)
	return nil
}

func TestTilerDefaults(t *testing.T) {
	tiler, err := NewGoTiler()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defaultOpts := NewDefaultTilerOptions()
	tr := tiler.treeProvider(treeOptions(defaultOpts, defaultOpts.attributes), "")
	switch tr.(type) {
	case *kd.Node:
	default:
		t.Errorf("unexpected tree type returned")
	}
	// this returns an error due to a non-esitant path
	// but we ignore it on purpose for the sake of this test
	l, _ := tiler.pointcloudReaderProvider([]string{""}, "EPSG:123", true, model.DefaultAttributes())
	switch l.(type) {
	case *pc.CombinedPointCloudReader:
	default:
		t.Errorf("unexpected las reader type returned")
	}
	// this returns an error due to a non-esitant path
	// but we ignore it on purpose for the sake of this test
	w, err := tiler.writerProvider("", NewDefaultTilerOptions())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	switch w.(type) {
	case *writer.StandardWriter:
	default:
		t.Errorf("unexpected writer type returned")
	}
}

func TestTilerProcessFile(t *testing.T) {
	tiler, err := NewGoTiler()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	w := &writer.MockWriter{}
	tr := &testtree.MockNode{}
	l := &pc.MockLasReader{}
	opts := NewDefaultTilerOptions()
	opts.mutators = []mutator.Mutator{requestingMutator{attrs: model.NewAttributes(model.AttrReturnNumber)}}
	c := context.TODO()
	var readerAttrs model.Attributes
	var gotTreeOpts tree.Options
	tiler.writerProvider = func(folder string, opts *TilerOptions) (writer.Writer, error) {
		return w, nil
	}
	tiler.treeProvider = func(opts tree.Options, output string) tree.Tree {
		gotTreeOpts = opts
		return tr
	}
	tiler.pointcloudReaderProvider = func(inputFiles []string, sourceCRS string, eightbit bool, attrs model.Attributes) (pointcloud.Reader, error) {
		readerAttrs = attrs
		return l, nil
	}

	tiler.ProcessFiles([]string{"abc.las"}, "out", "EPSG:123", opts, c)
	if !tr.LoadCalled {
		t.Errorf("Load was not called on the tree")
	}
	if actual := tr.Las; actual != l {
		t.Errorf("expected las reader %v got %v", l, actual)
	}
	if actual := tr.ConvFactory; actual == nil {
		t.Errorf("expected non-nil coordinate converter factory")
	}
	if actual := tr.Mut; actual == nil {
		t.Errorf("expected non-nil mutator")
	}
	if actual := tr.Ctx; actual != c {
		t.Errorf("expected different context")
	}
	if !readerAttrs.Has(model.AttrIntensity) || !readerAttrs.Has(model.AttrClassification) || !readerAttrs.Has(model.AttrReturnNumber) {
		t.Errorf("expected reader attrs to include output attrs plus mutator attrs, got %v", readerAttrs)
	}
	if !gotTreeOpts.Attributes.Has(model.AttrReturnNumber) {
		t.Errorf("expected tree input attrs to include mutator attr, got %v", gotTreeOpts.Attributes)
	}
	if gotTreeOpts.OutputAttributes.Has(model.AttrReturnNumber) {
		t.Errorf("did not expect mutator-only attr in tree output attrs, got %v", gotTreeOpts.OutputAttributes)
	}
	if !tr.BuildCalled {
		t.Errorf("Build was not called on the tree")
	}
	if !w.WriteCalled {
		t.Errorf("Write was not called on the writer")
	}
	if actual := w.Tr; actual != tr {
		t.Errorf("expected tree %v got %v", tr, actual)
	}
	if actual := w.FolderName; actual != "" {
		t.Errorf("expected folder name '%v' got %v", "", actual)
	}
	if actual := w.Ctx; actual != c {
		t.Errorf("expected different context")
	}
}

// paintingWriteMutator is a write-capable mutator that colors points at write time.
type paintingWriteMutator struct{}

func (paintingWriteMutator) RequiredAttributes() model.Attributes {
	return nil
}

func (paintingWriteMutator) MutateChunk(chunk mutator.PointChunk, localToGlobal model.Transform) []model.Point {
	return chunk.Points
}

func (paintingWriteMutator) MutateChunkOnWrite(chunk mutator.PointChunk, localToGlobal model.Transform) []model.Point {
	for i := range chunk.Points {
		chunk.Points[i].R = 42
	}
	return chunk.Points
}

func (paintingWriteMutator) Close() error {
	return nil
}

func TestTilerProcessFileWrapsTreeForWriteMutators(t *testing.T) {
	tiler, err := NewGoTiler()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	w := &writer.MockWriter{}
	tr := &testtree.MockNode{
		Pts: geom.NewLinkedPointStream(&geom.LinkedPoint{Pt: geom.NewPoint(1, 2, 3, 1, 2, 3)}, 1),
	}
	l := &pc.MockLasReader{}
	opts := NewDefaultTilerOptions()
	opts.mutators = []mutator.Mutator{paintingWriteMutator{}}
	tiler.writerProvider = func(folder string, opts *TilerOptions) (writer.Writer, error) {
		return w, nil
	}
	tiler.treeProvider = func(opts tree.Options, output string) tree.Tree {
		return tr
	}
	tiler.pointcloudReaderProvider = func(inputFiles []string, sourceCRS string, eightbit bool, attrs model.Attributes) (pointcloud.Reader, error) {
		return l, nil
	}

	if err := tiler.ProcessFiles([]string{"abc.las"}, "out", "EPSG:123", opts, context.TODO()); err != nil {
		t.Fatalf("ProcessFiles: %v", err)
	}
	if !w.WriteCalled {
		t.Fatal("Write was not called on the writer")
	}
	if w.Tr == tree.Tree(tr) {
		t.Fatal("expected the exported tree to be wrapped for write mutation")
	}
	pts, err := w.Tr.RootNode().Points()
	if err != nil {
		t.Fatalf("Points: %v", err)
	}
	pt, err := pts.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if pt.R != 42 {
		t.Fatalf("expected write-mutated point, got R=%d", pt.R)
	}
}

func TestTilerProcessFileWithPlacement(t *testing.T) {
	tiler, err := NewGoTiler()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	w := &writer.MockWriter{}
	tr := &testtree.MockNode{}
	l := &pc.MockLasReader{}
	opts := NewDefaultTilerOptions()
	opts.Apply(WithPlacement(Placement{Longitude: 90, Height: 100}))
	var readerCRS string
	tiler.writerProvider = func(folder string, opts *TilerOptions) (writer.Writer, error) {
		return w, nil
	}
	tiler.treeProvider = func(opts tree.Options, output string) tree.Tree {
		return tr
	}
	tiler.pointcloudReaderProvider = func(inputFiles []string, sourceCRS string, eightbit bool, attrs model.Attributes) (pointcloud.Reader, error) {
		readerCRS = sourceCRS
		return l, nil
	}

	if err := tiler.ProcessFiles([]string{"abc.las"}, "out", "", opts, context.TODO()); err != nil {
		t.Fatalf("ProcessFiles: %v", err)
	}
	if readerCRS != pointcloud.CRSLocal {
		t.Fatalf("expected reader to receive the local CRS sentinel, got %q", readerCRS)
	}
	// the converter handed to the tree must be the placement one: it maps the
	// local origin to the configured spot on the globe (lon 90, height 100)
	if tr.ConvFactory == nil {
		t.Fatal("expected a converter factory")
	}
	conv, err := tr.ConvFactory()
	if err != nil {
		t.Fatalf("ConvFactory: %v", err)
	}
	got, err := conv.ToWGS84Cartesian(pointcloud.CRSLocal, model.Vector{})
	if err != nil {
		t.Fatalf("ToWGS84Cartesian: %v", err)
	}
	if math.Abs(got.X) > 1e-6 || math.Abs(got.Y-(6378137+100)) > 1e-6 || math.Abs(got.Z) > 1e-6 {
		t.Fatalf("expected placement origin at lon 90 height 100, got %+v", got)
	}
	// CRS transformations must be unavailable in placement mode
	if _, err := conv.Transform("EPSG:4326", "EPSG:4978", model.Vector{}); err == nil {
		t.Fatal("expected CRS transformations to be unavailable in placement mode")
	}
}

func TestTilerProcessFileRejectsPlacementWithCRS(t *testing.T) {
	tiler, err := NewGoTiler()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	w := &writer.MockWriter{}
	tr := &testtree.MockNode{}
	opts := NewDefaultTilerOptions()
	opts.Apply(WithPlacement(Placement{}))
	tiler.writerProvider = func(folder string, opts *TilerOptions) (writer.Writer, error) {
		return w, nil
	}
	tiler.treeProvider = func(opts tree.Options, output string) tree.Tree {
		return tr
	}
	tiler.pointcloudReaderProvider = func(inputFiles []string, sourceCRS string, eightbit bool, attrs model.Attributes) (pointcloud.Reader, error) {
		return &pc.MockLasReader{}, nil
	}

	err = tiler.ProcessFiles([]string{"abc.las"}, "out", "EPSG:32633", opts, context.TODO())
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("expected placement+CRS to be rejected, got %v", err)
	}
	if tr.LoadCalled {
		t.Fatal("expected the run to abort before loading")
	}
}

func TestTilerProcessFileUsesConfiguredTreeProvider(t *testing.T) {
	tiler, err := NewGoTiler()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	w := &writer.MockWriter{}
	tr := &testtree.MockNode{}
	l := &pc.MockLasReader{}
	c := context.TODO()
	var gotTreeOpts tree.Options
	var gotOutput string

	tiler.writerProvider = func(folder string, opts *TilerOptions) (writer.Writer, error) {
		return w, nil
	}
	tiler.pointcloudReaderProvider = func(inputFiles []string, sourceCRS string, eightbit bool, attrs model.Attributes) (pointcloud.Reader, error) {
		return l, nil
	}

	opts := NewTilerOptions(
		WithWorkerNumber(3),
		WithPointsPerTile(100),
		WithRefineMode(model.RefineAdd),
		WithInitialGeometricError(42),
		WithTreeProvider(func(opts tree.Options, output string) tree.Tree {
			gotTreeOpts = opts
			gotOutput = output
			return tr
		}),
	)

	if err := tiler.ProcessFiles([]string{"abc.las"}, "out", "EPSG:123", opts, c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotOutput != "out" {
		t.Fatalf("expected output %q got %q", "out", gotOutput)
	}
	if gotTreeOpts.NumWorkers != 3 || gotTreeOpts.PointsPerTile != 200 || gotTreeOpts.RefineMode != model.RefineAdd || gotTreeOpts.InitialGeometricError != 42 {
		t.Fatalf("unexpected tree options: %+v", gotTreeOpts)
	}
	if !tr.LoadCalled || !tr.BuildCalled || !w.WriteCalled {
		t.Fatalf("expected injected tree to run through load/build/write")
	}
}

func TestTilerProcessFolder(t *testing.T) {
	tiler, err := NewGoTiler()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	w := &writer.MockWriter{}
	tr := &testtree.MockNode{}
	l := &pc.MockLasReader{}
	opts := NewDefaultTilerOptions()
	c := context.TODO()
	tiler.writerProvider = func(folder string, opts *TilerOptions) (writer.Writer, error) {
		return w, nil
	}
	tiler.treeProvider = func(opts tree.Options, output string) tree.Tree {
		return tr
	}
	files := []string{}
	tiler.pointcloudReaderProvider = func(inputFiles []string, sourceCRS string, eightbit bool, attrs model.Attributes) (pointcloud.Reader, error) {
		files = append(files, inputFiles...)
		return l, nil
	}

	tmp, err := os.MkdirTemp(os.TempDir(), "tst")
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	t.Cleanup(func() {
		os.RemoveAll(tmp)
	})
	utils.TouchFile(filepath.Join(tmp, "abc.las"))
	utils.TouchFile(filepath.Join(tmp, "def.xyz"))
	utils.TouchFile(filepath.Join(tmp, "ghi.las"))
	tiler.ProcessFolder(tmp, "out", "EPSG:123", opts, c)
	if !tr.LoadCalled {
		t.Errorf("Load was not called on the tree")
	}
	if actual := tr.Las; actual != l {
		t.Errorf("expected las reader %v got %v", l, actual)
	}
	if actual := tr.ConvFactory; actual == nil {
		t.Errorf("expected non-nil coordinate converter factory")
	}
	if actual := tr.Mut; actual == nil {
		t.Errorf("expected non-nil mutator")
	}
	if actual := tr.Ctx; actual != c {
		t.Errorf("expected different context")
	}
	if !tr.BuildCalled {
		t.Errorf("Build was not called on the tree")
	}
	if !w.WriteCalled {
		t.Errorf("Write was not called on the writer")
	}
	if actual := w.Tr; actual != tr {
		t.Errorf("expected tree %v got %v", tr, actual)
	}
	if actual := w.FolderName; actual != "" {
		t.Errorf("expected folder name '%v' got %v", "", actual)
	}
	if actual := w.Ctx; actual != c {
		t.Errorf("expected different context")
	}
	expected := []string{
		filepath.Join(tmp, "abc.las"),
		filepath.Join(tmp, "ghi.las"),
	}
	if !reflect.DeepEqual(files, expected) {
		t.Errorf("expected files processed %v, got %v", expected, files)
	}
}

func TestTilerProcessFolderReusesMutatorsAcrossFiles(t *testing.T) {
	tiler, err := NewGoTiler()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	w := &writer.MockWriter{}
	l := &pc.MockLasReader{}
	m := &countingMutator{}
	opts := NewDefaultTilerOptions()
	opts.mutators = []mutator.Mutator{m}
	c := context.TODO()
	tiler.writerProvider = func(folder string, opts *TilerOptions) (writer.Writer, error) {
		return w, nil
	}
	tiler.treeProvider = func(opts tree.Options, output string) tree.Tree {
		return &mutatingMockNode{}
	}
	tiler.pointcloudReaderProvider = func(inputFiles []string, sourceCRS string, eightbit bool, attrs model.Attributes) (pointcloud.Reader, error) {
		return l, nil
	}

	tmp, err := os.MkdirTemp(os.TempDir(), "tst")
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	t.Cleanup(func() {
		os.RemoveAll(tmp)
	})
	utils.TouchFile(filepath.Join(tmp, "abc.las"))
	utils.TouchFile(filepath.Join(tmp, "ghi.las"))
	if err := tiler.ProcessFolder(tmp, "out", "EPSG:123", opts, c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.mutateChunkCalls != 2 {
		t.Errorf("expected the mutator to be invoked for both files, got %d invocations", m.mutateChunkCalls)
	}
	if m.mutateAfterClose {
		t.Errorf("mutator was invoked after having been closed")
	}
	if m.closeCalls != 1 {
		t.Errorf("expected the mutator to be closed exactly once after the last file, got %d Close calls", m.closeCalls)
	}
}

func TestTilerProcessFilesValidatesOptions(t *testing.T) {
	testCases := []struct {
		name    string
		optFns  []tilerOptionsFn
		wantErr bool
	}{
		{name: "valid defaults", optFns: nil, wantErr: false},
		{name: "zero points per tile", optFns: []tilerOptionsFn{WithPointsPerTile(0)}, wantErr: true},
		{name: "negative points per tile", optFns: []tilerOptionsFn{WithPointsPerTile(-1)}, wantErr: true},
		{name: "zero workers", optFns: []tilerOptionsFn{WithWorkerNumber(0)}, wantErr: true},
		{name: "negative workers", optFns: []tilerOptionsFn{WithWorkerNumber(-1)}, wantErr: true},
		{name: "lowercase refine mode", optFns: []tilerOptionsFn{WithRefineMode(model.RefineMode("add"))}, wantErr: true},
		{name: "garbage refine mode", optFns: []tilerOptionsFn{WithRefineMode(model.RefineMode("garbage"))}, wantErr: true},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tiler, err := NewGoTiler()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			w := &writer.MockWriter{}
			tr := &testtree.MockNode{}
			l := &pc.MockLasReader{}
			readerCalled := false
			tiler.writerProvider = func(folder string, opts *TilerOptions) (writer.Writer, error) {
				return w, nil
			}
			tiler.treeProvider = func(opts tree.Options, output string) tree.Tree {
				return tr
			}
			tiler.pointcloudReaderProvider = func(inputFiles []string, sourceCRS string, eightbit bool, attrs model.Attributes) (pointcloud.Reader, error) {
				readerCalled = true
				return l, nil
			}
			opts := NewTilerOptions(tc.optFns...)
			err = tiler.ProcessFiles([]string{"abc.las"}, "out", "EPSG:123", opts, context.TODO())
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got nil")
				}
				if readerCalled {
					t.Errorf("expected validation to fail before any data is read")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
