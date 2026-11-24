// Package jobs contains Dispatchr's core job model and storage contract.
package jobs

import (
	"encoding/json"
	"time"
)

// Status describes a job's lifecycle state.
type Status string

const (
	StatusPending    Status = "pending"
	StatusProcessing Status = "processing"
	StatusCompleted  Status = "completed"
	StatusFailed     Status = "failed"
)

// Job is a unit of background work persisted by Dispatchr.
type Job struct {
	ID          string
	Type        string
	Payload     json.RawMessage
	Status      Status
	Attempts    int
	MaxAttempts int
	LastError   *string
	CompletedAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
