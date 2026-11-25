package transport

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/danielwangai/dispatchr/internal/config"
	"github.com/danielwangai/dispatchr/internal/logging"
	"github.com/danielwangai/dispatchr/internal/svc"

	"github.com/gorilla/mux"
)

// Router wraps mux.Router.
type Router struct {
	*mux.Router
}

// NewRouter creates a new Router.
func NewRouter() *Router {
	return &Router{Router: mux.NewRouter()}
}

// InitializeRoutes registers every HTTP endpoint for the application.
// Dependencies are supplied here so handlers remain straightforward to test.
func (r *Router) InitializeRoutes(ctx context.Context, appConfig *config.AppConfig, logger logging.Logger, jobService svc.JobService) {
	if logger == nil {
		logger = logging.Nop()
	}

	r.Use(requestLogger(ctx, logger))
	r.HandleFunc("/", helloWorldHandler).Methods(http.MethodGet)
	r.HandleFunc("/health", healthHandler(appConfig)).Methods(http.MethodGet)
	r.HandleFunc("/jobs", createJobHandler(jobService)).Methods(http.MethodPost)
	r.HandleFunc("/jobs/{id}", getJobHandler(jobService)).Methods(http.MethodGet)
}

func helloWorldHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"message": "Hello World"})
}

func healthHandler(appConfig *config.AppConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status": "ok",
			"port":   appConfig.WebServer.Port,
		})
	}
}

func requestLogger(ctx context.Context, logger logging.Logger) mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			startedAt := time.Now()
			response := &responseWriter{ResponseWriter: w, status: http.StatusOK}
			select {
			case <-ctx.Done():
				http.Error(response, "service unavailable", http.StatusServiceUnavailable)
				return
			default:
			}

			next.ServeHTTP(response, request)
			logger.Info("request complete",
				logging.String("method", request.Method),
				logging.String("path", request.URL.Path),
				logging.Int("status", response.status),
				logging.Duration("duration", time.Since(startedAt)),
			)
		})
	}
}

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (w *responseWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
