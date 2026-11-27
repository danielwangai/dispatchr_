//go:build integration

package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/danielwangai/dispatchr/internal/config"
	"github.com/danielwangai/dispatchr/internal/svc"
	"github.com/danielwangai/dispatchr/internal/transport"
	"github.com/danielwangai/dispatchr/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestJobAPISubmitAndGet(t *testing.T) {
	// Arrange a real router, service, and PostgreSQL store.
	pool := testPool(t)
	clearJobs(t, pool)
	router := transport.NewRouter()
	router.InitializeRoutes(context.Background(), testConfig(), nil, svc.New(postgres.NewStore(pool)))

	// Act: submit a job through the /jobs HTTP endpoint.
	createRequest := httptest.NewRequest(http.MethodPost, "/jobs", bytes.NewBufferString(`{"type":"send_email","payload":{"to":"customer@example.com"}}`))
	createResponse := httptest.NewRecorder()
	router.ServeHTTP(createResponse, createRequest)
	if createResponse.Code != http.StatusAccepted {
		t.Fatalf("create status = %d, want %d; body=%s", createResponse.Code, http.StatusAccepted, createResponse.Body)
	}

	var created struct {
		JobID  string `json:"job_id"`
		Status string `json:"status"`
	}
	if err := json.NewDecoder(createResponse.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if created.JobID == "" || created.Status != "pending" {
		t.Fatalf("create response = %#v, want pending job with ID", created)
	}

	// Act: retrieve that same persisted job through the HTTP endpoint.
	getRequest := httptest.NewRequest(http.MethodGet, "/jobs/"+created.JobID, nil)
	getResponse := httptest.NewRecorder()
	router.ServeHTTP(getResponse, getRequest)

	// Assert: data survives the full transport → service → PostgreSQL path.
	if getResponse.Code != http.StatusOK {
		t.Fatalf("get status = %d, want %d; body=%s", getResponse.Code, http.StatusOK, getResponse.Body)
	}
	var got struct {
		JobID       string          `json:"job_id"`
		Type        string          `json:"type"`
		Payload     json.RawMessage `json:"payload"`
		Status      string          `json:"status"`
		Attempts    int             `json:"attempts"`
		MaxAttempts int             `json:"max_attempts"`
	}
	if err := json.NewDecoder(getResponse.Body).Decode(&got); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	if got.JobID != created.JobID || got.Type != "send_email" || string(got.Payload) != `{"to":"customer@example.com"}` || got.Status != "pending" || got.Attempts != 0 || got.MaxAttempts != 3 {
		t.Fatalf("get response = %#v, want persisted pending job", got)
	}
}

func TestJobAPIGetMissingJob(t *testing.T) {
	// Arrange the real application stack with an empty jobs table.
	pool := testPool(t)
	clearJobs(t, pool)
	router := transport.NewRouter()
	router.InitializeRoutes(context.Background(), testConfig(), nil, svc.New(postgres.NewStore(pool)))

	// Act: retrieve a syntactically valid ID that is not stored.
	request := httptest.NewRequest(http.MethodGet, "/jobs/00000000-0000-0000-0000-0000000000aa", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	// Assert: the HTTP API reports an absent job as a not-found resource.
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusNotFound, response.Body)
	}
}

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	databaseURL := os.Getenv("DISPATCHR_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("DISPATCHR_TEST_DATABASE_URL must be set")
	}

	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("connect test database: %v", err)
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

func testConfig() *config.AppConfig {
	return &config.AppConfig{WebServer: &config.WebServerConfig{Port: "8080"}}
}
