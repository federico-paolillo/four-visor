// Package snapshot defines and enforces the exact snapshot version 1 JSON contract.
package snapshot

import "encoding/json"

const (
	// Version is the only snapshot schema version accepted by 4Visor.
	Version = 1
	// MaximumCatalogThreads is the version 1 catalog cardinality limit.
	MaximumCatalogThreads = 250
	// MaximumThreadPosts is the version 1 thread cardinality limit.
	MaximumThreadPosts = 250

	// StateFailed marks a known resource whose acquisition failed.
	StateFailed State = "failed"
	// StatePresent marks a complete resource payload.
	StatePresent State = "present"
	// StateOversize marks a thread truncated to the first 250 posts.
	StateOversize State = "oversize"
)

// State is a contract-owned resource state.
type State string

// Snapshot is one immutable version 1 lineage.
type Snapshot struct {
	SchemaVersion int    `json:"schemaVersion"`
	LineageID     string `json:"lineageId"`
	ObservedAt    string `json:"observedAt"`
	Boards        Boards `json:"boards"`
}

// Boards is either a failed board-list resource or its ordered items.
type Boards struct {
	State State        `json:"state"`
	Items *[]BoardItem `json:"items,omitempty"`
}

// BoardItem preserves one upstream board object and its optional catalog.
type BoardItem struct {
	Board   json.RawMessage `json:"board"`
	Catalog *Catalog        `json:"catalog,omitempty"`
}

// Catalog is either a failed catalog resource or its ordered pages.
type Catalog struct {
	State State   `json:"state"`
	Pages *[]Page `json:"pages,omitempty"`
}

// Page preserves upstream page metadata and its ordered thread entries.
type Page struct {
	Metadata json.RawMessage `json:"metadata"`
	Threads  []ThreadEntry   `json:"threads"`
}

// ThreadEntry preserves an upstream summary and its optional thread resource.
type ThreadEntry struct {
	Summary json.RawMessage `json:"summary"`
	Thread  *Thread         `json:"thread,omitempty"`
}

// Thread is failed, present with at most 250 posts, or oversize with exactly 250 posts.
type Thread struct {
	State State              `json:"state"`
	Posts *[]json.RawMessage `json:"posts,omitempty"`
}

// FailedResourceCount returns the number of explicit failed wrappers in the board tree.
func (boards Boards) FailedResourceCount() int {
	if boards.State == StateFailed {
		return 1
	}

	if boards.Items == nil {
		return 0
	}

	count := 0

	for _, item := range *boards.Items {
		count += failedCatalogResourceCount(item.Catalog)
	}

	return count
}

func failedCatalogResourceCount(catalog *Catalog) int {
	if catalog == nil {
		return 0
	}

	if catalog.State == StateFailed {
		return 1
	}

	if catalog.Pages == nil {
		return 0
	}

	count := 0

	for _, page := range *catalog.Pages {
		for _, entry := range page.Threads {
			if entry.Thread != nil && entry.Thread.State == StateFailed {
				count++
			}
		}
	}

	return count
}
