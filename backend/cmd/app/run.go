package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"git.disroot.org/federico-paolillo/four-visor.git/config"
	"git.disroot.org/federico-paolillo/four-visor.git/telemetry"
)

const (
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 10 * time.Second
	writeTimeout      = 30 * time.Second
	idleTimeout       = 120 * time.Second
	shutdownTimeout   = 10 * time.Second
)

// applicationError preserves an operational cause behind a value-free process diagnostic.
type applicationError struct {
	operation string
	cause     error
}

// inFlightHandlers closes the telemetry entry gate before joining handlers already inside it.
type inFlightHandlers struct {
	mu       sync.Mutex
	wait     sync.WaitGroup
	stopping bool
}

func (err *applicationError) Error() string {
	return err.operation
}

func (err *applicationError) Unwrap() error {
	return err.cause
}

// run composes the backend HTTP service and owns its bounded server lifecycle.
func run(parent context.Context, stderr io.Writer) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading configuration: %w", err)
	}

	providers, err := telemetry.New(parent, cfg.OTLPEndpoint, stderr)
	if err != nil {
		return &applicationError{operation: "creating OpenTelemetry", cause: err}
	}
	defer shutdownTelemetry(context.WithoutCancel(parent), providers)

	application, err := newApplication(cfg, providers)
	if err != nil {
		return &applicationError{operation: "creating application", cause: err}
	}

	server := &http.Server{
		Addr:              cfg.ServerAddress,
		Handler:           application.handler,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}

	var listenConfig net.ListenConfig

	listener, err := listenConfig.Listen(parent, "tcp", cfg.ServerAddress)
	if err != nil {
		return &applicationError{operation: "listening for HTTP", cause: err}
	}
	defer listener.Close() //nolint:errcheck // Shutdown or Serve owns the meaningful close result.

	return serve(parent, server, listener, &application)
}

func serve(parent context.Context, server *http.Server, listener net.Listener, application *application) error {
	signalCtx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stop()

	shutdown := func(server *http.Server) error {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), shutdownTimeout)
		defer cancel()

		return server.Shutdown(ctx)
	}

	return serveHTTP(signalCtx, server, listener, application.scheduler.Run, shutdown)
}

func serveHTTP(
	parent context.Context,
	server *http.Server,
	listener net.Listener,
	runScheduler func(context.Context),
	shutdown func(*http.Server) error,
) error {
	ctx, cancelApplication := context.WithCancel(parent)

	var handlers inFlightHandlers

	server.Handler = handlers.track(server.Handler)

	var scheduler sync.WaitGroup
	scheduler.Go(func() { runScheduler(ctx) })

	serverError := make(chan error, 1)
	go func() {
		serverError <- server.Serve(listener)
	}()

	var serveError error

	serverResultRead := false

	select {
	case serveError = <-serverError:
		serverResultRead = true
	case <-ctx.Done():
	}

	handlers.stop()
	cancelApplication()

	shutdownError := shutdown(server)

	var closeError error
	if shutdownError != nil {
		closeError = server.Close()
	}

	if !serverResultRead {
		serveError = <-serverError
	}

	scheduler.Wait()
	handlers.wait.Wait()

	return serverLifecycleError(serveError, shutdownError, closeError)
}

func (handlers *inFlightHandlers) track(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		handlers.mu.Lock()
		if handlers.stopping {
			handlers.mu.Unlock()
			http.Error(writer, "service unavailable", http.StatusServiceUnavailable)

			return
		}

		handlers.wait.Add(1)

		handlers.mu.Unlock()
		defer handlers.wait.Done()

		next.ServeHTTP(writer, request)
	})
}

func (handlers *inFlightHandlers) stop() {
	handlers.mu.Lock()
	handlers.stopping = true
	handlers.mu.Unlock()
}

func serverLifecycleError(serveError, shutdownError, closeError error) error {
	if errors.Is(serveError, http.ErrServerClosed) {
		serveError = nil
	}

	cause := errors.Join(
		wrapOperation("serving HTTP", serveError),
		wrapOperation("shutting down HTTP", shutdownError),
		wrapOperation("forcing HTTP connections closed", closeError),
	)
	if cause == nil {
		return nil
	}

	return &applicationError{operation: "stopping HTTP", cause: cause}
}

func wrapOperation(operation string, err error) error {
	if err == nil {
		return nil
	}

	return fmt.Errorf("%s: %w", operation, err)
}

func shutdownTelemetry(parent context.Context, providers *telemetry.Providers) {
	ctx, cancel := context.WithTimeout(parent, shutdownTimeout)
	defer cancel()

	err := providers.Shutdown(ctx)
	if err != nil {
		providers.Slog.Error("OpenTelemetry shutdown failed",
			slog.String("error.type", "unavailable"),
		)
	}
}
