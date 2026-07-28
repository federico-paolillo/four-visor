// Package health implements the backend's shallow Memcached and DNS health boundary.
package health

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const (
	memcachedDependency = "memcached"
	dnsDependency       = "dns"
)

var errInvalidMemcachedResponse = errors.New("invalid Memcached response")

// Checker is the dependency seam consumed by the health handler.
type Checker interface {
	Check(ctx context.Context) error
}

// Memcached verifies reachability with the smallest side-effect-free protocol command.
type Memcached struct {
	address string
	dialer  net.Dialer
}

// NewMemcached creates a Memcached reachability checker.
func NewMemcached(address string) *Memcached {
	return &Memcached{address: address}
}

// Check opens a bounded connection and requires a valid Memcached VERSION response.
func (checker *Memcached) Check(ctx context.Context) error {
	connection, err := checker.dialer.DialContext(ctx, "tcp", checker.address)
	if err != nil {
		return fmt.Errorf("connecting to Memcached: %w", err)
	}
	defer connection.Close() //nolint:errcheck // The health operation has no recovery action for close failure.

	stopClose := context.AfterFunc(ctx, func() { _ = connection.Close() })
	defer stopClose()

	if deadline, ok := ctx.Deadline(); ok {
		err = connection.SetDeadline(deadline)
		if err != nil {
			return fmt.Errorf("setting Memcached deadline: %w", err)
		}
	}

	_, err = io.WriteString(connection, "version\r\n")
	if err != nil {
		return contextError(ctx, fmt.Errorf("querying Memcached: %w", err))
	}

	response, err := bufio.NewReader(connection).ReadString('\n')
	if err != nil {
		return contextError(ctx, fmt.Errorf("reading Memcached response: %w", err))
	}

	if !strings.HasPrefix(response, "VERSION ") || !strings.HasSuffix(response, "\r\n") {
		return errInvalidMemcachedResponse
	}

	return nil
}

// Resolver is the DNS operation consumed by the DNS checker.
type Resolver interface {
	LookupHost(ctx context.Context, host string) ([]string, error)
}

// DNS verifies that the configured 4chan hostname resolves.
type DNS struct {
	name     string
	resolver Resolver
}

// NewDNS creates a DNS checker backed by the supplied resolver.
func NewDNS(name string, resolver Resolver) *DNS {
	return &DNS{name: name, resolver: resolver}
}

// Check resolves the configured hostname and rejects an empty successful result.
func (checker *DNS) Check(ctx context.Context) error {
	addresses, err := checker.resolver.LookupHost(ctx, checker.name)
	if err != nil {
		return fmt.Errorf("resolving 4chan: %w", err)
	}

	if len(addresses) == 0 {
		return errors.New("resolving 4chan: no addresses")
	}

	return nil
}

// Handler serves the single shallow health route.
type Handler struct {
	timeout   time.Duration
	logger    *slog.Logger
	tracer    trace.Tracer
	memcached Checker
	dns       Checker
}

// NewHandler creates a health handler with explicit dependency and telemetry seams.
func NewHandler(timeout time.Duration, logger *slog.Logger, tracer trace.Tracer, memcached, dns Checker) *Handler {
	return &Handler{timeout: timeout, logger: logger, tracer: tracer, memcached: memcached, dns: dns}
}

// ServeHTTP accepts only GET and reports a generic availability result.
func (handler *Handler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Content-Type", "text/plain; charset=utf-8")

	if request.Method != http.MethodGet {
		writer.Header().Set("Allow", http.MethodGet)
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)

		return
	}

	ctx, cancel := context.WithTimeout(request.Context(), handler.timeout)
	defer cancel()

	if !handler.check(ctx, memcachedDependency, handler.memcached) || !handler.check(ctx, dnsDependency, handler.dns) {
		http.Error(writer, "unavailable", http.StatusServiceUnavailable)

		return
	}

	writer.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(writer, "ok\n")
}

func (handler *Handler) check(ctx context.Context, dependency string, checker Checker) bool {
	root := trace.SpanFromContext(ctx)

	ctx, span := handler.tracer.Start(ctx, "health."+dependency)
	defer span.End()

	err := checker.Check(ctx)
	if err == nil {
		return true
	}

	errorType := classifyError(err)

	span.SetStatus(codes.Error, "health dependency unavailable")
	span.SetAttributes(attribute.String("error.type", errorType))

	root.SetStatus(codes.Error, "health dependency unavailable")
	root.SetAttributes(attribute.String("error.type", errorType))
	handler.logger.ErrorContext(ctx, "health dependency unavailable",
		slog.String("dependency", dependency),
		slog.String("error.type", errorType),
	)

	return false
}

func classifyError(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	default:
		return "unavailable"
	}
}

func contextError(ctx context.Context, fallback error) error {
	err := ctx.Err()
	if err != nil {
		return fmt.Errorf("Memcached operation interrupted: %w", err)
	}

	return fallback
}
