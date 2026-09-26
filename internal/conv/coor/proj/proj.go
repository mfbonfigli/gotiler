package proj

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mfbonfigli/gotiler/v3/tiler/model"
	"github.com/twpayne/go-proj/v10"
)

const epsg4978crs = "EPSG:4978"

type projCoordinateConverter struct {
	projections map[string]*proj.PJ
	searchPath  string
	context     *proj.Context
}

// Returns a new *projCoordinateConverter to perform coordinate conversion. This object can only be
// used by a single goroutine at a time.
func NewProjCoordinateConverter() (*projCoordinateConverter, error) {
	// Initialization of EPSG Proj4 database
	conv := &projCoordinateConverter{
		projections: make(map[string]*proj.PJ),
	}

	// create a single proj Context whose lifetime is tied to this converter
	ctx := proj.NewContext()

	// set the search path to the share folder in the same folder as the executable path
	execPath, err := os.Executable()
	if err != nil {
		return nil, err
	}
	conv.searchPath = projSearchPath(execPath)
	if _, err := os.Stat(conv.searchPath); err == nil {
		ctx.SetSearchPaths([]string{conv.searchPath})
	}
	conv.context = ctx
	return conv, nil
}

// projSearchPath returns the share folder next to the executable at execPath,
// resolving symbolic links first: package managers usually expose the
// executable through a link placed elsewhere (Homebrew, winget, a link in
// ~/.local/bin), while the PROJ data stays next to the real file.
func projSearchPath(execPath string) string {
	if resolved, err := filepath.EvalSymlinks(execPath); err == nil {
		execPath = resolved
	}
	return filepath.Join(filepath.Dir(execPath), "share")
}

// Converts the given coordinate from the given source crs to the given target crs.
func (cc *projCoordinateConverter) Transform(sourceCRS string, targetCRS string, coord model.Vector) (model.Vector, error) {
	if sourceCRS == targetCRS {
		return coord, nil
	}
	pj, err := cc.getProjection(sourceCRS, targetCRS)
	if err != nil {
		return model.Vector{}, err
	}
	c := proj.NewCoord(coord.X, coord.Y, coord.Z, 0)
	out, err := pj.Forward(c)
	if err != nil {
		return coord, fmt.Errorf("error while transforming coordinates: %w", err)
	}
	return model.Vector{X: out.X(), Y: out.Y(), Z: out.Z()}, nil
}

// TransformFlat transforms a flat slice of XYZ coordinates in-place between arbitrary CRSs.
// flatCoords layout: [X0, Y0, Z0, X1, Y1, Z1, ...], stride=3.
func (cc *projCoordinateConverter) TransformFlat(sourceCRS string, targetCRS string, flatCoords []float64) error {
	if sourceCRS == targetCRS || len(flatCoords) == 0 {
		return nil
	}
	if len(flatCoords)%3 != 0 {
		return fmt.Errorf("flat coordinate slice length %d is not a multiple of 3", len(flatCoords))
	}
	pj, err := cc.getProjection(sourceCRS, targetCRS)
	if err != nil {
		return err
	}
	return pj.ForwardFlatCoords(flatCoords, 3, 2, -1)
}

// Converts the input coordinate from the given CRS to EPSG:4978 srid
func (cc *projCoordinateConverter) ToWGS84Cartesian(sourceCRS string, coord model.Vector) (model.Vector, error) {
	if sourceCRS == epsg4978crs {
		return coord, nil
	}

	return cc.Transform(sourceCRS, epsg4978crs, coord)
}

// ToWGS84CartesianFlat transforms a flat slice of XYZ coordinates to EPSG:4978 in-place.
// flatCoords layout: [X₀, Y₀, Z₀, X₁, Y₁, Z₁, ...], stride=3.
func (cc *projCoordinateConverter) ToWGS84CartesianFlat(sourceCRS string, flatCoords []float64) error {
	return cc.TransformFlat(sourceCRS, epsg4978crs, flatCoords)
}

// Releases all projection objects from memory
func (cc *projCoordinateConverter) Cleanup() {
	for _, pj := range cc.projections {
		if pj != nil {
			pj.Destroy()
		}
	}
	// reset the projection cache
	cc.projections = make(map[string]*proj.PJ)

	// destroy the proj context to free C-allocated resources
	if cc.context != nil {
		cc.context.Destroy()
		cc.context = nil
	}
}

// Returns the projection object corresponding to the given crs representations, caching it internally to be reused
// This object is not designed for concurrent usage by multiple goroutines
func (cc *projCoordinateConverter) getProjection(source string, target string) (*proj.PJ, error) {
	uniqueProjectionCode := source + "#" + target
	if val, ok := cc.projections[uniqueProjectionCode]; ok {
		return val, nil
	}
	sourcePj, err := cc.context.New(source)
	if err != nil {
		return nil, err
	}
	defer sourcePj.Destroy()
	targetPJ, err := cc.context.New(target)
	if err != nil {
		return nil, err
	}
	defer targetPJ.Destroy()
	unnormalized, err := cc.context.NewCRSToCRSFromPJ(sourcePj, targetPJ, nil, "")
	if err != nil {
		return nil, fmt.Errorf("unable to initialize projection between %s and %s: %w", source, target, err)
	}
	// Explicitly destroy the unnormalized PJ at function return. Without this,
	// the object is only freed by the Go GC finalizer, which may run after
	// Cleanup() has already called context.Destroy(). proj_destroy internally
	// accesses the context, so calling it on a freed context causes a crash.
	defer unnormalized.Destroy()
	pj, err := unnormalized.NormalizeForVisualization()
	if err != nil {
		return nil, fmt.Errorf("unable to normalize the projection between %s and %s: %w", source, target, err)
	}

	cc.projections[uniqueProjectionCode] = pj

	return pj, nil
}
