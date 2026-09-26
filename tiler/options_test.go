package tiler

import (
	"io"
	"testing"

	"github.com/mfbonfigli/gotiler/v3/tiler/model"
	"github.com/mfbonfigli/gotiler/v3/tiler/mutator"
	"github.com/mfbonfigli/gotiler/v3/tiler/tree"
)

func TestOptions(t *testing.T) {
	m := mutator.NewZOffset(0.5)
	intensityOnly := model.NewAttributes(model.AttrIntensity)
	writerProvider := func(filename string) (io.WriteCloser, error) { return nil, nil }
	treeProvider := func(opts tree.Options, output string) tree.Tree { return nil }
	opts := NewTilerOptions(
		WithProgressCallback(func(event ProgressEvent) {}),
		WithEightBitColors(true),
		WithPointsPerTile(20000),
		WithWorkerNumber(3),
		WithMutators([]mutator.Mutator{m}),
		WithEncoder("pnts"),
		WithInitialGeometricError(128),
		WithGECorrection(2.5),
		WithAttributes(intensityOnly),
		WithWriterProvider(writerProvider),
		WithTreeProvider(treeProvider),
	)

	if opts.progressCallback == nil {
		t.Errorf("unexpected nil progress callback")
	}
	if opts.eightBitColors != true {
		t.Errorf("expected eightbitcolor to be %v got %v", true, opts.eightBitColors)
	}
	if opts.numWorkers != 3 {
		t.Errorf("expected numWorkers to be %v got %v", 3, opts.numWorkers)
	}
	if opts.PointsPerTile != 20000 {
		t.Errorf("expected points per tile to be %v got %v", 20000, opts.PointsPerTile)
	}
	if opts.mutators[0] != m && len(opts.mutators) != 1 {
		t.Error("expected 1 mutator to be registered")
	}
	if opts.encoderID != "pnts" {
		t.Errorf("expected encoder to be pnts, got %q", opts.encoderID)
	}
	if opts.initialGeometricError != 128 {
		t.Errorf("expected initialGeometricError to be %v got %v", 128, opts.initialGeometricError)
	}
	if opts.geCorrection != 2.5 {
		t.Errorf("expected geCorrection to be %v got %v", 2.5, opts.geCorrection)
	}
	if !opts.attributes.Has(model.AttrIntensity) || opts.attributes.Has(model.AttrClassification) {
		t.Errorf("expected attributes to contain only intensity, got %v", opts.attributes)
	}
	if opts.writerProvider == nil {
		t.Errorf("expected writer provider to be set")
	}
	if opts.treeProvider == nil {
		t.Errorf("expected tree provider to be set")
	}
}

func TestDefaultOptions(t *testing.T) {
	opts := NewDefaultTilerOptions()
	if opts.initialGeometricError != 0 {
		t.Errorf("expected default initialGeometricError to be 0 (auto), got %v", opts.initialGeometricError)
	}
	if opts.geCorrection != 1.0 {
		t.Errorf("expected default geCorrection to be 1.0, got %v", opts.geCorrection)
	}
	if !opts.attributes.Has(model.AttrIntensity) || !opts.attributes.Has(model.AttrClassification) {
		t.Errorf("expected default attributes to contain both intensity and classification, got %v", opts.attributes)
	}
}
