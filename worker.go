package dispatchr

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var (
	// ErrInvalidPollInterval indicates that a worker would otherwise busy-loop.
	ErrInvalidPollInterval = errors.New("worker poll interval must be greater than zero")
	// ErrStoreRequired indicates that a worker has no job persistence dependency.
	ErrStoreRequired = errors.New("job store is required")
	// ErrRegistryRequired indicates that a worker has no handlers to resolve jobs.
	ErrRegistryRequired = errors.New("handler registry is required")
)

// Worker claims jobs from a Store and processes them with registered handlers.
type Worker struct {
	store       Store
	registry    *Registry
	reportError func(error)
	wait        func(context.Context, time.Duration) bool
}

// WorkerOption configures a Worker.
type WorkerOption func(*Worker)

// WithErrorReporter receives unexpected store and transition errors before the
// worker retries. Applications can forward them to any logging provider.
func WithErrorReporter(reporter func(error)) WorkerOption {
	return func(worker *Worker) {
		if reporter != nil {
			worker.reportError = reporter
		}
	}
}

// NewWorker creates a worker with a Store and handler Registry.
func NewWorker(store Store, registry *Registry, options ...WorkerOption) (*Worker, error) {
	if store == nil {
		return nil, ErrStoreRequired
	}
	if registry == nil {
		return nil, ErrRegistryRequired
	}

	worker := &Worker{
		store:       store,
		registry:    registry,
		reportError: func(error) {},
		wait:        waitForNextPoll,
	}
	for _, option := range options {
		option(worker)
	}
	return worker, nil
}

// RunOnce claims and processes one job. Handler failures are persisted as failed
// jobs; only claim and state-transition failures are returned to the caller.
func (w *Worker) RunOnce(ctx context.Context) error {
	job, err := w.store.ClaimNext(ctx)
	if err != nil {
		return fmt.Errorf("claim next job: %w", err)
	}

	handler, err := w.registry.HandlerFor(job.Type)
	if err != nil {
		return w.recordFailure(ctx, job, fmt.Errorf("find handler for job type %q: %w", job.Type, err))
	}

	if err := handler.Handle(ctx, job); err != nil {
		return w.recordFailure(ctx, job, fmt.Errorf("run handler for job type %q: %w", job.Type, err))
	}

	if err := w.store.Complete(ctx, job.ID); err != nil {
		return fmt.Errorf("complete job %s: %w", job.ID, err)
	}
	return nil
}

// Run keeps processing jobs until ctx is cancelled. It waits after empty queues
// and unexpected errors so it does not busy-loop or exit during a temporary
// store outage.
func (w *Worker) Run(ctx context.Context, pollInterval time.Duration) error {
	if pollInterval <= 0 {
		return ErrInvalidPollInterval
	}

	for {
		if ctx.Err() != nil {
			return nil
		}

		err := w.RunOnce(ctx)
		if err == nil {
			continue
		}
		if ctx.Err() != nil {
			return nil
		}

		if !errors.Is(err, ErrNoJobAvailable) {
			w.reportError(err)
		}
		if w.wait(ctx, pollInterval) {
			return nil
		}
	}
}

func (w *Worker) recordFailure(ctx context.Context, job Job, cause error) error {
	if err := w.store.Fail(ctx, job.ID, cause.Error()); err != nil {
		return fmt.Errorf("fail job %s after execution error: %w", job.ID, err)
	}
	return nil
}

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
