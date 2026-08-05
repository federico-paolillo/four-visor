package main

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git.disroot.org/federico-paolillo/four-visor.git/snapshot"
)

type snapshotRoundTripFunc func(*http.Request) (*http.Response, error)

func (roundTrip snapshotRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

func TestExecuteSnapshotWritesValidJSON(t *testing.T) {
	t.Setenv("FOURVISOR_COMMIT_HASH", "0123456789abcdef0123456789abcdef01234567")
	t.Setenv("FOURVISOR_ACQUISITION_DEADLINE", "2h")
	t.Setenv("FOURVISOR_ACQUISITION_REQUEST_TIMEOUT", "3h")
	path := filepath.Join(t.TempDir(), "snapshot.json")
	client := &http.Client{Transport: snapshotRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodGet || request.URL.String() != "https://a.4cdn.org/boards.json" {
			t.Fatalf("snapshot request = %s %s", request.Method, request.URL)
		}
		deadline, ok := request.Context().Deadline()
		if remaining := time.Until(deadline); !ok || remaining < 119*time.Minute || remaining > 2*time.Hour {
			t.Fatalf("snapshot request deadline remaining = %s, present = %t", remaining, ok)
		}

		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"boards":[]}`)),
		}, nil
	})}

	if err := execute(t.Context(), []string{"snapshot", "--out", path}, io.Discard, client); err != nil {
		t.Fatalf("execute snapshot error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}
	if _, err := snapshot.Parse(data); err != nil {
		t.Fatalf("snapshot.Parse() error = %v", err)
	}
}

func TestExecuteRejectsInvalidCommands(t *testing.T) {
	for _, args := range [][]string{
		{"healthcheck", "http://127.0.0.1:65102/health"},
		{"serve"},
	} {
		if err := execute(t.Context(), args, io.Discard, http.DefaultClient); !errors.Is(err, errInvalidCommand) {
			t.Errorf("execute(%q) error = %v, want invalid command", args, err)
		}
	}
}

func TestSnapshotOutputPath(t *testing.T) {
	startedAt := time.Unix(1234567890, 0)

	path, err := snapshotOutputPath(nil, startedAt, io.Discard)
	if err != nil || path != "./snapshot-1234567890.json" {
		t.Fatalf("snapshotOutputPath() = %q, %v", path, err)
	}

	path, err = snapshotOutputPath([]string{"--out", "testdata/live.json"}, startedAt, io.Discard)
	if err != nil || path != "testdata/live.json" {
		t.Fatalf("snapshotOutputPath(--out) = %q, %v", path, err)
	}
}

func TestWriteSnapshotFileDoesNotClobber(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snapshot.json")
	if err := os.WriteFile(path, []byte("existing"), 0o644); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	if err := writeSnapshotFile(path, []byte("replacement")); err == nil {
		t.Fatal("writeSnapshotFile() error = nil, want existing-file failure")
	}

	data, err := os.ReadFile(path)
	if err != nil || string(data) != "existing" {
		t.Fatalf("existing output = %q, %v", data, err)
	}
}
