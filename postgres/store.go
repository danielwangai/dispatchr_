package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/danielwangai/dispatchr"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const jobColumns = `id, type, payload, status, attempts, max_attempts, last_error, completed_at, created_at, updated_at`

// Store persists jobs in PostgreSQL.
type Store struct {
	pool *pgxpool.Pool
}

var _ dispatchr.Store = (*Store)(nil)

// NewStore creates a Dispatchr store backed by a PostgreSQL pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Create persists a new job with its current lifecycle state and metadata.
func (s *Store) Create(ctx context.Context, job dispatchr.Job) error {
	const query = `
		INSERT INTO jobs (` + jobColumns + `)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`

	_, err := s.pool.Exec(ctx, query, job.ID, job.Type, job.Payload, job.Status,
		job.Attempts, job.MaxAttempts, job.LastError, job.CompletedAt, job.CreatedAt, job.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create job: %w", err)
	}
	return nil
}

// Get retrieves a job by ID. It returns dispatchr.ErrNotFound when absent.
func (s *Store) Get(ctx context.Context, id string) (dispatchr.Job, error) {
	const query = `SELECT ` + jobColumns + ` FROM jobs WHERE id = $1`

	job, err := scanJob(s.pool.QueryRow(ctx, query, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return dispatchr.Job{}, fmt.Errorf("%w: %s", dispatchr.ErrNotFound, id)
	}
	if err != nil {
		return dispatchr.Job{}, fmt.Errorf("get job: %w", err)
	}
	return job, nil
}

// ClaimNext atomically assigns the oldest eligible pending job to a worker.
// It prevents concurrent workers from receiving the same job.
func (s *Store) ClaimNext(ctx context.Context) (dispatchr.Job, error) {
	const query = `
		WITH next_job AS (
			SELECT id
			FROM jobs
			WHERE status = $1 AND attempts < max_attempts
			ORDER BY created_at, id
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		UPDATE jobs
		SET status = $2,
			attempts = jobs.attempts + 1,
			updated_at = NOW()
		FROM next_job
		WHERE jobs.id = next_job.id
		RETURNING jobs.id, jobs.type, jobs.payload, jobs.status, jobs.attempts,
			jobs.max_attempts, jobs.last_error, jobs.completed_at, jobs.created_at, jobs.updated_at`

	job, err := scanJob(s.pool.QueryRow(ctx, query, dispatchr.StatusPending, dispatchr.StatusProcessing))
	if errors.Is(err, pgx.ErrNoRows) {
		return dispatchr.Job{}, dispatchr.ErrNoJobAvailable
	}
	if err != nil {
		return dispatchr.Job{}, fmt.Errorf("claim next job: %w", err)
	}
	return job, nil
}

// Complete marks a processing job as completed and records its completion time.
func (s *Store) Complete(ctx context.Context, id string) error {
	const query = `
		UPDATE jobs
		SET status = $2, last_error = NULL, completed_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND status = $3`

	tag, err := s.pool.Exec(ctx, query, id, dispatchr.StatusCompleted, dispatchr.StatusProcessing)
	if err != nil {
		return fmt.Errorf("complete job: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return s.classifyProcessingError(ctx, id)
	}
	return nil
}

// Fail marks a processing job as failed and records the failure reason.
func (s *Store) Fail(ctx context.Context, id, reason string) error {
	const query = `
		UPDATE jobs
		SET status = $2, last_error = $3, completed_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND status = $4`

	tag, err := s.pool.Exec(ctx, query, id, dispatchr.StatusFailed, reason, dispatchr.StatusProcessing)
	if err != nil {
		return fmt.Errorf("fail job: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return s.classifyProcessingError(ctx, id)
	}
	return nil
}

func (s *Store) classifyProcessingError(ctx context.Context, id string) error {
	var status dispatchr.Status
	err := s.pool.QueryRow(ctx, "SELECT status FROM jobs WHERE id = $1", id).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: %s", dispatchr.ErrNotFound, id)
	}
	if err != nil {
		return fmt.Errorf("get job state: %w", err)
	}
	return fmt.Errorf("%w: job %s is %s", dispatchr.ErrInvalidState, id, status)
}

func scanJob(row pgx.Row) (dispatchr.Job, error) {
	var (
		job     dispatchr.Job
		payload []byte
		status  string
	)
	err := row.Scan(&job.ID, &job.Type, &payload, &status, &job.Attempts,
		&job.MaxAttempts, &job.LastError, &job.CompletedAt, &job.CreatedAt, &job.UpdatedAt)
	if err != nil {
		return dispatchr.Job{}, err
	}
	job.Payload = json.RawMessage(payload)
	job.Status = dispatchr.Status(status)
	return job, nil
}
