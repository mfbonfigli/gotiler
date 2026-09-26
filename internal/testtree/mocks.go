package testtree

import (
	"context"

	coor "github.com/mfbonfigli/gotiler/v3/tiler/coord"
	"github.com/mfbonfigli/gotiler/v3/tiler/geom"
	"github.com/mfbonfigli/gotiler/v3/tiler/model"
	"github.com/mfbonfigli/gotiler/v3/tiler/mutator"
	"github.com/mfbonfigli/gotiler/v3/tiler/pointcloud"
	"github.com/mfbonfigli/gotiler/v3/tiler/tree"
)

type MockNode struct {
	Bounds                    geom.BoundingBox
	ChildNodes                [8]tree.Node
	Pts                       geom.PointList
	TotalNumPts               int
	Root                      bool
	Leaf                      bool
	GeomError                 float64
	CenterX, CenterY, CenterZ float64
	// invocation params
	Las         pointcloud.Reader
	ConvFactory coor.ConverterFactory
	Mut         mutator.Mutator
	Ctx         context.Context
	LoadCalled  bool
	BuildCalled bool
	BuildCtx    context.Context
	Transform   *model.Transform
	MockRefine  model.RefineMode
	Summaries   []model.AttributeSummary
}

func (n *MockNode) ToParentCRS() *model.Transform {
	return n.Transform
}

func (n *MockNode) RefineMode() model.RefineMode {
	if n.MockRefine == model.RefineMode("") {
		return model.RefineAdd
	}
	return n.MockRefine
}

func (n *MockNode) AttributeSummaries() []model.AttributeSummary {
	return n.Summaries
}

func (n *MockNode) BoundingBox() geom.BoundingBox {
	return n.Bounds
}

func (n *MockNode) ChildrenAt(i uint8) tree.Node {
	if val := n.ChildNodes[i]; val != nil {
		return val
	}
	return nil
}

func (n *MockNode) Dispose() error {
	return nil
}

func (n *MockNode) Points() (geom.PointList, error) {
	return n.Pts, nil
}

func (n *MockNode) TotalNumberOfPoints() int {
	return n.TotalNumPts
}

func (n *MockNode) NumberOfPoints() int {
	return n.Pts.Len()
}

func (n *MockNode) IsRoot() bool {
	return n.Root
}

func (n *MockNode) IsLeaf() bool {
	return n.Leaf
}

func (n *MockNode) GeometricError() float64 {
	return n.GeomError
}

func (n *MockNode) Phases() []tree.PhaseInfo {
	return nil
}

func (n *MockNode) Build(ctx context.Context, reporter tree.ProgressReporter) error {
	n.BuildCalled = true
	n.BuildCtx = ctx
	return nil
}

func (n *MockNode) RootNode() tree.Node {
	return n
}

func (n *MockNode) Load(l pointcloud.Reader, c coor.ConverterFactory, m mutator.Mutator, ctx context.Context, reporter tree.ProgressReporter) error {
	n.LoadCalled = true
	n.Ctx = ctx
	n.Las = l
	n.ConvFactory = c
	n.Mut = m
	return nil
}
