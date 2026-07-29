package lineage

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"reflect"
	"testing"
	"time"
)

func TestBlocksAreDeterministicAndReassemble(t *testing.T) {
	data := bytes.Repeat([]byte("abcd"), blockSize/2+7)

	first, metadata, err := splitBlocks(data)
	if err != nil {
		t.Fatalf("splitBlocks() error = %v", err)
	}
	second, repeatedMetadata, err := splitBlocks(data)
	if err != nil {
		t.Fatalf("second splitBlocks() error = %v", err)
	}
	if !reflect.DeepEqual(first, second) || metadata != repeatedMetadata {
		t.Fatal("splitBlocks() output is not deterministic")
	}
	if metadata.BlockCount != 3 || len(first[0]) != blockSize || len(first[1]) != blockSize || len(first[2]) != 28 {
		t.Fatalf("blocks=%d lengths=[%d %d %d] metadata=%#v",
			len(first), len(first[0]), len(first[1]), len(first[2]), metadata)
	}

	assembled, err := reassemble(metadata, first)
	if err != nil {
		t.Fatalf("reassemble() error = %v", err)
	}
	if !bytes.Equal(assembled, data) {
		t.Fatal("reassemble() changed serialized bytes")
	}

	first[0][0] ^= 0xff
	if bytes.Equal(first[0], second[0]) {
		t.Fatal("splitBlocks() returned aliased blocks")
	}
}

func TestCompletionEncodingAndValidation(t *testing.T) {
	metadata := completion{BlockCount: 2, ByteLength: blockSize + 1}
	encoded, err := encodeCompletion(metadata)
	if err != nil {
		t.Fatalf("encodeCompletion() error = %v", err)
	}
	if got, want := string(encoded), fmt.Sprintf(`{"blockCount":2,"byteLength":%d}`, blockSize+1); got != want {
		t.Fatalf("encodeCompletion() = %s, want %s", got, want)
	}
	decoded, err := decodeCompletion(encoded)
	if err != nil || decoded != metadata {
		t.Fatalf("decodeCompletion() = %#v, %v", decoded, err)
	}

	for _, data := range []string{
		`{}`,
		`{"blockCount":0,"byteLength":1}`,
		`{"blockCount":1,"byteLength":0}`,
		fmt.Sprintf(`{"blockCount":1,"byteLength":%d}`, blockSize+1),
		`{"blockCount":1,"byteLength":1,"extra":true}`,
		`{"blockCount":1,"byteLength":1} {}`,
	} {
		if _, err := decodeCompletion([]byte(data)); !errors.Is(err, errInvalidCompletion) {
			t.Fatalf("decodeCompletion(%s) error = %v", data, err)
		}
	}
}

func TestReassemblyRejectsIncompleteBlocks(t *testing.T) {
	metadata := completion{BlockCount: 2, ByteLength: blockSize + 3}
	valid := [][]byte{make([]byte, blockSize), make([]byte, 3)}

	tests := [][][]byte{
		valid[:1],
		{make([]byte, blockSize-1), make([]byte, 3)},
		{make([]byte, blockSize), make([]byte, 2)},
	}
	for _, blocks := range tests {
		if _, err := reassemble(metadata, blocks); !errors.Is(err, errInvalidCompletion) {
			t.Fatalf("reassemble() error = %v", err)
		}
	}
}

func TestAppendBlockValidatesEachOrderedShape(t *testing.T) {
	metadata := completion{BlockCount: 2, ByteLength: blockSize + 3}
	data, err := appendBlock(nil, metadata, 0, make([]byte, blockSize))
	if err != nil || len(data) != blockSize {
		t.Fatalf("appendBlock(first) length=%d error=%v", len(data), err)
	}

	data, err = appendBlock(data, metadata, 1, []byte("end"))
	if err != nil || len(data) != metadata.ByteLength {
		t.Fatalf("appendBlock(last) length=%d error=%v", len(data), err)
	}

	for _, test := range []struct {
		index int
		block []byte
	}{
		{index: -1, block: nil},
		{index: 2, block: nil},
		{index: 0, block: make([]byte, blockSize-1)},
		{index: 1, block: []byte("no")},
	} {
		if _, err := appendBlock(nil, metadata, test.index, test.block); !errors.Is(err, errInvalidCompletion) {
			t.Fatalf("appendBlock(index=%d length=%d) error=%v", test.index, len(test.block), err)
		}
	}
}

func TestEvictionKeysDeleteCompletionBeforeOrderedBlocks(t *testing.T) {
	const lineageID = "01J1YQ7Y0M4S6R8T2V3W5X7Y9Z"
	metadata := completion{BlockCount: 3, ByteLength: 2*blockSize + 1}

	got, err := evictionKeys(lineageID, metadata)
	if err != nil {
		t.Fatalf("evictionKeys() error = %v", err)
	}
	want := []string{
		completionKey(lineageID),
		blockKey(lineageID, 0),
		blockKey(lineageID, 1),
		blockKey(lineageID, 2),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("evictionKeys() = %v, want %v", got, want)
	}
}

func TestMemcachedExpiration(t *testing.T) {
	now := time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		interval time.Duration
		want     int32
	}{
		{name: "within thirty days", interval: time.Hour, want: int32(now.Add(2 * time.Hour).Unix())},
		{name: "thirty day boundary", interval: 15 * 24 * time.Hour,
			want: int32(now.Add(30 * 24 * time.Hour).Unix())},
		{name: "absolute above thirty days", interval: 15*24*time.Hour + time.Second,
			want: int32(now.Add(30*24*time.Hour + 2*time.Second).Unix())},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			deadline, err := publicationDeadline(now, test.interval)
			if err != nil {
				t.Fatalf("publicationDeadline() error = %v", err)
			}
			got, err := memcachedExpiration(now, deadline)
			if err != nil || got != test.want {
				t.Fatalf("memcachedExpiration() = %d, %v, want %d", got, err, test.want)
			}
		})
	}

	deadline := now.Add(2*time.Hour + 500*time.Millisecond)
	if got, err := memcachedExpiration(now, deadline); err != nil || got != int32(deadline.Unix()) {
		t.Fatalf("quantized expiration = %d, %v, want %d", got, err, deadline.Unix())
	}
	if _, err := memcachedExpiration(deadline, deadline); !errors.Is(err, errExpiredPublication) {
		t.Fatalf("elapsed expiration error = %v", err)
	}
	sharedDeadline := now.Add(2 * time.Hour)
	if first, err := memcachedExpiration(now, sharedDeadline); err != nil {
		t.Fatalf("first common expiration error = %v", err)
	} else if later, err := memcachedExpiration(now.Add(time.Hour), sharedDeadline); err != nil || later != first {
		t.Fatalf("common expiration first=%d later=%d error=%v", first, later, err)
	}
	nonaligned := now.Add(500 * time.Millisecond)
	exactDeadline, err := publicationDeadline(nonaligned, 15*24*time.Hour)
	if err != nil || !exactDeadline.Equal(nonaligned.Add(30*24*time.Hour)) {
		t.Fatalf("exact publication deadline = %v, %v", exactDeadline, err)
	}
	if got, err := memcachedExpiration(nonaligned, exactDeadline); err != nil ||
		got != int32(exactDeadline.Unix()) {
		t.Fatalf("nonaligned thirty-day expiration = %d, %v", got, err)
	}
	absoluteDeadline := nonaligned.Add(31 * 24 * time.Hour)
	if got, err := memcachedExpiration(nonaligned, absoluteDeadline); err != nil ||
		got != int32(absoluteDeadline.Unix()) {
		t.Fatalf("absolute quantized expiration = %d, %v", got, err)
	}
	for _, interval := range []time.Duration{0, -time.Nanosecond, time.Nanosecond, 999 * time.Millisecond, 1500 * time.Millisecond} {
		if _, err := publicationDeadline(now, interval); !errors.Is(err, errInvalidInterval) {
			t.Fatalf("invalid interval %s error = %v", interval, err)
		}
	}
	if deadline, err := publicationDeadline(now, time.Second); err != nil || !deadline.Equal(now.Add(2*time.Second)) {
		t.Fatalf("minimum interval deadline = %v, %v", deadline, err)
	}
	if _, err := publicationDeadline(now, time.Duration(math.MaxInt64)); !errors.Is(err, errInvalidInterval) {
		t.Fatalf("overflow interval error = %v", err)
	}

	near2038 := time.Unix(math.MaxInt32-24*60*60, 0).UTC()
	lastRepresentable := time.Unix(math.MaxInt32, 0).UTC()
	if got, err := memcachedExpiration(near2038, lastRepresentable); err != nil || got != math.MaxInt32 {
		t.Fatalf("last representable expiration = %d, %v", got, err)
	}
	if got, err := memcachedExpiration(near2038, lastRepresentable.Add(time.Nanosecond)); err != nil || got != math.MaxInt32 {
		t.Fatalf("subsecond last expiration = %d, %v", got, err)
	}
	if _, err := memcachedExpiration(near2038, lastRepresentable.Add(time.Second)); !errors.Is(err, errInvalidInterval) {
		t.Fatalf("2038 overflow error = %v", err)
	}
	farDeadline, err := publicationDeadline(near2038, 16*24*time.Hour)
	if err != nil {
		t.Fatalf("publicationDeadline(2038) error = %v", err)
	}
	if _, err := memcachedExpiration(near2038, farDeadline); !errors.Is(err, errInvalidInterval) {
		t.Fatalf("2038 overflow error = %v", err)
	}
}
