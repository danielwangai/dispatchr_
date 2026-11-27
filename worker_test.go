package dispatchr

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestWorkerRunOnceCompletesHandledJob(t *testing.T) {
	// Arrange a processing job and a handler that completes successfully.
	store := &fakeStore{claimedJob: Job{ID: "job-123", Type: "send_email", Status: StatusProcessing}}
	registry := NewRegistry()
	if err := registry.Register("send_email", HandlerFunc(func(context.Context, Job) error { return nil })); err != nil {
		t.Fatalf("register handler: %v", err)
	}
	worker, err := NewWorker(store, registry)
	if err != nil {
		t.Fatalf("create worker: %v", err)
	}

	// Act.
	err = worker.RunOnce(context.Background())

	// Assert the successful handler transitions the claimed job to completed.
	if err != nil || store.completedID != "job-123" || store.failedID != "" {
		t.Fatalf("run error=%v completed=%q failed=%q, want completed job", err, store.completedID, store.failedID)
	}
}

func TestWorkerRunOnceRecordsHandlerFailure(t *testing.T) {
	// Arrange a handler that returns an application error.
	store := &fakeStore{claimedJob: Job{ID: "job-123", Type: "send_email", Status: StatusProcessing}}
	registry := NewRegistry()
	_ = registry.Register("send_email", HandlerFunc(func(context.Context, Job) error { return errors.New("provider unavailable") }))
	worker, _ := NewWorker(store, registry)

	// Act.
	err := worker.RunOnce(context.Background())

	// Assert application failures are persisted without stopping the worker cycle.
	if err != nil || store.failedID != "job-123" || !strings.Contains(store.failureReason, "provider unavailable") {
		t.Fatalf("run error=%v failed=%q reason=%q, want persisted failure", err, store.failedID, store.failureReason)
	}
}

func TestWorkerRunRetriesUnexpectedError(t *testing.T) {
	// Arrange one temporary store error followed by an empty queue.
	claimCalls := 0
	store := &fakeStore{claimFunc: func(context.Context) (Job, error) {
		claimCalls++
		if claimCalls == 1 {
			return Job{}, errors.New("database unavailable")
		}
		return Job{}, ErrNoJobAvailable
	}}
	reported := 0
	worker, _ := NewWorker(store, NewRegistry(), WithErrorReporter(func(error) { reported++ }))
	waits := 0
	worker.wait = func(context.Context, time.Duration) bool {
		waits++
		return waits == 2
	}

	// Act.
	err := worker.Run(context.Background(), time.Second)

	// Assert the store error is reported and a second claim is attempted.
	if err != nil || claimCalls != 2 || reported != 1 {
		t.Fatalf("run error=%v claims=%d reports=%d, want one retry", err, claimCalls, reported)
	}
}

func TestNewWorkerValidatesDependencies(t *testing.T) {
	// Arrange valid dependencies for each negative constructor case.
	store := &fakeStore{}
	registry := NewRegistry()

	// Act and assert.
	if _, err := NewWorker(nil, registry); !errors.Is(err, ErrStoreRequired) {
		t.Fatalf("nil store error = %v, want ErrStoreRequired", err)
	}
	if _, err := NewWorker(store, nil); !errors.Is(err, ErrRegistryRequired) {
		t.Fatalf("nil registry error = %v, want ErrRegistryRequired", err)
	}
}

type fakeStore struct {
	claimedJob    Job
	claimErr      error
	claimFunc     func(context.Context) (Job, error)
	completedID   string
	failedID      string
	failureReason string
}

func (*fakeStore) Create(context.Context, Job) error { return nil }
func (*fakeStore) Get(context.Context, string) (Job, error) {
	return Job{}, ErrNotFound
}
func (s *fakeStore) ClaimNext(ctx context.Context) (Job, error) {
	if s.claimFunc != nil {
		return s.claimFunc(ctx)
	}
	return s.claimedJob, s.claimErr
}
func (s *fakeStore) Complete(_ context.Context, id string) error {
	s.completedID = id
	return nil
}
func (s *fakeStore) Fail(_ context.Context, id, reason string) error {
	s.failedID = id
	s.failureReason = reason
	return nil
}

var _ Store = (*fakeStore)(nil)
