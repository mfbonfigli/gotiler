// Package tiling turns Gaussian splats into a 3D Tiles tileset, streaming
// them through temporary files so that inputs larger than memory fit.
package tiling

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"

	"github.com/mfbonfigli/gotiler/v3/internal/geom"
	"github.com/mfbonfigli/gotiler/v3/internal/splat"
	"github.com/mfbonfigli/gotiler/v3/internal/splat/lod"
	"github.com/mfbonfigli/gotiler/v3/internal/splat/store"
)

const (
	// maxSamples is how many splats the spatial partition is planned from.
	maxSamples = 262_144

	// samplingSeed seeds the choice of samples, so that tiling is
	// deterministic. It is gosplattiler's, which then plans the same tiles.
	samplingSeed = 1

	// boundsSigmas is how many standard deviations of each splat its bounds
	// hold.
	boundsSigmas = 3

	// checkEvery is how many splats are read between checks for cancellation.
	checkEvery = 4096
)

// Source yields the splats to tile, as ply.SplatReader does.
type Source interface {
	// SHDegree returns the degree of the spherical harmonics of the splats.
	SHDegree() int
	// Next returns the next splat, or io.EOF after the last one.
	Next() (splat.Splat, error)
}

// inputOptions select and turn the splats of a source.
type inputOptions struct {
	// minOpacity drops the splats of lower opacity.
	minOpacity float64
	// maxSHDegree drops the spherical harmonics of higher degrees. A negative
	// value keeps all of them.
	maxSHDegree int
	// upAxis names the input axis that points up, as upAxisRotation takes it.
	upAxis string
}

// input describes the splats a source left in a store.
type input struct {
	// path is the store, holding the splats in the input frame turned up.
	path string
	// count is how many splats the store holds, and read how many the source
	// gave.
	count, read int
	// shCount is the number of higher-degree spherical-harmonic coefficients
	// of each splat.
	shCount int
	// origin is the bottom center of the splats' bounds, in the frame of the
	// store: the tiles hold the splats relative to it.
	origin geom.Vec3
	// bounds holds the splats out to boundsSigmas, relative to origin.
	bounds geom.Bounds
	// samples are up to maxSamples splat centers, relative to origin, picked
	// at random with equal chances.
	samples []sample
}

// sample is a splat center the partition is planned from, with the weight of
// the splat.
type sample struct {
	center geom.Vec3
	weight float64
}

// readInput reads the splats of source into a new store at path, dropping
// those below the minimum opacity and the spherical harmonics above the
// maximum degree, and turning them so that their up axis is +Z.
func readInput(ctx context.Context, source Source, path string, opts inputOptions) (*input, error) {
	up, err := upAxisRotation(opts.upAxis)
	if err != nil {
		return nil, err
	}
	var rotation *splat.Rotation
	if up != geom.IdentityMat3() {
		r := splat.NewRotation(up)
		rotation = &r
	}
	degree := source.SHDegree()
	if opts.maxSHDegree >= 0 {
		degree = min(degree, opts.maxSHDegree)
	}
	in := &input{path: path, shCount: (degree+1)*(degree+1) - 1}

	writer, err := store.Create(path, in.shCount)
	if err != nil {
		return nil, err
	}
	defer writer.Close() //nolint:errcheck
	// Algorithm R: the k-th splat replaces a random sample with chance
	// maxSamples/k, which leaves every splat the same chance to be sampled.
	rng := rand.New(rand.NewSource(samplingSeed))
	var bounds geom.Bounds
	batch := make([]splat.Splat, 0, store.BatchSize)
	for {
		if in.read%checkEvery == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		s, err := source.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		in.read++
		if s.Opacity < opts.minOpacity {
			continue
		}
		if len(s.SHRest) > in.shCount {
			s.SHRest = s.SHRest[:in.shCount:in.shCount]
		}
		if in.shCount == 0 {
			s.SHRest = nil
		}
		if rotation != nil {
			s = rotation.Rotate(s)
		}

		bounds.Include(s.Bounds(boundsSigmas))
		in.count++
		if len(in.samples) < maxSamples {
			in.samples = append(in.samples, sample{center: s.Center, weight: lod.Weight(s)})
		} else if j := rng.Int63n(int64(in.count)); j < maxSamples {
			in.samples[j] = sample{center: s.Center, weight: lod.Weight(s)}
		}
		if batch = append(batch, s); len(batch) == cap(batch) {
			if err := writer.Write(batch); err != nil {
				return nil, err
			}
			batch = batch[:0]
		}
	}
	if err := writer.Write(batch); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	switch {
	case in.read == 0:
		return nil, errors.New("the input holds no valid splats")
	case in.count == 0:
		return nil, fmt.Errorf("none of the %d splats has an opacity of at least %v", in.read, opts.minOpacity)
	}

	in.origin = geom.Vec3{X: bounds.Center().X, Y: bounds.Center().Y, Z: bounds.Min.Z}
	in.bounds = bounds.Translate(in.origin.MulScalar(-1))
	for i := range in.samples {
		in.samples[i].center = in.samples[i].center.Sub(in.origin)
	}
	return in, nil
}
