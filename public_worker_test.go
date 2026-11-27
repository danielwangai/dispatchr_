package dispatchr_test

import (
	"context"
	"testing"

	"github.com/danielwangai/dispatchr"
)

// TestPublicWorkerAPI verifies an application can use the exported API without
// importing any package under internal.
func TestPublicWorkerAPI(t *testing.T) {
	// Arrange an application-owned Store and a registered handler.
	store := &externalStore{claimed: dispatchr.Job{ID: "job-123", Type: "send_email", Status: dispatchr.StatusProcessing}}
	registry := dispatchr.NewRegistry()
	if err := registry.Register("send_email", dispatchr.HandlerFunc(func(context.Context, dispatchr.Job) error { return nil })); err != nil {
		t.Fatalf("register handler: %v", err)
	}
	worker, err := dispatchr.NewWorker(store, registry)
	if err != nil {
		t.Fatalf("create worker: %v", err)
	}

	// Act: process one claimed job through the public Worker API.
	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatalf("run worker once: %v", err)
	}

	// Assert: the public Store contract receives the completed transition.
	if store.completedID != "job-123" {
		t.Fatalf("completed ID = %q, want job-123", store.completedID)
	}
}

type externalStore struct {
	claimed     dispatchr.Job
	completedID string
}

func (*externalStore) Create(context.Context, dispatchr.Job) error { return nil }
func (*externalStore) Get(context.Context, string) (dispatchr.Job, error) {
	return dispatchr.Job{}, dispatchr.ErrNotFound
}
func (s *externalStore) ClaimNext(context.Context) (dispatchr.Job, error) { return s.claimed, nil }
func (s *externalStore) Complete(_ context.Context, id string) error {
	s.completedID = id
	return nil
}
func (*externalStore) Fail(context.Context, string, string) error { return nil }

var _ dispatchr.Store = (*externalStore)(nil)
