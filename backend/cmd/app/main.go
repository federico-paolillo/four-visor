// Package main wires the configured 4Visor backend HTTP service.
package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
)

var errInvalidCommand = errors.New("invalid command")

// main reports the single safe process-boundary diagnostic and exits non-zero on failure.
func main() {
	err := execute(context.Background(), os.Args[1:], os.Stderr, http.DefaultClient)
	if err != nil {
		slog.New(slog.NewJSONHandler(os.Stderr, nil)).Error("backend stopped", slog.Any("error", err))
		os.Exit(1)
	}
}

func execute(ctx context.Context, args []string, stderr io.Writer, client *http.Client) error {
	switch {
	case len(args) == 0:
		return run(ctx, stderr)
	case len(args) == 2 && args[0] == "healthcheck":
		return checkBackendHealth(ctx, client, args[1])
	case len(args) > 0 && args[0] == "snapshot":
		return exportSnapshot(ctx, args[1:], stderr, client)
	default:
		return errInvalidCommand
	}
}
