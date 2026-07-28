package acquisition

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"

	"git.disroot.org/federico-paolillo/four-visor.git/snapshot"
)

type observedBoard struct {
	raw json.RawMessage
	id  string
}

func parseBoards(data []byte) ([]observedBoard, error) {
	var document map[string]json.RawMessage

	err := decodeDocument(data, &document)
	if err != nil {
		return nil, fmt.Errorf("decoding board list: %w", err)
	}

	if document == nil {
		return nil, errors.New("board list must be an object")
	}

	rawBoards, exists := document["boards"]
	if !exists || firstByte(rawBoards) != '[' {
		return nil, errors.New("board list must contain a boards array")
	}

	var values []json.RawMessage

	err = json.Unmarshal(rawBoards, &values)
	if err != nil {
		return nil, fmt.Errorf("decoding boards array: %w", err)
	}

	boards := make([]observedBoard, len(values))
	for index, raw := range values {
		board, err := parseBoard(raw)
		if err != nil {
			return nil, err
		}

		boards[index] = board
	}

	return boards, nil
}

func parseBoard(raw json.RawMessage) (observedBoard, error) {
	if firstByte(raw) != '{' {
		return observedBoard{}, errors.New("board entry must be an object")
	}

	var object map[string]json.RawMessage

	err := json.Unmarshal(raw, &object)
	if err != nil {
		return observedBoard{}, fmt.Errorf("decoding board entry: %w", err)
	}

	var id string

	err = json.Unmarshal(object["board"], &id)
	if err != nil || id == "" {
		return observedBoard{}, errors.New("board entry must contain a non-empty string identifier")
	}

	return observedBoard{raw: raw, id: id}, nil
}

func parseCatalog(data []byte) ([]snapshot.Page, error) {
	if firstByte(data) != '[' {
		return nil, errors.New("catalog must be an array")
	}

	var values []json.RawMessage

	err := decodeDocument(data, &values)
	if err != nil {
		return nil, fmt.Errorf("decoding catalog: %w", err)
	}

	pages := make([]snapshot.Page, len(values))
	remaining := snapshot.MaximumCatalogThreads

	for index, raw := range values {
		page, err := parsePage(raw, remaining)
		if err != nil {
			return nil, err
		}

		pages[index] = page
		remaining -= len(page.Threads)
	}

	return pages, nil
}

func parsePage(raw json.RawMessage, remaining int) (snapshot.Page, error) {
	if firstByte(raw) != '{' {
		return snapshot.Page{}, errors.New("catalog page must be an object")
	}

	var object map[string]json.RawMessage

	err := json.Unmarshal(raw, &object)
	if err != nil {
		return snapshot.Page{}, fmt.Errorf("decoding catalog page: %w", err)
	}

	rawThreads, exists := object["threads"]
	if !exists || firstByte(rawThreads) != '[' {
		return snapshot.Page{}, errors.New("catalog page must contain a threads array")
	}

	var summaries []json.RawMessage

	err = json.Unmarshal(rawThreads, &summaries)
	if err != nil {
		return snapshot.Page{}, fmt.Errorf("decoding catalog threads: %w", err)
	}

	for _, summary := range summaries {
		if firstByte(summary) != '{' {
			return snapshot.Page{}, errors.New("catalog summary must be an object")
		}
	}

	delete(object, "threads")

	metadata, err := json.Marshal(object)
	if err != nil {
		return snapshot.Page{}, fmt.Errorf("encoding catalog page metadata: %w", err)
	}

	count := min(len(summaries), remaining)
	threads := make([]snapshot.ThreadEntry, count)

	for index, summary := range summaries[:count] {
		threads[index] = snapshot.ThreadEntry{Summary: summary}
	}

	return snapshot.Page{Metadata: metadata, Threads: threads}, nil
}

func decodeDocument(data []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))

	err := decoder.Decode(destination)
	if err != nil {
		return fmt.Errorf("decoding JSON document: %w", err)
	}

	var trailing json.RawMessage

	err = decoder.Decode(&trailing)
	if !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}

		return fmt.Errorf("decoding trailing JSON: %w", err)
	}

	return nil
}

func (client *Client) boardsURL() string {
	return endpointURL(client.baseURL, "boards.json")
}

func (client *Client) catalogURL(board string) string {
	return endpointURL(client.baseURL, url.PathEscape(board), "catalog.json")
}

func endpointURL(base *url.URL, escapedElements ...string) string {
	target := *base
	escapedPath := strings.TrimSuffix(base.EscapedPath(), "/") + "/" + strings.Join(escapedElements, "/")

	decodedPath, err := url.PathUnescape(escapedPath)
	if err != nil {
		panic("escaped acquisition path cannot be invalid")
	}

	target.Path = decodedPath
	target.RawPath = escapedPath

	return target.String()
}

func firstByte(raw []byte) byte {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return 0
	}

	return trimmed[0]
}
