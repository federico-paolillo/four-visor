package lineage

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"testing"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/testcontainers/testcontainers-go"
	tcmemcached "github.com/testcontainers/testcontainers-go/modules/memcached"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

func TestMemcachedPublicationReplacementCleanupAndHTTPBytes(t *testing.T) {
	address, client := startMemcached(t, 64)
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	tracer := tracenoop.NewTracerProvider().Tracer("test/lineage")
	meter := metricnoop.NewMeterProvider().Meter("test/lineage")
	publisher, err := NewPublisher(address, logger, tracer, meter)
	if err != nil {
		t.Fatalf("NewPublisher() error = %v", err)
	}
	handler, err := NewSnapshotHandler(address, logger, tracer, meter)
	if err != nil {
		t.Fatalf("NewSnapshotHandler() error = %v", err)
	}

	old := testSnapshot(oldLineageID, 0)
	publish(t, publisher, t.Context(), old)
	pointer, err := client.Get(activePointerKey)
	if err != nil || string(pointer.Value) != oldLineageID {
		t.Fatalf("old active pointer = %#v, %v, want %s", pointer, err, oldLineageID)
	}

	value := testSnapshot(newLineageID, 2*blockSize+100)
	publish(t, publisher, t.Context(), value)
	pointer, err = client.Get(activePointerKey)
	if err != nil || string(pointer.Value) != newLineageID {
		t.Fatalf("new active pointer = %#v, %v, want %s", pointer, err, newLineageID)
	}
	if _, err := client.Get(completionKey(oldLineageID)); !errors.Is(err, memcache.ErrCacheMiss) {
		t.Fatalf("old completion Get() error = %v, want cache miss", err)
	}
	if _, err := client.Get(blockKey(oldLineageID, 0)); !errors.Is(err, memcache.ErrCacheMiss) {
		t.Fatalf("old block Get() error = %v, want cache miss", err)
	}

	want := mustMarshalSnapshot(t, value)
	request := httptest.NewRequest(http.MethodGet, "/snapshot", http.NoBody)
	request.Header.Set("Accept-Encoding", "br")
	request.Header.Set("Range", "bytes=0-9")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !bytes.Equal(response.Body.Bytes(), want) {
		t.Fatalf("response status=%d bytes=%d, want exact %d-byte publication", response.Code, response.Body.Len(), len(want))
	}
	assertSnapshotHeaders(t, response, len(want))
}

func TestMemcachedNoEvictionPreservesActiveLineageUnderMemoryPressure(t *testing.T) {
	address, client := startMemcached(t, 8)
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	tracer := tracenoop.NewTracerProvider().Tracer("test/lineage")
	meter := metricnoop.NewMeterProvider().Meter("test/lineage")
	publisher, err := NewPublisher(address, logger, tracer, meter)
	if err != nil {
		t.Fatalf("NewPublisher() error = %v", err)
	}

	old := testSnapshot(oldLineageID, 0)
	publish(t, publisher, t.Context(), old)
	if err := publisher.Publish(t.Context(), testSnapshot(newLineageID, 16*blockSize), time.Hour); err == nil {
		t.Fatal("memory-pressure Publish() error = nil")
	}
	pointer, err := client.Get(activePointerKey)
	if err != nil || string(pointer.Value) != oldLineageID {
		t.Fatalf("active pointer = %#v, %v, want preserved %s", pointer, err, oldLineageID)
	}

	handler, err := NewSnapshotHandler(address, logger, tracer, meter)
	if err != nil {
		t.Fatalf("NewSnapshotHandler() error = %v", err)
	}
	want := mustMarshalSnapshot(t, old)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/snapshot", http.NoBody))
	if response.Code != http.StatusOK || !bytes.Equal(response.Body.Bytes(), want) {
		t.Fatalf("preserved response status=%d bytes=%d, want exact %d-byte lineage", response.Code, response.Body.Len(), len(want))
	}
}

func startMemcached(t *testing.T, memoryMiB int) (string, *memcache.Client) {
	t.Helper()
	t.Setenv("TESTCONTAINERS_RYUK_DISABLED", "true")

	const image = "memcached:1.6.45-alpine@sha256:fb019eacc7baefab28dd9424a093181f9be578785ff820acfc223cca7d196eb3"
	memcachedContainer, err := tcmemcached.Run(
		t.Context(),
		image,
		testcontainers.WithCmd(
			"memcached",
			"--listen=0.0.0.0",
			"--port=11211",
			"--memory-limit="+strconv.Itoa(memoryMiB),
			"--max-item-size=1m",
			"--disable-evictions",
		),
		testcontainers.WithHostConfigModifier(func(hostConfig *container.HostConfig) {
			hostConfig.PortBindings = network.PortMap{
				network.MustParsePort("11211/tcp"): []network.PortBinding{{
					HostIP:   netip.MustParseAddr("127.0.0.1"),
					HostPort: "0",
				}},
			}
		}),
	)
	testcontainers.CleanupContainer(t, memcachedContainer)
	if err != nil {
		t.Fatalf("memcached.Run() error = %v", err)
	}
	endpoint, err := memcachedContainer.HostPort(t.Context())
	if err != nil {
		t.Fatalf("Memcached HostPort() error = %v", err)
	}
	_, port, err := net.SplitHostPort(endpoint)
	if err != nil {
		t.Fatalf("split Memcached endpoint %q: %v", endpoint, err)
	}
	address := net.JoinHostPort("127.0.0.1", port)
	client := memcache.New(address)
	client.Timeout = memcache.DefaultTimeout

	return address, client
}
