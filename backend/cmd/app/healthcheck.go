package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
)

const healthcheckTimeout = 3 * time.Second

const backendHealthURL = "http://127.0.0.1:65102/health"

var errBackendUnhealthy = errors.New("backend is unhealthy")

// checkBackendHealth verifies the running server through its existing shallow health boundary.
func checkBackendHealth(parent context.Context, client *http.Client, endpoint string) error {
	if endpoint != backendHealthURL {
		return errInvalidCommand
	}

	ctx, cancel := context.WithTimeout(parent, healthcheckTimeout)
	defer cancel()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, backendHealthURL, http.NoBody)
	if err != nil {
		return fmt.Errorf("creating backend health request: %w", err)
	}

	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("requesting backend health: %w", err)
	}
	defer response.Body.Close() //nolint:errcheck // The one-shot probe has no recovery action for close failure.

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: HTTP %d", errBackendUnhealthy, response.StatusCode)
	}

	return nil
}
