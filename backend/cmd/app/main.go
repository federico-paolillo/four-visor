// Package main wires the configured 4Visor backend HTTP service.
package main

import (
	"context"
	"log/slog"
	"os"
)

// main reports the single safe process-boundary diagnostic and exits non-zero on failure.
func main() {
	err := run(context.Background(), os.Stderr)
	if err != nil {
		slog.New(slog.NewJSONHandler(os.Stderr, nil)).Error("backend stopped", slog.Any("error", err))
		os.Exit(1)
	}
}
