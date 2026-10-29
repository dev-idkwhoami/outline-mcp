// Package history keeps bounded, content-free receipts in RAM for one server run.
// It performs no filesystem I/O and does not infer conversation identity.
package history

import (
	"crypto/rand"
	"encoding/json"
	"maps"
	"regexp"
	"slices"
	"sort"
	"sync"
	"time"

	"outline-mcp/internal/config"
)

type Entry struct {
	ID        string            `json:"id"`
	Started   time.Time         `json:"started"`
	Finished  *time.Time        `json:"finished,omitempty"`
	Tool      string            `json:"tool"`
	Endpoint  string            `json:"endpoint"`
	ReadOnly  bool              `json:"read_only"`
	Outcome   string            `json:"outcome"`
	Targets   map[string]string `json:"targets,omitempty"`
	ResultIDs []string          `json:"result_ids,omitempty"`
	Status    int               `json:"http_status,omitempty"`
	ErrorCode string            `json:"error_code,omitempty"`
}
type Store struct {
	runID   string
	enabled bool
	limit   int
	mu      sync.Mutex
	entries []Entry
}
type Recap struct {
	Enabled bool    `json:"enabled"`
	RunID   string  `json:"run_id"`
	Note    string  `json:"note"`
	Entries []Entry `json:"entries"`
}

func New(c config.Config) *Store {
	return &Store{runID: rand.Text(), enabled: c.History, limit: max(1, c.HistoryLimit)}
}

// URI explicitly identifies this volatile resource; it changes after restart.
func (s *Store) URI() string { return "outline://history/" + s.runID }

var identifier = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func (s *Store) Start(tool, endpoint string, readOnly bool, args map[string]any) string {
	if !s.enabled {
		return ""
	}
	e := Entry{ID: rand.Text(), Started: time.Now().UTC(), Tool: tool, Endpoint: endpoint, ReadOnly: readOnly, Outcome: "in_progress", Targets: map[string]string{}}
	for _, k := range []string{"id", "documentId", "collectionId", "commentId", "revisionId", "templateId", "userId", "groupId", "parentDocumentId"} {
		if v, ok := args[k].(string); ok && identifier.MatchString(v) {
			e.Targets[k] = v
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.entries) == s.limit {
		copy(s.entries, s.entries[1:])
		s.entries[len(s.entries)-1] = e
	} else {
		s.entries = append(s.entries, e)
	}
	return e.ID
}

func (s *Store) Finish(id, outcome string, status int, code string, response json.RawMessage) {
	if id == "" {
		return
	}
	var ids []string
	var body any
	if json.Unmarshal(response, &body) == nil {
		set := map[string]bool{}
		collectIDs(body, set)
		for id := range set {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		if len(ids) > 100 {
			ids = ids[:100]
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.entries {
		e := &s.entries[i]
		if e.ID != id {
			continue
		}
		now := time.Now().UTC()
		e.Finished = &now
		e.Outcome = outcome
		e.Status = status
		e.ResultIDs = ids
		if identifier.MatchString(code) {
			e.ErrorCode = code
		}
		return
	}
	// Calls can complete after retention evicts them. Do not resurrect old entries.
}

func collectIDs(v any, set map[string]bool) {
	switch x := v.(type) {
	case []any:
		for _, e := range x {
			collectIDs(e, set)
		}
	case map[string]any:
		for k, v := range x {
			if k == "id" {
				if id, ok := v.(string); ok && identifier.MatchString(id) {
					set[id] = true
				}
			} else if k == "data" || k == "documents" || k == "collections" || k == "memberships" || k == "users" || k == "groups" || k == "groupMemberships" {
				collectIDs(v, set)
			}
		}
	}
}

func (s *Store) Read() Recap {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := Recap{Enabled: s.enabled, RunID: s.runID, Entries: make([]Entry, len(s.entries)), Note: "Process-wide receipts held only in RAM, oldest first; they may span unrelated conversations. This is not a transcript or plan. Restarting loses all entries. Unknown/in-progress calls may have succeeded: check Outline before repeating writes. An empty recap does not prove no earlier work occurred."}
	if !s.enabled {
		out.Note = "History is disabled. Enable in-memory history through setup. No history is written to disk."
	}
	for i, e := range s.entries {
		e.Targets = maps.Clone(e.Targets)
		e.ResultIDs = slices.Clone(e.ResultIDs)
		if e.Finished != nil {
			finished := *e.Finished
			e.Finished = &finished
		}
		out.Entries[i] = e
	}
	return out
}
