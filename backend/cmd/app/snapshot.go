package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"git.disroot.org/federico-paolillo/four-visor.git/acquisition"
	"git.disroot.org/federico-paolillo/four-visor.git/config"
	"git.disroot.org/federico-paolillo/four-visor.git/snapshot"
	"git.disroot.org/federico-paolillo/four-visor.git/synchronization"
	"github.com/oklog/ulid/v2"
	"go.opentelemetry.io/otel"
)

// exportSnapshot observes one complete lineage and writes its validated JSON to a new file.
func exportSnapshot(parent context.Context, args []string, stderr io.Writer, httpClient *http.Client) error {
	startedAt := time.Now().UTC()

	path, err := snapshotOutputPath(args, startedAt, stderr)
	if err != nil {
		return err
	}

	settings, err := config.LoadAcquisition()
	if err != nil {
		return fmt.Errorf("loading acquisition configuration: %w", err)
	}

	transport := http.DefaultTransport
	if httpClient != nil && httpClient.Transport != nil {
		transport = httpClient.Transport
	}

	client, err := acquisition.New(
		acquisition.Policy{
			RateInterval:   settings.RateInterval,
			MaxConcurrency: settings.MaxConcurrency,
			RequestTimeout: settings.RequestTimeout,
			MaxRetries:     settings.MaxRetries,
			RetryBackoff:   settings.RetryBackoff,
		},
		settings.UserAgent,
		transport,
		slog.New(slog.NewJSONHandler(stderr, nil)),
		otel.Tracer("four-visor/snapshot-cli"),
		otel.Meter("four-visor/snapshot-cli"),
	)
	if err != nil {
		return fmt.Errorf("creating acquisition client: %w", err)
	}

	signalCtx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stop()

	ctx, cancel := context.WithTimeout(signalCtx, synchronization.LineageDeadline)
	defer cancel()

	identifier, err := ulid.New(ulid.Timestamp(startedAt), ulid.DefaultEntropy())
	if err != nil {
		return fmt.Errorf("generating snapshot lineage identifier: %w", err)
	}

	lineageID := identifier.String()

	boards, err := client.Observe(ctx, lineageID)
	if err != nil {
		return fmt.Errorf("acquiring snapshot: %w", err)
	}

	data, err := snapshot.Marshal(snapshot.Snapshot{
		SchemaVersion: snapshot.Version,
		LineageID:     lineageID,
		ObservedAt:    startedAt.Format(time.RFC3339Nano),
		Boards:        boards,
	})
	if err != nil {
		return fmt.Errorf("serializing snapshot: %w", err)
	}

	return writeSnapshotFile(path, data)
}

func snapshotOutputPath(args []string, startedAt time.Time, stderr io.Writer) (string, error) {
	flags := flag.NewFlagSet("snapshot", flag.ContinueOnError)
	flags.SetOutput(stderr)
	out := flags.String("out", "", "output path and filename")

	err := flags.Parse(args)
	if err != nil {
		return "", fmt.Errorf("%w: %w", errInvalidCommand, err)
	}

	if flags.NArg() != 0 {
		return "", errInvalidCommand
	}

	if *out != "" {
		return *out, nil
	}

	return fmt.Sprintf("./snapshot-%d.json", startedAt.Unix()), nil
}

func writeSnapshotFile(path string, data []byte) (result error) {
	//nolint:gosec // The CLI intentionally accepts an operator-selected destination.
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("creating snapshot output: %w", err)
	}

	defer func() {
		closeError := file.Close()
		if result == nil && closeError != nil {
			result = fmt.Errorf("closing snapshot output: %w", closeError)
		}

		if result != nil {
			removeError := os.Remove(path)
			if removeError != nil {
				result = errors.Join(result, fmt.Errorf("removing partial snapshot output: %w", removeError))
			}
		}
	}()

	written, err := file.Write(data)
	if err != nil {
		return fmt.Errorf("writing snapshot output: %w", err)
	}

	if written != len(data) {
		return fmt.Errorf("writing snapshot output: %w", io.ErrShortWrite)
	}

	return nil
}
