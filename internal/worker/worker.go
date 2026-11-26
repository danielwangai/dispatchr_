// Package worker coordinates claimed jobs with registered application handlers.
package worker

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/danielwangai/dispatchr"
	"github.com/danielwangai/dispatchr/internal/jobs"
	"github.com/danielwangai/dispatchr/internal/logging"
)

// ErrInvalidPollInterval indicates that a worker would otherwise busy-loop.
var ErrInvalidPollInterval = errors.New("worker poll interval must be greater than zero")

// Worker processes jobs from one store using handlers supplied by an application.
type Worker struct {
	store    jobs.Store
	registry *dispatchr.Registry
	logger   logging.Logger
	wait     func(context.Context, time.Duration) bool
}

// New creates a worker that claims jobs from store and resolves them in registry.
func New(store jobs.Store, registry *dispatchr.Registry, logger logging.Logger) *Worker {
	if logger == nil {
		logger = logging.Nop()
	}

	return &Worker{store: store, registry: registry, logger: logger, wait: waitForNextPoll}
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

// Run keeps processing jobs until ctx is cancelled. Empty queues and transient
// worker errors wait for pollInterval so the worker neither busy-loops nor exits
// during a temporary database outage.
func (w *Worker) Run(ctx context.Context, pollInterval time.Duration) error {
	if pollInterval <= 0 {
		return ErrInvalidPollInterval
	}

	for {
		// Step 1: stop cleanly before starting another claim when shutdown begins.
		if ctx.Err() != nil {
			return nil
		}

		// Step 2: process one job, then immediately try another after success.
		err := w.RunOnce(ctx)
		if err == nil {
			continue
		}
		if ctx.Err() != nil {
			return nil
		}

		// Step 3: an empty queue is normal; wait before polling PostgreSQL again.
		if errors.Is(err, jobs.ErrNoJobAvailable) {
			if w.wait(ctx, pollInterval) {
				return nil
			}
			continue
		}

		// Step 4: log unexpected store errors and retry after the polling interval.
		w.logger.Error("worker cycle failed; retrying", logging.Err(err), logging.Duration("retry_in", pollInterval))
		if w.wait(ctx, pollInterval) {
			return nil
		}
	}
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

// waitForNextPoll waits for the next poll or reports that shutdown was requested.
func waitForNextPoll(ctx context.Context, interval time.Duration) bool {
	timer := time.NewTimer(interval)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return true
	case <-timer.C:
		return false
	}
}
