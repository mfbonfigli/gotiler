package tiling

import (
	"sync/atomic"

	"github.com/mfbonfigli/gotiler/v3/tiler/tree"
)

// The phases of a tiling, in order, as Tile reports them.
var Phases = []tree.PhaseInfo{
	{Name: "reading", Label: "Reading", Unit: "splats"},
	{Name: "splitting", Label: "Splitting", Unit: "splats"},
	{Name: "building", Label: "Building", Unit: "tiles"},
}

// phaseReporter reports the progress of one phase, throttling its detail
// updates to one every so many items. A nil phaseReporter reports nothing.
// It is safe for concurrent use.
type phaseReporter struct {
	out   tree.ProgressReporter
	phase string
	every int64
	total atomic.Int64
	count atomic.Int64
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
		r.report(message, true, r.count.Load())
	}
}

// add counts n more items done, reporting when the count passes a multiple
// of every.
func (r *phaseReporter) add(n int) {
	if r == nil || n == 0 {
		return
	}
	count := r.count.Add(int64(n))
	if (count-int64(n))/r.every != count/r.every {
		r.report("", false, count)
	}
}

// setTotal changes the number of items the phase expects, once it is known.
func (r *phaseReporter) setTotal(total int64) {
	if r != nil {
		r.total.Store(total)
	}
}

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
