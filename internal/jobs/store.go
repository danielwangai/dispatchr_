package jobs

import (
	"context"
	"errors"
)

var (
	// ErrNotFound indicates that a job does not exist.
	ErrNotFound = errors.New("job not found")
	// ErrNoJobAvailable indicates that there are no pending jobs to claim.
	ErrNoJobAvailable = errors.New("no job available")
)

// Store contains method for coordinating the lifecycle jobs.
//
// Storage adapters such as PostgreSQL and MySQL implement this contract.
type Store interface {
	Create(ctx context.Context, job Job) error
	Get(ctx context.Context, id string) (Job, error)
	ClaimNext(ctx context.Context) (Job, error)
	Complete(ctx context.Context, id string) error
	Fail(ctx context.Context, id, reason string) error
}
