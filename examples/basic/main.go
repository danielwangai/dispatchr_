// The basic example runs Dispatchr's HTTP API and one worker in one process.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/danielwangai/dispatchr"
	"github.com/danielwangai/dispatchr/internal/config"
	"github.com/danielwangai/dispatchr/internal/logging"
	"github.com/danielwangai/dispatchr/internal/svc"
	"github.com/danielwangai/dispatchr/internal/transport"
	"github.com/danielwangai/dispatchr/postgres"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "example stopped with error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// Step 1: load local configuration and create application infrastructure.
	appConfig, err := config.FromEnv()
	if err != nil {
		return fmt.Errorf("load application configuration: %w", err)
	}
	logger, err := logging.New(appConfig.Logging)
	if err != nil {
		return fmt.Errorf("configure logger: %w", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := postgres.NewPool(ctx, appConfig.DB.ConnectionString())
	if err != nil {
		return fmt.Errorf("connect to PostgreSQL: %w", err)
	}
	defer pool.Close()

	// Step 2: register the application's background work by job type.
	registry, err := registerHandlers(logger)
	if err != nil {
		return err
	}
	store := postgres.NewStore(pool)

	// Step 3: build the HTTP API that accepts and exposes persisted jobs.
	server := transport.NewServer(appConfig.WebServer.Port)
	server.Router.InitializeRoutes(ctx, appConfig, logger, svc.New(store))
	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("API listening", logging.String("address", server.HTTPServer.Addr))
		if err := server.HTTPServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrors <- err
		}
	}()

	// Step 4: start a worker that claims and handles jobs from the same store.
	workerErrors := make(chan error, 1)
	go func() {
		worker, err := dispatchr.NewWorker(store, registry, dispatchr.WithErrorReporter(func(err error) {
			logger.Error("worker cycle failed; retrying", logging.Err(err))
		}))
		if err != nil {
			workerErrors <- err
			return
		}
		if err := worker.Run(ctx, time.Second); err != nil {
			workerErrors <- err
		}
	}()

	// Step 5: stop both components cleanly on a signal or an unexpected error.
	select {
	case err := <-serverErrors:
		stop()
		return fmt.Errorf("serve HTTP API: %w", err)
	case err := <-workerErrors:
		stop()
		return fmt.Errorf("run worker: %w", err)
	case <-ctx.Done():
	}

	shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.HTTPServer.Shutdown(shutdownContext); err != nil {
		return fmt.Errorf("shutdown HTTP API: %w", err)
	}
	logger.Info("example stopped")
	return nil
}

func registerHandlers(logger logging.Logger) (*dispatchr.Registry, error) {
	// Step 1: create the application's job-type registry.
	registry := dispatchr.NewRegistry()

	// Step 2: register a simulated email job.
	if err := registry.Register("send_email", dispatchr.HandlerFunc(func(_ context.Context, job dispatchr.Job) error {
		logger.Info("simulated email sent", logging.String("job_id", job.ID))
		return nil
	})); err != nil {
		return nil, fmt.Errorf("register send_email handler: %w", err)
	}

	// Step 3: register a simulated report-generation job.
	if err := registry.Register("generate_report", dispatchr.HandlerFunc(func(_ context.Context, job dispatchr.Job) error {
		logger.Info("simulated report generated", logging.String("job_id", job.ID))
		return nil
	})); err != nil {
		return nil, fmt.Errorf("register generate_report handler: %w", err)
	}

	return registry, nil
}
