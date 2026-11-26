// Package dispatchr exposes the extension points used to run background jobs.
package dispatchr

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/danielwangai/dispatchr/internal/jobs"
)

var (
	// ErrJobTypeRequired indicates that a handler was registered without a type.
	ErrJobTypeRequired = errors.New("job type is required")
	// ErrHandlerRequired indicates that a job type has no handler implementation.
	ErrHandlerRequired = errors.New("job handler is required")
	// ErrHandlerAlreadyRegistered indicates that a job type already has a handler.
	ErrHandlerAlreadyRegistered = errors.New("job handler already registered")
	// ErrHandlerNotFound indicates that no handler is registered for a job type.
	ErrHandlerNotFound = errors.New("job handler not found")
)

// Job is the job data supplied to a registered handler.
// It aliases Dispatchr's domain model so handlers receive no transport or
// database-specific representation.
type Job = jobs.Job

// Handler executes one job. Returning an error tells a worker that execution
// failed, allowing it to record the failure in the job store.
type Handler interface {
	Handle(ctx context.Context, job Job) error
}

// HandlerFunc adapts an ordinary function to the Handler interface.
type HandlerFunc func(ctx context.Context, job Job) error

// Handle runs the wrapped handler function.
func (f HandlerFunc) Handle(ctx context.Context, job Job) error {
	return f(ctx, job)
}

// Registry maps each job type to the handler that processes it.
// It is safe to register and look up handlers concurrently.
type Registry struct {
	mu       sync.RWMutex
	handlers map[string]Handler
}

// NewRegistry creates an empty job handler registry.
func NewRegistry() *Registry {
	return &Registry{handlers: make(map[string]Handler)}
}

// Register associates a job type with one handler. Duplicate registrations are
// rejected so startup configuration cannot silently replace a handler.
func (r *Registry) Register(jobType string, handler Handler) error {
	jobType = strings.TrimSpace(jobType)
	if jobType == "" {
		return ErrJobTypeRequired
	}
	if handler == nil {
		return ErrHandlerRequired
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.handlers[jobType]; exists {
		return ErrHandlerAlreadyRegistered
	}
	r.handlers[jobType] = handler
	return nil
}

// HandlerFor returns the handler registered for a job type.
func (r *Registry) HandlerFor(jobType string) (Handler, error) {
	r.mu.RLock()
	handler, exists := r.handlers[jobType]
	r.mu.RUnlock()
	if !exists {
		return nil, ErrHandlerNotFound
	}
	return handler, nil
}
