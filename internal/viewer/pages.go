package viewer

import (
	"sync"
	"time"
)

// pageTracker counts the viewer pages connected to the server and signals
// once the last one has stayed closed for the grace period, which lets a
// page reload without stopping the server. It waits indefinitely for the
// first page.
type pageTracker struct {
	grace time.Duration

	mu         sync.Mutex
	open       int
	timer      *time.Timer
	done       chan struct{}
	signalOnce sync.Once
}

func newPageTracker(grace time.Duration) *pageTracker {
	return &pageTracker{grace: grace, done: make(chan struct{})}
}

// pageOpened records an open page and reports whether it canceled the
// countdown started by the last page closing.
func (p *pageTracker) pageOpened() (canceledCountdown bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.open++
	if p.timer == nil {
		return false
	}
	p.timer.Stop()
	p.timer = nil
	return true
}

// pageClosed records a closed page and reports whether it was the last one,
// which starts the countdown to stopping.
func (p *pageTracker) pageClosed() (lastPage bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.open--
	if p.open > 0 {
		return false
	}
	p.timer = time.AfterFunc(p.grace, p.signalIfStillClosed)
	return true
}

func (p *pageTracker) signalIfStillClosed() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.open == 0 {
		p.signalOnce.Do(func() { close(p.done) })
	}
}

// allPagesClosed is closed once the last page has stayed closed for the
// grace period.
func (p *pageTracker) allPagesClosed() <-chan struct{} {
	return p.done
}
