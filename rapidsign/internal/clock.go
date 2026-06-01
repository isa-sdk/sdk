// Package internal holds non-exported facades for the rapidsign client.
//
// Per the project-wide facade rule, system boundaries (clock, random,
// gzip, environment) live behind injectable interfaces. Tests substitute
// deterministic implementations; production code wires the real ones at
// the package boundary.
package internal

import "time"

// Clock returns the current instant. Production code uses time.Now.
// Tests pin it to a frozen value so polling backoff and Retry-After
// math are reproducible.
type Clock func() time.Time

// RealClock returns time.Now and is the default for client construction.
func RealClock() Clock { return time.Now }

// Sleeper waits for d or returns the context error, whichever comes
// first. The interface lets tests assert "would have slept" durations
// without consuming wall-clock time.
type Sleeper interface {
	Sleep(stop <-chan struct{}, d time.Duration) bool
}

// RealSleeper is the default Sleeper backed by time.NewTimer.
type RealSleeper struct{}

// Sleep waits d or until stop closes. Returns true when the full
// duration elapsed, false when stop interrupted.
func (RealSleeper) Sleep(stop <-chan struct{}, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-stop:
		return false
	}
}
