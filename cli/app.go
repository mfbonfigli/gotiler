package cli

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/pprof"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mfbonfigli/gotiler/v3/plugins/compression"

	// registers the .e57 reader with the point cloud reader registry
	_ "github.com/mfbonfigli/gotiler/v3/plugins/e57"
	geotiffcolorizer "github.com/mfbonfigli/gotiler/v3/plugins/geotiff-colorizer"

	// registers the extended color ramp library with the colorizer registry
	_ "github.com/mfbonfigli/gotiler/v3/plugins/ramps"
	"github.com/mfbonfigli/gotiler/v3/plugins/subsampler"
	"github.com/mfbonfigli/gotiler/v3/plugins/threetz"
	"github.com/mfbonfigli/gotiler/v3/tiler"
	coordproj "github.com/mfbonfigli/gotiler/v3/tiler/coord/proj"
	"github.com/mfbonfigli/gotiler/v3/tiler/model"
	"github.com/mfbonfigli/gotiler/v3/tiler/mutator"
	"github.com/mfbonfigli/gotiler/v3/tiler/plugin"
	"github.com/mfbonfigli/gotiler/v3/version"
	"github.com/schollz/progressbar/v3"
	"github.com/urfave/cli/v3"
	"golang.org/x/term"
)

// defaultFlagStringer is the flag stringer provided by the cli library,
// captured before the tabular override below is installed.
var defaultFlagStringer = cli.FlagStringer

func init() {
	cli.FlagStringer = tabularFlagStringer
}

// tabularFlagStringer wraps long flag usage strings into multiple lines, each
// prefixed with a tab: the help tabwriter then aligns every line under the
// usage column, so the flag help reads like a table instead of overflowing to
// the start of the next terminal row.
func tabularFlagStringer(f cli.Flag) string {
	s := defaultFlagStringer(f)
	names, usage, ok := strings.Cut(s, "\t")
	if !ok {
		return s
	}
	words := strings.Fields(usage)
	if len(words) == 0 {
		return s
	}
	width := flagUsageWidth()
	var b strings.Builder
	b.WriteString(names)
	b.WriteString("\t")
	lineLen := 0
	for i, word := range words {
		if i > 0 {
			if lineLen+1+len(word) > width {
				b.WriteString("\n\t")
				lineLen = 0
			} else {
				b.WriteString(" ")
				lineLen++
			}
		}
		b.WriteString(word)
		lineLen += len(word)
	}
	return b.String()
}

// flagUsageWidth returns the target width of the flag usage column, derived
// from the terminal width when available.
func flagUsageWidth() int {
	// approximate width taken by the flag names column in the help output
	const nameColumnWidth = 40
	width := 80
	if tw, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil {
		width = tw - nameColumnWidth
	}
	if width < 40 {
		width = 40
	}
	if width > 100 {
		width = 100
	}
	return width
}

const DefaultVersion = "3.0.0"

// threeTZFilename is the name of the archive written inside each tileset's
// output folder when --3tz is active.
const threeTZFilename = "tileset.3tz"

type TilerProvider func() (tiler.Tiler, error)

type CommandFactory func(Context) *cli.Command

type Context struct {
	BuildInfo BuildInfo
	Branding  Branding
}

type BuildInfo struct {
	Version        string
	DefaultVersion string
	GitCommit      string
}

func (b BuildInfo) VersionString() string {
	v := b.Version
	if v == "" {
		v = b.DefaultVersion
	}
	if v == "" {
		v = DefaultVersion
	}
	commit := b.GitCommit
	if commit == "" {
		commit = "(na)"
	}
	return fmt.Sprintf("%s-%s", v, commit)
}

type Branding struct {
	Name  string
	Usage string
	Logo  string
	Color string
}

type Options struct {
	BuildInfo     BuildInfo
	Branding      Branding
	TilerProvider TilerProvider
	ExtraCommands []CommandFactory
	// PointCloudCommandName moves the point-cloud flags and action under the
	// named subcommand. Leave empty to expose point-cloud conversion at root.
	PointCloudCommandName    string
	PointCloudCommandAliases []string
}

func DefaultTilerProvider() (tiler.Tiler, error) {
	return tiler.NewGoTiler()
}

const color = "\x1b[38;2;0;175;185m"

const logo = `
{{color}}
 ██████╗  ██████╗ ████████╗██╗██╗     ███████╗██████╗      ██████╗██╗     ██╗
██╔════╝ ██╔═══██╗╚══██╔══╝██║██║     ██╔════╝██╔══██╗    ██╔════╝██║     ██║
██║  ███╗██║   ██║   ██║   ██║██║     █████╗  ██████╔╝    ██║     ██║     ██║
██║   ██║██║   ██║   ██║   ██║██║     ██╔══╝  ██╔══██╗    ██║     ██║     ██║
╚██████╔╝╚██████╔╝   ██║   ██║███████╗███████╗██║  ██║    ╚██████╗███████╗██║
 ╚═════╝  ╚═════╝    ╚═╝   ╚═╝╚══════╝╚══════╝╚═╝  ╚═╝     ╚═════╝╚══════╝╚═╝
-----------------------------------------------------------------------------
A fast OGC 3D Tiles generator for point clouds.
Copyright YYYY - Massimo Federico Bonfigli
build: ZZZZ
{{end_color}}
`

func DefaultBranding() Branding {
	return Branding{
		Name:  "gotiler",
		Usage: "transforms point cloud files into OGC 3D Tiles 1.0 or 1.1 format",
		Logo:  logo,
		Color: color,
	}
}

func NewApp(opts Options) *cli.Command {
	if opts.BuildInfo.DefaultVersion == "" {
		opts.BuildInfo.DefaultVersion = DefaultVersion
	}
	opts.Branding = normalizeBranding(opts.Branding)
	if opts.TilerProvider == nil {
		opts.TilerProvider = DefaultTilerProvider
	}
	c := defaultCliOptions()
	cmdCtx := Context{
		BuildInfo: opts.BuildInfo,
		Branding:  opts.Branding,
	}
	pointCloudAction := func(ctx context.Context, cmd *cli.Command) error {
		if cmd.Args().Len() == 0 {
			_ = cli.ShowAppHelp(cmd)
			return fmt.Errorf("input path must be set")
		}
		return pointCloudCommand(opts.TilerProvider, c, cmd.Args().First())
	}
	commands := []*cli.Command{
		{
			Name:  "version",
			Usage: "print the gotiler version",
			Action: func(ctx context.Context, cmd *cli.Command) error {
				root := cmd.Root()
				fmt.Fprintln(root.Writer, root.Version)
				return nil
			},
		},
	}
	var appFlags []cli.Flag
	var appAction cli.ActionFunc
	if opts.PointCloudCommandName == "" {
		appFlags = getPointCloudFlags(c)
		appAction = pointCloudAction
	} else {
		pointCloudCommandName := opts.PointCloudCommandName
		commands = append(commands, &cli.Command{
			Name:    pointCloudCommandName,
			Aliases: opts.PointCloudCommandAliases,
			Usage:   "convert a point cloud file or folder into 3D tiles",
			Flags:   getPointCloudFlags(c),
			Action: func(ctx context.Context, cmd *cli.Command) error {
				if cmd.Args().Len() == 0 {
					_ = cli.ShowSubcommandHelp(cmd)
					return fmt.Errorf("input path must be set")
				}
				return pointCloudCommand(opts.TilerProvider, c, cmd.Args().First())
			},
		})
	}
	for _, factory := range opts.ExtraCommands {
		if cmd := factory(cmdCtx); cmd != nil {
			commands = append(commands, cmd)
		}
	}
	return &cli.Command{
		Name:                  opts.Branding.Name,
		Usage:                 opts.Branding.Usage,
		Version:               opts.BuildInfo.VersionString(),
		HideVersion:           true,
		Flags:                 appFlags,
		Action:                appAction,
		Commands:              commands,
		EnableShellCompletion: true,
	}
}

func normalizeBranding(branding Branding) Branding {
	defaults := DefaultBranding()
	if branding.Name == "" {
		branding.Name = defaults.Name
	}
	if branding.Usage == "" {
		branding.Usage = defaults.Usage
	}
	if branding.Logo == "" {
		branding.Logo = defaults.Logo
	}
	if branding.Color == "" {
		branding.Color = defaults.Color
	}
	return branding
}

func getPointCloudFlags(c *cliOpts) []cli.Flag {
	stdFlags := getFlags(c)
	joinFlag := &cli.BoolFlag{
		Name:        "join",
		Aliases:     []string{"j"},
		Value:       c.join,
		Usage:       "when the input is a folder, merge point cloud files into a single cloud. The files must have the same properties (CRS etc)",
		Destination: &c.join,
	}
	return append(stdFlags, joinFlag)
}

func getFlags(c *cliOpts) []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:        "out",
			Aliases:     []string{"o"},
			Value:       c.output,
			Usage:       "full path of the output folder where to save the resulting 3D Tiles tilesets",
			Destination: &c.output,
		},
		&cli.StringFlag{
			Name:        "crs",
			Aliases:     []string{"c"},
			Value:       c.crs,
			Usage:       "String representing the input CRS. For example, EPSG:4326 or EPSG:28355+5773 or a generic Proj4 string. Bare numbers will be interpreted as EPSG codes. If empty the system will attempt to autodetect the CRS from the file metadata if possible. In case of multiple files, the CRS must be consistent else an error will be thrown.",
			Destination: &c.crs,
		},
		&cli.Float64Flag{
			Name:        "z-offset",
			Aliases:     []string{"z"},
			Value:       c.zOffset,
			Usage:       "z offset to apply to the point, in meters. only use it if the input elevation is referred to the WGS84 ellipsoid or geoid",
			Destination: &c.zOffset,
		},
		&cli.IntFlag{
			Name:        "points-per-tile",
			Aliases:     []string{"p"},
			Value:       c.pointsPerTile,
			Usage:       "approximate number of points that a tile should contain. It is a target value that regulates the average tile size, but tiles with more or less points are still possible. The value should be above 5000 and below 5 million points. Default is 50000 points.",
			Destination: &c.pointsPerTile,
		},
		&cli.StringFlag{
			Name:        "refine-mode",
			Aliases:     []string{"r"},
			Value:       c.refineMode,
			Usage:       `Refine mode to be used to generate the tiles. Can be either 'add' (default) or 'replace'. `,
			Destination: &c.refineMode,
		},
		&cli.BoolFlag{
			Name:        "8-bit",
			Value:       c.eightBit,
			Usage:       "set to interpret the input points color as part of a 8bit color space",
			Destination: &c.eightBit,
		},
		&cli.StringFlag{
			Name:        "version",
			Aliases:     []string{"v"},
			Value:       c.version,
			Usage:       "sets the version of the tileset to generate. Could be either 1.0 or 1.1",
			Destination: &c.version,
		},
		&cli.Float64Flag{
			Name:        "initial-geometric-error",
			Value:       c.initialGeometricError,
			Usage:       "minimum target geometric error in meters for the root tile. Higher values produce a coarser root tile visible from farther away. Use 0 (default) to derive it from the dataset size.",
			Destination: &c.initialGeometricError,
		},
		&cli.Float64Flag{
			Name:        "ge-correction",
			Value:       c.geCorrection,
			Usage:       "multiplier applied to all geometric error values in the output tileset.json. Controls at what distance the viewer switches between LOD levels. Values > 1 make tiles appear at greater distances, < 1 only up close. Does not change how many LOD levels are built or which points belong to each level. Default is 1.0.",
			Destination: &c.geCorrection,
		},
		&cli.StringFlag{
			Name:        "attributes",
			Value:       c.attributes,
			Usage:       `comma-separated list of optional per-point attributes to include in the output tiles. Accepts any attribute exposed by the input files, matched case-insensitively: standard names such as "intensity", "classification", "return_number", "number_of_returns"; reader-specific ones (e.g. "gps_time", "scan_angle", "point_source_id", "user_data" for LAS/LAZ); and any extra-byte or extension attribute defined in the source. Attributes not found in the source are skipped, and attributes whose data type cannot be represented by the chosen tileset version may be omitted from the output. Use "none" to export no attributes (smaller tiles, no metadata). Default is "intensity,classification".`,
			Destination: &c.attributes,
		},
		&cli.StringFlag{
			Name:        "colorize",
			Value:       c.colorize,
			Usage:       `colorize points from a numeric attribute or local point coordinate using "attribute:gradient[:modifier...]", for example "z:viridis" or "intensity:turbo:reverse:steps=8". The gradient is stretched between the 2nd and 98th percentile of the values found in the data; gradients encoding absolute scales, like "las-classification", are applied as is. Gradients include "viridis", "magma", "inferno", "plasma", "cividis", "turbo", "grayscale", "heat", "las-classification", plus an extended library: topographic ("terrain", "gist-earth", "batlow", "oleron", "nuuk", "topo"), sequential ("cubehelix", "mako", "rocket", "haline", "amp", "ylgnbu", "blues", "viridis-pastel"), diverging ("rdbu", "brbg", "spectral", "piyg", "coolwarm", "seismic", "balance", "roma", "berlin"), and categorical ("dark2", "paired", "set2", "accent"); see the README for details. Modifiers: "reverse" flips the color order, "steps=N" quantizes into N discrete bands, "stretch=pLow,pHigh" sets the percentile stretch ("stretch=minmax" scales over the full range), "blend=0.5" mixes the gradient with the original point color.`,
			Destination: &c.colorize,
		},
		&cli.BoolFlag{
			Name:        "include-withheld",
			Value:       c.includeWithheld,
			Usage:       "include points marked as withheld. By default, withheld points are filtered out when the source exposes a withheld attribute.",
			Destination: &c.includeWithheld,
		},
		&cli.Float64Flag{
			Name:        "longitude",
			Value:       c.longitude,
			Usage:       "only with --crs local: the longitude in EPSG:4326 coordinates (degrees) at which to place the model's origin.",
			Destination: &c.longitude,
		},
		&cli.Float64Flag{
			Name:        "latitude",
			Value:       c.latitude,
			Usage:       "only with --crs local: the latitude in EPSG:4326 coordinates (degrees) at which to place the model's origin.",
			Destination: &c.latitude,
		},
		&cli.Float64Flag{
			Name:        "height",
			Value:       c.height,
			Usage:       "only with --crs local: the height in meters relative to the WGS84 ellipsoid at which to place the model's origin.",
			Destination: &c.height,
		},
		&cli.Float64Flag{
			Name:        "heading",
			Value:       c.heading,
			Usage:       "only with --crs local: the rotation in degrees from the local north direction where a positive angle is increasing eastward.",
			Destination: &c.heading,
		},
		&cli.Float64Flag{
			Name:        "pitch",
			Value:       c.pitch,
			Usage:       "only with --crs local: the rotation in degrees from the local east-north plane. Positive pitch angles are above the plane. Negative pitch angles are below the plane.",
			Destination: &c.pitch,
		},
		&cli.Float64Flag{
			Name:        "roll",
			Value:       c.roll,
			Usage:       "only with --crs local: the rotation in degrees applied to the local east axis.",
			Destination: &c.roll,
		},
		&cli.Float64Flag{
			Name:        "scale",
			Aliases:     []string{"s"},
			Value:       c.scale,
			Usage:       "only with --crs local: the uniform scale to apply to the model. The tiler assumes the units for the input model(s) are in meters.",
			Destination: &c.scale,
		},
		&cli.StringFlag{
			Name:        "input-up-axis",
			Value:       c.inputUpAxis,
			Usage:       `only with --crs local: overrides the model's default up axis and treats the given axis ("x", "y" or "z") as up.`,
			Destination: &c.inputUpAxis,
		},
		&cli.StringFlag{
			Name:        "compression",
			Value:       c.compression,
			Usage:       `compression applied to the output tiles: "meshopt" (default) compresses GLB tile content with meshoptimizer EXT_meshopt_compression and quantization, "none" disables compression. Compression requires tileset version 1.1; version 1.0 (.pnts) output is never compressed.`,
			Destination: &c.compression,
		},
		&cli.BoolFlag{
			Name:        "meshopt-khr",
			Value:       c.meshoptKHR,
			Usage:       `only with --compression meshopt: compress the tiles with the Khronos KHR_meshopt_compression extension and its version 1 codec, which produces smaller tiles than the default EXT_meshopt_compression. Fewer viewers support it: CesiumJS added it in version 1.143.`,
			Destination: &c.meshoptKHR,
		},
		&cli.Float64Flag{
			Name:        "subsample",
			Value:       c.subsample,
			Usage:       "percentage of points to keep, in (0, 100]. Points are dropped uniformly at random while reading. Default is 100 (keep everything).",
			Destination: &c.subsample,
		},
		&cli.StringFlag{
			Name:        "geotiff-colorize",
			Value:       c.geoTIFFColorize,
			Usage:       "colorize points on the fly from an RGB/RGBA GeoTIFF orthophoto",
			Destination: &c.geoTIFFColorize,
		},
		&cli.BoolFlag{
			Name:        "3tz",
			Value:       c.threeTZ,
			Usage:       `write each tileset as a single OGC 3D Tiles Archive (".3tz" file) saved inside the tileset's output folder, instead of a folder of loose tiles. Folder inputs without --join produce one archive per file, each inside its own output subfolder.`,
			Destination: &c.threeTZ,
		},
		&cli.BoolFlag{
			Name:        "plain",
			Value:       c.plain,
			Usage:       "disable progress bars and print only milestone updates as plain text",
			Destination: &c.plain,
		},
		&cli.StringFlag{
			Name:        "profile",
			Value:       c.profile,
			Usage:       "Enable pprof profiling. Values: 'cpu' for CPU profile, 'memory' for heap profile. Empty = disabled.",
			Destination: &c.profile,
			Hidden:      true,
		},
		&cli.StringFlag{
			Name:        "profile-output",
			Value:       c.profileOutput,
			Usage:       "Output file path for profile data. Defaults to 'pprof_<type>.prof' in the current directory.",
			Destination: &c.profileOutput,
			Hidden:      true,
		},
	}
}

type cliOpts struct {
	output                string
	crs                   string
	pointsPerTile         int
	zOffset               float64
	refineMode            string
	eightBit              bool
	join                  bool
	version               string
	plain                 bool
	profile               string
	profileOutput         string
	initialGeometricError float64
	geCorrection          float64
	attributes            string
	includeWithheld       bool
	colorize              string
	// placement flags, active with --crs local
	longitude   float64
	latitude    float64
	height      float64
	heading     float64
	pitch       float64
	roll        float64
	scale       float64
	inputUpAxis string
	// output and pipeline extensions
	compression     string
	meshoptKHR      bool
	subsample       float64
	geoTIFFColorize string
	threeTZ         bool
}

func defaultCliOptions() *cliOpts {
	return &cliOpts{
		crs:                   "",
		pointsPerTile:         50000,
		zOffset:               0,
		eightBit:              false,
		join:                  false,
		version:               "1.1",
		refineMode:            "add",
		plain:                 false,
		profile:               "",
		profileOutput:         "",
		initialGeometricError: 0,
		geCorrection:          1.0,
		attributes:            "intensity,classification",
		includeWithheld:       false,
		colorize:              "",
		longitude:             0,
		latitude:              0,
		height:                0,
		heading:               0,
		pitch:                 0,
		roll:                  0,
		scale:                 1,
		inputUpAxis:           "z",
		compression:           "meshopt",
		meshoptKHR:            false,
		subsample:             100,
		geoTIFFColorize:       "",
		threeTZ:               false,
	}
}

func (c *cliOpts) validate() {
	if c.output == "" {
		log.Fatal("output flag must be set")
	}
	if c.pointsPerTile < 5000 || c.pointsPerTile > 5_000_000 {
		log.Fatal("points-per-tile should be between 5000 and 5 million")
	}
	if c.refineMode != "add" && c.refineMode != "replace" {
		log.Fatal("refine-mode should be either 'add' or 'replace'")
	}
	if _, ok := version.Parse(c.version); !ok {
		log.Fatal("invalid tileset version, the only allowed values are '1.0' and '1.1'")
	}
	if c.profile != "" && c.profile != "cpu" && c.profile != "memory" {
		log.Fatal("invalid profile type, the only allowed values are 'cpu' and 'memory'")
	}
	if c.initialGeometricError < 0 {
		log.Fatal("initial-geometric-error must be greater than or equal to 0")
	}
	if c.geCorrection <= 0 {
		log.Fatal("ge-correction must be greater than 0")
	}
	if _, err := model.ParseAttributes(strings.Split(c.attributes, ",")); err != nil {
		log.Fatalf("--attributes: %v", err)
	}
	if _, err := parseColorizer(c.colorize); err != nil {
		log.Fatalf("--colorize: %v", err)
	}
	if c.placementMode() {
		if _, err := c.placement().Transform(); err != nil {
			log.Fatalf("invalid placement: %v", err)
		}
	} else if c.placementFlagsSet() {
		log.Fatal(`the placement flags (--longitude, --latitude, --height, --heading, --pitch, --roll, --scale, --input-up-axis) require --crs local`)
	}
	if c.compression != "meshopt" && c.compression != "none" {
		log.Fatalf(`--compression: unsupported value %q, expected "meshopt" or "none"`, c.compression)
	}
	if c.meshoptKHR && (c.compression != "meshopt" || c.version == "1.0") {
		log.Fatal("--meshopt-khr requires --compression meshopt and tileset version 1.1")
	}
	if c.subsample <= 0 || c.subsample > 100 {
		log.Fatalf("--subsample: percentage must be in (0, 100], got %v", c.subsample)
	}
}

// Box rendering constants.
const (
	boxTotalWidth = 76 // total visual width including border chars
	boxInnerWidth = 72 // visual width between "│ " and " │"
)

// visualWidth approximates the terminal display width of s.
// Supplementary-plane code points (> U+FFFF) count as 2 columns.
// U+FE0F (variation selector-16) counts as 0 columns.
// Everything else counts as 1 column.
func visualWidth(s string) int {
	w := 0
	for _, r := range s {
		switch {
		case r == 0xFE0F:
			// zero-width variation selector
		case r > 0xFFFF:
			w += 2
		default:
			w += 1
		}
	}
	return w
}

// truncLeft truncates s to width display columns, keeping the tail and
// prepending "..." when s is too long. Returns s unchanged if it fits.
func truncLeft(s string, width int) string {
	if visualWidth(s) <= width {
		return s
	}
	runes := []rune(s)
	cur := 0
	start := len(runes)
	for start > 0 {
		rw := 1
		if runes[start-1] > 0xFFFF {
			rw = 2
		}
		if cur+rw > width-3 {
			break
		}
		cur += rw
		start--
	}
	return "..." + string(runes[start:])
}

// padToWidth right-pads s with spaces to reach width display columns.
// Truncates to width-3 columns and appends "..." if s is too wide.
func padToWidth(s string, width int) string {
	vw := visualWidth(s)
	if vw < width {
		return s + strings.Repeat(" ", width-vw)
	}
	if vw == width {
		return s
	}
	var buf []rune
	cur := 0
	for _, r := range s {
		rw := 1
		if r > 0xFFFF {
			rw = 2
		}
		if cur+rw > width-3 {
			break
		}
		buf = append(buf, r)
		cur += rw
	}
	return string(buf) + "..."
}

func boxHeader(title string) string {
	const leftDashes = "────────"
	rightW := boxTotalWidth - 2 - 8 - visualWidth(title)
	if rightW < 2 {
		rightW = 2
	}
	return "┌" + leftDashes + title + strings.Repeat("─", rightW) + "┐"
}

func boxFooter() string {
	return "└" + strings.Repeat("─", boxTotalWidth-2) + "┘"
}

func boxRow(label, value string) string {
	valueWidth := boxInnerWidth - visualWidth(label)
	if valueWidth < 0 {
		valueWidth = 0
	}
	return "│ " + label + padToWidth(value, valueWidth) + " │"
}

// boxRowWrapped is like boxRow but wraps value at comma boundaries when it
// exceeds the available width, producing multiple box rows instead of truncating.
func boxRowWrapped(label, value string) string {
	valueWidth := boxInnerWidth - visualWidth(label)
	if valueWidth < 1 {
		valueWidth = 1
	}
	if visualWidth(value) <= valueWidth {
		return boxRow(label, value)
	}
	parts := strings.Split(value, ",")
	blankLabel := strings.Repeat(" ", visualWidth(label))
	var lines []string
	cur := ""
	for i, p := range parts {
		token := p
		if i < len(parts)-1 {
			token = p + ","
		}
		if cur == "" {
			cur = token
		} else if visualWidth(cur+token) <= valueWidth {
			cur += token
		} else {
			lines = append(lines, cur)
			cur = token
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	var rows []string
	for i, line := range lines {
		lbl := blankLabel
		if i == 0 {
			lbl = label
		}
		rows = append(rows, boxRow(lbl, line))
	}
	return strings.Join(rows, "\n")
}

func formatComma(n int) string {
	s := fmt.Sprintf("%d", n)
	var buf []byte
	for i, ch := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			buf = append(buf, ',')
		}
		buf = append(buf, byte(ch))
	}
	return string(buf)
}

func FormatComma(n int) string {
	return formatComma(n)
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func (c *cliOpts) printSummary(input, mode string) {
	crsDisp := c.crs
	if crsDisp == "" {
		crsDisp = "(autodetect)"
	}
	joinDisp := "Disabled"
	if c.join {
		joinDisp = "Enabled"
	}
	initialGeometricErrorDisp := formatInitialGeometricError(c.initialGeometricError)

	if c.plain {
		fmt.Fprintf(os.Stderr, "Input:      %s\n", input)
		fmt.Fprintf(os.Stderr, "Output:     %s\n", c.output)
		fmt.Fprintf(os.Stderr, "Mode:       %s\n", mode)
		fmt.Fprintf(os.Stderr, "Join:       %s\n", joinDisp)
		fmt.Fprintf(os.Stderr, "CRS:        %s\n", crsDisp)
		fmt.Fprintf(os.Stderr, "Pts/Tile:   %s\n", formatComma(c.pointsPerTile))
		fmt.Fprintf(os.Stderr, "Version:    %s\n", c.version)
		fmt.Fprintf(os.Stderr, "Z-Offset:   %g meters\n", c.zOffset)
		fmt.Fprintf(os.Stderr, "Refine:     %s\n", capitalize(c.refineMode))
		fmt.Fprintf(os.Stderr, "8-Bit:      %s\n", capitalize(fmt.Sprintf("%v", c.eightBit)))
		fmt.Fprintf(os.Stderr, "Init GE:    %s\n", initialGeometricErrorDisp)
		fmt.Fprintf(os.Stderr, "GE Corr:    %gx\n", c.geCorrection)
		fmt.Fprintf(os.Stderr, "Attribs:    %s\n", c.attributes)
		fmt.Fprintf(os.Stderr, "Colorize:   %s\n", colorizeSummary(c.colorize))
		fmt.Fprintf(os.Stderr, "Incl Withheld: %v\n", c.includeWithheld)
		if c.placementMode() {
			for _, row := range c.placementRows() {
				fmt.Fprintf(os.Stderr, "%-19s %s\n", row[0]+":", row[1])
			}
		}
		fmt.Fprintf(os.Stderr, "Compress:   %s\n", c.compressionSummary())
		fmt.Fprintf(os.Stderr, "Subsample:  %s\n", c.subsampleSummary())
		fmt.Fprintf(os.Stderr, "GeoTIFF:    %s\n", colorizeSummary(c.geoTIFFColorize))
		fmt.Fprintf(os.Stderr, "3TZ:        %s\n", c.threeTZSummary())
		fmt.Fprintln(os.Stderr)
		return
	}

	fmt.Fprintln(os.Stderr, boxHeader("📥 Inputs & Outputs "))
	fmt.Fprintln(os.Stderr, boxRow("• Input Source:       ", truncLeft(input, boxInnerWidth-23)))
	fmt.Fprintln(os.Stderr, boxRow("• Output Destination: ", truncLeft(c.output, boxInnerWidth-23)))
	fmt.Fprintln(os.Stderr, boxRow("• Execution Mode:     ", mode))
	fmt.Fprintln(os.Stderr, boxRow("• Join Clouds:        ", joinDisp))
	fmt.Fprintln(os.Stderr, boxFooter())
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, boxHeader("📊 Processing Options "))
	fmt.Fprintln(os.Stderr, boxRow("• Source CRS:         ", crsDisp))
	fmt.Fprintln(os.Stderr, boxRow("• Target Pts/Tile:    ", formatComma(c.pointsPerTile)))
	fmt.Fprintln(os.Stderr, boxRow("• 3D Tiles Version:   ", c.version))
	fmt.Fprintln(os.Stderr, boxRow("• Z-Offset Correct:   ", fmt.Sprintf("%g meters", c.zOffset)))
	fmt.Fprintln(os.Stderr, boxRow("• Refine Mode:        ", capitalize(c.refineMode)))
	fmt.Fprintln(os.Stderr, boxRow("• 8-Bit Color Corr:   ", capitalize(fmt.Sprintf("%v", c.eightBit))))
	fmt.Fprintln(os.Stderr, boxRow("• Initial Geom Err:   ", initialGeometricErrorDisp))
	fmt.Fprintln(os.Stderr, boxRow("• GE Correction:      ", fmt.Sprintf("%gx", c.geCorrection)))
	fmt.Fprintln(os.Stderr, boxRowWrapped("• Attributes:         ", c.attributes))
	fmt.Fprintln(os.Stderr, boxRow("• Colorize:             ", colorizeSummary(c.colorize)))
	fmt.Fprintln(os.Stderr, boxRow("• Include Withheld:     ", fmt.Sprintf("%v", c.includeWithheld)))
	if c.placementMode() {
		for _, row := range c.placementRows() {
			fmt.Fprintln(os.Stderr, boxRow("• "+padToWidth(row[0]+":", 20)+" ", row[1]))
		}
	}
	fmt.Fprintln(os.Stderr, boxRow("• Compression:          ", c.compressionSummary()))
	fmt.Fprintln(os.Stderr, boxRow("• Subsample:            ", c.subsampleSummary()))
	fmt.Fprintln(os.Stderr, boxRow("• GeoTIFF Colorize:     ", colorizeSummary(c.geoTIFFColorize)))
	fmt.Fprintln(os.Stderr, boxRow("• 3TZ Archive:          ", c.threeTZSummary()))
	fmt.Fprintln(os.Stderr, boxFooter())
	fmt.Fprintln(os.Stderr)
}

func (c *cliOpts) compressionSummary() string {
	switch {
	case c.compression == "none":
		return "Disabled"
	case c.version == "1.0":
		// .pnts output has no compressed variant
		return "Disabled (3D Tiles 1.0)"
	case c.meshoptKHR:
		return "KHR_meshopt_compression + KHR_mesh_quantization"
	default:
		return "EXT_meshopt_compression + KHR_mesh_quantization"
	}
}

func (c *cliOpts) subsampleSummary() string {
	if c.subsample >= 100 {
		return "Disabled"
	}
	return fmt.Sprintf("%g%% of points kept", c.subsample)
}

func (c *cliOpts) threeTZSummary() string {
	if !c.threeTZ {
		return "Disabled"
	}
	return threeTZFilename + " in each tileset's output folder"
}

func formatInitialGeometricError(ge float64) string {
	if ge <= 0 {
		return "auto"
	}
	return fmt.Sprintf("%g meters", ge)
}

func colorizeSummary(spec string) string {
	if spec == "" {
		return "disabled"
	}
	return spec
}

// placementMode reports whether the input is ungeoreferenced local cartesian
// data to be placed on the globe through the placement flags ("--crs local").
func (c *cliOpts) placementMode() bool {
	return strings.EqualFold(strings.TrimSpace(c.crs), "local")
}

// placement builds the tiler placement from the placement flags.
func (c *cliOpts) placement() tiler.Placement {
	return tiler.Placement{
		Longitude: c.longitude,
		Latitude:  c.latitude,
		Height:    c.height,
		Heading:   c.heading,
		Pitch:     c.pitch,
		Roll:      c.roll,
		Scale:     c.scale,
		UpAxis:    tiler.Axis(strings.ToLower(strings.TrimSpace(c.inputUpAxis))),
	}
}

// placementFlagsSet reports whether any placement flag differs from its default.
func (c *cliOpts) placementFlagsSet() bool {
	return c.longitude != 0 || c.latitude != 0 || c.height != 0 || c.heading != 0 ||
		c.pitch != 0 || c.roll != 0 || c.scale != 1 || !strings.EqualFold(c.inputUpAxis, "z")
}

// placementRows returns the label/value summary rows of the active placement.
func (c *cliOpts) placementRows() [][2]string {
	p := c.placement()
	scale := p.Scale
	if scale == 0 {
		scale = 1
	}
	up := string(p.UpAxis)
	if up == "" {
		up = "z"
	}
	return [][2]string{
		{"Placement Lon/Lat", fmt.Sprintf("%g, %g", p.Longitude, p.Latitude)},
		{"Placement Height", fmt.Sprintf("%gm", p.Height)},
		{"Placement H/P/R", fmt.Sprintf("%g/%g/%g", p.Heading, p.Pitch, p.Roll)},
		{"Placement Scale", fmt.Sprintf("%g", scale)},
		{"Placement Up Axis", up},
	}
}

func parseColorizer(spec string) (*mutator.Colorizer, error) {
	if spec == "" {
		return nil, nil
	}
	parts := strings.Split(spec, ":")
	if len(parts) < 2 {
		return nil, fmt.Errorf(`expected "attribute:gradient[:modifier...]"`)
	}
	attribute := parts[0]
	gradient := parts[1]
	if attribute == "" {
		return nil, fmt.Errorf("attribute name cannot be empty")
	}
	if gradient == "" {
		return nil, fmt.Errorf("gradient name cannot be empty")
	}
	var opts []mutator.ColorizerOption
	for _, mod := range parts[2:] {
		opt, err := parseColorizerModifier(mod)
		if err != nil {
			return nil, err
		}
		opts = append(opts, opt)
	}
	return mutator.NewColorizer(attribute, gradient, opts...)
}

// parseColorizerModifier parses one colorize spec modifier: "reverse",
// "steps=N", "stretch=pLow,pHigh" (or "stretch=minmax"), "blend=alpha".
func parseColorizerModifier(mod string) (mutator.ColorizerOption, error) {
	key, value, hasValue := strings.Cut(mod, "=")
	switch key {
	case "reverse":
		if hasValue {
			return nil, fmt.Errorf(`modifier "reverse" takes no value`)
		}
		return mutator.WithReverse(), nil
	case "steps":
		n, err := strconv.Atoi(value)
		if err != nil {
			return nil, fmt.Errorf("invalid steps %q: %w", value, err)
		}
		return mutator.WithSteps(n), nil
	case "stretch":
		if value == "minmax" {
			return mutator.WithStretch(0, 100), nil
		}
		lowStr, highStr, ok := strings.Cut(value, ",")
		if !ok {
			return nil, fmt.Errorf(`invalid stretch %q: expected "pLow,pHigh" or "minmax"`, value)
		}
		low, err := strconv.ParseFloat(lowStr, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid stretch lower percentile %q: %w", lowStr, err)
		}
		high, err := strconv.ParseFloat(highStr, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid stretch upper percentile %q: %w", highStr, err)
		}
		return mutator.WithStretch(low, high), nil
	case "blend":
		alpha, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid blend %q: %w", value, err)
		}
		return mutator.WithBlend(alpha), nil
	default:
		return nil, fmt.Errorf("unknown colorize modifier %q", mod)
	}
}

func (c *cliOpts) getTilerOptions() *tiler.TilerOptions {
	c.validate()
	mutators := []mutator.Mutator{
		mutator.NewZOffset(float32(c.zOffset)),
	}
	if !c.includeWithheld {
		mutators = append(mutators, mutator.NewWithheldFilter())
	}
	if colorizer, err := parseColorizer(c.colorize); err != nil {
		log.Fatalf("--colorize: %v", err)
	} else if colorizer != nil {
		mutators = append(mutators, colorizer)
	}
	if c.subsample < 100 {
		mutators = append(mutators, subsampler.New(c.subsample/100))
	}
	if c.geoTIFFColorize != "" {
		converter, err := coordproj.NewConverter()
		if err != nil {
			log.Fatalf("--geotiff-colorize: %v", err)
		}
		geoColorizer, err := geotiffcolorizer.NewColorizerFromFile(c.geoTIFFColorize, converter)
		if err != nil {
			log.Fatalf("--geotiff-colorize: %v", err)
		}
		mutators = append(mutators, geoColorizer)
	}
	refineMode := model.RefineAdd
	if c.refineMode == "replace" {
		refineMode = model.RefineReplace
	}
	progressCb := tiler.ProgressCallback(progressListener)
	if c.plain {
		progressCb = plainListener
	}
	// Already validated in validate(), so error can be safely ignored here.
	attrs, _ := model.ParseAttributes(strings.Split(c.attributes, ","))
	tilerOpts := tiler.NewTilerOptions(
		tiler.WithEightBitColors(c.eightBit),
		tiler.WithMutators(mutators),
		tiler.WithPointsPerTile(c.pointsPerTile),
		tiler.WithProgressCallback(progressCb),
		tiler.WithEncoder(c.encoderID()),
		tiler.WithRefineMode(refineMode),
		tiler.WithInitialGeometricError(c.initialGeometricError),
		tiler.WithGECorrection(c.geCorrection),
		tiler.WithAttributes(attrs),
	)
	if c.placementMode() {
		tilerOpts.Apply(tiler.WithPlacement(c.placement()))
	}
	return tilerOpts
}

// encoderID selects the geometry encoder: compressed GLB by default; .pnts
// (tileset version 1.0) output has no compressed variant.
func (c *cliOpts) encoderID() string {
	switch {
	case c.version == "1.0":
		return plugin.EncoderPNTS
	case c.compression == "meshopt" && c.meshoptKHR:
		return compression.EncoderCompressedGLBKHR
	case c.compression == "meshopt":
		return compression.EncoderCompressedGLB
	default:
		return plugin.EncoderGLB
	}
}

func pointCloudCommand(provider TilerProvider, opts *cliOpts, inputPath string) error {
	if inputPath == "" {
		return fmt.Errorf("input path must be set")
	}
	info, err := os.Stat(inputPath)
	if err != nil {
		return fmt.Errorf("input path %q is not accessible: %w", inputPath, err)
	}
	if !info.IsDir() && opts.join {
		return fmt.Errorf("--join can only be used when input is a folder")
	}

	t, err := provider()
	if err != nil {
		log.Fatal(err)
	}
	tilerOpts := opts.getTilerOptions()
	crs := opts.crs
	if opts.placementMode() {
		// placement mode: the tiler receives no CRS and places the local
		// coordinates on the globe through the placement transform
		crs = ""
	} else if code, err := strconv.Atoi(crs); err == nil {
		crs = fmt.Sprintf("EPSG:%d", code)
	}

	if !info.IsDir() {
		opts.printSummary(inputPath, "File")
		runnable := func(ctx context.Context) error {
			if err := t.ProcessFiles([]string{inputPath}, opts.output, crs, tilerOpts, ctx); err != nil {
				return err
			}
			return opts.packageTilesets(nil)
		}
		launch(opts, runnable)
		return nil
	}

	mode := "Folder"
	if opts.join {
		mode = "Folder (joined)"
	}
	opts.printSummary(inputPath, mode)
	runnable := func(ctx context.Context) error {
		if opts.join {
			files, err := findPointCloudFilesInFolder(inputPath)
			if err != nil {
				return err
			}
			if err := t.ProcessFiles(files, opts.output, crs, tilerOpts, ctx); err != nil {
				return err
			}
			return opts.packageTilesets(nil)
		}
		if err := t.ProcessFolder(inputPath, opts.output, crs, tilerOpts, ctx); err != nil {
			return err
		}
		// folder mode produces one tileset per file, each in a subfolder of
		// the output folder named after the file
		files, err := findPointCloudFilesInFolder(inputPath)
		if err != nil {
			return err
		}
		return opts.packageTilesets(files)
	}
	launch(opts, runnable)
	return nil
}

// packageTilesets archives the written tilesets into .3tz files when --3tz is
// active. With no input files it packages the single tileset in the output
// folder; otherwise it packages one tileset subfolder per input file.
func (c *cliOpts) packageTilesets(inputFiles []string) error {
	if !c.threeTZ {
		return nil
	}
	folders := []string{c.output}
	if len(inputFiles) > 0 {
		folders = folders[:0]
		for _, f := range inputFiles {
			sub := strings.TrimSuffix(filepath.Base(f), filepath.Ext(f))
			folders = append(folders, filepath.Join(c.output, sub))
		}
	}
	for _, folder := range folders {
		if err := packageTileset(folder); err != nil {
			return fmt.Errorf("--3tz: %v", err)
		}
	}
	return nil
}

// packageTileset archives the tileset in outputFolder into a .3tz file inside
// the folder itself, then removes the loose tileset files, leaving the archive
// as the only artifact.
func packageTileset(outputFolder string) error {
	archivePath := filepath.Join(outputFolder, threeTZFilename)
	archive, err := threetz.New(archivePath)
	if err != nil {
		return err
	}
	err = filepath.WalkDir(outputFolder, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || path == archivePath {
			return nil
		}
		rel, err := filepath.Rel(outputFolder, path)
		if err != nil {
			return err
		}
		src, err := os.Open(path)
		if err != nil {
			return err
		}
		defer src.Close()
		dst, err := archive.Open(filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		if _, err := io.Copy(dst, src); err != nil {
			_ = dst.Close()
			return err
		}
		return dst.Close()
	})
	if err == nil {
		err = archive.Finalize()
	} else {
		_ = archive.Finalize()
	}
	if err != nil {
		// do not leave a partial archive behind
		_ = os.Remove(archivePath)
		return err
	}
	// remove the loose tileset files, leaving only the archive
	entries, err := os.ReadDir(outputFolder)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.Name() == threeTZFilename {
			continue
		}
		if err := os.RemoveAll(filepath.Join(outputFolder, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

func launch(opts *cliOpts, function func(ctx context.Context) error) {
	stopProfile := setupProfiling(opts)

	ctx, _ := signal.NotifyContext(context.Background(), os.Interrupt)
	wg := &sync.WaitGroup{}
	wg.Add(1)
	var runErr error
	go func() {
		defer wg.Done()
		runErr = function(ctx)
	}()
	wg.Wait()

	if stopProfile != nil {
		stopProfile()
	}
	if runErr != nil {
		fmt.Fprintf(os.Stderr, "\nError: %v\n", runErr)
		os.Exit(1)
	}
}

// setupProfiling starts the requested pprof profile.
// Returns a cleanup function that stops profiling and closes the output file.
func setupProfiling(opts *cliOpts) func() {
	return SetupProfiling(opts.profile, opts.profileOutput)
}

func SetupProfiling(profile string, output string) func() {
	profileType := strings.ToLower(profile)
	if profileType == "" {
		return nil
	}

	outputPath := output
	if outputPath == "" {
		outputPath = "pprof_" + profileType + ".prof"
	}

	f, err := os.Create(outputPath)
	if err != nil {
		log.Printf("WARNING: failed to create profile output file %s: %v", outputPath, err)
		return nil
	}

	switch profileType {
	case "cpu":
		if err := pprof.StartCPUProfile(f); err != nil {
			log.Printf("WARNING: failed to start CPU profile: %v", err)
			f.Close()
			return nil
		}
		fmt.Printf("[pprof] CPU profiling started, output: %s\n", outputPath)
		return func() {
			pprof.StopCPUProfile()
			f.Close()
			fmt.Printf("[pprof] CPU profile written to %s\n", outputPath)
		}
	case "memory":
		fmt.Printf("[pprof] Heap profiling enabled, output: %s\n", outputPath)
		return func() {
			if err := pprof.WriteHeapProfile(f); err != nil {
				log.Printf("WARNING: failed to write heap profile: %v", err)
			} else {
				fmt.Printf("[pprof] Heap profile written to %s\n", outputPath)
			}
			f.Close()
		}
	default:
		f.Close()
		log.Printf("WARNING: unknown profile type: %s", profileType)
		return nil
	}
}

// cliProgress holds the single active progress bar and tracks the current phase.
// It is goroutine-safe: multiple worker callbacks may call handle concurrently.
type cliProgress struct {
	mu           sync.Mutex
	bar          *progressbar.ProgressBar
	currentPhase int
	barMax       int   // max value used when creating the current bar
	totalPts     int64 // cumulative points seen across pts-unit phases
}

var clProg = &cliProgress{}

func (p *cliProgress) handle(e tiler.ProgressEvent) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Phase transition: complete the old bar and open a new one.
	if e.Phase.Index != p.currentPhase {
		if p.bar != nil {
			_ = p.bar.Set(p.barMax)
			fmt.Fprintln(os.Stderr) // move past the completed bar line
		}
		p.currentPhase = e.Phase.Index

		// Use the actual item count as the bar max when we know the total;
		// fall back to percent-based (max=100) when ItemTotal is unknown or N/A.
		useCountBased := e.ItemTotal > 0
		if useCountBased {
			p.barMax = int(e.ItemTotal)
		} else {
			p.barMax = 100
		}

		desc := fmt.Sprintf("[%d/%d] %-12s", e.Phase.Index, e.Phase.Total, e.Phase.Name)
		opts := []progressbar.Option{
			progressbar.OptionSetDescription(desc),
			progressbar.OptionSetWriter(os.Stderr),
			progressbar.OptionSetWidth(40),
			progressbar.OptionSetPredictTime(false),
			progressbar.OptionSetRenderBlankState(true),
			progressbar.OptionEnableColorCodes(true),
			progressbar.OptionSetTheme(progressbar.Theme{
				Saucer:        color + "█[reset]",
				SaucerHead:    "[light_blue]█[reset]",
				SaucerPadding: " ",
				BarStart:      "|",
				BarEnd:        "|",
			}),
		}
		if useCountBased && e.Phase.Unit != "" {
			opts = append(opts,
				progressbar.OptionShowIts(),
				progressbar.OptionSetItsString(e.Phase.Unit),
			)
		}
		p.bar = progressbar.NewOptions(p.barMax, opts...)
	}

	// Capture total point count from the preparation milestone.
	if e.Phase.Name == "Preparation" && e.Level == tiler.ProgressMilestone && e.ItemTotal > 0 {
		p.totalPts = e.ItemTotal
	}

	// Advance the bar.
	if p.bar != nil {
		if e.ItemTotal > 0 {
			_ = p.bar.Set(int(e.ItemCount))
		} else if e.Percent >= 0 {
			_ = p.bar.Set(int(e.Percent))
		}
	}

	// After the last phase finishes, add a trailing newline and print elapsed time.
	if e.Level == tiler.ProgressMilestone && e.Percent == 100 && e.Phase.Index == e.Phase.Total {
		fmt.Fprintln(os.Stderr)
		elapsed := time.Duration(e.ElapsedMs) * time.Millisecond
		fmt.Fprintf(os.Stderr, "\nCompleted in %s%s\n\n", elapsed.Round(time.Millisecond), formatThroughput(p.totalPts, elapsed))
		printDonationMessage()
		p.bar = nil
	}
}

func progressListener(e tiler.ProgressEvent) {
	clProg.handle(e)
}

func plainListener(e tiler.ProgressEvent) {
	if e.Level != tiler.ProgressMilestone {
		return
	}
	fmt.Fprintf(os.Stderr, "[%d/%d] %s: %s\n", e.Phase.Index, e.Phase.Total, e.Phase.Name, e.Message)
	if e.Percent == 100 && e.Phase.Index == e.Phase.Total {
		elapsed := time.Duration(e.ElapsedMs) * time.Millisecond
		fmt.Fprintf(os.Stderr, "Completed in %s%s\n\n", elapsed.Round(time.Millisecond), formatThroughput(clProg.totalPts, elapsed))
		printDonationMessage()
	}
}

// formatThroughput returns " (X.XM pts/sec)" when pts > 0, or "" otherwise.
func formatThroughput(pts int64, elapsed time.Duration) string {
	if pts <= 0 || elapsed <= 0 {
		return ""
	}
	rate := float64(pts) / elapsed.Seconds()
	switch {
	case rate >= 1e6:
		return fmt.Sprintf(" (%.1fM pts/sec)", rate/1e6)
	case rate >= 1e3:
		return fmt.Sprintf(" (%.1fK pts/sec)", rate/1e3)
	default:
		return fmt.Sprintf(" (%.0f pts/sec)", rate)
	}
}

var donationMessages = []string{
	"🌟 Happy with your new 3D Tiles? Consider starring the project: https://github.com/mfbonfigli/gotiler",
	"⏳ Has GoTiler CLI saved your team time? Help keep the engine open and sustainable: https://ko-fi.com/mfbonfigli",
	"💚 Enjoying GoTiler CLI? Support open-source maintenance and development: https://ko-fi.com/mfbonfigli",
}

func printDonationMessage() {
	PrintDonationMessage()
}

func PrintDonationMessage() {
	const reset = "\x1b[0m"
	msgColor := "\033[1;32m"
	fmt.Fprintln(os.Stderr, msgColor+donationMessages[rand.Intn(len(donationMessages))]+reset)
}

func PrintBanner(w io.Writer, branding Branding, build BuildInfo) {
	if w == nil {
		w = os.Stderr
	}
	branding = normalizeBranding(branding)
	reset := "\x1b[0m"
	banner := strings.ReplaceAll(branding.Logo, "YYYY", strconv.Itoa(time.Now().Year()))
	banner = strings.ReplaceAll(banner, "ZZZZ", build.VersionString())
	banner = strings.ReplaceAll(banner, "{{color}}", branding.Color)
	banner = strings.ReplaceAll(banner, "{{end_color}}", reset)
	fmt.Fprint(w, banner)
}
