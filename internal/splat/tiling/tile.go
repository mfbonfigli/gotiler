package tiling

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"

	"github.com/mfbonfigli/gotiler/v3/internal/geom"
	"github.com/mfbonfigli/gotiler/v3/internal/splat/spz"
	"github.com/mfbonfigli/gotiler/v3/tiler/tree"
)

// dataDirName is the folder of the tile contents, next to tileset.json.
const dataDirName = "data"

const (
	// readReportEvery and routeReportEvery are how many splats pass between
	// progress reports of reading and splitting.
	readReportEvery  = 100_000
	routeReportEvery = 100_000
)

// Options configure Tile. Start from DefaultOptions.
type Options struct {
	// SplatsPerTile is the most splats a tile holds. A leaf tile can hold a
	// little more, as the partition is planned from samples.
	SplatsPerTile int
	// MaxSHDegree is the highest spherical-harmonic degree kept, from 0 to 3.
	MaxSHDegree int
	// MinOpacity drops the splats of lower opacity, from 0 to 1.
	MinOpacity float64
	// UpAxis names the input axis that points up: x, y or z, each optionally
	// signed, as in "-y". Empty means z.
	UpAxis string
	// GECorrection scales the geometric errors of the tiles: higher values
	// make viewers switch to finer tiles sooner.
	GECorrection float64
	// RootTransform is the transform of the root tile, a 4x4 matrix in
	// column-major order, as 3D Tiles stores it, or nil for none. It places
	// the tileset, whose tiles hold the splats relative to Result.Origin.
	RootTransform *[16]float64
	// SPZVersion is the version of the SPZ payloads of the tiles.
	SPZVersion spz.Version
	// Workers is how many splats or tiles are processed in parallel.
	Workers int
	// Progress receives the progress of the phases listed in Phases. It can
	// be nil.
	Progress tree.ProgressReporter
}

// DefaultOptions returns the default options: 40,000 splats per tile, every
// splat and spherical-harmonic degree kept, Z up, no root transform, SPZ
// version 2 and one worker per CPU.
func DefaultOptions() Options {
	return Options{
		SplatsPerTile: 40_000,
		MaxSHDegree:   3,
		GECorrection:  1,
		SPZVersion:    spz.DefaultVersion,
		Workers:       runtime.NumCPU(),
	}
}

// Validate reports the first option out of range. Tile validates its options
// too, before writing anything.
func (o Options) Validate() error {
	switch {
	case o.SplatsPerTile < 1:
		return fmt.Errorf("the splats per tile must be at least 1, not %d", o.SplatsPerTile)
	case o.MaxSHDegree < 0 || o.MaxSHDegree > 3:
		return fmt.Errorf("the maximum spherical-harmonic degree must be from 0 to 3, not %d", o.MaxSHDegree)
	case !(o.MinOpacity >= 0 && o.MinOpacity <= 1):
		return fmt.Errorf("the minimum opacity must be from 0 to 1, not %v", o.MinOpacity)
	case !(o.GECorrection > 0) || math.IsInf(o.GECorrection, 0):
		return fmt.Errorf("the geometric error correction must be positive, not %v", o.GECorrection)
	case o.SPZVersion != spz.Version2 && o.SPZVersion != spz.Version3:
		return fmt.Errorf("the SPZ version must be 2 or 3, not %d", o.SPZVersion)
	case o.Workers < 1:
		return fmt.Errorf("the workers must be at least 1, not %d", o.Workers)
	}
	if o.RootTransform != nil {
		for _, v := range o.RootTransform {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return errors.New("the root transform is not finite")
			}
		}
	}
	_, err := upAxisRotation(o.UpAxis)
	return err
}

// Result describes a tileset that Tile wrote.
type Result struct {
	// Read is how many splats the source gave, and Splats how many the
	// tileset holds, after dropping the faint ones.
	Read, Splats int
	// SHDegree is the spherical-harmonic degree of the splats of the tileset.
	SHDegree int
	// Tiles is the number of tiles, and Depth the number of levels below the
	// root.
	Tiles, Depth int
	// Origin is the point, in the input frame turned up, that the tiles hold
	// the splats relative to: the bottom center of the splats' bounds.
	Origin geom.Vec3
	// GeometricError is the geometric error of the root tile.
	GeometricError float64
}

// Tile writes the splats of source as a 3D Tiles tileset to dir: tileset.json
// and the tile contents in its data folder, glTF files of SPZ-compressed
// splats.
//
// Each leaf tile holds the splats of one region of a binary partition of the
// space, and each tile above holds about half the splats of its two children,
// merged from theirs. It works on temporary files in dir, so that the input
// does not need to fit in memory, and removes them before returning, even
// after an error or cancellation.
func Tile(ctx context.Context, source Source, dir string, opts Options) (Result, error) {
	if err := opts.Validate(); err != nil {
		return Result{}, err
	}
	dataDir := filepath.Join(dir, dataDirName)
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return Result{}, err
	}
	tempDir, err := os.MkdirTemp(dir, ".gotiler-splat-")
	if err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(tempDir) //nolint:errcheck

	reading := newPhaseReporter(opts.Progress, Phases[0].Name, int64(source.NumberOfSplats()), readReportEvery)
	reading.milestone("reading the splats")
	in, err := readInput(ctx, source, filepath.Join(tempDir, "input.bin"), inputOptions{
		minOpacity:  opts.MinOpacity,
		maxSHDegree: opts.MaxSHDegree,
		upAxis:      opts.UpAxis,
		perTile:     opts.SplatsPerTile,
	}, reading)
	if err != nil {
		return Result{}, err
	}
	reading.setTotal(int64(in.read))
	reading.milestone(fmt.Sprintf("read %d splats, kept %d", in.read, in.count))

	splitting := newPhaseReporter(opts.Progress, Phases[1].Name, int64(in.count), routeReportEvery)
	splitting.milestone("splitting the splats into tiles")
	plan := buildPlan(in.samples, in.count, opts.SplatsPerTile)
	if err := route(ctx, in, plan, tempDir, opts.Workers, splitting); err != nil {
		return Result{}, err
	}
	if err := os.Remove(in.path); err != nil {
		return Result{}, err
	}
	splitting.milestone("split the splats")

	tiles := countTiles(plan)
	building := newPhaseReporter(opts.Progress, Phases[2].Name, int64(tiles), 1)
	building.milestone(fmt.Sprintf("building %d tiles", tiles))
	b := &builder{
		tempDir:  tempDir,
		dataDir:  dataDir,
		shCount:  in.shCount,
		perTile:  opts.SplatsPerTile,
		geFactor: opts.GECorrection,
		version:  opts.SPZVersion,
		progress: building,
	}
	root, err := b.build(ctx, plan, opts.Workers)
	if err != nil {
		return Result{}, err
	}
	// Leaves hold their splats out to boundsSigmas, so the root bounds hold
	// all the input.
	if err := writeTileset(dir, root, opts.RootTransform); err != nil {
		return Result{}, err
	}
	building.milestone("wrote tileset.json")

	return Result{
		Read:           in.read,
		Splats:         in.count,
		SHDegree:       shDegree(in.shCount),
		Tiles:          int(b.tiles.Load()),
		Depth:          depth(root),
		Origin:         in.origin,
		GeometricError: root.geometricError,
	}, nil
}

// countTiles returns how many tiles the routed plan makes: one per leaf with
// splats, and one per node with splats on both sides.
func countTiles(n *node) int {
	if n.isLeaf() {
		return min(n.count, 1)
	}
	left, right := countTiles(n.children[0]), countTiles(n.children[1])
	if left > 0 && right > 0 {
		return left + right + 1
	}
	return left + right
}

// depth returns the number of levels of tiles below t.
func depth(t *tile) int {
	d := 0
	for _, child := range t.children {
		d = max(d, depth(child)+1)
	}
	return d
}

// shDegree returns the spherical-harmonic degree of shCount coefficients.
func shDegree(shCount int) int {
	return int(math.Round(math.Sqrt(float64(shCount+1)))) - 1
}
