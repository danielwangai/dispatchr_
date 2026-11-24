package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/danielwangai/dispatchr/internal/jobs"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const jobColumns = `id, type, payload, status, attempts, max_attempts, last_error, completed_at, created_at, updated_at`

// Store persists jobs in PostgreSQL.
type Store struct {
	pool *pgxpool.Pool
}

// New creates a job store that uses the provided PostgreSQL connection pool.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Create persists a new job with its current lifecycle state and metadata.
func (s *Store) Create(ctx context.Context, job jobs.Job) error {
	const query = `
		INSERT INTO jobs (` + jobColumns + `)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`

	_, err := s.pool.Exec(ctx, query,
		job.ID,
		job.Type,
		job.Payload,
		job.Status,
		job.Attempts,
		job.MaxAttempts,
		job.LastError,
		job.CompletedAt,
		job.CreatedAt,
		job.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("create job: %w", err)
	}

	return nil
}

// Get retrieves a job by ID. It returns jobs.ErrNotFound when no job exists.
func (s *Store) Get(ctx context.Context, id string) (jobs.Job, error) {
	const query = `SELECT ` + jobColumns + ` FROM jobs WHERE id = $1`

	job, err := scanJob(s.pool.QueryRow(ctx, query, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return jobs.Job{}, fmt.Errorf("%w: %s", jobs.ErrNotFound, id)
	}
	if err != nil {
		return jobs.Job{}, fmt.Errorf("get job: %w", err)
	}

	return job, nil
}

// ClaimNext atomically assigns the oldest eligible pending job to a worker.
// It prevents concurrent workers from receiving the same job.
// Jobs are currently claimed FIFO; priority ordering will be added later.
func (s *Store) ClaimNext(ctx context.Context) (jobs.Job, error) {
	// Select and lock one job, then transition it before releasing the lock.
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

	// Execute the atomic claim and map the returned row.
	job, err := scanJob(s.pool.QueryRow(ctx, query, jobs.StatusPending, jobs.StatusProcessing))
	// An empty result means no eligible pending job exists.
	if errors.Is(err, pgx.ErrNoRows) {
		return jobs.Job{}, jobs.ErrNoJobAvailable
	}
	// Preserve unexpected database failures for the caller.
	if err != nil {
		return jobs.Job{}, fmt.Errorf("claim next job: %w", err)
	}

	return job, nil
}

// scanJob maps a PostgreSQL row to Dispatchr's database-independent job model.
func scanJob(row pgx.Row) (jobs.Job, error) {
	var (
		job     jobs.Job
		payload []byte
		status  string
	)

	err := row.Scan(
		&job.ID,
		&job.Type,
		&payload,
		&status,
		&job.Attempts,
		&job.MaxAttempts,
		&job.LastError,
		&job.CompletedAt,
		&job.CreatedAt,
		&job.UpdatedAt,
	)
	if err != nil {
		return jobs.Job{}, err
	}

	job.Payload = json.RawMessage(payload)
	job.Status = jobs.Status(status)
	return job, nil
}
