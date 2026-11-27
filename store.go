package dispatchr

import (
	"context"
	"errors"
)

var (
	// ErrNotFound indicates that a job does not exist.
	ErrNotFound = errors.New("job not found")
	// ErrNoJobAvailable indicates that there are no pending jobs to claim.
	ErrNoJobAvailable = errors.New("no job available")
	// ErrInvalidState indicates that a job cannot make the requested transition.
	ErrInvalidState = errors.New("invalid job state")
)

// Store persists jobs and coordinates their lifecycle transitions.
// Database adapters such as PostgreSQL implement this interface.
type Store interface {
	Create(ctx context.Context, job Job) error
	Get(ctx context.Context, id string) (Job, error)
	ClaimNext(ctx context.Context) (Job, error)
	Complete(ctx context.Context, id string) error
	Fail(ctx context.Context, id, reason string) error
}
