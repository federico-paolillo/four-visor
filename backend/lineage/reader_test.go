package lineage

import (
	"bytes"
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"git.disroot.org/federico-paolillo/four-visor.git/snapshot"
	"github.com/bradfitz/gomemcache/memcache"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

func TestSnapshotReaderReturnsExactValidatedStoredBytes(t *testing.T) {
	data, err := os.ReadFile("../../testdata/snapshot-v1/valid/backend-serialized.json")
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}
	parsed, err := snapshot.Parse(data)
	if err != nil {
		t.Fatalf("snapshot.Parse(fixture) error = %v", err)
	}

	cache := newFakeMemcache()
	seedSerializedLineage(t, cache, parsed.LineageID, data)
	got, err := readSnapshotData(testSnapshotReader(t, cache), t.Context())
	if err != nil {
		t.Fatalf("snapshotReader.read() error = %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("snapshotReader.read() changed stored serialization")
	}
	if _, err := snapshot.Parse(got); err != nil {
		t.Fatalf("snapshot.Parse(response) error = %v", err)
	}
	zIndex := bytes.Index(got, []byte(`"z": 1`))
	aIndex := bytes.Index(got, []byte(`"a": 2`))
	if zIndex < 0 || aIndex < 0 || zIndex >= aIndex {
		t.Fatal("opaque object spelling or key order changed")
	}
}

func TestSnapshotReaderReadsEveryBlockInOrder(t *testing.T) {
	cache := newFakeMemcache()
	value := testSnapshot(newLineageID, 2*blockSize+100)
	data, err := snapshot.Marshal(value)
	if err != nil {
		t.Fatalf("snapshot.Marshal() error = %v", err)
	}
	metadata := seedSerializedLineage(t, cache, value.LineageID, data)

	var keys []string
	cache.before = func(operation, key string) error {
		if operation == "get" {
			keys = append(keys, key)
		}

		return nil
	}

	got, err := readSnapshotData(testSnapshotReader(t, cache), t.Context())
	if err != nil {
		t.Fatalf("snapshotReader.read() error = %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("multi-block read changed serialized bytes")
	}

	want := []string{activePointerKey, completionKey(value.LineageID)}
	for index := range metadata.BlockCount {
		want = append(want, blockKey(value.LineageID, index))
	}
	if !slices.Equal(keys, want) {
		t.Fatalf("cache read order = %v, want %v", keys, want)
	}
}

func TestSnapshotReaderClassifiesMissingComponents(t *testing.T) {
	value := testSnapshot(newLineageID, 2*blockSize+100)
	data, err := snapshot.Marshal(value)
	if err != nil {
		t.Fatalf("snapshot.Marshal() error = %v", err)
	}

	tests := []struct {
		name   string
		remove func(*fakeMemcache, completion)
	}{
		{name: "pointer", remove: func(cache *fakeMemcache, _ completion) { delete(cache.items, activePointerKey) }},
		{name: "completion", remove: func(cache *fakeMemcache, _ completion) {
			delete(cache.items, completionKey(value.LineageID))
		}},
	}
	for index := range 3 {
		tests = append(tests, struct {
			name   string
			remove func(*fakeMemcache, completion)
		}{
			name: "block " + string(rune('0'+index)),
			remove: func(cache *fakeMemcache, metadata completion) {
				if index < metadata.BlockCount {
					delete(cache.items, blockKey(value.LineageID, index))
				}
			},
		})
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cache := newFakeMemcache()
			metadata := seedSerializedLineage(t, cache, value.LineageID, data)
			test.remove(cache, metadata)

			_, err := readSnapshotData(testSnapshotReader(t, cache), t.Context())
			if !errors.Is(err, memcache.ErrCacheMiss) || errors.Is(err, errCorruptSnapshot) {
				t.Fatalf("snapshotReader.read() error = %v, want cache miss", err)
			}
		})
	}
}

func TestSnapshotReaderRejectsPresentCorruption(t *testing.T) {
	value := testSnapshot(newLineageID, blockSize+100)
	valid, err := snapshot.Marshal(value)
	if err != nil {
		t.Fatalf("snapshot.Marshal() error = %v", err)
	}

	tests := []struct {
		name    string
		mutate  func(*fakeMemcache, completion)
		replace func([]byte) []byte
	}{
		{
			name: "invalid pointer",
			mutate: func(cache *fakeMemcache, _ completion) {
				cache.items[activePointerKey].Value = []byte("invalid")
			},
		},
		{
			name: "malformed completion",
			mutate: func(cache *fakeMemcache, _ completion) {
				cache.items[completionKey(value.LineageID)].Value = []byte("not-json")
			},
		},
		{
			name: "inconsistent completion",
			mutate: func(cache *fakeMemcache, metadata completion) {
				cache.items[completionKey(value.LineageID)].Value = []byte(`{"blockCount":1,"byteLength":524289}`)
				if metadata.BlockCount == 1 {
					t.Fatal("test requires multiple blocks")
				}
			},
		},
		{
			name: "short block",
			mutate: func(cache *fakeMemcache, _ completion) {
				item := cache.items[blockKey(value.LineageID, 0)]
				item.Value = item.Value[:len(item.Value)-1]
			},
		},
		{
			name: "invalid JSON",
			replace: func(data []byte) []byte {
				result := bytes.Clone(data)
				result[0] = '!'

				return result
			},
		},
		{
			name: "unsupported version",
			replace: func(data []byte) []byte {
				return bytes.Replace(data, []byte(`"schemaVersion":1`), []byte(`"schemaVersion":2`), 1)
			},
		},
		{
			name: "lineage mismatch",
			replace: func(data []byte) []byte {
				return bytes.Replace(data, []byte(value.LineageID), []byte(unexpectedLineageID), 1)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cache := newFakeMemcache()
			data := valid
			if test.replace != nil {
				data = test.replace(valid)
			}
			metadata := seedSerializedLineage(t, cache, value.LineageID, data)
			if test.mutate != nil {
				test.mutate(cache, metadata)
			}

			_, err := readSnapshotData(testSnapshotReader(t, cache), t.Context())
			if !errors.Is(err, errCorruptSnapshot) || errors.Is(err, memcache.ErrCacheMiss) {
				t.Fatalf("snapshotReader.read() error = %v, want corruption", err)
			}
		})
	}
}

func TestSnapshotReaderCancellationStopsSequentialWork(t *testing.T) {
	value := testSnapshot(newLineageID, 2*blockSize+100)
	data, err := snapshot.Marshal(value)
	if err != nil {
		t.Fatalf("snapshot.Marshal() error = %v", err)
	}

	t.Run("before pointer", func(t *testing.T) {
		cache := newFakeMemcache()
		seedSerializedLineage(t, cache, value.LineageID, data)
		var calls atomic.Int64
		cache.before = func(string, string) error {
			calls.Add(1)

			return nil
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		_, err := readSnapshotData(testSnapshotReader(t, cache), ctx)
		if !errors.Is(err, context.Canceled) || calls.Load() != 0 {
			t.Fatalf("read error=%v calls=%d, want cancellation before cache", err, calls.Load())
		}
	})

	t.Run("after first block", func(t *testing.T) {
		cache := newFakeMemcache()
		seedSerializedLineage(t, cache, value.LineageID, data)
		ctx, cancel := context.WithCancel(t.Context())
		var keys []string
		cache.after = func(operation, key string) error {
			if operation == "get" {
				keys = append(keys, key)
			}
			if key == blockKey(value.LineageID, 0) {
				cancel()
			}

			return nil
		}

		_, err := readSnapshotData(testSnapshotReader(t, cache), ctx)
		want := []string{activePointerKey, completionKey(value.LineageID), blockKey(value.LineageID, 0)}
		if !errors.Is(err, context.Canceled) || !slices.Equal(keys, want) {
			t.Fatalf("read error=%v keys=%v, want canceled after %v", err, keys, want)
		}
	})

	t.Run("before validation", func(t *testing.T) {
		cache := newFakeMemcache()
		reader := testSnapshotReader(t, cache)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		_, err := reader.validate(ctx, value.LineageID, data)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("validate() error = %v, want cancellation", err)
		}
	})
}

func TestSnapshotReaderPinsInitialPointer(t *testing.T) {
	oldValue := testSnapshot(oldLineageID, 0)
	oldData, err := snapshot.Marshal(oldValue)
	if err != nil {
		t.Fatalf("snapshot.Marshal() error = %v", err)
	}

	t.Run("complete old lineage succeeds", func(t *testing.T) {
		cache := newFakeMemcache()
		seedSerializedLineage(t, cache, oldLineageID, oldData)
		var pointerReads int
		cache.after = func(operation, key string) error {
			if operation == "get" && key == activePointerKey {
				pointerReads++
				cache.items[activePointerKey] = &memcache.Item{Value: []byte(newLineageID)}
			}

			return nil
		}

		got, err := readSnapshotData(testSnapshotReader(t, cache), t.Context())
		if err != nil || !bytes.Equal(got, oldData) || pointerReads != 1 {
			t.Fatalf("read bytes=%d error=%v pointerReads=%d", len(got), err, pointerReads)
		}
	})

	t.Run("old eviction returns miss without retry", func(t *testing.T) {
		cache := newFakeMemcache()
		seedSerializedLineage(t, cache, oldLineageID, oldData)
		var pointerReads int
		cache.after = func(operation, key string) error {
			if operation == "get" && key == activePointerKey {
				pointerReads++
				cache.items[activePointerKey] = &memcache.Item{Value: []byte(newLineageID)}
				delete(cache.items, completionKey(oldLineageID))
			}

			return nil
		}

		_, err := readSnapshotData(testSnapshotReader(t, cache), t.Context())
		if !errors.Is(err, memcache.ErrCacheMiss) || pointerReads != 1 {
			t.Fatalf("read error=%v pointerReads=%d, want one pinned miss", err, pointerReads)
		}
	})
}

func seedSerializedLineage(t *testing.T, cache *fakeMemcache, lineageID string, data []byte) completion {
	t.Helper()
	blocks, metadata, err := splitBlocks(data)
	if err != nil {
		t.Fatalf("splitBlocks() error = %v", err)
	}
	metadataBytes, err := encodeCompletion(metadata)
	if err != nil {
		t.Fatalf("encodeCompletion() error = %v", err)
	}

	cache.items[activePointerKey] = &memcache.Item{Value: []byte(lineageID)}
	cache.items[completionKey(lineageID)] = &memcache.Item{Value: metadataBytes}
	for index, block := range blocks {
		cache.items[blockKey(lineageID, index)] = &memcache.Item{Value: bytes.Clone(block)}
	}

	return metadata
}

func testSnapshotReader(t *testing.T, cache cacheClient) *snapshotReader {
	t.Helper()
	instrumented, err := newInstrumentedCache(
		cache,
		tracenoop.NewTracerProvider().Tracer("test/reader"),
		metricnoop.NewMeterProvider().Meter("test/reader"),
	)
	if err != nil {
		t.Fatalf("newInstrumentedCache() error = %v", err)
	}

	return &snapshotReader{
		cache:  instrumented,
		tracer: tracenoop.NewTracerProvider().Tracer("test/reader"),
	}
}

func readSnapshotData(reader *snapshotReader, ctx context.Context) ([]byte, error) {
	result, err := reader.readSnapshot(ctx)

	return result.data, err
}

func countLines(value string) int {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0
	}

	return strings.Count(trimmed, "\n") + 1
}
