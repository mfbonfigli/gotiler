package tiler

import (
	"time"

	"github.com/mfbonfigli/gotiler/v3/tiler/tree"
)

// ProgressLevel indicates how important a ProgressEvent is.
type ProgressLevel int

const (
	// ProgressMilestone marks phase-start, phase-end, and error events.
	// These are always emitted regardless of any throttling.
	ProgressMilestone ProgressLevel = iota

	// ProgressDetail marks intermediate updates (e.g. "read 500k/2M points").
	// The CLI may render these as progress-bar ticks and ignore them for plain
	// log output; library callers can filter by checking Level.
	ProgressDetail
)

// Phase describes one step of the tiling pipeline.
type Phase struct {
	// Index is 1-based.
	Index int
	// Total is the total number of phases in the pipeline.
	Total int
	// Name is a short human-readable label, e.g. "Reading".
	Name string
	// Unit is the throughput label for the CLI rate display, e.g. "pts", "nodes", "tiles".
	// An empty string means no rate display for this phase.
	Unit string
}

// ProgressEvent is the value delivered to a ProgressCallback.
type ProgressEvent struct {
	Phase Phase

	// Percent is completion within the current phase, 0–100.
	// -1 means the total is unknown (indeterminate).
	Percent float64

	// Message is a human-readable description of the current state.
	Message string

	// Level indicates whether this is a milestone or a detail update.
	Level ProgressLevel

	// ElapsedMs is the number of milliseconds since the job started.
	ElapsedMs int64

	// InputDesc identifies the input file(s) being processed.
	InputDesc string

	// ItemCount is the cumulative number of items processed so far in this phase.
	ItemCount int64

	// ItemTotal is the expected total item count for this phase.
	// 0 means not applicable; -1 means unknown total.
	ItemTotal int64
}

// ProgressCallback is called whenever a ProgressEvent is emitted.
// The callback must not block; heavy work should be dispatched asynchronously.
type ProgressCallback func(event ProgressEvent)

// phaseMappedReporter implements tree.ProgressReporter.
// It maps the phase name strings emitted by internal components to the
// user-facing Phase structs defined in the tiler package, then forwards
// the translated event to a ProgressCallback.
type phaseMappedReporter struct {
	cb        ProgressCallback
	inputDesc string
	start     time.Time
	phases    map[string]Phase
}

// newPhaseMappedReporter returns a tree.ProgressReporter that translates
// internal ProgressUpdate phase strings into user-facing ProgressEvent values.
// Returns nil when cb is nil, so callers always get a nil-safe reporter.
func newPhaseMappedReporter(cb ProgressCallback, inputDesc string, start time.Time, phases map[string]Phase) tree.ProgressReporter {
	if cb == nil {
		return nil
	}
	return &phaseMappedReporter{
		cb:        cb,
		inputDesc: inputDesc,
		start:     start,
		phases:    phases,
	}
}

func (r *phaseMappedReporter) Report(u tree.ProgressUpdate) {
	phase, ok := r.phases[u.Phase]
	if !ok {
		return // unknown phase name from an internal component – silently skip
	}
	level := ProgressDetail
	if u.IsMilestone {
		level = ProgressMilestone
	}
	r.cb(ProgressEvent{
		Phase:     phase,
		Percent:   u.Percent,
		Message:   u.Message,
		Level:     level,
		ElapsedMs: time.Since(r.start).Milliseconds(),
		InputDesc: r.inputDesc,
		ItemCount: u.ItemCount,
		ItemTotal: u.ItemTotal,
	})
}
