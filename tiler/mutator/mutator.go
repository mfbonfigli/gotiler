package mutator

import (
	"github.com/mfbonfigli/gotiler/v3/tiler/model"
)

// Mutator defines a generic interface to manipulate coordinates or attributes
// of point batches.
type Mutator interface {
	// RequiredAttributes returns optional per-point attributes this mutator
	// needs the reader to provide as input. Names are canonicalized by the
	// tiler before they are passed to readers.
	RequiredAttributes() model.Attributes

	// MutateChunk transforms or discards the points in input.
	//
	// Points are expressed in the local CRS with Z-up. AttributeLayout describes
	// the packed attributes attached to each point. localToGlobal can be used to
	// forward transform from the local CRS to global EPSG:4978 and inverse
	// transform from global CRS back to the local CRS.
	//
	// Implementations may mutate chunk.Points in place and should return the
	// kept points, usually as a subslice of chunk.Points.
	//
	// MutateChunk may be invoked concurrently from multiple goroutines on the
	// same instance: implementations must be safe for concurrent use, and any
	// internal state requires synchronization. Chunks never overlap, so mutating
	// chunk.Points in place is safe without locking.
	MutateChunk(chunk PointChunk, localToGlobal model.Transform) []model.Point

	// Close releases resources held by the mutator. It is called once by the
	// tiler after all mutation work completes.
	Close() error
}

// WriteMutator is optionally implemented by Mutators that also (or only)
// transform points while tiles are written, after the whole point cloud has
// been loaded.
//
// MutateChunkOnWrite differs from MutateChunk in three ways:
//
//   - It must return exactly the points it received: mutating chunk.Points in
//     place is allowed, but adding or dropping points is an error, because tile
//     bounding volumes, point counts and geometric errors are already fixed by
//     the time tiles are written.
//   - It must be a pure function of the point data: the same point can be
//     processed more than once, both because coarser tiles may contain copies
//     of points that also live in finer tiles and because a tile's points may
//     be re-read. Per-run state accumulated during MutateChunk must be frozen
//     before the first MutateChunkOnWrite call uses it.
//   - Within one tiling run, all MutateChunk calls complete before the first
//     MutateChunkOnWrite call, and runs never overlap. When the same mutator
//     instance is reused for several runs (e.g. a folder of files), a
//     MutateChunk call arriving after MutateChunkOnWrite calls signals that a
//     new run has started, so per-run state must be reset.
//
// Like MutateChunk, MutateChunkOnWrite may be invoked concurrently from
// multiple goroutines on the same instance.
type WriteMutator interface {
	Mutator

	// MutateChunkOnWrite transforms the points of one output tile chunk. The
	// points, layout and transform follow the same conventions as MutateChunk.
	MutateChunkOnWrite(chunk PointChunk, localToGlobal model.Transform) []model.Point
}

// PointChunk is a mutable batch of local-coordinate points passed to
// chunk-aware mutators. AttributeLayout describes the packed Attributes blob
// attached to each point.
type PointChunk struct {
	Points          []model.Point
	AttributeLayout []model.AttributeLayoutEntry
}

// AttributeView returns a typed view over the i-th point's packed attributes.
func (c PointChunk) AttributeView(i int) model.AttributeView {
	return model.NewAttributeView(c.AttributeLayout, c.Points[i].Attributes)
}

// MutateChunk applies m to the whole chunk.
func MutateChunk(m Mutator, chunk PointChunk, localToGlobal model.Transform) []model.Point {
	if m == nil || len(chunk.Points) == 0 {
		return chunk.Points
	}
	return m.MutateChunk(chunk, localToGlobal)
}
