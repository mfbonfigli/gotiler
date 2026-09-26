package cli

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mfbonfigli/gotiler/v3/plugins/compression"
	"github.com/mfbonfigli/gotiler/v3/plugins/subsampler"
	"github.com/mfbonfigli/gotiler/v3/tiler"
	"github.com/mfbonfigli/gotiler/v3/tiler/model"
	"github.com/mfbonfigli/gotiler/v3/tiler/mutator"
	"github.com/mfbonfigli/gotiler/v3/tiler/plugin"
	urfavecli "github.com/urfave/cli/v3"
)

func TestDefaultTiler(t *testing.T) {
	tl, err := DefaultTilerProvider()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	switch tl.(type) {
	case *tiler.GoTiler:
	default:
		t.Errorf("unexpected tiler type returned")
	}
}

func TestNewAppSupportsBrandingAndExtraCommands(t *testing.T) {
	called := false
	app := NewApp(Options{
		BuildInfo: BuildInfo{Version: "9.9.9", GitCommit: "abc"},
		Branding: Branding{
			Name:  "gotiler-cli",
			Usage: "private tiler",
		},
		ExtraCommands: []CommandFactory{
			func(ctx Context) *urfavecli.Command {
				if ctx.BuildInfo.VersionString() != "9.9.9-abc" {
					t.Fatalf("unexpected build info in command context: %s", ctx.BuildInfo.VersionString())
				}
				return &urfavecli.Command{
					Name: "extra",
					Action: func(context.Context, *urfavecli.Command) error {
						called = true
						return nil
					},
				}
			},
		},
	})
	if app.Name != "gotiler-cli" || app.Usage != "private tiler" || app.Version != "9.9.9-abc" {
		t.Fatalf("unexpected app metadata: name=%q usage=%q version=%q", app.Name, app.Usage, app.Version)
	}
	if app.Command("version") == nil {
		t.Fatal("expected version command to be registered")
	}
	if app.Command("file") != nil || app.Command("folder") != nil || app.Command("pointcloud") != nil || app.Command("pc") != nil || app.Command("pcloud") != nil {
		t.Fatal("expected point-cloud commands to be absent by default")
	}
	if err := app.Run(context.Background(), []string{"gotiler-cli", "extra"}); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("expected extra command to be called")
	}
}

func TestMainVersionCommand(t *testing.T) {
	called := false
	app := NewApp(Options{
		BuildInfo: BuildInfo{Version: "9.9.9", GitCommit: "abc"},
		TilerProvider: func() (tiler.Tiler, error) {
			called = true
			return &tiler.MockTiler{}, nil
		},
	})
	buf := &bytes.Buffer{}
	app.Writer = buf

	if err := app.Run(context.Background(), []string{"gotiler", "version"}); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("expected version command to not initialize the tiler")
	}
	if actual := buf.String(); actual != "9.9.9-abc\n" {
		t.Fatalf("expected version output %q, got %q", "9.9.9-abc\n", actual)
	}
}

func TestHelpWrapsFlagUsageIntoAlignedColumns(t *testing.T) {
	app := NewApp(Options{TilerProvider: func() (tiler.Tiler, error) {
		return &tiler.MockTiler{}, nil
	}})
	buf := &bytes.Buffer{}
	app.Writer = buf
	if err := app.Run(context.Background(), []string{"gotiler", "--help"}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, flag := range []string{"--colorize", "--compression", "--meshopt-khr", "--subsample", "--geotiff-colorize", "--3tz"} {
		if !strings.Contains(out, flag) {
			t.Fatalf("expected help to document %s, got %q", flag, out)
		}
	}
	// long usage strings must be wrapped into tab-aligned lines instead of
	// spilling over the terminal edge (in tests the fallback width is 80,
	// plus the names column)
	for _, line := range strings.Split(out, "\n") {
		if len(line) > 150 {
			t.Fatalf("help line exceeds the wrapped width: %q", line)
		}
	}
}

func TestMainWithoutArgsShowsHelp(t *testing.T) {
	called := false
	app := NewApp(Options{TilerProvider: func() (tiler.Tiler, error) {
		called = true
		return &tiler.MockTiler{}, nil
	}})
	buf := &bytes.Buffer{}
	app.Writer = buf

	err := app.Run(context.Background(), []string{"gotiler"})
	if err == nil {
		t.Fatal("expected missing input to fail")
	}
	if called {
		t.Fatal("expected missing input to not initialize the tiler")
	}
	out := buf.String()
	if !strings.Contains(out, "COMMANDS") || !strings.Contains(out, "version") || !strings.Contains(out, "--out") {
		t.Fatalf("expected app help to include commands, version, and point-cloud flags, got %q", out)
	}
}

func TestConfiguredPointCloudCommandWithoutArgsShowsHelp(t *testing.T) {
	called := false
	app := NewApp(Options{
		PointCloudCommandName: "pcloud",
		TilerProvider: func() (tiler.Tiler, error) {
			called = true
			return &tiler.MockTiler{}, nil
		},
	})
	buf := &bytes.Buffer{}
	app.Writer = buf

	err := app.Run(context.Background(), []string{"gotiler", "pcloud"})
	if err == nil {
		t.Fatal("expected missing input to fail")
	}
	if called {
		t.Fatal("expected missing input to not initialize the tiler")
	}
	out := buf.String()
	if !strings.Contains(out, "pcloud") || !strings.Contains(out, "--out") {
		t.Fatalf("expected pcloud help to include command name and flags, got %q", out)
	}
}

func TestMainProcessFile(t *testing.T) {
	tmp := t.TempDir()
	input := filepath.Join(tmp, "myfile.las")
	if err := touchFile(input); err != nil {
		t.Fatalf("unexpected error %v", err)
	}

	mockTiler := &tiler.MockTiler{}
	app := NewApp(Options{TilerProvider: func() (tiler.Tiler, error) {
		return mockTiler, nil
	}})
	err := app.Run(context.Background(), []string{"gotiler",
		"-out", ".\\abc",
		"-crs", "EPSG:4979",
		"-z-offset", "-1",
		"-points-per-tile", "5000",
		"--8-bit",
		"-r", "replace",
		"--initial-geometric-error", "128",
		"--ge-correction", "1.5",
		"--attributes", "intensity",
		input})
	if err != nil {
		t.Fatal(err)
	}
	if mockTiler.ProcessFilesCalled != true {
		t.Error("expected processFiles called but was not")
	}
	if actual := mockTiler.InputFiles; !reflect.DeepEqual(actual, []string{input}) {
		t.Errorf("expected tiler to be called with %v but got %v", []string{input}, actual)
	}
	if actual := mockTiler.SourceCRS; actual != "EPSG:4979" {
		t.Errorf("expected tiler to be called with epsg %v but got epsg %v", 4979, actual)
	}
	if actual := mockTiler.OutputFolder; actual != ".\\abc" {
		t.Errorf("expected tiler to be called with output folder %v but got %v", ".\\abc", actual)
	}
	if actual := mockTiler.EightBit; actual != true {
		t.Errorf("expected tiler to be called with EightBit %v but got %v", true, actual)
	}
	if actual := mockTiler.PtsPerTile; actual != 5000 {
		t.Errorf("expected tiler to be called with PtsPerTile %v but got %v", 5000, actual)
	}
	if actual := mockTiler.Mutators[0].(*mutator.ZOffset).Offset; actual != -1 {
		t.Errorf("expected tiler to be called with ZOffset mutator with offset %v but got %v", -1, actual)
	}
	if actual := len(mockTiler.Mutators); actual != 2 {
		t.Errorf("expected 2 mutators but got %v", actual)
	}
	if _, ok := mockTiler.Mutators[1].(*mutator.WithheldFilter); !ok {
		t.Errorf("expected second mutator to filter withheld points, got %T", mockTiler.Mutators[1])
	}
	if actual := mockTiler.RefineMode; actual != model.RefineReplace {
		t.Errorf("expected tiler to be called with refine mode %v but got %v", "replace", actual)
	}
	if actual := mockTiler.EncoderID; actual != compression.EncoderCompressedGLB {
		t.Errorf("expected tiler to be called with encoder %v but got %v", compression.EncoderCompressedGLB, actual)
	}
	if actual := mockTiler.InitialGeometricError; actual != 128 {
		t.Errorf("expected tiler to be called with InitialGeometricError %v but got %v", 128, actual)
	}
	if actual := mockTiler.GECorrection; actual != 1.5 {
		t.Errorf("expected tiler to be called with GECorrection %v but got %v", 1.5, actual)
	}
	if !mockTiler.Attributes.Has(model.AttrIntensity) || mockTiler.Attributes.Has(model.AttrClassification) {
		t.Errorf("expected Attributes to contain only intensity, got %v", mockTiler.Attributes)
	}
}

func TestMainProcessFolder(t *testing.T) {
	input := t.TempDir()
	mockTiler := &tiler.MockTiler{}
	app := NewApp(Options{TilerProvider: func() (tiler.Tiler, error) {
		return mockTiler, nil
	}})
	err := app.Run(context.Background(), []string{"gotiler",
		"-out", ".\\abc",
		"-c", "4979",
		"-z-offset", "-1",
		"-points-per-tile", "5000",
		"--8-bit",
		"-v", "1.0",
		"-refine-mode", "add",
		input})
	if err != nil {
		t.Fatal(err)
	}
	if mockTiler.ProcessFolderCalled != true {
		t.Error("expected processFolder called but was not")
	}
	if actual := mockTiler.InputFolder; !reflect.DeepEqual(actual, input) {
		t.Errorf("expected tiler to be called with %v but got %v", input, actual)
	}
	if actual := mockTiler.SourceCRS; actual != "EPSG:4979" {
		t.Errorf("expected tiler to be called with epsg %v but got epsg %v", 4979, actual)
	}
	if actual := mockTiler.OutputFolder; actual != ".\\abc" {
		t.Errorf("expected tiler to be called with output folder %v but got %v", ".\\abc", actual)
	}
	if actual := mockTiler.EightBit; actual != true {
		t.Errorf("expected tiler to be called with EightBit %v but got %v", true, actual)
	}
	if actual := mockTiler.PtsPerTile; actual != 5000 {
		t.Errorf("expected tiler to be called with PtsPerTile %v but got %v", 5000, actual)
	}
	if actual := mockTiler.Mutators[0].(*mutator.ZOffset).Offset; actual != -1 {
		t.Errorf("expected tiler to be called with ZOffset mutator with offset %v but got %v", -1, actual)
	}
	if actual := len(mockTiler.Mutators); actual != 2 {
		t.Errorf("expected 2 mutators but got %v", actual)
	}
	if _, ok := mockTiler.Mutators[1].(*mutator.WithheldFilter); !ok {
		t.Errorf("expected second mutator to filter withheld points, got %T", mockTiler.Mutators[1])
	}
	if actual := mockTiler.RefineMode; actual != model.RefineAdd {
		t.Errorf("expected tiler to be called with refine mode %v but got %v", "replace", actual)
	}
	if actual := mockTiler.EncoderID; actual != plugin.EncoderPNTS {
		t.Errorf("expected tiler to be called with encoder %v but got %v", plugin.EncoderPNTS, actual)
	}
	if actual := mockTiler.InitialGeometricError; actual != 0 {
		t.Errorf("expected default InitialGeometricError to be 0 (auto) but got %v", actual)
	}
}

func TestConfiguredPointCloudCommandFolderJoin(t *testing.T) {
	tmp, err := os.MkdirTemp(os.TempDir(), "tst")
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	t.Cleanup(func() {
		os.RemoveAll(tmp)
	})

	touchFile(filepath.Join(tmp, "test0.las"))
	touchFile(filepath.Join(tmp, "test0.xyz"))
	touchFile(filepath.Join(tmp, "test1.LAS"))
	touchFile(filepath.Join(tmp, "test2.LAS"))

	mockTiler := &tiler.MockTiler{}
	app := NewApp(Options{
		PointCloudCommandName: "pcloud",
		TilerProvider: func() (tiler.Tiler, error) {
			return mockTiler, nil
		},
	})
	err = app.Run(context.Background(), []string{"gotiler", "pcloud",
		"-out", ".\\abc",
		"-crs", "4979",
		"-z-offset", "-1",
		"-p", "5000",
		"--8-bit",
		"-v", "1.1",
		"-join",
		tmp})
	if err != nil {
		t.Fatal(err)
	}
	if mockTiler.ProcessFolderCalled != false {
		t.Error("expected processFolder to not be called but it was")
	}
	if mockTiler.ProcessFilesCalled != true {
		t.Error("expected processFiles called but was not")
	}
	expected := []string{
		filepath.Join(tmp, "test0.las"),
		filepath.Join(tmp, "test1.LAS"),
		filepath.Join(tmp, "test2.LAS"),
	}
	if actual := mockTiler.InputFiles; !reflect.DeepEqual(actual, expected) {
		t.Errorf("expected tiler to be called with %v but got %v", expected, actual)
	}
	if actual := mockTiler.SourceCRS; actual != "EPSG:4979" {
		t.Errorf("expected tiler to be called with epsg %v but got epsg %v", 4979, actual)
	}
	if actual := mockTiler.OutputFolder; actual != ".\\abc" {
		t.Errorf("expected tiler to be called with output folder %v but got %v", ".\\abc", actual)
	}
	if actual := mockTiler.EightBit; actual != true {
		t.Errorf("expected tiler to be called with EightBit %v but got %v", true, actual)
	}
	if actual := mockTiler.PtsPerTile; actual != 5000 {
		t.Errorf("expected tiler to be called with PtsPerTile %v but got %v", 5000, actual)
	}
	if actual := mockTiler.Mutators[0].(*mutator.ZOffset).Offset; actual != -1 {
		t.Errorf("expected tiler to be called with ZOffset mutator with offset %v but got %v", -1, actual)
	}
	if actual := len(mockTiler.Mutators); actual != 2 {
		t.Errorf("expected 2 mutators but got %v", actual)
	}
	if _, ok := mockTiler.Mutators[1].(*mutator.WithheldFilter); !ok {
		t.Errorf("expected second mutator to filter withheld points, got %T", mockTiler.Mutators[1])
	}
	if actual := mockTiler.EncoderID; actual != compression.EncoderCompressedGLB {
		t.Errorf("expected tiler to be called with encoder %v but got %v", compression.EncoderCompressedGLB, actual)
	}
}

func TestMainProcessesFileWithoutSubcommand(t *testing.T) {
	tmp := t.TempDir()
	input := filepath.Join(tmp, "myfile.las")
	if err := touchFile(input); err != nil {
		t.Fatalf("unexpected error %v", err)
	}

	mockTiler := &tiler.MockTiler{}
	app := NewApp(Options{TilerProvider: func() (tiler.Tiler, error) {
		return mockTiler, nil
	}})
	err := app.Run(context.Background(), []string{"gotiler",
		"-out", ".\\abc",
		"-crs", "4979",
		"-points-per-tile", "5000",
		input})
	if err != nil {
		t.Fatal(err)
	}
	if !mockTiler.ProcessFilesCalled {
		t.Fatal("expected processFiles called but was not")
	}
	if actual := mockTiler.InputFiles; !reflect.DeepEqual(actual, []string{input}) {
		t.Errorf("expected tiler to be called with %v but got %v", []string{input}, actual)
	}
	if actual := mockTiler.SourceCRS; actual != "EPSG:4979" {
		t.Errorf("expected tiler to be called with epsg %v but got epsg %v", 4979, actual)
	}
	if actual := mockTiler.OutputFolder; actual != ".\\abc" {
		t.Errorf("expected tiler to be called with output folder %v but got %v", ".\\abc", actual)
	}
}

func TestMainProcessFileIncludeWithheldOmitsWithheldFilter(t *testing.T) {
	tmp := t.TempDir()
	input := filepath.Join(tmp, "myfile.las")
	if err := touchFile(input); err != nil {
		t.Fatalf("unexpected error %v", err)
	}

	mockTiler := &tiler.MockTiler{}
	app := NewApp(Options{TilerProvider: func() (tiler.Tiler, error) {
		return mockTiler, nil
	}})
	err := app.Run(context.Background(), []string{"gotiler",
		"-out", ".\\abc",
		"--include-withheld",
		input})
	if err != nil {
		t.Fatal(err)
	}
	if !mockTiler.ProcessFilesCalled {
		t.Fatal("expected processFiles called but was not")
	}
	if actual := len(mockTiler.Mutators); actual != 1 {
		t.Fatalf("expected only z-offset mutator when withheld points are included, got %d", actual)
	}
	if _, ok := mockTiler.Mutators[0].(*mutator.ZOffset); !ok {
		t.Fatalf("expected z-offset mutator, got %T", mockTiler.Mutators[0])
	}
}

func TestMainProcessFileColorizeAddsColorizerMutator(t *testing.T) {
	tmp := t.TempDir()
	input := filepath.Join(tmp, "myfile.las")
	if err := touchFile(input); err != nil {
		t.Fatalf("unexpected error %v", err)
	}

	mockTiler := &tiler.MockTiler{}
	app := NewApp(Options{TilerProvider: func() (tiler.Tiler, error) {
		return mockTiler, nil
	}})
	err := app.Run(context.Background(), []string{"gotiler",
		"-out", ".\\abc",
		"--colorize", "z:viridis",
		input})
	if err != nil {
		t.Fatal(err)
	}
	if !mockTiler.ProcessFilesCalled {
		t.Fatal("expected processFiles called but was not")
	}
	if actual := len(mockTiler.Mutators); actual != 3 {
		t.Fatalf("expected z-offset, withheld filter, and colorizer mutators, got %d", actual)
	}
	if _, ok := mockTiler.Mutators[0].(*mutator.ZOffset); !ok {
		t.Fatalf("expected first mutator to be z-offset, got %T", mockTiler.Mutators[0])
	}
	if _, ok := mockTiler.Mutators[1].(*mutator.WithheldFilter); !ok {
		t.Fatalf("expected second mutator to be withheld filter, got %T", mockTiler.Mutators[1])
	}
	colorizer, ok := mockTiler.Mutators[2].(*mutator.Colorizer)
	if !ok {
		t.Fatalf("expected third mutator to be colorizer, got %T", mockTiler.Mutators[2])
	}
	if got := colorizer.RequiredAttributes(); len(got) != 0 {
		t.Fatalf("expected z colorizer to require no reader attributes, got %v", got)
	}
}

func TestParseColorizer(t *testing.T) {
	colorizer, err := parseColorizer("z:viridis")
	if err != nil {
		t.Fatalf("parseColorizer: %v", err)
	}
	if colorizer == nil {
		t.Fatal("expected colorizer")
	}
	if got := colorizer.RequiredAttributes(); len(got) != 0 {
		t.Fatalf("expected z colorizer to require no reader attributes, got %v", got)
	}

	colorizer, err = parseColorizer("intensity:grayscale")
	if err != nil {
		t.Fatalf("parseColorizer: %v", err)
	}
	if got := colorizer.RequiredAttributes(); !got.Has(model.AttrIntensity) {
		t.Fatalf("expected intensity colorizer to request intensity, got %v", got)
	}

	colorizer, err = parseColorizer("classification:las-classification")
	if err != nil {
		t.Fatalf("parseColorizer: %v", err)
	}
	if got := colorizer.RequiredAttributes(); !got.Has(model.AttrClassification) {
		t.Fatalf("expected classification colorizer to request classification, got %v", got)
	}
}

func TestParseColorizerModifiers(t *testing.T) {
	valid := []string{
		"z:viridis:reverse",
		"intensity:turbo:reverse",
		"z:turbo:steps=8",
		"z:viridis:stretch=5,95",
		"z:viridis:stretch=minmax",
		"z:viridis:blend=0.5",
		"intensity:turbo:reverse:steps=8:stretch=2,98:blend=0.75",
	}
	for _, spec := range valid {
		if _, err := parseColorizer(spec); err != nil {
			t.Fatalf("parseColorizer(%q): %v", spec, err)
		}
	}
}

func TestParseColorizerExtendedRamps(t *testing.T) {
	// ramps registered by the plugins/ramps package
	valid := []string{
		"z:terrain",
		"z:batlow",
		"intensity:mako",
		"z:rdbu",
		"z:viridis-pastel",
		"classification:dark2:stretch=minmax",
	}
	for _, spec := range valid {
		if _, err := parseColorizer(spec); err != nil {
			t.Fatalf("parseColorizer(%q): %v", spec, err)
		}
	}
}

func TestParseColorizerRejectsInvalidSpec(t *testing.T) {
	cases := []string{
		"z",
		":viridis",
		"z:",
		"z:not-a-gradient",
		"z:viridis:-30:30",
		"z:viridis:nope",
		"z:viridis:reverse=yes",
		"z:viridis:steps=1",
		"z:viridis:steps=abc",
		"z:viridis:stretch=98,2",
		"z:viridis:stretch=2",
		"z:viridis:blend=0",
		"z:viridis:blend=2",
	}
	for _, spec := range cases {
		if _, err := parseColorizer(spec); err == nil {
			t.Fatalf("expected %q to be rejected", spec)
		}
	}
}

func TestMainProcessesFolderJoinWithoutSubcommand(t *testing.T) {
	tmp := t.TempDir()
	touchFile(filepath.Join(tmp, "test0.las"))
	touchFile(filepath.Join(tmp, "test1.LAS"))
	touchFile(filepath.Join(tmp, "test2.xyz"))

	mockTiler := &tiler.MockTiler{}
	app := NewApp(Options{TilerProvider: func() (tiler.Tiler, error) {
		return mockTiler, nil
	}})
	err := app.Run(context.Background(), []string{"gotiler",
		"-out", ".\\abc",
		"-crs", "4979",
		"-points-per-tile", "5000",
		"-join",
		tmp})
	if err != nil {
		t.Fatal(err)
	}
	if mockTiler.ProcessFolderCalled {
		t.Fatal("expected processFolder to not be called")
	}
	if !mockTiler.ProcessFilesCalled {
		t.Fatal("expected processFiles called but was not")
	}
	expected := []string{
		filepath.Join(tmp, "test0.las"),
		filepath.Join(tmp, "test1.LAS"),
	}
	if actual := mockTiler.InputFiles; !reflect.DeepEqual(actual, expected) {
		t.Errorf("expected tiler to be called with %v but got %v", expected, actual)
	}
}

func TestMainProcessFileRejectsJoin(t *testing.T) {
	tmp := t.TempDir()
	input := filepath.Join(tmp, "myfile.las")
	if err := touchFile(input); err != nil {
		t.Fatalf("unexpected error %v", err)
	}

	mockTiler := &tiler.MockTiler{}
	app := NewApp(Options{TilerProvider: func() (tiler.Tiler, error) {
		return mockTiler, nil
	}})
	err := app.Run(context.Background(), []string{"gotiler", "-out", ".\\abc", "--join", input})
	if err == nil {
		t.Fatal("expected --join with file input to fail")
	}
	if mockTiler.ProcessFilesCalled || mockTiler.ProcessFolderCalled {
		t.Fatal("expected no tiler processing after invalid file --join input")
	}
}

func TestMainProcessFileWithLocalPlacement(t *testing.T) {
	tmp := t.TempDir()
	input := filepath.Join(tmp, "myfile.las")
	if err := touchFile(input); err != nil {
		t.Fatal(err)
	}
	mockTiler := &tiler.MockTiler{}
	app := NewApp(Options{TilerProvider: func() (tiler.Tiler, error) {
		return mockTiler, nil
	}})
	err := app.Run(context.Background(), []string{"gotiler",
		"-out", ".\\abc",
		"--plain",
		"--crs", "local",
		"--longitude", "12.5",
		"--latitude", "41.9",
		"--height", "76",
		"--heading", "45",
		"--pitch", "-5",
		"--roll", "2",
		"-s", "2",
		"--input-up-axis", "y",
		input})
	if err != nil {
		t.Fatal(err)
	}
	if !mockTiler.ProcessFilesCalled {
		t.Fatal("expected ProcessFiles to be called")
	}
	// in placement mode the tiler must receive no source CRS
	if mockTiler.SourceCRS != "" {
		t.Fatalf("expected empty source CRS in placement mode, got %q", mockTiler.SourceCRS)
	}
	p := mockTiler.Placement
	if p == nil {
		t.Fatal("expected a placement to be configured")
	}
	if p.Longitude != 12.5 || p.Latitude != 41.9 || p.Height != 76 ||
		p.Heading != 45 || p.Pitch != -5 || p.Roll != 2 || p.Scale != 2 || p.UpAxis != tiler.AxisY {
		t.Fatalf("unexpected placement %+v", p)
	}
}

func TestMainProcessFileLocalPlacementDefaults(t *testing.T) {
	tmp := t.TempDir()
	input := filepath.Join(tmp, "myfile.las")
	if err := touchFile(input); err != nil {
		t.Fatal(err)
	}
	mockTiler := &tiler.MockTiler{}
	app := NewApp(Options{TilerProvider: func() (tiler.Tiler, error) {
		return mockTiler, nil
	}})
	if err := app.Run(context.Background(), []string{"gotiler", "-out", ".\\abc", "--plain", "--crs", "local", input}); err != nil {
		t.Fatal(err)
	}
	p := mockTiler.Placement
	if p == nil {
		t.Fatal("expected a placement to be configured")
	}
	// defaults: ellipsoid anchor at lat 0 lon 0, scale 1, z-up
	if p.Longitude != 0 || p.Latitude != 0 || p.Height != 0 ||
		p.Heading != 0 || p.Pitch != 0 || p.Roll != 0 || p.Scale != 1 || p.UpAxis != tiler.AxisZ {
		t.Fatalf("unexpected default placement %+v", p)
	}
}

func TestMainProcessFileWithoutPlacementHasNoPlacement(t *testing.T) {
	tmp := t.TempDir()
	input := filepath.Join(tmp, "myfile.las")
	if err := touchFile(input); err != nil {
		t.Fatal(err)
	}
	mockTiler := &tiler.MockTiler{}
	app := NewApp(Options{TilerProvider: func() (tiler.Tiler, error) {
		return mockTiler, nil
	}})
	if err := app.Run(context.Background(), []string{"gotiler", "-out", ".\\abc", "--plain", "-c", "EPSG:32633", input}); err != nil {
		t.Fatal(err)
	}
	if mockTiler.Placement != nil {
		t.Fatalf("expected no placement for georeferenced input, got %+v", mockTiler.Placement)
	}
	if mockTiler.SourceCRS != "EPSG:32633" {
		t.Fatalf("expected the CRS to pass through, got %q", mockTiler.SourceCRS)
	}
}

// runApp runs the app against a mock tiler with the given extra args on a
// throwaway las input file.
func runApp(t *testing.T, extraArgs ...string) *tiler.MockTiler {
	t.Helper()
	tmp := t.TempDir()
	input := filepath.Join(tmp, "myfile.las")
	if err := touchFile(input); err != nil {
		t.Fatalf("touchFile: %v", err)
	}
	mockTiler := &tiler.MockTiler{}
	app := NewApp(Options{TilerProvider: func() (tiler.Tiler, error) {
		return mockTiler, nil
	}})
	args := append([]string{"gotiler", "-out", filepath.Join(tmp, "out"), "--plain"}, extraArgs...)
	args = append(args, input)
	if err := app.Run(context.Background(), args); err != nil {
		t.Fatalf("app.Run: %v", err)
	}
	if !mockTiler.ProcessFilesCalled {
		t.Fatal("expected ProcessFiles to be called")
	}
	return mockTiler
}

func TestCompressedEncoderIsDefault(t *testing.T) {
	mockTiler := runApp(t)
	if mockTiler.EncoderID != compression.EncoderCompressedGLB {
		t.Fatalf("expected compressed GLB encoder by default, got %q", mockTiler.EncoderID)
	}
}

func TestMeshoptKHRSelectsKHREncoder(t *testing.T) {
	mockTiler := runApp(t, "--meshopt-khr")
	if mockTiler.EncoderID != compression.EncoderCompressedGLBKHR {
		t.Fatalf("expected compressed GLB KHR encoder, got %q", mockTiler.EncoderID)
	}
}

func TestCompressionSummary(t *testing.T) {
	cases := []struct {
		name   string
		modify func(*cliOpts)
		want   string
	}{
		{"default", func(c *cliOpts) {}, "EXT_meshopt_compression + KHR_mesh_quantization"},
		{"khr", func(c *cliOpts) { c.meshoptKHR = true }, "KHR_meshopt_compression + KHR_mesh_quantization"},
		{"none", func(c *cliOpts) { c.compression = "none" }, "Disabled"},
		{"pnts", func(c *cliOpts) { c.version = "1.0" }, "Disabled (3D Tiles 1.0)"},
	}
	for _, tc := range cases {
		c := defaultCliOptions()
		tc.modify(c)
		got := c.compressionSummary()
		if got != tc.want {
			t.Errorf("%s: compressionSummary() = %q, want %q", tc.name, got, tc.want)
		}
		// the value must fit the summary box without truncation
		if row := boxRow("• Compression:          ", got); strings.Contains(row, "...") {
			t.Errorf("%s: summary box row is truncated: %q", tc.name, row)
		}
	}
}

func TestCompressionNoneKeepsPlainGLB(t *testing.T) {
	mockTiler := runApp(t, "--compression", "none")
	if mockTiler.EncoderID != plugin.EncoderGLB {
		t.Fatalf("expected plain GLB encoder, got %q", mockTiler.EncoderID)
	}
}

func TestTilesetVersion10IsNeverCompressed(t *testing.T) {
	mockTiler := runApp(t, "-v", "1.0")
	if mockTiler.EncoderID != plugin.EncoderPNTS {
		t.Fatalf("expected pnts encoder for tileset 1.0, got %q", mockTiler.EncoderID)
	}
}

func TestSubsampleAddsMutator(t *testing.T) {
	mockTiler := runApp(t, "--subsample", "25")
	var found *subsampler.Subsampler
	for _, m := range mockTiler.Mutators {
		if s, ok := m.(*subsampler.Subsampler); ok {
			found = s
		}
	}
	if found == nil {
		t.Fatalf("expected a subsampler mutator, got %T", mockTiler.Mutators)
	}
	if found.Percentage != 0.25 {
		t.Fatalf("expected subsampler fraction 0.25, got %v", found.Percentage)
	}
}

func TestSubsampleDisabledByDefault(t *testing.T) {
	mockTiler := runApp(t)
	for _, m := range mockTiler.Mutators {
		if _, ok := m.(*subsampler.Subsampler); ok {
			t.Fatal("expected no subsampler mutator by default")
		}
	}
}

func TestColorizeModifiersAccepted(t *testing.T) {
	mockTiler := runApp(t, "--colorize", "z:viridis:reverse:steps=8:stretch=5,95:blend=0.5")
	found := false
	for _, m := range mockTiler.Mutators {
		if _, ok := m.(*mutator.Colorizer); ok {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a colorizer mutator, got %d mutators", len(mockTiler.Mutators))
	}
}

func TestFolderJoinIncludesRegisteredFormats(t *testing.T) {
	tmp := t.TempDir()
	for _, name := range []string{"a.las", "b.laz", "c.e57", "d.xyz"} {
		if err := touchFile(filepath.Join(tmp, name)); err != nil {
			t.Fatal(err)
		}
	}
	mockTiler := &tiler.MockTiler{}
	app := NewApp(Options{TilerProvider: func() (tiler.Tiler, error) {
		return mockTiler, nil
	}})
	if err := app.Run(context.Background(), []string{"gotiler", "-out", ".\\abc", "--plain", "-crs", "4979", "--join", tmp}); err != nil {
		t.Fatal(err)
	}
	// e57 is registered by the plugins/e57 reader; unknown formats are skipped
	expected := []string{
		filepath.Join(tmp, "a.las"),
		filepath.Join(tmp, "b.laz"),
		filepath.Join(tmp, "c.e57"),
	}
	if actual := mockTiler.InputFiles; !reflect.DeepEqual(actual, expected) {
		t.Errorf("expected tiler to be called with %v but got %v", expected, actual)
	}
}

func Test3TZPackagesTilesetInsideOutputFolder(t *testing.T) {
	tmp := t.TempDir()
	input := filepath.Join(tmp, "myfile.las")
	if err := touchFile(input); err != nil {
		t.Fatalf("touchFile: %v", err)
	}
	// simulate a written tileset: the mock tiler writes nothing itself
	out := filepath.Join(tmp, "out")
	writeFile(t, filepath.Join(out, "tileset.json"), `{"asset":{"version":"1.1"}}`)
	writeFile(t, filepath.Join(out, "data", "d.glb"), "glb-bytes")

	mockTiler := &tiler.MockTiler{}
	app := NewApp(Options{TilerProvider: func() (tiler.Tiler, error) {
		return mockTiler, nil
	}})
	if err := app.Run(context.Background(), []string{"gotiler", "-out", out, "--plain", "--3tz", input}); err != nil {
		t.Fatalf("app.Run: %v", err)
	}

	archivePath := filepath.Join(out, threeTZFilename)
	entries := readArchiveNames(t, archivePath)
	if !entries["tileset.json"] || !entries["data/d.glb"] {
		t.Fatalf("expected archive to contain the tileset files, got %v", entries)
	}
	// the loose tileset files must be gone, only the archive remains
	remaining, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 1 || remaining[0].Name() != threeTZFilename {
		t.Fatalf("expected only the archive to remain in the output folder, got %v", remaining)
	}
}

func Test3TZFolderModePackagesEachTileset(t *testing.T) {
	tmp := t.TempDir()
	input := filepath.Join(tmp, "clouds")
	if err := os.Mkdir(input, 0o777); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.las", "b.las"} {
		if err := touchFile(filepath.Join(input, name)); err != nil {
			t.Fatal(err)
		}
	}
	// simulate the tilesets ProcessFolder would write, one subfolder per file
	out := filepath.Join(tmp, "out")
	for _, name := range []string{"a", "b"} {
		writeFile(t, filepath.Join(out, name, "tileset.json"), `{"asset":{"version":"1.1"}}`)
		writeFile(t, filepath.Join(out, name, "data", "d.glb"), "glb-bytes")
	}

	mockTiler := &tiler.MockTiler{}
	app := NewApp(Options{TilerProvider: func() (tiler.Tiler, error) {
		return mockTiler, nil
	}})
	if err := app.Run(context.Background(), []string{"gotiler", "-out", out, "--plain", "--3tz", input}); err != nil {
		t.Fatalf("app.Run: %v", err)
	}
	if !mockTiler.ProcessFolderCalled {
		t.Fatal("expected ProcessFolder to be called")
	}
	for _, name := range []string{"a", "b"} {
		archivePath := filepath.Join(out, name, threeTZFilename)
		entries := readArchiveNames(t, archivePath)
		if !entries["tileset.json"] {
			t.Fatalf("expected per-tileset archive at %s to contain tileset.json, got %v", archivePath, entries)
		}
	}
}

func readArchiveNames(t *testing.T, archivePath string) map[string]bool {
	t.Helper()
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		t.Fatalf("expected a readable 3tz archive at %s: %v", archivePath, err)
	}
	defer r.Close()
	names := map[string]bool{}
	for _, f := range r.File {
		names[f.Name] = true
	}
	return names
}

func TestPackageTilesetFailsWithoutRootTileset(t *testing.T) {
	out := t.TempDir()
	writeFile(t, filepath.Join(out, "data", "d.glb"), "glb-bytes")
	if err := packageTileset(out); err == nil {
		t.Fatal("expected packaging to fail without a root tileset.json")
	}
	if _, err := os.Stat(filepath.Join(out, threeTZFilename)); !os.IsNotExist(err) {
		t.Fatal("expected no partial archive to be left behind")
	}
}
