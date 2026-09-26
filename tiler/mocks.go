package tiler

import (
	"context"

	"github.com/mfbonfigli/gotiler/v3/tiler/model"
	"github.com/mfbonfigli/gotiler/v3/tiler/mutator"
)

type MockTiler struct {
	InputFiles          []string
	InputFolder         string
	OutputFolder        string
	SourceCRS           string
	Mutators            []mutator.Mutator
	Opts                *TilerOptions
	Ctx                 context.Context
	ProcessFilesCalled  bool
	ProcessFolderCalled bool
	// opts settings
	EightBit              bool
	PtsPerTile            int
	EncoderID             string
	RefineMode            model.RefineMode
	InitialGeometricError float64
	GECorrection          float64
	Attributes            model.Attributes
	Placement             *Placement
	err                   error
}

func (m *MockTiler) ProcessFiles(inputFiles []string, outputFolder string, sourceCRS string, opts *TilerOptions, ctx context.Context) error {
	m.InputFiles = inputFiles
	m.OutputFolder = outputFolder
	m.SourceCRS = sourceCRS
	m.Opts = opts
	m.Ctx = ctx
	m.ProcessFilesCalled = true
	m.EightBit = opts.eightBitColors
	m.PtsPerTile = opts.PointsPerTile
	m.EncoderID = opts.encoderID
	m.Mutators = opts.mutators
	m.RefineMode = opts.refineMode
	m.InitialGeometricError = opts.initialGeometricError
	m.GECorrection = opts.geCorrection
	m.Attributes = opts.attributes
	m.Placement = opts.placement
	return m.err
}

func (m *MockTiler) ProcessFolder(inputFolder, outputFolder string, sourceCRS string, opts *TilerOptions, ctx context.Context) error {
	m.InputFolder = inputFolder
	m.OutputFolder = outputFolder
	m.SourceCRS = sourceCRS
	m.Opts = opts
	m.Ctx = ctx
	m.ProcessFolderCalled = true
	m.EightBit = opts.eightBitColors
	m.PtsPerTile = opts.PointsPerTile
	m.EncoderID = opts.encoderID
	m.Mutators = opts.mutators
	m.RefineMode = opts.refineMode
	m.InitialGeometricError = opts.initialGeometricError
	m.GECorrection = opts.geCorrection
	m.Attributes = opts.attributes
	m.Placement = opts.placement
	return m.err
}
