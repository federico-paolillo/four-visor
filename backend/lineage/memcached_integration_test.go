package lineage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"git.disroot.org/federico-paolillo/four-visor.git/snapshot"
	"github.com/bradfitz/gomemcache/memcache"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

type faultMemcache struct {
	base         *memcache.Client
	before       func(operation, key string) error
	after        func(operation, key string) error
	transformGet func(key string, value []byte) []byte
}

func (cache *faultMemcache) Add(item *memcache.Item) error {
	if err := integrationHook(cache.before, "add", item.Key); err != nil {
		return err
	}
	if err := cache.base.Add(item); err != nil {
		return err
	}

	return integrationHook(cache.after, "add", item.Key)
}

func (cache *faultMemcache) Set(item *memcache.Item) error {
	if err := integrationHook(cache.before, "set", item.Key); err != nil {
		return err
	}
	if err := cache.base.Set(item); err != nil {
		return err
	}

	return integrationHook(cache.after, "set", item.Key)
}

func (cache *faultMemcache) Get(key string) (*memcache.Item, error) {
	if err := integrationHook(cache.before, "get", key); err != nil {
		return nil, err
	}
	item, err := cache.base.Get(key)
	if err != nil {
		return nil, err
	}
	if cache.transformGet != nil {
		item.Value = cache.transformGet(key, item.Value)
	}
	if err := integrationHook(cache.after, "get", key); err != nil {
		return nil, err
	}

	return item, nil
}

func (cache *faultMemcache) Delete(key string) error {
	if err := integrationHook(cache.before, "delete", key); err != nil {
		return err
	}
	if err := cache.base.Delete(key); err != nil {
		return err
	}

	return integrationHook(cache.after, "delete", key)
}

func integrationHook(hook func(string, string) error, operation, key string) error {
	if hook == nil {
		return nil
	}

	return hook(operation, key)
}

func TestMemcachedPublicationReplacementAndCacheLoss(t *testing.T) {
	client := integrationMemcached(t)
	publisher := integrationPublisher(t, client)
	old := testSnapshot(oldLineageID, 0)
	newValue := testSnapshot(newLineageID, blockSize+100)

	publish(t, publisher, t.Context(), old)
	assertRealLineage(t, client, oldLineageID, old)
	publish(t, publisher, t.Context(), newValue)
	assertRealLineage(t, client, newLineageID, newValue)
	if _, err := client.Get(completionKey(oldLineageID)); !errors.Is(err, memcache.ErrCacheMiss) {
		t.Fatalf("old completion Get() error = %v, want cache miss", err)
	}
	if _, err := client.Get(blockKey(oldLineageID, 0)); !errors.Is(err, memcache.ErrCacheMiss) {
		t.Fatalf("old block Get() error = %v, want cache miss", err)
	}

	if err := client.FlushAll(); err != nil {
		t.Fatalf("FlushAll() error = %v", err)
	}
	if _, err := client.Get(activePointerKey); !errors.Is(err, memcache.ErrCacheMiss) {
		t.Fatalf("active pointer after cache loss error = %v, want miss", err)
	}
}

func TestMemcachedCandidateKeyCollisionPreservesOldLineage(t *testing.T) {
	client := integrationMemcached(t)
	publisher := integrationPublisher(t, client)
	old := testSnapshot(oldLineageID, 0)
	publish(t, publisher, t.Context(), old)

	err := client.Add(&memcache.Item{Key: blockKey(newLineageID, 0), Value: []byte("orphan")})
	if err != nil {
		t.Fatalf("seeding collision Add() error = %v", err)
	}
	err = publisher.Publish(t.Context(), testSnapshot(newLineageID, 0), time.Hour)
	if !errors.Is(err, memcache.ErrNotStored) {
		t.Fatalf("Publish() error = %v, want immutable-key collision", err)
	}
	assertRealLineage(t, client, oldLineageID, old)
}

func TestMemcachedInjectedFailuresPreserveOldLineage(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*faultMemcache, context.CancelCauseFunc, error)
		verifyErr error
	}{
		{
			name: "block write",
			configure: func(cache *faultMemcache, _ context.CancelCauseFunc, failure error) {
				cache.before = func(operation, key string) error {
					if operation == "add" && key == blockKey(newLineageID, 1) {
						return failure
					}
					return nil
				}
			},
		},
		{
			name: "block verification",
			configure: func(cache *faultMemcache, _ context.CancelCauseFunc, _ error) {
				cache.transformGet = func(key string, value []byte) []byte {
					if key == blockKey(newLineageID, 0) {
						return []byte("corrupt")
					}
					return value
				}
			},
			verifyErr: errVerificationFailed,
		},
		{
			name: "completion write",
			configure: func(cache *faultMemcache, _ context.CancelCauseFunc, failure error) {
				cache.before = func(operation, key string) error {
					if operation == "add" && key == completionKey(newLineageID) {
						return failure
					}
					return nil
				}
			},
		},
		{
			name: "completion verification",
			configure: func(cache *faultMemcache, _ context.CancelCauseFunc, _ error) {
				cache.transformGet = func(key string, value []byte) []byte {
					if key == completionKey(newLineageID) {
						return []byte("corrupt")
					}
					return value
				}
			},
			verifyErr: errVerificationFailed,
		},
		{
			name: "pointer store",
			configure: func(cache *faultMemcache, _ context.CancelCauseFunc, failure error) {
				cache.before = func(operation, key string) error {
					if operation == "set" && key == activePointerKey {
						return failure
					}
					return nil
				}
			},
		},
		{
			name: "cancellation after completion verification",
			configure: func(cache *faultMemcache, cancel context.CancelCauseFunc, failure error) {
				cache.after = func(operation, key string) error {
					if operation == "get" && key == completionKey(newLineageID) {
						cancel(failure)
					}
					return nil
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := integrationMemcached(t)
			old := testSnapshot(oldLineageID, 0)
			publish(t, integrationPublisher(t, client), t.Context(), old)

			faults := &faultMemcache{base: client}
			ctx, cancel := context.WithCancelCause(t.Context())
			failure := errors.New("injected integration failure")
			test.configure(faults, cancel, failure)

			err := integrationPublisher(t, faults).Publish(
				ctx,
				testSnapshot(newLineageID, blockSize+100),
				time.Hour,
			)
			want := failure
			if test.verifyErr != nil {
				want = test.verifyErr
			}
			if !errors.Is(err, want) {
				t.Fatalf("Publish() error = %v, want %v", err, want)
			}
			assertRealLineage(t, client, oldLineageID, old)
		})
	}
}

func TestMemcachedAppliedSetReconciliationAndCleanupFailure(t *testing.T) {
	t.Run("applied set response failure", func(t *testing.T) {
		client := integrationMemcached(t)
		publish(t, integrationPublisher(t, client), t.Context(), testSnapshot(oldLineageID, 0))

		faults := &faultMemcache{base: client}
		faults.after = func(operation, key string) error {
			if operation == "set" && key == activePointerKey {
				return errors.New("response lost")
			}
			return nil
		}
		value := testSnapshot(newLineageID, 0)
		publish(t, integrationPublisher(t, faults), t.Context(), value)
		assertRealLineage(t, client, newLineageID, value)
	})

	t.Run("cleanup failure", func(t *testing.T) {
		client := integrationMemcached(t)
		publish(t, integrationPublisher(t, client), t.Context(), testSnapshot(oldLineageID, 0))

		failure := errors.New("delete unavailable")
		faults := &faultMemcache{base: client}
		faults.before = func(operation, key string) error {
			if operation == "delete" && key == blockKey(oldLineageID, 0) {
				return failure
			}
			return nil
		}
		value := testSnapshot(newLineageID, 0)
		err := integrationPublisher(t, faults).Publish(t.Context(), value, time.Hour)
		var cleanup *CleanupError
		if !errors.As(err, &cleanup) || !errors.Is(err, failure) {
			t.Fatalf("Publish() error = %v, want cleanup failure", err)
		}
		assertRealLineage(t, client, newLineageID, value)
		if _, err := client.Get(blockKey(oldLineageID, 0)); err != nil {
			t.Fatalf("old block TTL fallback Get() error = %v", err)
		}
	})
}

func TestMemcachedMemoryPressureCannotEvictActiveLineage(t *testing.T) {
	if os.Getenv("FOURVISOR_TEST_MEMCACHED_PRESSURE") != "1" {
		t.Skip("memory-pressure Memcached phase is not selected")
	}

	client := integrationMemcached(t)
	publisher := integrationPublisher(t, client)
	old := testSnapshot(oldLineageID, 0)
	publish(t, publisher, t.Context(), old)

	err := publisher.Publish(t.Context(), testSnapshot(newLineageID, 16*blockSize), time.Hour)
	if err == nil {
		t.Fatal("memory-pressure Publish() error = nil")
	}
	assertRealLineage(t, client, oldLineageID, old)
}

func TestConcurrentMemcachedPointerReadersObserveOnlyCompleteValues(t *testing.T) {
	client := integrationMemcached(t)
	publish(t, integrationPublisher(t, client), t.Context(), testSnapshot(oldLineageID, 0))

	setEntered := make(chan struct{})
	releaseSet := make(chan struct{})
	var enterOnce sync.Once
	faults := &faultMemcache{base: client}
	faults.before = func(operation, key string) error {
		if operation == "set" && key == activePointerKey {
			enterOnce.Do(func() { close(setEntered) })
			<-releaseSet
		}
		return nil
	}
	newValue := testSnapshot(newLineageID, blockSize+100)
	faultPublisher := integrationPublisher(t, faults)
	publishResult := make(chan error, 1)
	go func() {
		publishResult <- faultPublisher.Publish(t.Context(), newValue, time.Hour)
	}()
	<-setEntered

	const readerCount = 8
	stop := make(chan struct{})
	started := make(chan struct{}, readerCount)
	readerErrors := make(chan error, readerCount)
	var readers sync.WaitGroup
	var oldReads atomic.Int64
	var newReads atomic.Int64
	for range readerCount {
		readers.Go(func() {
			item, err := client.Get(activePointerKey)
			if err != nil || string(item.Value) != oldLineageID {
				readerErrors <- errors.New("reader did not observe old pointer before replacement")
				started <- struct{}{}
				return
			}
			oldReads.Add(1)
			started <- struct{}{}
			for {
				select {
				case <-stop:
					return
				default:
				}

				item, err := client.Get(activePointerKey)
				if err != nil {
					readerErrors <- err
					return
				}
				switch string(item.Value) {
				case oldLineageID:
					oldReads.Add(1)
				case newLineageID:
					newReads.Add(1)
					if err := realLineageError(client, newLineageID); err != nil {
						readerErrors <- err
						return
					}
				default:
					readerErrors <- errors.New("reader observed malformed pointer")
					return
				}
			}
		})
	}
	for range readerCount {
		<-started
	}
	close(releaseSet)
	if err := <-publishResult; err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	for newReads.Load() == 0 {
		item, err := client.Get(activePointerKey)
		if err != nil {
			t.Fatalf("post-activation pointer Get() error = %v", err)
		}
		if string(item.Value) == newLineageID {
			newReads.Add(1)
		}
	}
	close(stop)
	readers.Wait()
	close(readerErrors)
	for err := range readerErrors {
		t.Fatalf("concurrent pointer reader error = %v", err)
	}
	if oldReads.Load() == 0 || newReads.Load() == 0 {
		t.Fatalf("pointer observations old=%d new=%d", oldReads.Load(), newReads.Load())
	}
	assertRealLineage(t, client, newLineageID, newValue)
}

func integrationMemcached(t *testing.T) *memcache.Client {
	t.Helper()
	address := os.Getenv("FOURVISOR_TEST_MEMCACHED_ADDRESS")
	if address == "" {
		t.Skip("FOURVISOR_TEST_MEMCACHED_ADDRESS is not set")
	}

	client := memcache.New(address)
	client.Timeout = time.Second
	if err := client.FlushAll(); err != nil {
		t.Fatalf("initial FlushAll() error = %v", err)
	}
	t.Cleanup(func() {
		if err := client.FlushAll(); err != nil {
			t.Errorf("cleanup FlushAll() error = %v", err)
		}
	})

	return client
}

func integrationPublisher(t *testing.T, client cacheClient) *Publisher {
	t.Helper()
	publisher, err := newPublisher(
		client,
		slog.New(slog.NewJSONHandler(io.Discard, nil)),
		tracenoop.NewTracerProvider().Tracer("test/lineage"),
		metricnoop.NewMeterProvider().Meter("test/lineage"),
	)
	if err != nil {
		t.Fatalf("newPublisher() error = %v", err)
	}

	return publisher
}

func assertRealLineage(
	t *testing.T,
	client *memcache.Client,
	lineageID string,
	want snapshot.Snapshot,
) {
	t.Helper()
	pointer, err := client.Get(activePointerKey)
	if err != nil || string(pointer.Value) != lineageID {
		t.Fatalf("active pointer = %#v, %v, want %s", pointer, err, lineageID)
	}

	data, err := readRealLineage(client, lineageID)
	if err != nil {
		t.Fatalf("readRealLineage() error = %v", err)
	}
	wantData, err := snapshot.Marshal(want)
	if err != nil {
		t.Fatalf("snapshot.Marshal() error = %v", err)
	}
	if !bytes.Equal(data, wantData) {
		t.Fatal("real Memcached lineage bytes changed")
	}
}

func realLineageError(client *memcache.Client, lineageID string) error {
	data, err := readRealLineage(client, lineageID)
	if err != nil {
		return err
	}
	parsed, err := snapshot.Parse(data)
	if err != nil {
		return err
	}
	if parsed.LineageID != lineageID {
		return errors.New("reassembled lineage identifier mismatch")
	}

	return nil
}

func readRealLineage(client *memcache.Client, lineageID string) ([]byte, error) {
	metadataItem, err := client.Get(completionKey(lineageID))
	if err != nil {
		return nil, err
	}
	metadata, err := decodeCompletion(metadataItem.Value)
	if err != nil {
		return nil, err
	}
	blocks := make([][]byte, metadata.BlockCount)
	for index := range metadata.BlockCount {
		item, err := client.Get(blockKey(lineageID, index))
		if err != nil {
			return nil, err
		}
		blocks[index] = item.Value
	}

	return reassemble(metadata, blocks)
}

func TestIntegrationUsesOnlyLoopbackProjectPort(t *testing.T) {
	address := os.Getenv("FOURVISOR_TEST_MEMCACHED_ADDRESS")
	if address != "" && address != "127.0.0.1:65198" {
		t.Fatalf("integration Memcached address = %q", strings.TrimSpace(address))
	}
}
