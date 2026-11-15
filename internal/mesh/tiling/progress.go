package tiling

import (
	"sync"
	"sync/atomic"

	"github.com/mfbonfigli/gotiler/v3/tiler/tree"
)

// The phases of a tiling, in order, as Tile reports them.
var Phases = []tree.PhaseInfo{
	{Name: "reading", Label: "Reading", Unit: "bytes"},
	{Name: "textures", Label: "Reading textures", Unit: "images"},
	{Name: "splitting", Label: "Splitting", Unit: "triangles"},
	{Name: "building", Label: "Building", Unit: "tiles"},
}

// phaseReporter reports the progress of one phase, throttling its detail
// updates to one every so many items. A nil phaseReporter reports nothing.
// It is safe for concurrent use, and reports its updates in the order of
// their counts.
type phaseReporter struct {
	out   tree.ProgressReporter
	phase string
	every int64
	total atomic.Int64
	// mu orders the updates: it is held from counting the items to reporting
	// them, so that an update never follows one of a higher count.
	mu    sync.Mutex
	count int64
}

func newPhaseReporter(out tree.ProgressReporter, phase string, total, every int64) *phaseReporter {
	if out == nil {
		return nil
	}
	r := &phaseReporter{out: out, phase: phase, every: every}
	r.total.Store(total)
	return r
}

// milestone reports a start, an end or another landmark of the phase.
func (r *phaseReporter) milestone(message string) {
	if r != nil {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.report(message, true, r.count)
	}
}

// add counts n more items done, reporting when the count passes a multiple
// of every.
func (r *phaseReporter) add(n int) {
	if r == nil || n == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.count += int64(n)
	if (r.count-int64(n))/r.every != r.count/r.every {
		r.report("", false, r.count)
	}
}

// setTotal changes the number of items the phase expects, once it is known.
func (r *phaseReporter) setTotal(total int64) {
	if r != nil {
		r.total.Store(total)
	}
}

// report sends an update. The caller holds mu.
func (r *phaseReporter) report(message string, milestone bool, count int64) {
	total := r.total.Load()
	percent := -1.0
	if total > 0 {
		percent = min(100, 100*float64(count)/float64(total))
	}
	r.out.Report(tree.ProgressUpdate{
		Phase:       r.phase,
		Percent:     percent,
		Message:     message,
		IsMilestone: milestone,
		ItemCount:   count,
		ItemTotal:   total,
	})
}
