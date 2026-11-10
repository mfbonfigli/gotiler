package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/mfbonfigli/gotiler/v3/internal/splat/tiling"
	"github.com/mfbonfigli/gotiler/v3/internal/viewer"
	"github.com/mfbonfigli/gotiler/v3/plugins/ply"
	"github.com/mfbonfigli/gotiler/v3/tiler"
	"github.com/mfbonfigli/gotiler/v3/tiler/tree"
	"github.com/urfave/cli/v3"
	"golang.org/x/term"
)

const (
	minSplatsPerTile = 1000
	maxSplatsPerTile = 1_000_000
)

// The defaults suit large scenes on a map, where viewer memory is the limit,
// unlike the tiling's, which keep everything.
const (
	// defaultMaxSHDegree drops the colors that change with the view
	// direction: at degree 3, CesiumJS takes about 5 times the memory per
	// splat.
	defaultMaxSHDegree = 0
	// defaultMinOpacity drops the almost invisible splats, which add little
	// to the image.
	defaultMinOpacity = 0.05
)

// tileSplats writes a splat tileset. Tests replace it to check the options
// the command passes.
var tileSplats = tiling.Tile

// interactive reports whether a user is watching the command's output, so
// that opening the viewer makes sense. Tests replace it.
var interactive = func() bool { return term.IsTerminal(int(os.Stderr.Fd())) }

// splatOptions holds the flags of the splat command.
type splatOptions struct {
	output        string
	splatsPerTile int
	maxSHDegree   int
	minOpacity    float64
	geCorrection  float64
	upAxis        string
	// placement of the tileset, through its root transform
	longitude, latitude, height float64
	heading, pitch, roll        float64
	scale                       float64
	noPreview                   bool
	viewerPort                  int
	plain                       bool
}

func defaultSplatOptions() *splatOptions {
	defaults := tiling.DefaultOptions()
	return &splatOptions{
		splatsPerTile: defaults.SplatsPerTile,
		maxSHDegree:   defaultMaxSHDegree,
		minOpacity:    defaultMinOpacity,
		geCorrection:  defaults.GECorrection,
		upAxis:        "z",
		scale:         1,
	}
}

func newSplatCommand() *cli.Command {
	o := defaultSplatOptions()
	return &cli.Command{
		Name:      "splat",
		Usage:     "convert a 3D Gaussian splat PLY file into 3D Tiles",
		ArgsUsage: "<splats.ply>",
		Description: "Tiles a PLY file of 3D Gaussian splats, as trainers like the reference 3DGS implementation write them, " +
			"into 3D Tiles 1.1 with SPZ-compressed glTF splats (KHR_gaussian_splatting), which CesiumJS renders. " +
			"The splats are placed on the globe through the placement flags, and then in the browser viewer, " +
			"which opens after tiling to check the tileset and move it into place.",
		Flags: o.flags(),
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() != 1 {
				_ = cli.ShowSubcommandHelp(cmd)
				return errors.New("the path of one splat .ply file must be set")
			}
			ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
			defer stop()
			return o.run(ctx, cmd.Args().First())
		},
	}
}

func (o *splatOptions) flags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: "out", Aliases: []string{"o"}, Destination: &o.output,
			Usage: "the output folder of the tileset"},
		&cli.IntFlag{Name: "splats-per-tile", Value: o.splatsPerTile, Destination: &o.splatsPerTile,
			Usage: fmt.Sprintf("the most splats a tile holds, from %d to %d. Default %d.", minSplatsPerTile, maxSplatsPerTile, o.splatsPerTile)},
		&cli.IntFlag{Name: "max-sh-degree", Value: o.maxSHDegree, Destination: &o.maxSHDegree,
			Usage: fmt.Sprintf("the highest spherical-harmonic degree to keep, from 0 to 3. Higher degrees keep the colors that change with the view direction, but take much more memory in viewers. Default %d; 3 keeps the input's.", o.maxSHDegree)},
		&cli.Float64Flag{Name: "min-opacity", Value: o.minOpacity, Destination: &o.minOpacity,
			Usage: fmt.Sprintf("drop the splats of lower opacity, from 0 to 1. Default %g drops the almost invisible ones; 0 keeps them all.", o.minOpacity)},
		&cli.Float64Flag{Name: "ge-correction", Value: o.geCorrection, Destination: &o.geCorrection,
			Usage: "multiplier of the geometric errors of the tiles. Values above 1 make viewers switch to finer tiles from farther away, sharper but heavier; values below 1 only up close. Default 1."},
		&cli.StringFlag{Name: "input-up-axis", Value: o.upAxis, Destination: &o.upAxis,
			Usage: `the input axis that points up: "x", "y" or "z", optionally signed. Scenes trained with OpenCV or COLMAP conventions usually need "-y". Default "z".`},
		&cli.Float64Flag{Name: "longitude", Destination: &o.longitude,
			Usage: "the longitude in degrees (EPSG:4326) at which to place the bottom center of the splats."},
		&cli.Float64Flag{Name: "latitude", Destination: &o.latitude,
			Usage: "the latitude in degrees (EPSG:4326) at which to place the bottom center of the splats."},
		&cli.Float64Flag{Name: "height", Destination: &o.height,
			Usage: "the height in meters above the WGS84 ellipsoid at which to place the bottom center of the splats."},
		&cli.Float64Flag{Name: "heading", Destination: &o.heading,
			Usage: "the rotation in degrees from the local north direction, positive eastward."},
		&cli.Float64Flag{Name: "pitch", Destination: &o.pitch,
			Usage: "the rotation in degrees from the local east-north plane, positive above it."},
		&cli.Float64Flag{Name: "roll", Destination: &o.roll,
			Usage: "the rotation in degrees about the local east axis."},
		&cli.Float64Flag{Name: "scale", Aliases: []string{"s"}, Value: o.scale, Destination: &o.scale,
			Usage: "the uniform scale of the splats. Trained scenes often have arbitrary units: set the scale that makes them meters. Default 1."},
		&cli.BoolFlag{Name: "no-preview", Destination: &o.noPreview,
			Usage: "do not open the tileset in the browser viewer after tiling. Without it, the viewer opens to check the tileset and move it into place, unless the output is not a terminal. --port and the " + ionTokenEnvVar + " environment variable configure the viewer."},
		viewerPortFlag(&o.viewerPort),
		&cli.BoolFlag{Name: "plain", Destination: &o.plain,
			Usage: "disable progress bars and print only milestone updates as plain text"},
	}
}

// tilingOptions returns the options of the tiling, with progress reported to
// progress.
func (o *splatOptions) tilingOptions(progress tree.ProgressReporter) (tiling.Options, error) {
	placement, err := o.placement().Transform()
	if err != nil {
		return tiling.Options{}, fmt.Errorf("invalid placement: %w", err)
	}
	transform := placement.ForwardColumnMajor()
	opts := tiling.DefaultOptions()
	opts.SplatsPerTile = o.splatsPerTile
	opts.MaxSHDegree = o.maxSHDegree
	opts.MinOpacity = o.minOpacity
	opts.UpAxis = o.upAxis
	opts.GECorrection = o.geCorrection
	opts.RootTransform = &transform
	opts.Progress = progress
	return opts, opts.Validate()
}

// placement returns the placement of the tileset. The input up axis is not
// part of it: the tiling turns the splats themselves.
func (o *splatOptions) placement() tiler.Placement {
	return tiler.Placement{
		Longitude: o.longitude,
		Latitude:  o.latitude,
		Height:    o.height,
		Heading:   o.heading,
		Pitch:     o.pitch,
		Roll:      o.roll,
		Scale:     o.scale,
		UpAxis:    tiler.AxisZ,
	}
}

func (o *splatOptions) validate(input string) error {
	if o.output == "" {
		return errors.New("the output folder must be set with --out")
	}
	if o.splatsPerTile < minSplatsPerTile || o.splatsPerTile > maxSplatsPerTile {
		return fmt.Errorf("--splats-per-tile must be from %d to %d, not %d", minSplatsPerTile, maxSplatsPerTile, o.splatsPerTile)
	}
	if !(o.scale > 0) {
		return fmt.Errorf("--scale must be positive, not %v", o.scale)
	}
	info, err := os.Stat(input)
	switch {
	case err != nil:
		return fmt.Errorf("the input %s is not accessible: %w", input, err)
	case info.IsDir():
		return fmt.Errorf("the input %s is a folder: the splat command tiles a single .ply file", input)
	case !strings.EqualFold(filepath.Ext(input), ".ply"):
		return fmt.Errorf("the input %s is not a .ply file", input)
	}
	return nil
}

// run tiles the splats of input, then shows the tileset in the viewer unless
// the flags or the output rule it out.
func (o *splatOptions) run(ctx context.Context, input string) error {
	if err := o.validate(input); err != nil {
		return err
	}
	progress := newSplatProgress(o.plain, input)
	opts, err := o.tilingOptions(progress)
	if err != nil {
		return err
	}
	reader, err := ply.NewSplatReader(input)
	if err != nil {
		return err
	}
	defer reader.Close() //nolint:errcheck

	preview := !o.noPreview
	if preview && !interactive() {
		preview = false
		fmt.Fprintln(os.Stderr, "The output is not a terminal: the viewer will not open after tiling.")
	}
	o.printSummary(input, reader, preview)

	result, err := tileSplats(ctx, reader, o.output, opts)
	if err != nil {
		return err
	}
	o.printResult(result, reader.Skipped())

	if preview {
		if err := serveViewer(ctx, filepath.Join(o.output, "tileset.json"), viewer.Options{Port: o.viewerPort, IonToken: os.Getenv(ionTokenEnvVar), StartEditing: true}); err != nil {
			return err
		}
	}
	printDonationMessage()
	return nil
}

func (o *splatOptions) printSummary(input string, reader *ply.SplatReader, preview bool) {
	placement := o.placement()
	shDegree := fmt.Sprintf("%d", reader.SHDegree())
	if o.maxSHDegree < reader.SHDegree() {
		shDegree = fmt.Sprintf("%d, capped to %d", reader.SHDegree(), o.maxSHDegree)
	}
	rows := [][2]string{
		{"Input", input},
		{"Output", o.output},
		{"Splats", formatComma(reader.NumberOfSplats())},
		{"SH Degree", shDegree},
		{"Splats/Tile", formatComma(o.splatsPerTile)},
		{"Min Opacity", fmt.Sprintf("%g", o.minOpacity)},
		{"GE Correction", fmt.Sprintf("%gx", o.geCorrection)},
		{"Up Axis", o.upAxis},
		{"Lon/Lat", fmt.Sprintf("%g, %g", placement.Longitude, placement.Latitude)},
		{"Height", fmt.Sprintf("%gm", placement.Height)},
		{"H/P/R", fmt.Sprintf("%g/%g/%g", placement.Heading, placement.Pitch, placement.Roll)},
		{"Scale", fmt.Sprintf("%g", placement.Scale)},
		{"Viewer", capitalize(fmt.Sprintf("%v", preview))},
	}
	if o.plain {
		for _, row := range rows {
			fmt.Fprintf(os.Stderr, "%-14s %s\n", row[0]+":", row[1])
		}
		fmt.Fprintln(os.Stderr)
		return
	}
	fmt.Fprintln(os.Stderr, boxHeader("💫 Gaussian Splats "))
	for _, row := range rows {
		label := "• " + padToWidth(row[0]+":", 20) + " "
		fmt.Fprintln(os.Stderr, boxRow(label, truncLeft(row[1], boxInnerWidth-visualWidth(label))))
	}
	fmt.Fprintln(os.Stderr, boxFooter())
	fmt.Fprintln(os.Stderr)
}

func (o *splatOptions) printResult(r tiling.Result, skipped int) {
	fmt.Fprintf(os.Stderr, "Tiled %s splats into %s tiles, %d levels deep, with spherical harmonics of degree %d.\n",
		formatComma(r.Splats), formatComma(r.Tiles), r.Depth, r.SHDegree)
	if dropped := r.Read - r.Splats; dropped > 0 {
		fmt.Fprintf(os.Stderr, "Dropped %s splats below the minimum opacity.\n", formatComma(dropped))
	}
	if skipped > 0 {
		fmt.Fprintf(os.Stderr, "Skipped %s vertices with invalid values.\n", formatComma(skipped))
	}
}

// splatProgress shows the progress of a splat tiling as the point cloud
// command does, with progress bars or plain milestones.
type splatProgress struct {
	listener tiler.ProgressCallback
	phases   map[string]tiler.Phase
	input    string
	start    time.Time
}

func newSplatProgress(plain bool, input string) *splatProgress {
	p := &splatProgress{listener: progressListener, phases: map[string]tiler.Phase{}, input: input, start: time.Now()}
	if plain {
		p.listener = plainListener
	}
	for i, phase := range tiling.Phases {
		p.phases[phase.Name] = tiler.Phase{Index: i + 1, Total: len(tiling.Phases), Name: phase.Label, Unit: phase.Unit}
	}
	return p
}

// Report forwards a progress update of the tiling to the listener.
func (p *splatProgress) Report(u tree.ProgressUpdate) {
	phase, ok := p.phases[u.Phase]
	if !ok {
		return
	}
	level := tiler.ProgressDetail
	if u.IsMilestone {
		level = tiler.ProgressMilestone
	}
	p.listener(tiler.ProgressEvent{
		Phase:     phase,
		Percent:   u.Percent,
		Message:   u.Message,
		Level:     level,
		ElapsedMs: time.Since(p.start).Milliseconds(),
		InputDesc: p.input,
		ItemCount: u.ItemCount,
		ItemTotal: u.ItemTotal,
	})
}
