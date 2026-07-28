package snapshot

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const fixtureRoot = "../../testdata/snapshot-v1"

func TestSharedFixtureCorpus(t *testing.T) {
	tests := []struct {
		directory string
		want      error
	}{
		{directory: "valid"},
		{directory: "invalid-contract", want: ErrInvalidContract},
		{directory: "unsupported-version", want: ErrUnsupportedVersion},
		{directory: "invalid-json", want: ErrInvalidJSON},
	}

	for _, test := range tests {
		t.Run(test.directory, func(t *testing.T) {
			paths, err := filepath.Glob(filepath.Join(fixtureRoot, test.directory, "*.json"))
			if err != nil {
				t.Fatalf("filepath.Glob() error = %v", err)
			}
			if len(paths) == 0 {
				t.Fatal("fixture directory is empty")
			}

			for _, path := range paths {
				t.Run(filepath.Base(path), func(t *testing.T) {
					data, readError := os.ReadFile(path)
					if readError != nil {
						t.Fatalf("os.ReadFile() error = %v", readError)
					}

					parsed, parseError := Parse(data)
					if test.want == nil {
						if parseError != nil {
							t.Fatalf("Parse() error = %v", parseError)
						}
						if validationError := Validate(parsed); validationError != nil {
							t.Fatalf("Validate() error = %v", validationError)
						}

						return
					}
					if !errors.Is(parseError, test.want) {
						t.Fatalf("Parse() error = %v, want classification %v", parseError, test.want)
					}
				})
			}
		})
	}
}

func TestMarshalMatchesFrontendIntegrationFixture(t *testing.T) {
	firstPosts := []json.RawMessage{
		json.RawMessage(`{"no":300,"com":"<b>first</b>","file":{"name":"one","ext":".png"},"unknown":[3,1,2]}`),
		json.RawMessage(`{"no":301,"com":"<i>second</i>","nullable":null}`),
	}
	emptyPosts := []json.RawMessage{}
	threads := []ThreadEntry{
		{
			Summary: json.RawMessage(`{"no":300,"subject":"first","custom":{"z":1,"a":2}}`),
			Thread:  &Thread{State: StatePresent, Posts: &firstPosts},
		},
		{
			Summary: json.RawMessage(`{"no":200,"subject":"second"}`),
			Thread:  &Thread{State: StatePresent, Posts: &emptyPosts},
		},
	}
	pages := []Page{{
		Metadata: json.RawMessage(`{"page":1,"extra":"kept","ranges":[3,2,1]}`),
		Threads:  threads,
	}}
	items := []BoardItem{{
		Board: json.RawMessage(`{"board":"g","title":"Technology","ws_board":1,"nested":{"enabled":true,"value":null}}`),
		Catalog: &Catalog{
			State: StatePresent,
			Pages: &pages,
		},
	}}
	value := Snapshot{
		SchemaVersion: Version,
		LineageID:     "01J1YQ7Y0M4S6R8T2V3W5X7Y9Z",
		ObservedAt:    "2026-07-26T12:00:00+00:00",
		Boards:        Boards{State: StatePresent, Items: &items},
	}

	got, err := Marshal(value)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	want, err := os.ReadFile(filepath.Join(fixtureRoot, "valid", "backend-serialized.json"))
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}
	if !sameJSON(t, got, want) {
		t.Fatalf("Marshal() output does not match backend-serialized.json\ngot: %s\nwant: %s", got, want)
	}

	parsed, err := Parse(got)
	if err != nil {
		t.Fatalf("Parse(Marshal()) error = %v", err)
	}
	if parsed.ObservedAt != value.ObservedAt {
		t.Fatalf("observedAt = %q, want preserved %q", parsed.ObservedAt, value.ObservedAt)
	}
	if !bytes.Contains((*(*parsed.Boards.Items)[0].Catalog.Pages)[0].Threads[0].Summary, []byte(`"z":1,"a":2`)) {
		t.Fatal("opaque summary field order was not preserved")
	}
}

func TestUTCSpellingsAndULIDCasing(t *testing.T) {
	for _, observedAt := range []string{
		"2026-07-26T12:00:00Z",
		"2026-07-26T12:00:00.12345678901234567890Z",
		"2026-07-26T12:00:00+00:00",
		"2026-07-26T12:00:00.5+00:00",
	} {
		t.Run(observedAt, func(t *testing.T) {
			data := minimalDocument("01j1yq7y0m4s6r8t2v3w5x7y9z", observedAt)
			parsed, err := Parse(data)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			if parsed.LineageID != "01j1yq7y0m4s6r8t2v3w5x7y9z" || parsed.ObservedAt != observedAt {
				t.Fatalf("Parse() normalized contract strings: %#v", parsed)
			}
		})
	}

	for _, observedAt := range []string{
		"2026-07-26T12:00:00-00:00",
		"2026-07-26T12:00:00+00:01",
		"2026-07-26T12:00:00-01:00",
	} {
		t.Run(observedAt, func(t *testing.T) {
			_, err := Parse(minimalDocument("01J1YQ7Y0M4S6R8T2V3W5X7Y9Z", observedAt))
			if !errors.Is(err, ErrInvalidContract) {
				t.Fatalf("Parse() error = %v, want invalid contract", err)
			}
		})
	}
}

func TestErrorClassificationCauseAndDiagnostic(t *testing.T) {
	_, err := Parse([]byte(`{"schemaVersion":!}`))
	if !errors.Is(err, ErrInvalidJSON) {
		t.Fatalf("Parse() error = %v, want invalid JSON", err)
	}
	var syntaxError *json.SyntaxError
	if !errors.As(err, &syntaxError) {
		t.Fatalf("Parse() error = %v, want preserved *json.SyntaxError", err)
	}

	const secret = "attacker-controlled-secret"
	_, err = Parse([]byte(`{"schemaVersion":1,"lineageId":"01J1YQ7Y0M4S6R8T2V3W5X7Y9Z","observedAt":"2026-07-26T12:00:00Z","boards":{"state":"failed"},"` + secret + `":true}`))
	if !errors.Is(err, ErrInvalidContract) || strings.Contains(err.Error(), secret) {
		t.Fatalf("Parse() error = %v, want redacted invalid contract", err)
	}

	_, err = Parse([]byte(`{"schemaVersion":2,"lineageId":"01J1YQ7Y0M4S6R8T2V3W5X7Y9Z","observedAt":"2026-07-26T12:00:00Z","boards":{"state":"failed"}}`))
	if !errors.Is(err, ErrUnsupportedVersion) {
		t.Fatalf("Parse() error = %v, want unsupported version", err)
	}
}

func minimalDocument(lineageID, observedAt string) []byte {
	value := map[string]any{
		"schemaVersion": Version,
		"lineageId":     lineageID,
		"observedAt":    observedAt,
		"boards":        map[string]any{"state": StateFailed},
	}
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}

	return data
}

func sameJSON(t *testing.T, left, right []byte) bool {
	t.Helper()
	var leftValue any
	if err := json.Unmarshal(left, &leftValue); err != nil {
		t.Fatalf("json.Unmarshal(left) error = %v", err)
	}
	var rightValue any
	if err := json.Unmarshal(right, &rightValue); err != nil {
		t.Fatalf("json.Unmarshal(right) error = %v", err)
	}

	return reflect.DeepEqual(leftValue, rightValue)
}
