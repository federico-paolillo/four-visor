package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"git.disroot.org/federico-paolillo/four-visor.git/config"
	"git.disroot.org/federico-paolillo/four-visor.git/telemetry"
	metricsdk "go.opentelemetry.io/otel/sdk/metric"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
)

func TestApplicationRegistersOnlyInternalSnapshotRoute(t *testing.T) {
	tracerProvider := tracesdk.NewTracerProvider()
	t.Cleanup(func() { _ = tracerProvider.Shutdown(t.Context()) })
	meterProvider := metricsdk.NewMeterProvider()
	t.Cleanup(func() { _ = meterProvider.Shutdown(t.Context()) })
	providers := &telemetry.Providers{
		Tracer: tracerProvider,
		Meter:  meterProvider,
		Slog:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	application, err := newApplication(config.Config{
		HealthTimeout:    time.Second,
		MemcachedAddress: "127.0.0.1:65198",
		DNSName:          "a.4cdn.org",
		Acquisition: config.Acquisition{
			RateInterval:   time.Second,
			MaxConcurrency: 1,
			RequestTimeout: time.Second,
			MaxRetries:     0,
			RetryBackoff:   time.Second,
			UserAgent:      "4Visor/0123456789abcdef0123456789abcdef01234567",
		},
		Synchronization: config.Synchronization{
			Interval:                time.Hour,
			FailedResourceTolerance: 10,
		},
	}, providers)
	if err != nil {
		t.Fatalf("newApplication() error = %v", err)
	}

	response := httptest.NewRecorder()
	application.handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/snapshot", http.NoBody))
	if response.Code != http.StatusMethodNotAllowed || response.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("POST /snapshot status=%d Allow=%q", response.Code, response.Header().Get("Allow"))
	}

	for _, path := range []string{"/api/snapshot", "/snapshot/", "/manifest", "/blocks/0", "/boards", "/threads/1"} {
		response = httptest.NewRecorder()
		application.handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, http.NoBody))
		if response.Code != http.StatusNotFound {
			t.Fatalf("GET %s status = %d, want 404", path, response.Code)
		}
	}
}

func TestServeHTTPDrainsBlockedHandlerAfterUnexpectedAcceptError(t *testing.T) {
	listener := appTestListener(t)
	failAccept := make(chan struct{})
	acceptFailure := errors.New("accept failed")
	listenerWithFailure := &failAfterFirstAccept{
		Listener: listener,
		fail:     failAccept,
		err:      acceptFailure,
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	exited := make(chan struct{})
	server := &http.Server{Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		close(entered)
		<-release
		close(exited)
	})}
	schedulerCanceled := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		result <- serveHTTP(
			t.Context(),
			server,
			listenerWithFailure,
			func(ctx context.Context) {
				<-ctx.Done()
				close(schedulerCanceled)
			},
			func(server *http.Server) error { return server.Shutdown(t.Context()) },
		)
	}()

	requestDone := requestAppServer(listener.Addr().String())
	<-entered
	close(failAccept)
	<-schedulerCanceled

	select {
	case err := <-result:
		t.Fatalf("serveHTTP returned before handler drained: %v", err)
	default:
	}

	close(release)
	<-exited
	if err := <-result; !errors.Is(err, acceptFailure) {
		t.Fatalf("serveHTTP error = %v, want accept failure", err)
	}
	<-requestDone
}

func TestServeHTTPForcesBlockedHandlerClosedAfterShutdownTimeout(t *testing.T) {
	listener := appTestListener(t)
	entered := make(chan struct{})
	canceled := make(chan struct{})
	release := make(chan struct{})
	exited := make(chan struct{})
	server := &http.Server{Handler: http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		close(entered)
		<-request.Context().Done()
		close(canceled)
		<-release
		close(exited)
	})}
	parent, cancel := context.WithCancel(t.Context())
	schedulerCanceled := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		result <- serveHTTP(
			parent,
			server,
			listener,
			func(ctx context.Context) {
				<-ctx.Done()
				close(schedulerCanceled)
			},
			func(server *http.Server) error {
				expired, stop := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
				defer stop()

				return server.Shutdown(expired)
			},
		)
	}()

	requestDone := requestAppServer(listener.Addr().String())
	<-entered
	cancel()
	<-canceled
	<-schedulerCanceled

	select {
	case err := <-result:
		t.Fatalf("serveHTTP returned before forced-close handler exited: %v", err)
	default:
	}

	close(release)
	<-exited
	if err := <-result; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("serveHTTP error = %v, want shutdown deadline", err)
	}
	<-requestDone
}

func TestServerLifecycleErrorPreservesEveryCause(t *testing.T) {
	serveFailure := errors.New("serve failed")
	shutdownFailure := errors.New("shutdown failed")
	closeFailure := errors.New("close failed")

	err := serverLifecycleError(serveFailure, shutdownFailure, closeFailure)
	for _, cause := range []error{serveFailure, shutdownFailure, closeFailure} {
		if !errors.Is(err, cause) {
			t.Fatalf("serverLifecycleError() = %v, missing %v", err, cause)
		}
	}
}

type failAfterFirstAccept struct {
	net.Listener
	accepted bool
	fail     <-chan struct{}
	err      error
}

func (listener *failAfterFirstAccept) Accept() (net.Conn, error) {
	if listener.accepted {
		<-listener.fail

		return nil, listener.err
	}

	connection, err := listener.Listener.Accept()
	listener.accepted = err == nil

	return connection, err
}

func appTestListener(t *testing.T) net.Listener {
	t.Helper()
	for port := 65110; port <= 65119; port++ {
		listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			t.Cleanup(func() { _ = listener.Close() })

			return listener
		}
	}
	t.Fatal("no application test port available in 65110-65119")

	return nil
}

func requestAppServer(address string) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)

		response, err := http.Get("http://" + address) //nolint:noctx // Server cancellation owns this test request.
		if err == nil {
			_ = response.Body.Close()
		}
	}()

	return done
}
