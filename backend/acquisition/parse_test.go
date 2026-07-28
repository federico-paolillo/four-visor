package acquisition

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
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

func TestParseThreadBoundsAndPreservesOpaquePosts(t *testing.T) {
	tests := []struct {
		name      string
		count     int
		wantState snapshot.State
	}{
		{name: "empty", count: 0, wantState: snapshot.StatePresent},
		{name: "limit", count: snapshot.MaximumThreadPosts, wantState: snapshot.StatePresent},
		{name: "oversize", count: snapshot.MaximumThreadPosts + 1, wantState: snapshot.StateOversize},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			thread, err := parseThread(threadDocument(t, test.count))
			if err != nil {
				t.Fatalf("parseThread() error = %v", err)
			}
			if thread.State != test.wantState || thread.Posts == nil ||
				len(*thread.Posts) != min(test.count, snapshot.MaximumThreadPosts) {
				t.Fatalf("parseThread() = %#v", thread)
			}
			if test.count > 0 && string((*thread.Posts)[0]) !=
				`{"no":1,"com":"<b>post 1</b>","media":{"tim":1001,"ext":".png"}}` {
				t.Fatalf("opaque first post changed: %s", (*thread.Posts)[0])
			}
			if test.count > snapshot.MaximumThreadPosts {
				if cap(*thread.Posts) != snapshot.MaximumThreadPosts {
					t.Fatalf("retained post capacity = %d", cap(*thread.Posts))
				}
				for _, post := range *thread.Posts {
					if string(post) == `{"no":251,"com":"<b>post 251</b>","media":{"tim":1251,"ext":".png"}}` {
						t.Fatal("post 251 was retained")
					}
				}
			}
		})
	}
}

func TestThreadNumber(t *testing.T) {
	number, err := threadNumber(json.RawMessage(`{"no":18446744073709551615,"other":{"kept":true}}`))
	if err != nil || number != ^uint64(0) {
		t.Fatalf("threadNumber() = %d, %v", number, err)
	}

	for _, summary := range []string{
		`{}`,
		`{"no":0}`,
		`{"no":-1}`,
		`{"no":1.5}`,
		`{"no":"1"}`,
	} {
		if _, err := threadNumber(json.RawMessage(summary)); err == nil {
			t.Fatalf("threadNumber(%s) error = nil", summary)
		}
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
		{name: "thread not object", parse: parseThreadError, data: `[]`},
		{name: "missing posts", parse: parseThreadError, data: `{}`},
		{name: "null posts", parse: parseThreadError, data: `{"posts":null}`},
		{name: "post not object", parse: parseThreadError, data: `{"posts":[1]}`},
		{name: "trailing thread", parse: parseThreadError, data: `{"posts":[]} {}`},
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

func threadDocument(t *testing.T, count int) []byte {
	t.Helper()
	var output strings.Builder
	output.WriteString(`{"posts":[`)
	for index := range count {
		if index > 0 {
			output.WriteByte(',')
		}
		_, _ = fmt.Fprintf(&output,
			`{"no":%d,"com":"<b>post %d</b>","media":{"tim":%d,"ext":".png"}}`,
			index+1,
			index+1,
			1001+index,
		)
	}
	output.WriteString(`]}`)

	return []byte(output.String())
}

func parseBoardsError(data []byte) error {
	_, err := parseBoards(data)

	return err
}

func parseCatalogError(data []byte) error {
	_, err := parseCatalog(data)

	return err
}

func parseThreadError(data []byte) error {
	_, err := parseThread(data)

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
