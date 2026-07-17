package routing

import "time"

// Clock abstracts time for deterministic testing.
type Clock interface {
	// Now returns the current time.
	Now() time.Time

	// After returns a channel that sends the current time after d.
	After(d time.Duration) <-chan time.Time

	// NewTicker returns a new Ticker that ticks at interval d.
	NewTicker(d time.Duration) *time.Ticker
}
