//go:build integration

package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"sync"
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

// TestStoreClaimNextClaimsOldestPendingJob verifies FIFO claiming and state updates.
func TestStoreClaimNextClaimsOldestPendingJob(t *testing.T) {
	pool := testPool(t)
	clearJobs(t, pool)

	// Arrange two pending jobs with different submission times.
	store := postgres.New(pool)
	now := time.Now().UTC().Truncate(time.Microsecond)
	oldest := pendingJob("00000000-0000-0000-0000-000000000006", now.Add(-time.Minute))
	newer := pendingJob("00000000-0000-0000-0000-000000000007", now)
	for _, job := range []jobs.Job{oldest, newer} {
		if err := store.Create(context.Background(), job); err != nil {
			t.Fatalf("create job: %v", err)
		}
	}

	// Act: claim one job.
	claimed, err := store.ClaimNext(context.Background())

	// Assert: the older job is processing and has one attempt.
	if err != nil {
		t.Fatalf("claim next job: %v", err)
	}
	if claimed.ID != oldest.ID || claimed.Status != jobs.StatusProcessing || claimed.Attempts != 1 {
		t.Fatalf("claimed job %#v, want oldest job processing with one attempt", claimed)
	}
}

// TestStoreClaimNextReturnsNoJobAvailable verifies empty queues return the expected error.
func TestStoreClaimNextReturnsNoJobAvailable(t *testing.T) {
	pool := testPool(t)
	clearJobs(t, pool)

	// Act: claim from an empty queue.
	_, err := postgres.New(pool).ClaimNext(context.Background())

	// Assert: no work is available.
	if !errors.Is(err, jobs.ErrNoJobAvailable) {
		t.Fatalf("claim next error = %v, want jobs.ErrNoJobAvailable", err)
	}
}

// TestStoreClaimNextDoesNotDoubleClaimJob verifies competing claimers cannot share a job.
// It is like two staff members reaching for one ticket: only one may take it.
// A start gate releases both claimers together, and several rounds increase race coverage.
func TestStoreClaimNextDoesNotDoubleClaimJob(t *testing.T) {
	pool := testPool(t)
	store := postgres.New(pool)

	// Repeat the race because goroutine scheduling is nondeterministic.
	for range 20 {
		// Start every round with one pending job.
		clearJobs(t, pool)
		job := pendingJob("00000000-0000-0000-0000-000000000008", time.Now().UTC().Truncate(time.Microsecond))
		if err := store.Create(context.Background(), job); err != nil {
			t.Fatalf("create job: %v", err)
		}

		// Prepare two claimers and hold both behind the start gate.
		type claimResult struct {
			job jobs.Job
			err error
		}
		start := make(chan struct{})
		ready := make(chan struct{}, 2)
		results := make(chan claimResult, 2)
		var waitGroup sync.WaitGroup
		for range 2 {
			waitGroup.Add(1)
			go func() {
				defer waitGroup.Done()
				ready <- struct{}{}
				<-start
				claimed, err := store.ClaimNext(context.Background())
				results <- claimResult{job: claimed, err: err}
			}()
		}

		// Wait until both claimers are ready, then release them together.
		for range 2 {
			<-ready
		}
		close(start)
		waitGroup.Wait()
		close(results)

		// Assert that one claimer receives the job and the other receives no work.
		claimedCount := 0
		for result := range results {
			if result.err == nil {
				if result.job.ID != job.ID {
					t.Fatalf("claimed job ID = %s, want %s", result.job.ID, job.ID)
				}
				claimedCount++
				continue
			}
			if !errors.Is(result.err, jobs.ErrNoJobAvailable) {
				t.Fatalf("claim next error = %v, want jobs.ErrNoJobAvailable", result.err)
			}
		}
		if claimedCount != 1 {
			t.Fatalf("claimed count = %d, want 1", claimedCount)
		}
	}
}

// TestStoreComplete marks a processing job as completed.
func TestStoreComplete(t *testing.T) {
	pool := testPool(t)
	clearJobs(t, pool)
	store := postgres.New(pool)

	// Arrange a job already claimed by a worker.
	job := processingJob("00000000-0000-0000-0000-000000000009", time.Now().UTC().Truncate(time.Microsecond))
	if err := store.Create(context.Background(), job); err != nil {
		t.Fatalf("create job: %v", err)
	}

	// Act: record successful execution.
	if err := store.Complete(context.Background(), job.ID); err != nil {
		t.Fatalf("complete job: %v", err)
	}

	// Assert: the job is completed and has a completion time.
	got, err := store.Get(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if got.Status != jobs.StatusCompleted || got.CompletedAt == nil || got.LastError != nil {
		t.Fatalf("completed job %#v, want completed status, completion time, and no error", got)
	}
}

// TestStoreFail marks a processing job as failed and preserves its error.
func TestStoreFail(t *testing.T) {
	pool := testPool(t)
	clearJobs(t, pool)
	store := postgres.New(pool)

	// Arrange a job already claimed by a worker.
	job := processingJob("00000000-0000-0000-0000-000000000010", time.Now().UTC().Truncate(time.Microsecond))
	if err := store.Create(context.Background(), job); err != nil {
		t.Fatalf("create job: %v", err)
	}

	// Act: record a handler failure.
	const reason = "email provider returned 503"
	if err := store.Fail(context.Background(), job.ID, reason); err != nil {
		t.Fatalf("fail job: %v", err)
	}

	// Assert: the job is failed with its failure details.
	got, err := store.Get(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if got.Status != jobs.StatusFailed || got.CompletedAt == nil || got.LastError == nil || *got.LastError != reason {
		t.Fatalf("failed job %#v, want failed status, completion time, and error %q", got, reason)
	}
}

// TestStoreCompletionTransitionsRejectPendingJobs verifies workers cannot finish unclaimed jobs.
func TestStoreCompletionTransitionsRejectPendingJobs(t *testing.T) {
	pool := testPool(t)
	clearJobs(t, pool)
	store := postgres.New(pool)

	// Arrange a job that no worker has claimed.
	job := pendingJob("00000000-0000-0000-0000-000000000011", time.Now().UTC().Truncate(time.Microsecond))
	if err := store.Create(context.Background(), job); err != nil {
		t.Fatalf("create job: %v", err)
	}

	// Act: attempt both terminal transitions without first claiming the job.
	completionErr := store.Complete(context.Background(), job.ID)
	failErr := store.Fail(context.Background(), job.ID, "should not be saved")

	// Assert: both transitions are rejected and the job remains pending.
	if !errors.Is(completionErr, jobs.ErrInvalidState) {
		t.Fatalf("complete error = %v, want jobs.ErrInvalidState", completionErr)
	}
	if !errors.Is(failErr, jobs.ErrInvalidState) {
		t.Fatalf("fail error = %v, want jobs.ErrInvalidState", failErr)
	}
	got, err := store.Get(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if got.Status != jobs.StatusPending || got.LastError != nil || got.CompletedAt != nil {
		t.Fatalf("pending job %#v was unexpectedly changed", got)
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

func pendingJob(id string, createdAt time.Time) jobs.Job {
	return jobs.Job{
		ID:          id,
		Type:        "send_email",
		Payload:     json.RawMessage(`{"to":"customer@example.com"}`),
		Status:      jobs.StatusPending,
		MaxAttempts: 3,
		CreatedAt:   createdAt,
		UpdatedAt:   createdAt,
	}
}

func processingJob(id string, createdAt time.Time) jobs.Job {
	job := pendingJob(id, createdAt)
	job.Status = jobs.StatusProcessing
	job.Attempts = 1
	return job
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
