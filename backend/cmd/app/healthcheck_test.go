package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
)

type healthRoundTripFunc func(*http.Request) (*http.Response, error)

func (roundTrip healthRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

type healthResponseBody struct {
	closed bool
	read   bool
}

func (body *healthResponseBody) Read([]byte) (int, error) {
	body.read = true

	return 0, io.EOF
}

func (body *healthResponseBody) Close() error {
	body.closed = true

	return nil
}

func TestExecuteHealthcheck(t *testing.T) {
	body := &healthResponseBody{}
	client := &http.Client{Transport: healthRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodGet || request.URL.String() != "http://127.0.0.1:65102/health" {
			t.Fatalf("health request = %s %s", request.Method, request.URL)
		}

		return &http.Response{StatusCode: http.StatusOK, Body: body}, nil
	})}

	err := execute(
		t.Context(),
		[]string{"healthcheck", "http://127.0.0.1:65102/health"},
		io.Discard,
		client,
	)
	if err != nil {
		t.Fatalf("execute healthcheck error = %v", err)
	}
	if body.read || !body.closed {
		t.Fatalf("health response body read=%t closed=%t", body.read, body.closed)
	}
}

func TestExecuteRejectsInvalidCommands(t *testing.T) {
	for _, args := range [][]string{
		{"healthcheck"},
		{"healthcheck", "http://backend:65102/health"},
		{"healthcheck", "http://127.0.0.1:65102/health", "extra"},
		{"serve"},
	} {
		if err := execute(t.Context(), args, io.Discard, http.DefaultClient); !errors.Is(err, errInvalidCommand) {
			t.Errorf("execute(%q) error = %v, want invalid command", args, err)
		}
	}
}

func TestBackendHealthRejectsStatusWithoutReadingBody(t *testing.T) {
	for _, status := range []int{http.StatusNoContent, http.StatusServiceUnavailable} {
		body := &healthResponseBody{}
		client := &http.Client{Transport: healthRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: status, Body: body}, nil
		})}

		err := checkBackendHealth(t.Context(), client, "http://127.0.0.1:65102/health")
		if !errors.Is(err, errBackendUnhealthy) || !strings.Contains(err.Error(), fmt.Sprintf("HTTP %d", status)) {
			t.Errorf("status %d error = %v, want safe status failure", status, err)
		}
		if body.read || !body.closed {
			t.Errorf("status %d response body read=%t closed=%t", status, body.read, body.closed)
		}
	}
}

func TestBackendHealthPreservesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := checkBackendHealth(ctx, http.DefaultClient, "http://127.0.0.1:65102/health")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("checkBackendHealth error = %v, want cancellation", err)
	}
}

func TestBackendHealthHasBoundedContext(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client := &http.Client{Transport: healthRoundTripFunc(func(request *http.Request) (*http.Response, error) {
			<-request.Context().Done()

			return nil, request.Context().Err()
		})}

		err := checkBackendHealth(t.Context(), client, "http://127.0.0.1:65102/health")
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("checkBackendHealth error = %v, want deadline", err)
		}
	})
}
