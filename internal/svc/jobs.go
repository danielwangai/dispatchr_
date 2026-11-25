// Package svc contains Dispatchr's application use cases.
package svc

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/danielwangai/dispatchr/internal/jobs"
	"github.com/google/uuid"
)

const defaultMaxAttempts = 3

var (
	// ErrInvalidMaxAttempts indicates that a job cannot be attempted.
	ErrInvalidMaxAttempts = errors.New("max attempts must be greater than zero")
	ErrJobTypeRequired    = errors.New("job type is required")
)

// JobService defines the job use cases required by HTTP transport.
type JobService interface {
	SubmitJob(ctx context.Context, input SubmitJobInput) (jobs.Job, error)
	GetJob(ctx context.Context, id string) (jobs.Job, error)
}

// SubmitJobInput contains the producer-controlled details of a new job.
type SubmitJobInput struct {
	Type        string
	Payload     json.RawMessage
	MaxAttempts *int
}

// Service implements job-related application use cases.
type Service struct {
	store jobs.Store
	newID func() string
	now   func() time.Time
}

var _ JobService = (*Service)(nil)

// New creates a job service with production ID and clock dependencies.
func New(store jobs.Store) *Service {
	return NewWithDependencies(store, uuid.NewString, time.Now)
}

// NewWithDependencies creates a job service with replaceable time and ID sources.
// It is useful when deterministic tests need fixed values.
func NewWithDependencies(store jobs.Store, newID func() string, now func() time.Time) *Service {
	return &Service{store: store, newID: newID, now: now}
}

// SubmitJob validates business rules, creates job metadata, and persists the job.
func (s *Service) SubmitJob(ctx context.Context, input SubmitJobInput) (jobs.Job, error) {
	jobType := strings.TrimSpace(input.Type)
	if jobType == "" {
		return jobs.Job{}, ErrJobTypeRequired
	}

	maxAttempts := defaultMaxAttempts
	if input.MaxAttempts != nil {
		maxAttempts = *input.MaxAttempts
	}
	if maxAttempts <= 0 {
		return jobs.Job{}, ErrInvalidMaxAttempts
	}

	createdAt := s.now().UTC()
	job := jobs.Job{
		ID:          s.newID(),
		Type:        jobType,
		Payload:     input.Payload,
		Status:      jobs.StatusPending,
		MaxAttempts: maxAttempts,
		CreatedAt:   createdAt,
		UpdatedAt:   createdAt,
	}
	if err := s.store.Create(ctx, job); err != nil {
		return jobs.Job{}, err
	}

	return job, nil
}

// GetJob retrieves a job by ID from the configured store.
func (s *Service) GetJob(ctx context.Context, id string) (jobs.Job, error) {
	return s.store.Get(ctx, id)
}
