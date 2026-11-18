package transport

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/danielwangai/dispatchr/internal/config"
	"github.com/danielwangai/dispatchr/internal/logging"
)

// Server owns the application's HTTP transport dependencies.
type Server struct {
	Router     *Router
	HTTPServer *http.Server
}

// NewServer creates an HTTP server around a new application router.
func NewServer(port string) *Server {
	router := NewRouter()
	return &Server{
		Router: router,
		HTTPServer: &http.Server{
			Addr:         fmt.Sprintf(":%s", port),
			Handler:      router,
			IdleTimeout:  time.Minute,
			ReadTimeout:  10 * time.Second,
			WriteTimeout: 30 * time.Second,
		},
	}
}

// RunServer composes the application's infrastructure and runs the HTTP
// server until it receives an interrupt or termination signal.
func RunServer() error {
	appConfig, err := config.FromEnv()
	if err != nil {
		return fmt.Errorf("load application configuration: %w", err)
	}

	// logs
	log, err := logging.New(appConfig.Logging)
	if err != nil {
		return fmt.Errorf("configure logger: %w", err)
	}

	// Cancel on Ctrl+C or a container shutdown signal.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	server := NewServer(appConfig.WebServer.Port)

	server.Router.InitializeRoutes(ctx, appConfig, log)

	serverErrors := make(chan error, 1)
	go func() {
		log.Info("server listening", logging.String("address", server.HTTPServer.Addr))
		if err := server.HTTPServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrors <- err
		}
	}()

	select {
	case err := <-serverErrors:
		log.Error("server failed", logging.Err(err))
		return err
	case <-ctx.Done():
		log.Info("shutdown signal received")
	}

	// Allow active requests up to five seconds to finish.
	shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.HTTPServer.Shutdown(shutdownContext); err != nil {
		log.Error("graceful shutdown failed", logging.Err(err))
		return fmt.Errorf("shutdown server: %w", err)
	}
	log.Info("server stopped")
	return nil
}
