package worker

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/danielwangai/dispatchr"
	"github.com/danielwangai/dispatchr/internal/jobs"
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
	err := New(store, registry).RunOnce(context.Background())

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
	err := New(store, registry).RunOnce(context.Background())

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
	err := New(store, dispatchr.NewRegistry()).RunOnce(context.Background())

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
	err := New(store, dispatchr.NewRegistry()).RunOnce(context.Background())

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
	err := New(store, registry).RunOnce(context.Background())

	// Step 3: assert infrastructure failures are returned instead of being hidden.
	if err == nil || !strings.Contains(err.Error(), "database unavailable") {
		t.Fatalf("run once error = %v, want completion persistence error", err)
	}
}

type fakeStore struct {
	claimedJob    jobs.Job
	claimErr      error
	completedID   string
	completeErr   error
	failedID      string
	failureReason string
	failErr       error
}

func (*fakeStore) Create(context.Context, jobs.Job) error { return nil }
func (*fakeStore) Get(context.Context, string) (jobs.Job, error) {
	return jobs.Job{}, jobs.ErrNotFound
}
func (s *fakeStore) ClaimNext(context.Context) (jobs.Job, error) { return s.claimedJob, s.claimErr }
func (s *fakeStore) Complete(_ context.Context, id string) error {
	s.completedID = id
	return s.completeErr
}
func (s *fakeStore) Fail(_ context.Context, id, reason string) error {
	s.failedID = id
	s.failureReason = reason
	return s.failErr
}

var _ jobs.Store = (*fakeStore)(nil)
