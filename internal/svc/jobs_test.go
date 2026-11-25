package svc

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/danielwangai/dispatchr/internal/jobs"
)

func TestServiceSubmitJobBuildsAndStoresPendingJob(t *testing.T) {
	// Arrange deterministic dependencies and a store that records the job it receives.
	store := &fakeStore{}
	now := time.Date(2026, time.November, 25, 12, 0, 0, 0, time.UTC)
	service := NewWithDependencies(store, func() string { return "job-123" }, func() time.Time { return now })

	// Act: submit a job without an attempt limit so the service applies its default.
	job, err := service.SubmitJob(context.Background(), SubmitJobInput{
		Type:    " send_email ",
		Payload: json.RawMessage(`{"to":"customer@example.com"}`),
	})
	if err != nil {
		t.Fatalf("submit job: %v", err)
	}

	// Assert: the service owns generated metadata and stores a pending job.
	if job.ID != "job-123" || job.Type != "send_email" || job.Status != jobs.StatusPending || job.MaxAttempts != defaultMaxAttempts {
		t.Fatalf("job = %#v, want generated pending job", job)
	}
	if !job.CreatedAt.Equal(now) || !job.UpdatedAt.Equal(now) || store.created.ID != job.ID {
		t.Fatalf("stored job = %#v, want %#v", store.created, job)
	}
}

func TestServiceSubmitJobRejectsEmptyJobType(t *testing.T) {
	// Arrange a job that could never be processed.
	store := &fakeStore{}
	zero := 0
	service := NewWithDependencies(store, func() string { return "unused" }, time.Now)

	// Act.
	_, err := service.SubmitJob(context.Background(), SubmitJobInput{Type: "", MaxAttempts: &zero})

	// Assert
	if !errors.Is(err, ErrJobTypeRequired) {
		t.Fatalf("submit error = %v, want ErrJobTypeRequired", err)
	}
	if store.createCalled {
		t.Fatal("store.Create was called for an invalid job")
	}
}

func TestServiceSubmitJobRejectsInvalidMaxAttempts(t *testing.T) {
	// Arrange a job that could never be processed.
	store := &fakeStore{}
	zero := 0
	service := NewWithDependencies(store, func() string { return "unused" }, time.Now)

	// Act.
	_, err := service.SubmitJob(context.Background(), SubmitJobInput{Type: "send_email", MaxAttempts: &zero})

	// Assert
	if !errors.Is(err, ErrInvalidMaxAttempts) {
		t.Fatalf("submit error = %v, want ErrInvalidMaxAttempts", err)
	}
	if store.createCalled {
		t.Fatal("store.Create was called for an invalid job")
	}
}

func TestServiceGetJobDelegatesToStore(t *testing.T) {
	// Arrange a store result.
	want := jobs.Job{ID: "job-123", Type: "send_email"}
	store := &fakeStore{job: want}
	service := New(store)

	// Act.
	got, err := service.GetJob(context.Background(), "job-123")
	if err != nil {
		t.Fatalf("get job: %v", err)
	}

	// Assert: the service returns the domain model without creating a duplicate DTO.
	if !reflect.DeepEqual(got, want) || store.getID != want.ID {
		t.Fatalf("got %#v for ID %q, want %#v for ID %q", got, store.getID, want, want.ID)
	}
}

type fakeStore struct {
	created      jobs.Job
	createCalled bool
	job          jobs.Job
	getID        string
}

func (s *fakeStore) Create(_ context.Context, job jobs.Job) error {
	s.createCalled = true
	s.created = job
	return nil
}

func (s *fakeStore) Get(_ context.Context, id string) (jobs.Job, error) {
	s.getID = id
	return s.job, nil
}

func (*fakeStore) ClaimNext(context.Context) (jobs.Job, error) {
	return jobs.Job{}, jobs.ErrNoJobAvailable
}
func (*fakeStore) Complete(context.Context, string) error     { return nil }
func (*fakeStore) Fail(context.Context, string, string) error { return nil }

var _ jobs.Store = (*fakeStore)(nil)
