package contextmgr

import "sync"

type AutoTracker struct {
	failures int
	tripped  bool
	lastErr  string
	mu       sync.Mutex
}

func NewAutoTracker() *AutoTracker {
	return &AutoTracker{}
}

func (a *AutoTracker) Tripped() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.tripped
}

func (a *AutoTracker) RecordSuccess() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.failures = 0
	a.tripped = false
	a.lastErr = ""
}

func (a *AutoTracker) RecordFailure(err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.failures++
	if err != nil {
		a.lastErr = err.Error()
	}
	if a.failures >= AutoSummaryFailureLimit {
		a.tripped = true
	}
}

func (a *AutoTracker) Snapshot() AutoStatus {
	a.mu.Lock()
	defer a.mu.Unlock()
	return AutoStatus{Failures: a.failures, Tripped: a.tripped, LastError: a.lastErr}
}
