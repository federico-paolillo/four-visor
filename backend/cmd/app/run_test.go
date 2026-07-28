package main

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"git.disroot.org/federico-paolillo/four-visor.git/config"
	"git.disroot.org/federico-paolillo/four-visor.git/telemetry"
	metricsdk "go.opentelemetry.io/otel/sdk/metric"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
)

func TestApplicationHandlerRegistersOnlyInternalSnapshotRoute(t *testing.T) {
	tracerProvider := tracesdk.NewTracerProvider()
	t.Cleanup(func() { _ = tracerProvider.Shutdown(t.Context()) })
	meterProvider := metricsdk.NewMeterProvider()
	t.Cleanup(func() { _ = meterProvider.Shutdown(t.Context()) })
	providers := &telemetry.Providers{
		Tracer: tracerProvider,
		Meter:  meterProvider,
		Slog:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	handler, err := applicationHandler(config.Config{
		HealthTimeout:    time.Second,
		MemcachedAddress: "127.0.0.1:65198",
		DNSName:          "a.4cdn.org",
	}, providers)
	if err != nil {
		t.Fatalf("applicationHandler() error = %v", err)
	}

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/snapshot", http.NoBody))
	if response.Code != http.StatusMethodNotAllowed || response.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("POST /snapshot status=%d Allow=%q", response.Code, response.Header().Get("Allow"))
	}

	for _, path := range []string{"/api/snapshot", "/snapshot/", "/manifest", "/blocks/0", "/boards", "/threads/1"} {
		response = httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, http.NoBody))
		if response.Code != http.StatusNotFound {
			t.Fatalf("GET %s status = %d, want 404", path, response.Code)
		}
	}
}
