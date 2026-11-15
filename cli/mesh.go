package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/mfbonfigli/gotiler/v3/internal/mesh/tiling"
	"github.com/mfbonfigli/gotiler/v3/internal/viewer"
	"github.com/mfbonfigli/gotiler/v3/tiler"
	"github.com/mfbonfigli/gotiler/v3/tiler/tree"
	"github.com/urfave/cli/v3"
)

const (
	minTrianglesPerTile = 1000
	maxTrianglesPerTile = 1_000_000
	minMeshTextureSize  = 256
	maxMeshTextureSize  = 8192
)

// meshCommandName is the command that tiles textured meshes.
const meshCommandName = "mesh"

// The values of the --compression flag.
const (
	compressionMeshopt = "meshopt"
	compressionNone    = "none"
)

// tileMesh writes a mesh tileset. Tests replace it to check the options the
// command passes.
var tileMesh = tiling.Tile

// meshOptions holds the flags of the mesh command.
type meshOptions struct {
	output           string
	trianglesPerTile int
	maxTextureSize   int
	textureQuality   int
	compression      string
	geCorrection     float64
	upAxis           string
	// maxTilesPerTileset is the most tiles a tileset JSON file holds
	maxTilesPerTileset int
	// placement of the tileset, through its root transform
	longitude, latitude, height float64
	heading, pitch, roll        float64
	scale                       float64
	noPreview                   bool
	viewerPort                  int
	plain                       bool
}

func defaultMeshOptions() *meshOptions {
	defaults := tiling.DefaultOptions()
	return &meshOptions{
		trianglesPerTile:   defaults.TrianglesPerTile,
		maxTextureSize:     defaults.MaxTextureSize,
		textureQuality:     defaults.TextureQuality,
		compression:        compressionMeshopt,
		geCorrection:       defaults.GECorrection,
		upAxis:             "z",
		scale:              1,
		maxTilesPerTileset: defaults.MaxTilesPerTileset,
	}
}

func newMeshCommand() *cli.Command {
	o := defaultMeshOptions()
	return &cli.Command{
		Name:      meshCommandName,
		Usage:     "convert a textured OBJ mesh into 3D Tiles (experimental)",
		ArgsUsage: "<mesh.obj>",
		Description: "Tiles a textured triangle mesh in a Wavefront OBJ file, as photogrammetry tools export them, " +
			"into 3D Tiles 1.1 with glTF tiles, each with a texture of its own. " +
			"The mesh and its textures do not need to fit in memory. " +
			"The mesh is placed on the globe through the placement flags, and then in the browser viewer, " +
			"which opens after tiling to check the tileset and move it into place.",
		Flags: o.flags(),
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() != 1 {
				_ = cli.ShowSubcommandHelp(cmd)
				return errors.New("the path of one .obj file must be set")
			}
			ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
			defer stop()
			return o.run(ctx, cmd.Args().First())
		},
	}
}

func (o *meshOptions) flags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: "out", Aliases: []string{"o"}, Destination: &o.output,
			Usage: "the output folder of the tileset"},
		&cli.IntFlag{Name: "triangles-per-tile", Value: o.trianglesPerTile, Destination: &o.trianglesPerTile,
			Usage: fmt.Sprintf("the triangles a tile holds, about, from %d to %d. Default %d.", minTrianglesPerTile, maxTrianglesPerTile, o.trianglesPerTile)},
		&cli.IntFlag{Name: "max-texture-size", Value: o.maxTextureSize, Destination: &o.maxTextureSize,
			Usage: fmt.Sprintf("the largest side of the texture of a tile in pixels, from %d to %d. Tiles whose textures would be larger get them scaled down. Default %d.", minMeshTextureSize, maxMeshTextureSize, o.maxTextureSize)},
		&cli.IntFlag{Name: "texture-quality", Value: o.textureQuality, Destination: &o.textureQuality,
			Usage: fmt.Sprintf("the JPEG quality of the textures, from 1 to 100. Default %d.", o.textureQuality)},
		&cli.StringFlag{Name: "compression", Value: o.compression, Destination: &o.compression,
			Usage: fmt.Sprintf("the compression of the geometry of the tiles: %q (EXT_meshopt_compression) or %q. Default %q.", compressionMeshopt, compressionNone, o.compression)},
		&cli.Float64Flag{Name: "ge-correction", Value: o.geCorrection, Destination: &o.geCorrection,
			Usage: "multiplier of the geometric errors of the tiles. Values above 1 make viewers switch to finer tiles from farther away, sharper but heavier; values below 1 only up close. Default 1."},
		&cli.IntFlag{Name: "max-tiles-per-tileset", Value: o.maxTilesPerTileset, Destination: &o.maxTilesPerTileset,
			Usage: fmt.Sprintf("the most tiles a tileset JSON file holds. Larger tilesets keep their top levels in tileset.json and move the subtrees below into external tilesets in the data folder, so viewers read only the top of the tree first. 0 writes a single tileset.json. Default %d.", o.maxTilesPerTileset)},
		&cli.StringFlag{Name: "input-up-axis", Value: o.upAxis, Destination: &o.upAxis,
			Usage: `the input axis that points up: "x", "y" or "z", optionally signed. Default "z".`},
		&cli.Float64Flag{Name: "longitude", Destination: &o.longitude,
			Usage: "the longitude in degrees (EPSG:4326) at which to place the origin of the mesh."},
		&cli.Float64Flag{Name: "latitude", Destination: &o.latitude,
			Usage: "the latitude in degrees (EPSG:4326) at which to place the origin of the mesh."},
		&cli.Float64Flag{Name: "height", Destination: &o.height,
			Usage: "the height in meters above the WGS84 ellipsoid at which to place the origin of the mesh."},
		&cli.Float64Flag{Name: "heading", Destination: &o.heading,
			Usage: "the rotation in degrees from the local north direction, positive eastward."},
		&cli.Float64Flag{Name: "pitch", Destination: &o.pitch,
			Usage: "the rotation in degrees from the local east-north plane, positive above it."},
		&cli.Float64Flag{Name: "roll", Destination: &o.roll,
			Usage: "the rotation in degrees about the local east axis."},
		&cli.Float64Flag{Name: "scale", Aliases: []string{"s"}, Value: o.scale, Destination: &o.scale,
			Usage: "the uniform scale of the mesh: the length of its unit in meters. Default 1."},
		&cli.BoolFlag{Name: "no-preview", Destination: &o.noPreview,
			Usage: "do not open the tileset in the browser viewer after tiling. Without it, the viewer opens to check the tileset and move it into place, unless the output is not a terminal."},
		viewerPortFlag(&o.viewerPort),
		&cli.BoolFlag{Name: "plain", Destination: &o.plain,
			Usage: "disable progress bars and print only milestone updates as plain text"},
	}
}

// tilingOptions returns the options of the tiling, with progress reported to
// progress.
func (o *meshOptions) tilingOptions(progress tree.ProgressReporter) (tiling.Options, error) {
	placement, err := o.placement().Transform()
	if err != nil {
		return tiling.Options{}, fmt.Errorf("invalid placement: %w", err)
	}
	transform := placement.ForwardColumnMajor()
	opts := tiling.DefaultOptions()
	opts.TrianglesPerTile = o.trianglesPerTile
	opts.MaxTextureSize = o.maxTextureSize
	opts.TextureQuality = o.textureQuality
	opts.Compress = o.compression == compressionMeshopt
	opts.UpAxis = o.upAxis
	opts.GECorrection = o.geCorrection
	opts.MaxTilesPerTileset = o.maxTilesPerTileset
	opts.RootTransform = &transform
	opts.Progress = progress
	return opts, opts.Validate()
}

// placement returns the placement of the tileset. The input up axis is not
// part of it: the tiling turns the mesh itself.
func (o *meshOptions) placement() tiler.Placement {
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

func (o *meshOptions) validate(input string) error {
	switch {
	case o.output == "":
		return errors.New("the output folder must be set with --out")
	case o.trianglesPerTile < minTrianglesPerTile || o.trianglesPerTile > maxTrianglesPerTile:
		return fmt.Errorf("--triangles-per-tile must be from %d to %d, not %d", minTrianglesPerTile, maxTrianglesPerTile, o.trianglesPerTile)
	case o.maxTextureSize < minMeshTextureSize || o.maxTextureSize > maxMeshTextureSize:
		return fmt.Errorf("--max-texture-size must be from %d to %d, not %d", minMeshTextureSize, maxMeshTextureSize, o.maxTextureSize)
	case o.textureQuality < 1 || o.textureQuality > 100:
		return fmt.Errorf("--texture-quality must be from 1 to 100, not %d", o.textureQuality)
	case o.compression != compressionMeshopt && o.compression != compressionNone:
		return fmt.Errorf("--compression must be %q or %q, not %q", compressionMeshopt, compressionNone, o.compression)
	case !(o.scale > 0):
		return fmt.Errorf("--scale must be positive, not %v", o.scale)
	case o.maxTilesPerTileset < 0:
		return fmt.Errorf("--max-tiles-per-tileset must be 0 or more, not %d", o.maxTilesPerTileset)
	}
	info, err := os.Stat(input)
	switch {
	case err != nil:
		return fmt.Errorf("the input %s is not accessible: %w", input, err)
	case info.IsDir():
		return fmt.Errorf("the input %s is a folder: the mesh command tiles a single .obj file", input)
	case !strings.EqualFold(filepath.Ext(input), ".obj"):
		return fmt.Errorf("the input %s is not a .obj file", input)
	}
	return nil
}

// run tiles the mesh of input, then shows the tileset in the viewer unless
// the flags or the output rule it out.
func (o *meshOptions) run(ctx context.Context, input string) error {
	if err := o.validate(input); err != nil {
		return err
	}
	opts, err := o.tilingOptions(newPhaseProgress(o.plain, input, tiling.Phases))
	if err != nil {
		return err
	}
	preview := !o.noPreview
	if preview && !interactive() {
		preview = false
		fmt.Fprintln(os.Stderr, "The output is not a terminal: the viewer will not open after tiling.")
	}
	o.printSummary(input, preview)
	result, err := tileMesh(ctx, input, o.output, opts)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Tiled %s triangles into %s tiles, %d levels deep.\n",
		formatComma(int(result.Read)), formatComma(result.Tiles), result.Depth)
	for _, texture := range result.MissingTextures {
		fmt.Fprintf(os.Stderr, "The texture %s could not be read: its triangles show the color of their material.\n", texture)
	}
	if preview {
		if err := serveViewer(ctx, filepath.Join(o.output, "tileset.json"), viewer.Options{Port: o.viewerPort, IonToken: os.Getenv(ionTokenEnvVar), StartEditing: true}); err != nil {
			return err
		}
	}
	printDonationMessage()
	return nil
}

// printSummary prints the setup of the tiling as the other commands do.
func (o *meshOptions) printSummary(input string, preview bool) {
	placement := o.placement()
	inputs := []summaryRow{
		{"Input Source", "Input", input},
		{"Output Destination", "Output", o.output},
	}
	processing := []summaryRow{
		{"Triangles/Tile", "Tris/Tile", formatComma(o.trianglesPerTile)},
		{"Max Texture Size", "Texture", fmt.Sprintf("%d px, JPEG quality %d", o.maxTextureSize, o.textureQuality)},
		{"Compression", "Compress", o.compression},
		{"GE Correction", "GE Corr", fmt.Sprintf("%gx", o.geCorrection)},
		{"Max Tiles/File", "Tiles/File", tilesPerFileDisp(o.maxTilesPerTileset)},
		{"Input Up Axis", "Up Axis", o.upAxis},
		{"Placement Lon/Lat", "Placement Lon/Lat", fmt.Sprintf("%g, %g", placement.Longitude, placement.Latitude)},
		{"Placement Height", "Placement Height", fmt.Sprintf("%gm", placement.Height)},
		{"Placement H/P/R", "Placement H/P/R", fmt.Sprintf("%g/%g/%g", placement.Heading, placement.Pitch, placement.Roll)},
		{"Placement Scale", "Placement Scale", fmt.Sprintf("%g", placement.Scale)},
		{"Viewer Preview", "Preview", capitalize(fmt.Sprintf("%v", preview))},
	}
	if o.plain {
		for _, row := range append(inputs, processing...) {
			width := 11
			if strings.HasPrefix(row.plainLabel, "Placement") {
				width = 19
			}
			fmt.Fprintf(os.Stderr, "%-*s %s\n", width, row.plainLabel+":", row.value)
		}
		fmt.Fprintln(os.Stderr)
		return
	}
	printSummaryBox("📥 Inputs & Outputs ", inputs)
	printSummaryBox("📊 Processing Options ", processing)
}
