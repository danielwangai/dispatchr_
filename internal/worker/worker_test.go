package worker

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/danielwangai/dispatchr"
	"github.com/danielwangai/dispatchr/internal/jobs"
	"github.com/danielwangai/dispatchr/internal/logging"
)

func TestWorkerRunOnceCompletesHandledJob(t *testing.T) {
	// Step 1: arrange one job that can be claimed and a successful application handler.
	job := jobs.Job{ID: "job-123", Type: "send_email", Status: jobs.StatusProcessing}
	store := &fakeStore{claimedJob: job}
	registry := dispatchr.NewRegistry()
	called := false
	if err := registry.Register("send_email", dispatchr.HandlerFunc(func(_ context.Context, got dispatchr.Job) error {
		called = got.ID == job.ID
		return nil
	})); err != nil {
		t.Fatalf("register handler: %v", err)
	}

	// Step 2: run one worker cycle.
	err := New(store, registry, nil).RunOnce(context.Background())

	// Step 3: assert the handler ran and the worker completed the claimed job.
	if err != nil {
		t.Fatalf("run once: %v", err)
	}
	if !called || store.completedID != job.ID || store.failedID != "" {
		t.Fatalf("called=%t completed=%q failed=%q, want handled and completed job", called, store.completedID, store.failedID)
	}
}

func TestWorkerRunOnceRecordsHandlerFailure(t *testing.T) {
	// Step 1: arrange a claimed job whose handler returns an application error.
	job := jobs.Job{ID: "job-123", Type: "send_email", Status: jobs.StatusProcessing}
	store := &fakeStore{claimedJob: job}
	registry := dispatchr.NewRegistry()
	if err := registry.Register("send_email", dispatchr.HandlerFunc(func(context.Context, dispatchr.Job) error {
		return errors.New("email provider unavailable")
	})); err != nil {
		t.Fatalf("register handler: %v", err)
	}

	// Step 2: run one worker cycle.
	err := New(store, registry, nil).RunOnce(context.Background())

	// Step 3: assert the failure was persisted and did not stop the worker cycle.
	if err != nil {
		t.Fatalf("run once: %v", err)
	}
	if store.completedID != "" || store.failedID != job.ID || !strings.Contains(store.failureReason, "email provider unavailable") {
		t.Fatalf("completed=%q failed=%q reason=%q, want persisted handler failure", store.completedID, store.failedID, store.failureReason)
	}
}

func TestWorkerRunOnceRecordsMissingHandler(t *testing.T) {
	// Step 1: arrange a claimed job whose type has no registered application handler.
	job := jobs.Job{ID: "job-123", Type: "generate_report", Status: jobs.StatusProcessing}
	store := &fakeStore{claimedJob: job}

	// Step 2: run one worker cycle with an empty registry.
	err := New(store, dispatchr.NewRegistry(), nil).RunOnce(context.Background())

	// Step 3: assert the job is failed with an actionable configuration error.
	if err != nil {
		t.Fatalf("run once: %v", err)
	}
	if store.failedID != job.ID || !strings.Contains(store.failureReason, "job handler not found") {
		t.Fatalf("failed=%q reason=%q, want missing handler failure", store.failedID, store.failureReason)
	}
}

func TestWorkerRunOnceReturnsClaimError(t *testing.T) {
	// Step 1: arrange a store with no pending jobs to claim.
	store := &fakeStore{claimErr: jobs.ErrNoJobAvailable}

	// Step 2: run one worker cycle.
	err := New(store, dispatchr.NewRegistry(), nil).RunOnce(context.Background())

	// Step 3: assert the polling loop can identify the empty-queue condition.
	if !errors.Is(err, jobs.ErrNoJobAvailable) {
		t.Fatalf("run once error = %v, want ErrNoJobAvailable", err)
	}
}

func TestWorkerRunOnceReturnsTransitionError(t *testing.T) {
	// Step 1: arrange a successfully handled job whose completion cannot be persisted.
	job := jobs.Job{ID: "job-123", Type: "send_email", Status: jobs.StatusProcessing}
	store := &fakeStore{claimedJob: job, completeErr: errors.New("database unavailable")}
	registry := dispatchr.NewRegistry()
	if err := registry.Register("send_email", dispatchr.HandlerFunc(func(context.Context, dispatchr.Job) error { return nil })); err != nil {
		t.Fatalf("register handler: %v", err)
	}

	// Step 2: run one worker cycle.
	err := New(store, registry, nil).RunOnce(context.Background())

	// Step 3: assert infrastructure failures are returned instead of being hidden.
	if err == nil || !strings.Contains(err.Error(), "database unavailable") {
		t.Fatalf("run once error = %v, want completion persistence error", err)
	}
}

func TestWorkerRunContinuesAfterSuccessfulJob(t *testing.T) {
	// Step 1: arrange a successful job whose completion requests a clean shutdown.
	ctx, cancel := context.WithCancel(context.Background())
	job := jobs.Job{ID: "job-123", Type: "send_email", Status: jobs.StatusProcessing}
	store := &fakeStore{claimedJob: job, completeFunc: func(context.Context, string) error {
		cancel()
		return nil
	}}
	registry := dispatchr.NewRegistry()
	if err := registry.Register("send_email", dispatchr.HandlerFunc(func(context.Context, dispatchr.Job) error { return nil })); err != nil {
		t.Fatalf("register handler: %v", err)
	}

	// Step 2: start the continuous worker loop.
	err := New(store, registry, nil).Run(ctx, time.Hour)

	// Step 3: assert it completed the job and stopped cleanly after cancellation.
	if err != nil || store.completedID != job.ID {
		t.Fatalf("run error = %v, completed=%q, want clean completion", err, store.completedID)
	}
}

func TestWorkerRunStopsWhileWaitingForEmptyQueue(t *testing.T) {
	// Step 1: arrange an empty queue and observe the first poll.
	ctx, cancel := context.WithCancel(context.Background())
	claimed := make(chan struct{})
	store := &fakeStore{claimFunc: func(context.Context) (jobs.Job, error) {
		close(claimed)
		return jobs.Job{}, jobs.ErrNoJobAvailable
	}}
	worker := New(store, dispatchr.NewRegistry(), nil)
	done := make(chan error, 1)

	// Step 2: start a long poll interval, then cancel after the empty claim.
	go func() { done <- worker.Run(ctx, time.Hour) }()
	<-claimed
	cancel()

	// Step 3: assert cancellation interrupts the wait instead of waiting an hour.
	if err := <-done; err != nil {
		t.Fatalf("run error = %v, want clean shutdown", err)
	}
}

func TestWorkerRunLogsAndRetriesStoreError(t *testing.T) {
	// Step 1: arrange one transient claim error followed by an empty queue.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	claimCalls := 0
	store := &fakeStore{claimFunc: func(context.Context) (jobs.Job, error) {
		claimCalls++
		if claimCalls == 1 {
			return jobs.Job{}, errors.New("database unavailable")
		}
		return jobs.Job{}, jobs.ErrNoJobAvailable
	}}
	logger := &fakeLogger{}
	worker := New(store, dispatchr.NewRegistry(), logger)
	waits := 0
	worker.wait = func(context.Context, time.Duration) bool {
		waits++
		return waits == 2
	}

	// Step 2: run the loop with an immediate test wait function.
	err := worker.Run(ctx, time.Second)

	// Step 3: assert the error was logged and a second claim was attempted.
	if err != nil || claimCalls != 2 || logger.errorCount != 1 {
		t.Fatalf("run error=%v claims=%d logs=%d, want retry after one logged error", err, claimCalls, logger.errorCount)
	}
}

func TestWorkerRunRejectsNonPositivePollInterval(t *testing.T) {
	// Step 1: arrange a worker without starting any database operation.
	worker := New(&fakeStore{}, dispatchr.NewRegistry(), nil)

	// Step 2: start it with an invalid interval.
	err := worker.Run(context.Background(), 0)

	// Step 3: assert invalid configuration fails fast.
	if !errors.Is(err, ErrInvalidPollInterval) {
		t.Fatalf("run error = %v, want ErrInvalidPollInterval", err)
	}
}

type fakeStore struct {
	claimedJob    jobs.Job
	claimErr      error
	claimFunc     func(context.Context) (jobs.Job, error)
	completedID   string
	completeErr   error
	completeFunc  func(context.Context, string) error
	failedID      string
	failureReason string
	failErr       error
}

func (*fakeStore) Create(context.Context, jobs.Job) error { return nil }
func (*fakeStore) Get(context.Context, string) (jobs.Job, error) {
	return jobs.Job{}, jobs.ErrNotFound
}
func (s *fakeStore) ClaimNext(ctx context.Context) (jobs.Job, error) {
	if s.claimFunc != nil {
		return s.claimFunc(ctx)
	}
	return s.claimedJob, s.claimErr
}
func (s *fakeStore) Complete(ctx context.Context, id string) error {
	s.completedID = id
	if s.completeFunc != nil {
		return s.completeFunc(ctx, id)
	}
	return s.completeErr
}
func (s *fakeStore) Fail(_ context.Context, id, reason string) error {
	s.failedID = id
	s.failureReason = reason
	return s.failErr
}

var _ jobs.Store = (*fakeStore)(nil)

type fakeLogger struct {
	errorCount int
}

func (*fakeLogger) Debug(string, ...logging.Field) {}
func (*fakeLogger) Info(string, ...logging.Field)  {}
func (l *fakeLogger) Error(string, ...logging.Field) {
	l.errorCount++
}

var _ logging.Logger = (*fakeLogger)(nil)
