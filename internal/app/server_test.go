package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"outline-mcp/internal/config"
)

func connect(t *testing.T, c config.Config) (context.Context, *mcp.ClientSession) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	server, err := NewServer(c, "test")
	if err != nil {
		t.Fatal(err)
	}
	a, b := mcp.NewInMemoryTransports()
	ss, err := server.Connect(ctx, a, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, b, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return ctx, cs
}

func TestUnknownWriteOutcome(t *testing.T) {
	for _, tt := range []struct {
		name    string
		status  int
		outcome string
	}{
		{"uncertain write", 503, "unknown"}, {"rejected write", 403, "failed"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(tt.status)
				io.WriteString(w, `{"ok":false,"error":"test_error"}`)
			}))
			defer api.Close()
			ctx, session := connect(t, config.Config{BaseURL: api.URL, APIKey: "test-key", History: true, HistoryLimit: 10, TimeoutSeconds: 2})
			res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "outline_documents_create", Arguments: map[string]any{"title": "private text"}})
			if err != nil || !res.IsError {
				t.Fatalf("expected tool error: %v %+v", err, res)
			}
			if calls != 1 {
				t.Fatalf("calls: %d; want 1", calls)
			}
			resources, err := session.ListResources(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			resource, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: resources.Resources[0].URI})
			if err != nil {
				t.Fatal(err)
			}
			var recap struct {
				Entries []struct {
					Outcome string `json:"outcome"`
				}
			}
			if err = json.Unmarshal([]byte(resource.Contents[0].Text), &recap); err != nil {
				t.Fatal(err)
			}
			if len(recap.Entries) != 1 || recap.Entries[0].Outcome != tt.outcome {
				t.Fatalf("unexpected recap: %s", resource.Contents[0].Text)
			}
		})
	}
}

func TestErrorsReachModelAsTextAndStructuredContent(t *testing.T) {
	calls := 0
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(401)
		io.WriteString(w, `{"ok":false,"message":"key revoked"}`)
	}))
	defer api.Close()
	ctx, session := connect(t, config.Config{BaseURL: api.URL, APIKey: "test-key", HistoryLimit: 10, TimeoutSeconds: 2})
	for _, tt := range []struct {
		args                map[string]any
		code, hint, outcome string
	}{
		{map[string]any{"text": "missing id"}, "invalid_arguments", "required", "not_sent"},
		{map[string]any{"id": "doc"}, "authentication_failed", "setup", "failed"},
	} {
		res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "outline_documents_update", Arguments: tt.args})
		if err != nil || !res.IsError {
			t.Fatalf("error was hidden from model: %v", err)
		}
		if res.StructuredContent == nil || len(res.Content) == 0 {
			t.Fatal("missing structured/text error")
		}
		text, ok := res.Content[0].(*mcp.TextContent)
		if !ok {
			t.Fatal("missing text")
		}
		var payload struct {
			Error struct {
				Endpoint, Code, Message, Detail string
				NextStep                        string `json:"next_step"`
			}
			Receipt struct{ Outcome string }
		}
		if err = json.Unmarshal([]byte(text.Text), &payload); err != nil {
			t.Fatal(err)
		}
		if payload.Error.Code != tt.code || payload.Error.Endpoint != "documents.update" || payload.Error.Message == "" || payload.Error.NextStep == "" || payload.Receipt.Outcome != tt.outcome {
			t.Fatalf("uninformative: %s", text.Text)
		}
		if !strings.Contains(text.Text, tt.hint) {
			t.Fatalf("missing recovery detail: %s", text.Text)
		}
	}
	if calls != 1 {
		t.Fatalf("invalid args reached server: %d calls", calls)
	}
}
