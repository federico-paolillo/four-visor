package acquisition

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"git.disroot.org/federico-paolillo/four-visor.git/snapshot"
)

func TestParseBoardsPreservesOpaqueObjectsAndOrder(t *testing.T) {
	data := []byte(`{"boards":[{"board":"z","nested":{"value":1.20},"unknown":[3,2,1]},{"board":"a","title":null}]}`)

	boards, err := parseBoards(data)
	if err != nil {
		t.Fatalf("parseBoards() error = %v", err)
	}
	if got := []string{boards[0].id, boards[1].id}; !reflect.DeepEqual(got, []string{"z", "a"}) {
		t.Fatalf("board order = %v, want [z a]", got)
	}
	if string(boards[0].raw) != `{"board":"z","nested":{"value":1.20},"unknown":[3,2,1]}` {
		t.Fatalf("opaque board changed: %s", boards[0].raw)
	}
}

func TestParseCatalogKeepsPageBoundariesAndFirst250Threads(t *testing.T) {
	data := catalogDocument(t, []int{249, 3, 1})

	pages, err := parseCatalog(data)
	if err != nil {
		t.Fatalf("parseCatalog() error = %v", err)
	}
	if got := []int{len(pages[0].Threads), len(pages[1].Threads), len(pages[2].Threads)}; !reflect.DeepEqual(got, []int{249, 1, 0}) {
		t.Fatalf("page thread counts = %v, want [249 1 0]", got)
	}

	for index, page := range pages {
		var metadata map[string]json.RawMessage
		if err := json.Unmarshal(page.Metadata, &metadata); err != nil {
			t.Fatalf("page %d metadata error = %v", index, err)
		}
		if _, exists := metadata["threads"]; exists {
			t.Fatalf("page %d metadata retained threads", index)
		}
		if string(metadata["page"]) != fmt.Sprint(index+1) || string(metadata["nested"]) != `{"kept":[3,2,1]}` {
			t.Fatalf("page %d metadata changed: %s", index, page.Metadata)
		}
	}

	var boundary map[string]int
	if err := json.Unmarshal(pages[1].Threads[0].Summary, &boundary); err != nil {
		t.Fatalf("boundary summary error = %v", err)
	}
	if boundary["no"] != 250 {
		t.Fatalf("boundary thread = %d, want 250", boundary["no"])
	}
}

func TestUpstreamValidation(t *testing.T) {
	tests := []struct {
		name  string
		parse func([]byte) error
		data  string
	}{
		{name: "malformed boards", parse: parseBoardsError, data: `{"boards":[`},
		{name: "trailing boards", parse: parseBoardsError, data: `{"boards":[]} {}`},
		{name: "null board root", parse: parseBoardsError, data: `null`},
		{name: "missing boards", parse: parseBoardsError, data: `{}`},
		{name: "null boards", parse: parseBoardsError, data: `{"boards":null}`},
		{name: "board not object", parse: parseBoardsError, data: `{"boards":["a"]}`},
		{name: "missing board identifier", parse: parseBoardsError, data: `{"boards":[{"title":"A"}]}`},
		{name: "empty board identifier", parse: parseBoardsError, data: `{"boards":[{"board":""}]}`},
		{name: "catalog not array", parse: parseCatalogError, data: `{}`},
		{name: "catalog page not object", parse: parseCatalogError, data: `[null]`},
		{name: "missing threads", parse: parseCatalogError, data: `[{"page":1}]`},
		{name: "null threads", parse: parseCatalogError, data: `[{"page":1,"threads":null}]`},
		{name: "summary not object", parse: parseCatalogError, data: `[{"page":1,"threads":[1]}]`},
		{name: "trailing catalog", parse: parseCatalogError, data: `[] []`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.parse([]byte(test.data)); err == nil {
				t.Fatal("parse error = nil, want invalid upstream response")
			}
		})
	}
}

func catalogDocument(t *testing.T, pageSizes []int) []byte {
	t.Helper()
	pages := make([]map[string]any, len(pageSizes))
	number := 1
	for pageIndex, size := range pageSizes {
		threads := make([]map[string]int, size)
		for threadIndex := range size {
			threads[threadIndex] = map[string]int{"no": number}
			number++
		}
		pages[pageIndex] = map[string]any{
			"page":    pageIndex + 1,
			"nested":  map[string]any{"kept": []int{3, 2, 1}},
			"threads": threads,
		}
	}

	data, err := json.Marshal(pages)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	return data
}

func parseBoardsError(data []byte) error {
	_, err := parseBoards(data)

	return err
}

func parseCatalogError(data []byte) error {
	_, err := parseCatalog(data)

	return err
}

func completeSnapshot(boards snapshot.Boards) snapshot.Snapshot {
	return snapshot.Snapshot{
		SchemaVersion: snapshot.Version,
		LineageID:     "01J1YQ7Y0M4S6R8T2V3W5X7Y9Z",
		ObservedAt:    "2026-07-28T12:00:00Z",
		Boards:        boards,
	}
}
