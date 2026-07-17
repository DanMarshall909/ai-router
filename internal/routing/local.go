package routing

import "context"

// LocalManager provides the routing policy with local model state
// without importing the implementation.
type LocalManager interface {
	// State returns the current lifecycle state of the local model.
	State() LocalModelState

	// IsBusy reports whether any local request is in flight.
	IsBusy() bool

	// Start initiates local model startup. Returns an error if startup
	// cannot begin (e.g. breaker is open).
	Start(ctx context.Context) error
}
