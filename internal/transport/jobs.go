package transport

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/danielwangai/dispatchr/internal/jobs"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

const defaultMaxAttempts = 3

type createJobRequest struct {
	Type        string          `json:"type"`
	Payload     json.RawMessage `json:"payload"`
	MaxAttempts *int            `json:"max_attempts"`
}

type createJobResponse struct {
	JobID  string      `json:"job_id"`
	Status jobs.Status `json:"status"`
}

type getJobResponse struct {
	JobID       string          `json:"job_id"`
	Type        string          `json:"type"`
	Payload     json.RawMessage `json:"payload"`
	Status      jobs.Status     `json:"status"`
	Attempts    int             `json:"attempts"`
	MaxAttempts int             `json:"max_attempts"`
	LastError   *string         `json:"last_error,omitempty"`
	CompletedAt *time.Time      `json:"completed_at,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

func createJobHandler(store jobs.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, request *http.Request) {
		if store == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "job storage unavailable"})
			return
		}

		input, err := decodeCreateJobRequest(request)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}

		maxAttempts := defaultMaxAttempts
		if input.MaxAttempts != nil {
			maxAttempts = *input.MaxAttempts
		}
		if maxAttempts <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "max_attempts must be greater than zero"})
			return
		}

		now := time.Now().UTC()
		job := jobs.Job{
			ID:          uuid.NewString(),
			Type:        strings.TrimSpace(input.Type),
			Payload:     input.Payload,
			Status:      jobs.StatusPending,
			MaxAttempts: maxAttempts,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if err := store.Create(request.Context(), job); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "create job"})
			return
		}

		writeJSON(w, http.StatusAccepted, createJobResponse{JobID: job.ID, Status: job.Status})
	}
}

func getJobHandler(store jobs.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, request *http.Request) {
		if store == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "job storage unavailable"})
			return
		}

		id := mux.Vars(request)["id"]
		if _, err := uuid.Parse(id); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "job ID must be a valid UUID"})
			return
		}

		job, err := store.Get(request.Context(), id)
		if errors.Is(err, jobs.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "job not found"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "get job"})
			return
		}

		writeJSON(w, http.StatusOK, getJobResponse{
			JobID:       job.ID,
			Type:        job.Type,
			Payload:     job.Payload,
			Status:      job.Status,
			Attempts:    job.Attempts,
			MaxAttempts: job.MaxAttempts,
			LastError:   job.LastError,
			CompletedAt: job.CompletedAt,
			CreatedAt:   job.CreatedAt,
			UpdatedAt:   job.UpdatedAt,
		})
	}
}

func decodeCreateJobRequest(request *http.Request) (createJobRequest, error) {
	var input createJobRequest
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		if errors.Is(err, io.EOF) {
			return createJobRequest{}, errors.New("request body is required")
		}
		return createJobRequest{}, errors.New("request body must be valid JSON")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return createJobRequest{}, errors.New("request body must contain one JSON object")
	}
	if strings.TrimSpace(input.Type) == "" {
		return createJobRequest{}, errors.New("type is required")
	}
	if len(input.Payload) == 0 || !json.Valid(input.Payload) {
		return createJobRequest{}, errors.New("payload must be valid JSON")
	}

	return input, nil
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
