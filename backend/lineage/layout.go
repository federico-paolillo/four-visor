// Package lineage publishes and reconstructs immutable snapshot lineages in Memcached.
package lineage

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"time"
)

const (
	blockSize            = 512 * 1024
	activePointerKey     = "fourvisor:lineage:active"
	lineageKeyPrefix     = "fourvisor:lineage:"
	completionKeySuffix  = ":complete"
	blockKeyPrefix       = ":block:"
	memcachedRelativeMax = 30 * 24 * time.Hour
)

var (
	errInvalidCompletion  = errors.New("invalid lineage completion metadata")
	errInvalidInterval    = errors.New("invalid synchronization interval")
	errExpiredPublication = errors.New("lineage publication expiry elapsed")
)

type completion struct {
	BlockCount int `json:"blockCount"`
	ByteLength int `json:"byteLength"`
}

func splitBlocks(data []byte) ([][]byte, completion, error) {
	if len(data) == 0 {
		return nil, completion{}, fmt.Errorf("%w: empty document", errInvalidCompletion)
	}

	count := 1 + (len(data)-1)/blockSize

	blocks := make([][]byte, count)
	for index := range count {
		start := index * blockSize
		end := min(start+blockSize, len(data))
		blocks[index] = bytes.Clone(data[start:end])
	}

	metadata := completion{BlockCount: count, ByteLength: len(data)}

	return blocks, metadata, nil
}

func encodeCompletion(metadata completion) ([]byte, error) {
	err := validateCompletion(metadata)
	if err != nil {
		return nil, err
	}

	data, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("encoding completion metadata: %w", err)
	}

	return data, nil
}

func decodeCompletion(data []byte) (completion, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()

	var metadata completion

	err := decoder.Decode(&metadata)
	if err != nil {
		return completion{}, fmt.Errorf("%w: %w", errInvalidCompletion, err)
	}

	var trailing json.RawMessage

	err = decoder.Decode(&trailing)
	if !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple JSON values")
		}

		return completion{}, fmt.Errorf("%w: %w", errInvalidCompletion, err)
	}

	err = validateCompletion(metadata)
	if err != nil {
		return completion{}, err
	}

	return metadata, nil
}

func validateCompletion(metadata completion) error {
	if metadata.BlockCount <= 0 || metadata.ByteLength <= 0 ||
		1+(metadata.ByteLength-1)/blockSize != metadata.BlockCount {
		return errInvalidCompletion
	}

	return nil
}

func reassemble(metadata completion, blocks [][]byte) ([]byte, error) {
	err := validateCompletion(metadata)
	if err != nil {
		return nil, err
	}

	if len(blocks) != metadata.BlockCount {
		return nil, fmt.Errorf("%w: block count mismatch", errInvalidCompletion)
	}

	data := make([]byte, 0, metadata.ByteLength)

	for index, block := range blocks {
		want := blockSize
		if index == metadata.BlockCount-1 {
			want = metadata.ByteLength - index*blockSize
		}

		if len(block) != want {
			return nil, fmt.Errorf("%w: block length mismatch", errInvalidCompletion)
		}

		data = append(data, block...)
	}

	return data, nil
}

func completionKey(lineageID string) string {
	return lineageKeyPrefix + lineageID + completionKeySuffix
}

func blockKey(lineageID string, index int) string {
	return lineageKeyPrefix + lineageID + blockKeyPrefix + strconv.Itoa(index)
}

func evictionKeys(lineageID string, metadata completion) ([]string, error) {
	err := validateCompletion(metadata)
	if err != nil {
		return nil, err
	}

	keys := make([]string, 1, metadata.BlockCount+1)
	keys[0] = completionKey(lineageID)

	for index := range metadata.BlockCount {
		keys = append(keys, blockKey(lineageID, index))
	}

	return keys, nil
}

func publicationDeadline(now time.Time, interval time.Duration) (time.Time, error) {
	if interval <= 0 || interval > time.Duration(math.MaxInt64)/2 {
		return time.Time{}, errInvalidInterval
	}

	target := now.Add(2 * interval)
	if !target.After(now) {
		return time.Time{}, errInvalidInterval
	}

	return target, nil
}

func memcachedExpiration(now, deadline time.Time) (int32, error) {
	if !deadline.After(now) {
		return 0, errExpiredPublication
	}

	absolute := deadline.Unix()
	if deadline.Nanosecond() != 0 {
		absolute++
	}

	if absolute <= int64(memcachedRelativeMax/time.Second) || absolute > math.MaxInt32 {
		return 0, errInvalidInterval
	}

	return int32(absolute), nil
}
