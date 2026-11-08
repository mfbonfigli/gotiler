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

// pageOpened records an open page, canceling the countdown started by the
// last page closing.
func (p *pageTracker) pageOpened() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.open++
	if p.timer != nil {
		p.timer.Stop()
		p.timer = nil
	}
}

// pageClosed records a closed page and, if it was the last one, starts the
// countdown to signaling.
func (p *pageTracker) pageClosed() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.open--
	if p.open == 0 {
		p.timer = time.AfterFunc(p.grace, p.signalIfStillClosed)
	}
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
