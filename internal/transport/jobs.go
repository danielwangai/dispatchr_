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
