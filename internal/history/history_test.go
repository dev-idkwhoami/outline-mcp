package history

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"outline-mcp/internal/config"
)

func TestVolatileHistoryPrivacyAndRetention(t *testing.T) {
	c := config.Config{History: true, HistoryLimit: 2}
	s := New(c)
	id := s.Start("outline_documents_update", "documents.update", false, map[string]any{"id": "doc-1", "text": "private document text", "title": "private title"})
	s.Finish(id, "completed", 200, "", json.RawMessage(`{"data":{"id":"doc-1","text":"private document text"}}`))
	recap := s.Read()
	if len(recap.Entries) != 1 || recap.Entries[0].Outcome != "completed" {
		t.Fatalf("%+v", recap)
	}
	raw, _ := json.Marshal(recap)
	for _, secret := range []string{"private document text", "private title"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("history leaked %s", secret)
		}
	}
	restarted := New(c)
	if len(restarted.Read().Entries) != 0 || restarted.URI() == s.URI() {
		t.Fatal("new run reused history or resource identity")
	}
	pending := s.Start("outline_documents_create", "documents.create", false, map[string]any{})
	if s.Read().Entries[1].Outcome != "in_progress" {
		t.Fatal("unfinished call incorrectly completed")
	}
	s.Finish(pending, "unknown", 503, "unavailable", nil)
	third := s.Start("outline_documents_info", "documents.info", true, map[string]any{"id": "doc-1"})
	s.Finish(third, "completed", 200, "", nil)
	recap = s.Read()
	if len(recap.Entries) != 2 || recap.Entries[0].ID != pending {
		t.Fatalf("retention failed: %+v", recap)
	}
}

func TestConcurrentSnapshotsAndIsolation(t *testing.T) {
	s := New(config.Config{History: true, HistoryLimit: 100})
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Go(func() {
			id := s.Start("tool", "documents.update", false, map[string]any{"id": "doc"})
			_ = s.Read()
			s.Finish(id, "completed", 200, "", json.RawMessage(`{"data":{"id":"result"}}`))
		})
	}
	wg.Wait()
	recap := s.Read()
	if len(recap.Entries) != 20 {
		t.Fatalf("%d", len(recap.Entries))
	}
	recap.Entries[0].Targets["id"] = "changed"
	recap.Entries[0].ResultIDs[0] = "changed"
	*recap.Entries[0].Finished = recap.Entries[0].Started
	fresh := s.Read()
	if fresh.Entries[0].Targets["id"] != "doc" || fresh.Entries[0].ResultIDs[0] != "result" {
		t.Fatal("snapshot aliases internal data")
	}
}

func TestPendingEvictionAndDisabledHistory(t *testing.T) {
	s := New(config.Config{History: true, HistoryLimit: 1})
	first := s.Start("tool", "documents.create", false, nil)
	second := s.Start("tool", "documents.update", false, nil)
	s.Finish(first, "completed", 200, "", nil)
	if entries := s.Read().Entries; len(entries) != 1 || entries[0].ID != second {
		t.Fatal("evicted call was resurrected")
	}
	disabled := New(config.Config{History: false, HistoryLimit: 1})
	id := disabled.Start("tool", "documents.create", false, nil)
	disabled.Finish(id, "completed", 200, "", nil)
	if id != "" || disabled.Read().Enabled || len(disabled.Read().Entries) != 0 {
		t.Fatal("disabled history retained entries")
	}
}
