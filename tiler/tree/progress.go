package tree

// PhaseInfo describes a processing phase of a Tree implementation.
type PhaseInfo struct {
	Name  string
	Label string
	Unit  string
}

// ProgressUpdate carries progress information for a pipeline operation.
type ProgressUpdate struct {
	Phase       string
	Percent     float64
	Message     string
	IsMilestone bool
	ItemCount   int64
	ItemTotal   int64
}

// ProgressReporter receives progress updates from tree operations.
type ProgressReporter interface {
	Report(ProgressUpdate)
}

// ReportProgress calls r.Report(u) when r is non-nil.
func ReportProgress(r ProgressReporter, u ProgressUpdate) {
	if r != nil {
		r.Report(u)
	}
}
