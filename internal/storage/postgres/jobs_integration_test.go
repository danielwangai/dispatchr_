//go:build integration

package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/danielwangai/dispatchr/internal/jobs"
	"github.com/danielwangai/dispatchr/internal/storage/postgres"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestStoreCreateAndGet(t *testing.T) {
	pool := testPool(t)
	clearJobs(t, pool)

	// Arrange.
	createdAt := time.Now().UTC().Truncate(time.Microsecond)
	job := jobs.Job{
		ID:          "00000000-0000-0000-0000-000000000001",
		Type:        "send_email",
		Payload:     json.RawMessage(`{"to":"customer@example.com"}`),
		Status:      jobs.StatusPending,
		Attempts:    0,
		MaxAttempts: 3,
		CreatedAt:   createdAt,
		UpdatedAt:   createdAt,
	}

	store := postgres.New(pool)
	// Act.
	if err := store.Create(context.Background(), job); err != nil {
		t.Fatalf("create job: %v", err)
	}

	got, err := store.Get(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}

	// Assert.
	if got.ID != job.ID || got.Type != job.Type || got.Status != job.Status || got.Attempts != job.Attempts || got.MaxAttempts != job.MaxAttempts {
		t.Fatalf("got job metadata %#v, want %#v", got, job)
	}
	assertJSONEqual(t, got.Payload, job.Payload)
	if got.LastError != nil || got.CompletedAt != nil {
		t.Fatalf("got nullable fields last_error=%v completed_at=%v, want nil", got.LastError, got.CompletedAt)
	}
	if !got.CreatedAt.Equal(job.CreatedAt) || !got.UpdatedAt.Equal(job.UpdatedAt) {
		t.Fatalf("got timestamps created_at=%s updated_at=%s, want %s", got.CreatedAt, got.UpdatedAt, job.CreatedAt)
	}
}

func TestStoreGetNonExistentJob(t *testing.T) {
	pool := testPool(t)
	clearJobs(t, pool)

	// Act.
	_, err := postgres.New(pool).Get(context.Background(), "00000000-0000-0000-0000-000000000002")

	// Assert.
	if !errors.Is(err, jobs.ErrNotFound) {
		t.Fatalf("get missing job error = %v, want jobs.ErrNotFound", err)
	}
}

func TestStoreCreateAndGetFailedJob(t *testing.T) {
	pool := testPool(t)
	clearJobs(t, pool)

	// Arrange.
	lastError := "email provider returned 503"
	completedAt := time.Now().UTC().Truncate(time.Microsecond)
	job := jobs.Job{
		ID:          "00000000-0000-0000-0000-000000000003",
		Type:        "send_email",
		Payload:     json.RawMessage(`{"to":"customer@example.com"}`),
		Status:      jobs.StatusFailed,
		Attempts:    3,
		MaxAttempts: 3,
		LastError:   &lastError,
		CompletedAt: &completedAt,
		CreatedAt:   completedAt,
		UpdatedAt:   completedAt,
	}

	store := postgres.New(pool)
	// Act.
	if err := store.Create(context.Background(), job); err != nil {
		t.Fatalf("create failed job: %v", err)
	}

	got, err := store.Get(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("get failed job: %v", err)
	}

	// Assert.
	if got.LastError == nil || *got.LastError != lastError {
		t.Fatalf("got last_error %v, want %q", got.LastError, lastError)
	}
	if got.CompletedAt == nil || !got.CompletedAt.Equal(completedAt) {
		t.Fatalf("got completed_at %v, want %s", got.CompletedAt, completedAt)
	}
}

func TestStoreCreateRejectsDuplicateID(t *testing.T) {
	pool := testPool(t)
	clearJobs(t, pool)

	// Arrange.
	createdAt := time.Now().UTC().Truncate(time.Microsecond)
	job := jobs.Job{
		ID:          "00000000-0000-0000-0000-000000000004",
		Type:        "send_email",
		Payload:     json.RawMessage(`{"to":"customer@example.com"}`),
		Status:      jobs.StatusPending,
		MaxAttempts: 3,
		CreatedAt:   createdAt,
		UpdatedAt:   createdAt,
	}
	store := postgres.New(pool)
	// create job(first time)
	if err := store.Create(context.Background(), job); err != nil {
		t.Fatalf("create first job: %v", err)
	}

	// Act.
	// create job as duplicate(run store.Create twice with the same job)
	err := store.Create(context.Background(), job)

	// Assert.
	var databaseError *pgconn.PgError
	if !errors.As(err, &databaseError) || databaseError.Code != "23505" {
		t.Fatalf("duplicate create error = %v, want PostgreSQL unique violation", err)
	}
}

func TestStoreCreateRejectsZeroMaxAttempts(t *testing.T) {
	pool := testPool(t)
	clearJobs(t, pool)

	// Arrange.
	createdAt := time.Now().UTC().Truncate(time.Microsecond)
	job := jobs.Job{
		ID:          "00000000-0000-0000-0000-000000000005",
		Type:        "send_email",
		Payload:     json.RawMessage(`{"to":"customer@example.com"}`),
		Status:      jobs.StatusPending,
		MaxAttempts: 0,
		CreatedAt:   createdAt,
		UpdatedAt:   createdAt,
	}

	// Act.
	err := postgres.New(pool).Create(context.Background(), job)

	// Assert.
	var databaseError *pgconn.PgError
	if !errors.As(err, &databaseError) || databaseError.Code != "23514" {
		t.Fatalf("create error = %v, want PostgreSQL check violation", err)
	}
}

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	databaseURL := os.Getenv("DISPATCHR_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("DISPATCHR_TEST_DATABASE_URL must be set; run make integration-test")
	}

	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func clearJobs(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), "TRUNCATE jobs"); err != nil {
		t.Fatalf("clear jobs: %v", err)
	}
}

func assertJSONEqual(t *testing.T, got, want json.RawMessage) {
	t.Helper()

	var gotValue, wantValue any
	if err := json.Unmarshal(got, &gotValue); err != nil {
		t.Fatalf("unmarshal returned payload: %v", err)
	}
	if err := json.Unmarshal(want, &wantValue); err != nil {
		t.Fatalf("unmarshal expected payload: %v", err)
	}
	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Fatalf("got payload %s, want %s", got, want)
	}
}
