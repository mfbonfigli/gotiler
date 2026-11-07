package viewer

import (
	"testing"
	"time"
)

const testGrace = 30 * time.Millisecond

func signaled(p *pageTracker, within time.Duration) bool {
	select {
	case <-p.allPagesClosed():
		return true
	case <-time.After(within):
		return false
	}
}

func TestPageTrackerWaitsForTheFirstPage(t *testing.T) {
	p := newPageTracker(testGrace)
	if signaled(p, 5*testGrace) {
		t.Error("signaled before any page was opened")
	}
}

func TestPageTrackerSignalsAfterTheLastPageCloses(t *testing.T) {
	p := newPageTracker(testGrace)
	p.pageOpened()
	p.pageOpened()
	p.pageClosed()
	if signaled(p, 5*testGrace) {
		t.Fatal("signaled while a page was still open")
	}
	p.pageClosed()
	if !signaled(p, 20*testGrace) {
		t.Error("did not signal after the last page closed")
	}
}

func TestPageTrackerToleratesAReloadWithinTheGracePeriod(t *testing.T) {
	p := newPageTracker(testGrace)
	p.pageOpened()
	p.pageClosed()
	p.pageOpened()
	if signaled(p, 5*testGrace) {
		t.Fatal("signaled although the page reopened within the grace period")
	}
	p.pageClosed()
	if !signaled(p, 20*testGrace) {
		t.Error("did not signal after the reopened page closed")
	}
}
