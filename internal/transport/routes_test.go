package transport

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielwangai/dispatchr/internal/config"
)

func TestHelloWorldHandler(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()

	router := NewRouter()
	router.InitializeRoutes(request.Context(), testConfig(), nil)
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, response.Code)
	}
	if got, want := response.Body.String(), "{\"message\":\"Hello World\"}\n"; got != want {
		t.Errorf("expected body %q, got %q", want, got)
	}
}

func testConfig() *config.AppConfig {
	return &config.AppConfig{WebServer: &config.WebServerConfig{Port: "8080"}}
}
