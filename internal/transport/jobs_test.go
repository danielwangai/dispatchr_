package transport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danielwangai/dispatchr"
	"github.com/danielwangai/dispatchr/internal/svc"
	"github.com/gorilla/mux"
)

func TestCreateJobHandlerSubmitsRequestToService(t *testing.T) {
	// Arrange a fake service so this test covers HTTP behavior without PostgreSQL.
	service := &fakeJobService{
		submittedJob: dispatchr.Job{ID: "job-123", Status: dispatchr.StatusPending},
	}
	request := httptest.NewRequest(http.MethodPost, "/jobs", strings.NewReader(`{"type":"send_email","payload":{"to":"customer@example.com"}}`))
	response := httptest.NewRecorder()

	// Act: submit a valid HTTP request.
	createJobHandler(service).ServeHTTP(response, request)

	// Assert: transport maps JSON to service input and returns the accepted job.
	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusAccepted)
	}
	if service.submitInput.Type != "send_email" || string(service.submitInput.Payload) != `{"to":"customer@example.com"}` {
		t.Fatalf("service input = %#v, want request values", service.submitInput)
	}

	var body createJobResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.JobID != "job-123" || body.Status != dispatchr.StatusPending {
		t.Fatalf("response = %#v, want accepted job", body)
	}
}

func TestCreateJobHandlerRejectsMalformedRequest(t *testing.T) {
	// Arrange malformed JSON. The service should not be called for a malformed HTTP request.
	service := &fakeJobService{}
	request := httptest.NewRequest(http.MethodPost, "/jobs", strings.NewReader(`{"type":`))
	response := httptest.NewRecorder()

	// Act.
	createJobHandler(service).ServeHTTP(response, request)

	// Assert.
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
	if service.submitCalled {
		t.Fatal("service.SubmitJob was called for malformed JSON")
	}
}

func TestCreateJobHandlerMapsServiceValidationError(t *testing.T) {
	// Arrange valid JSON whose business rule is rejected by the service.
	service := &fakeJobService{submitErr: svc.ErrInvalidMaxAttempts}
	request := httptest.NewRequest(http.MethodPost, "/jobs", strings.NewReader(`{"type":"send_email","payload":{},"max_attempts":0}`))
	response := httptest.NewRecorder()

	// Act.
	createJobHandler(service).ServeHTTP(response, request)

	// Assert: service validation is exposed as a client error.
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
}

func TestGetJobHandlerReturnsServiceJob(t *testing.T) {
	// Arrange a fake job returned by the application layer.
	id := "00000000-0000-0000-0000-000000000001"
	createdAt := time.Date(2026, time.November, 25, 12, 0, 0, 0, time.UTC)
	service := &fakeJobService{job: dispatchr.Job{
		ID: id, Type: "send_email", Payload: json.RawMessage(`{"to":"customer@example.com"}`),
		Status: dispatchr.StatusPending, MaxAttempts: 3, CreatedAt: createdAt, UpdatedAt: createdAt,
	}}
	request := httptest.NewRequest(http.MethodGet, "/jobs/"+id, nil)
	request = withRouteID(request, id)
	response := httptest.NewRecorder()

	// Act.
	getJobHandler(service).ServeHTTP(response, request)

	// Assert: transport serializes the domain job as its HTTP response.
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if service.getID != id {
		t.Fatalf("service requested ID = %q, want %q", service.getID, id)
	}
}

func TestGetJobHandlerRejectsMalformedIDBeforeService(t *testing.T) {
	// Arrange a non-UUID path value.
	service := &fakeJobService{}
	request := withRouteID(httptest.NewRequest(http.MethodGet, "/jobs/not-a-uuid", nil), "not-a-uuid")
	response := httptest.NewRecorder()

	// Act.
	getJobHandler(service).ServeHTTP(response, request)

	// Assert: invalid HTTP path data never reaches the service.
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
	if service.getCalled {
		t.Fatal("service.GetJob was called for malformed ID")
	}
}

func TestGetJobHandlerMapsMissingJob(t *testing.T) {
	// Arrange a valid UUID for which the service finds no persisted job.
	id := "00000000-0000-0000-0000-000000000002"
	service := &fakeJobService{getErr: dispatchr.ErrNotFound}
	request := withRouteID(httptest.NewRequest(http.MethodGet, "/jobs/"+id, nil), id)
	response := httptest.NewRecorder()

	// Act.
	getJobHandler(service).ServeHTTP(response, request)

	// Assert: an absent resource is different from an invalid path value.
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
}

func withRouteID(request *http.Request, id string) *http.Request {
	return mux.SetURLVars(request, map[string]string{"id": id})
}

type fakeJobService struct {
	submitInput  svc.SubmitJobInput
	submittedJob dispatchr.Job
	submitErr    error
	submitCalled bool
	job          dispatchr.Job
	getErr       error
	getID        string
	getCalled    bool
}

func (s *fakeJobService) SubmitJob(_ context.Context, input svc.SubmitJobInput) (dispatchr.Job, error) {
	s.submitCalled = true
	s.submitInput = input
	return s.submittedJob, s.submitErr
}

func (s *fakeJobService) GetJob(_ context.Context, id string) (dispatchr.Job, error) {
	s.getCalled = true
	s.getID = id
	return s.job, s.getErr
}

var _ svc.JobService = (*fakeJobService)(nil)
