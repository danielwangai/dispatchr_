// Package worker coordinates claimed jobs with registered application handlers.
package worker

import (
	"context"
	"fmt"

	"github.com/danielwangai/dispatchr"
	"github.com/danielwangai/dispatchr/internal/jobs"
)

// Worker processes jobs from one store using handlers supplied by an application.
type Worker struct {
	store    jobs.Store
	registry *dispatchr.Registry
}

// New creates a worker that claims jobs from store and resolves them in registry.
func New(store jobs.Store, registry *dispatchr.Registry) *Worker {
	return &Worker{store: store, registry: registry}
}

// RunOnce claims and processes one job. It returns an error only when the worker
// cannot claim or persist a state transition. Handler failures are persisted as
// failed jobs, allowing the next polling cycle to continue normally.
func (w *Worker) RunOnce(ctx context.Context) error {
	// Step 1: atomically claim one eligible pending job.
	job, err := w.store.ClaimNext(ctx)
	if err != nil {
		return fmt.Errorf("claim next job: %w", err)
	}

	// Step 2: resolve the application handler for the claimed job type.
	handler, err := w.registry.HandlerFor(job.Type)
	if err != nil {
		return w.recordFailure(ctx, job, fmt.Errorf("find handler for job type %q: %w", job.Type, err))
	}

	// Step 3: execute the handler with the job's persisted data.
	if err := handler.Handle(ctx, job); err != nil {
		return w.recordFailure(ctx, job, fmt.Errorf("run handler for job type %q: %w", job.Type, err))
	}

	// Step 4: record successful work only after the handler completes.
	if err := w.store.Complete(ctx, job.ID); err != nil {
		return fmt.Errorf("complete job %s: %w", job.ID, err)
	}

	return nil
}

// recordFailure records an execution failure and keeps it out of the worker's
// control flow when persistence succeeds.
func (w *Worker) recordFailure(ctx context.Context, job jobs.Job, cause error) error {
	// Step 5: persist the reason so clients can inspect the failed job.
	if err := w.store.Fail(ctx, job.ID, cause.Error()); err != nil {
		return fmt.Errorf("fail job %s after execution error: %w", job.ID, err)
	}

	return nil
}
