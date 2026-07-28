package health

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

type checkFunc func(context.Context) error

func (check checkFunc) Check(ctx context.Context) error {
	return check(ctx)
}

type resolverFunc func(context.Context, string) ([]string, error)

func (resolve resolverFunc) LookupHost(ctx context.Context, name string) ([]string, error) {
	return resolve(ctx, name)
}

func TestMemcachedCheck(t *testing.T) {
	address := startMemcached(t)
	if err := NewMemcached(address).Check(t.Context()); err != nil {
		t.Fatalf("Memcached.Check() error = %v", err)
	}
}

func TestHealthResponses(t *testing.T) {
	tests := []struct {
		name      string
		cache     Checker
		dns       Checker
		wantCode  int
		wantLog   bool
		forbidden string
	}{
		{
			name:     "healthy",
			cache:    NewMemcached(startMemcached(t)),
			dns:      checkFunc(func(context.Context) error { return nil }),
			wantCode: http.StatusOK,
		},
		{
			name:      "cache unavailable",
			cache:     checkFunc(func(context.Context) error { return errors.New("secret-cache.example:65100") }),
			dns:       checkFunc(func(context.Context) error { return nil }),
			wantCode:  http.StatusServiceUnavailable,
			wantLog:   true,
			forbidden: "secret-cache.example",
		},
		{
			name:      "DNS unavailable",
			cache:     checkFunc(func(context.Context) error { return nil }),
			dns:       checkFunc(func(context.Context) error { return errors.New("secret-dns.example") }),
			wantCode:  http.StatusServiceUnavailable,
			wantLog:   true,
			forbidden: "secret-dns.example",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var logs bytes.Buffer
			handler := testHandler(time.Second, &logs, test.cache, test.dns)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health", http.NoBody))

			if response.Code != test.wantCode {
				t.Fatalf("status = %d, want %d", response.Code, test.wantCode)
			}
			if test.forbidden != "" && (strings.Contains(response.Body.String(), test.forbidden) ||
				strings.Contains(logs.String(), test.forbidden)) {
				t.Fatalf("response or log disclosed dependency detail: body=%q log=%q", response.Body, logs.String())
			}
			lines := strings.Count(strings.TrimSpace(logs.String()), "\n")
			if test.wantLog {
				if lines != 0 || strings.TrimSpace(logs.String()) == "" {
					t.Fatalf("error log count != 1: %q", logs.String())
				}
				var record map[string]any
				if err := json.Unmarshal(logs.Bytes(), &record); err != nil {
					t.Fatalf("error log is not JSON: %v", err)
				}
				if record["error.type"] != "unavailable" {
					t.Fatalf("error.type = %v, want unavailable", record["error.type"])
				}
			} else if logs.Len() != 0 {
				t.Fatalf("healthy request emitted log: %q", logs.String())
			}
		})
	}
}

func TestHealthTimeout(t *testing.T) {
	checker := checkFunc(func(ctx context.Context) error {
		<-ctx.Done()

		return ctx.Err()
	})
	var logs bytes.Buffer
	handler := testHandler(10*time.Millisecond, &logs, checker, checkFunc(func(context.Context) error { return nil }))
	started := time.Now()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health", http.NoBody))

	if response.Code != http.StatusServiceUnavailable || time.Since(started) > time.Second {
		t.Fatalf("timeout response status=%d duration=%s", response.Code, time.Since(started))
	}
	if !strings.Contains(logs.String(), `"error.type":"timeout"`) {
		t.Fatalf("timeout log = %q", logs.String())
	}
}

func TestHealthCancellationStopsDependencyFlow(t *testing.T) {
	entered := make(chan struct{})
	observed := make(chan error, 1)
	cache := checkFunc(func(ctx context.Context) error {
		close(entered)
		<-ctx.Done()
		observed <- ctx.Err()

		return ctx.Err()
	})
	var dnsCalls atomic.Int64
	dns := checkFunc(func(context.Context) error {
		dnsCalls.Add(1)

		return nil
	})
	handler := testHandler(time.Second, io.Discard, cache, dns)
	ctx, cancel := context.WithCancel(t.Context())
	request := httptest.NewRequest(http.MethodGet, "/health", http.NoBody).WithContext(ctx)
	response := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(response, request)
		close(done)
	}()

	<-entered
	cancel()
	<-done
	if !errors.Is(<-observed, context.Canceled) || dnsCalls.Load() != 0 {
		t.Fatalf("cancellation was not propagated or DNS was called: dnsCalls=%d", dnsCalls.Load())
	}
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", response.Code)
	}
}

func TestHealthRejectsUnsupportedRequests(t *testing.T) {
	var checks atomic.Int64
	checker := checkFunc(func(context.Context) error {
		checks.Add(1)

		return nil
	})
	handler := testHandler(time.Second, io.Discard, checker, checker)
	mux := http.NewServeMux()
	mux.Handle("/health", handler)

	tests := []struct {
		method string
		path   string
		status int
	}{
		{method: http.MethodPost, path: "/health", status: http.StatusMethodNotAllowed},
		{method: http.MethodHead, path: "/health", status: http.StatusMethodNotAllowed},
		{method: http.MethodGet, path: "/ready", status: http.StatusNotFound},
		{method: http.MethodGet, path: "/snapshot", status: http.StatusNotFound},
	}

	for _, test := range tests {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(test.method, test.path, http.NoBody))
		if response.Code != test.status {
			t.Errorf("%s %s status = %d, want %d", test.method, test.path, response.Code, test.status)
		}
	}
	if checks.Load() != 0 {
		t.Fatalf("dependency checks = %d, want 0", checks.Load())
	}
}

func TestDNSCheck(t *testing.T) {
	resolver := resolverFunc(func(_ context.Context, name string) ([]string, error) {
		if name != "a.4cdn.org" {
			return nil, fmt.Errorf("unexpected name %q", name)
		}

		return []string{"192.0.2.1"}, nil
	})
	if err := NewDNS("a.4cdn.org", resolver).Check(t.Context()); err != nil {
		t.Fatalf("DNS.Check() error = %v", err)
	}
}

func testHandler(timeout time.Duration, output io.Writer, cache, dns Checker) *Handler {
	logger := slog.New(slog.NewJSONHandler(output, nil))
	tracer := tracenoop.NewTracerProvider().Tracer("test")

	return NewHandler(timeout, logger, tracer, cache, dns)
}

func startMemcached(t *testing.T) string {
	t.Helper()
	var listener net.Listener
	var err error
	for port := 65190; port <= 65198; port++ {
		listener, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			break
		}
	}
	if err != nil {
		t.Fatalf("listen for test Memcached: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer connection.Close() //nolint:errcheck // Test server teardown has no recovery action.
		request, readErr := bufio.NewReader(connection).ReadString('\n')
		if readErr == nil && request == "version\r\n" {
			_, _ = io.WriteString(connection, "VERSION 1.6.45\r\n")
		}
	}()

	return listener.Addr().String()
}
