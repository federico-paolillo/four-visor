package lineage

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/bradfitz/gomemcache/memcache"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

func TestMemcachedSnapshotServingReturnsPublishedBytes(t *testing.T) {
	client := integrationMemcached(t)
	value := testSnapshot(newLineageID, 2*blockSize+100)
	publish(t, integrationPublisher(t, client), t.Context(), value)
	want := mustMarshalSnapshot(t, value)
	handler := integrationSnapshotHandler(t)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/snapshot", http.NoBody))
	if response.Code != http.StatusOK || !bytes.Equal(response.Body.Bytes(), want) {
		t.Fatalf("response status=%d bytes=%d, want exact %d-byte publication", response.Code, response.Body.Len(), len(want))
	}
	assertSnapshotHeaders(t, response, len(want))
}

func TestMemcachedSnapshotServingReturnsGoneForEveryMissingComponent(t *testing.T) {
	value := testSnapshot(newLineageID, 2*blockSize+100)

	for _, component := range []string{"pointer", "completion"} {
		t.Run(component, func(t *testing.T) {
			client := integrationMemcached(t)
			publish(t, integrationPublisher(t, client), t.Context(), value)
			key := activePointerKey
			if component == "completion" {
				key = completionKey(value.LineageID)
			}
			if err := client.Delete(key); err != nil {
				t.Fatalf("Delete(%s) error = %v", component, err)
			}

			assertIntegrationSnapshotStatus(t, integrationSnapshotHandler(t), http.StatusGone)
		})
	}

	data := mustMarshalSnapshot(t, value)
	blocks, _, err := splitBlocks(data)
	if err != nil {
		t.Fatalf("splitBlocks() error = %v", err)
	}
	for index := range blocks {
		t.Run("block "+string(rune('0'+index)), func(t *testing.T) {
			client := integrationMemcached(t)
			publish(t, integrationPublisher(t, client), t.Context(), value)
			if err := client.Delete(blockKey(value.LineageID, index)); err != nil {
				t.Fatalf("Delete(block %d) error = %v", index, err)
			}

			assertIntegrationSnapshotStatus(t, integrationSnapshotHandler(t), http.StatusGone)
		})
	}
}

func TestMemcachedSnapshotServingRejectsPresentCorruption(t *testing.T) {
	value := testSnapshot(newLineageID, blockSize+100)
	tests := []struct {
		name   string
		mutate func(*testing.T, *memcache.Client)
	}{
		{
			name: "invalid pointer",
			mutate: func(t *testing.T, client *memcache.Client) {
				t.Helper()
				if err := client.Set(&memcache.Item{Key: activePointerKey, Value: []byte("invalid")}); err != nil {
					t.Fatalf("Set(pointer) error = %v", err)
				}
			},
		},
		{
			name: "malformed completion",
			mutate: func(t *testing.T, client *memcache.Client) {
				t.Helper()
				if err := client.Set(&memcache.Item{
					Key: completionKey(value.LineageID), Value: []byte("not-json"),
				}); err != nil {
					t.Fatalf("Set(completion) error = %v", err)
				}
			},
		},
		{
			name: "wrong block shape",
			mutate: func(t *testing.T, client *memcache.Client) {
				t.Helper()
				item := integrationBlock(t, client, value.LineageID, 0)
				item.Value = item.Value[:len(item.Value)-1]
				if err := client.Set(item); err != nil {
					t.Fatalf("Set(short block) error = %v", err)
				}
			},
		},
		{
			name: "invalid JSON",
			mutate: func(t *testing.T, client *memcache.Client) {
				t.Helper()
				item := integrationBlock(t, client, value.LineageID, 0)
				item.Value[0] = '!'
				if err := client.Set(item); err != nil {
					t.Fatalf("Set(invalid JSON block) error = %v", err)
				}
			},
		},
		{
			name: "unsupported version",
			mutate: func(t *testing.T, client *memcache.Client) {
				t.Helper()
				item := integrationBlock(t, client, value.LineageID, 0)
				item.Value = bytes.Replace(item.Value, []byte(`"schemaVersion":1`), []byte(`"schemaVersion":2`), 1)
				if err := client.Set(item); err != nil {
					t.Fatalf("Set(version block) error = %v", err)
				}
			},
		},
		{
			name: "lineage mismatch",
			mutate: func(t *testing.T, client *memcache.Client) {
				t.Helper()
				item := integrationBlock(t, client, value.LineageID, 0)
				item.Value = bytes.Replace(item.Value, []byte(value.LineageID), []byte(unexpectedLineageID), 1)
				if err := client.Set(item); err != nil {
					t.Fatalf("Set(mismatched lineage block) error = %v", err)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := integrationMemcached(t)
			publish(t, integrationPublisher(t, client), t.Context(), value)
			test.mutate(t, client)

			assertIntegrationSnapshotStatus(t, integrationSnapshotHandler(t), http.StatusInternalServerError)
		})
	}
}

func integrationSnapshotHandler(t *testing.T) *SnapshotHandler {
	t.Helper()
	address := os.Getenv("FOURVISOR_TEST_MEMCACHED_ADDRESS")
	if address == "" {
		t.Skip("FOURVISOR_TEST_MEMCACHED_ADDRESS is not set")
	}

	handler, err := NewSnapshotHandler(
		address,
		slog.New(slog.NewJSONHandler(io.Discard, nil)),
		tracenoop.NewTracerProvider().Tracer("test/snapshot"),
		metricnoop.NewMeterProvider().Meter("test/snapshot"),
	)
	if err != nil {
		t.Fatalf("NewSnapshotHandler() error = %v", err)
	}

	return handler
}

func integrationBlock(t *testing.T, client *memcache.Client, lineageID string, index int) *memcache.Item {
	t.Helper()
	item, err := client.Get(blockKey(lineageID, index))
	if err != nil {
		t.Fatalf("Get(block %d) error = %v", index, err)
	}

	return item
}

func assertIntegrationSnapshotStatus(t *testing.T, handler *SnapshotHandler, status int) {
	t.Helper()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/snapshot", http.NoBody))
	if response.Code != status {
		t.Fatalf("status = %d, want %d; body=%q", response.Code, status, response.Body)
	}
	assertSnapshotHeaders(t, response, response.Body.Len())
	if bytes.Contains(response.Body.Bytes(), []byte(`"schemaVersion"`)) {
		t.Fatalf("failure response contains partial snapshot: %q", response.Body)
	}
}
