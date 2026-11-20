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
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestStoreCreateAndGet(t *testing.T) {
	pool := testPool(t)
	clearJobs(t, pool)

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
	if err := store.Create(context.Background(), job); err != nil {
		t.Fatalf("create job: %v", err)
	}

	got, err := store.Get(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}

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

func TestStoreGetMissingJob(t *testing.T) {
	pool := testPool(t)
	clearJobs(t, pool)

	_, err := postgres.New(pool).Get(context.Background(), "00000000-0000-0000-0000-000000000002")
	if !errors.Is(err, jobs.ErrNotFound) {
		t.Fatalf("get missing job error = %v, want jobs.ErrNotFound", err)
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
