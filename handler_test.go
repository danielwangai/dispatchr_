package dispatchr_test

import (
	"context"
	"errors"
	"testing"

	"github.com/danielwangai/dispatchr"
)

func TestHandlerFuncExecutesFunction(t *testing.T) {
	// Arrange a function-backed handler that records the job it receives.
	called := false
	handler := dispatchr.HandlerFunc(func(_ context.Context, job dispatchr.Job) error {
		called = job.ID == "job-123"
		return nil
	})

	// Act: execute it through the Handler interface method.
	err := handler.Handle(context.Background(), dispatchr.Job{ID: "job-123"})

	// Assert: HandlerFunc behaves like an ordinary Handler implementation.
	if err != nil {
		t.Fatalf("handle job: %v", err)
	}
	if !called {
		t.Fatal("handler function did not receive the job")
	}
}

func TestRegistryRegisterAndLookup(t *testing.T) {
	// Arrange an empty registry and a handler supplied by a Dispatchr user.
	registry := dispatchr.NewRegistry()
	handler := dispatchr.HandlerFunc(func(context.Context, dispatchr.Job) error { return nil })

	// Act: register and look up the application's send_email handler.
	if err := registry.Register("send_email", handler); err != nil {
		t.Fatalf("register handler: %v", err)
	}
	got, err := registry.HandlerFor("send_email")

	// Assert: a worker can retrieve the exact registered handler by job type.
	if err != nil {
		t.Fatalf("look up handler: %v", err)
	}
	if got == nil {
		t.Fatal("looked up handler is nil")
	}
}

func TestRegistryRejectsInvalidRegistration(t *testing.T) {
	// Arrange an empty registry and one valid handler.
	registry := dispatchr.NewRegistry()
	handler := dispatchr.HandlerFunc(func(context.Context, dispatchr.Job) error { return nil })

	// Act and assert: reject a missing type, a missing handler, and a duplicate.
	if err := registry.Register("", handler); !errors.Is(err, dispatchr.ErrJobTypeRequired) {
		t.Fatalf("empty type error = %v, want ErrJobTypeRequired", err)
	}
	if err := registry.Register("send_email", nil); !errors.Is(err, dispatchr.ErrHandlerRequired) {
		t.Fatalf("nil handler error = %v, want ErrHandlerRequired", err)
	}
	if err := registry.Register("send_email", handler); err != nil {
		t.Fatalf("register handler: %v", err)
	}
	if err := registry.Register("send_email", handler); !errors.Is(err, dispatchr.ErrHandlerAlreadyRegistered) {
		t.Fatalf("duplicate handler error = %v, want ErrHandlerAlreadyRegistered", err)
	}
}

func TestRegistryReturnsErrorForUnknownHandler(t *testing.T) {
	// Arrange an empty registry.
	registry := dispatchr.NewRegistry()

	// Act: look up a job type the application did not register.
	_, err := registry.HandlerFor("generate_report")

	// Assert: the worker can distinguish missing configuration from execution failure.
	if !errors.Is(err, dispatchr.ErrHandlerNotFound) {
		t.Fatalf("lookup error = %v, want ErrHandlerNotFound", err)
	}
}
