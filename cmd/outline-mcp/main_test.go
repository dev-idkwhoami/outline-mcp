package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"outline-mcp/internal/catalog"
	"outline-mcp/internal/config"
)

// Run the real CLI entry point in a separate process with genuine stdio pipes.
func TestMCPProcess(t *testing.T) {
	if os.Getenv("OUTLINE_TEST_CHILD") != "1" {
		return
	}
	if err := execute(context.Background(), []string{"run", "--config", os.Getenv("OUTLINE_TEST_CONFIG")}, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

func TestStdioAllToolsAndHistory(t *testing.T) {
	for _, protocol := range []string{"2025-11-25", "2026-07-28"} {
		t.Run(protocol, func(t *testing.T) { testStdioAllToolsAndHistory(t, protocol) })
	}
}

func testStdioAllToolsAndHistory(t *testing.T, protocol string) {
	var mu sync.Mutex
	paths := []string{}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("incorrect key")
		}
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		io.WriteString(w, `{"ok":true,"data":{"id":"receipt-object"}}`)
	}))
	defer api.Close()
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.json")
	err := config.Save(cfg, config.Config{BaseURL: api.URL, APIKey: "test-key", History: true, HistoryLimit: 100, TimeoutSeconds: 10})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	start := func() *mcp.ClientSession {
		cmd := exec.Command(os.Args[0], "-test.run=^TestMCPProcess$")
		for _, e := range os.Environ() {
			if !strings.HasPrefix(e, "OUTLINE_") {
				cmd.Env = append(cmd.Env, e)
			}
		}
		cmd.Env = append(cmd.Env, "OUTLINE_TEST_CHILD=1", "OUTLINE_TEST_CONFIG="+cfg)
		cmd.Stderr = os.Stderr
		client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
		session, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, &mcp.ClientSessionOptions{ProtocolVersion: protocol})
		if err != nil {
			t.Fatal(err)
		}
		if got := session.InitializeResult().ProtocolVersion; got != protocol {
			t.Fatalf("negotiated %s; want %s", got, protocol)
		}
		return session
	}
	session := start()
	defer session.Close()
	listed, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Tools) != 74 {
		t.Fatalf("tool count: %d", len(listed.Tools))
	}
	tools, err := catalog.Load()
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]any{"id": "11111111-1111-4111-8111-111111111111", "documentId": "11111111-1111-4111-8111-111111111111", "collectionId": "11111111-1111-4111-8111-111111111111", "commentId": "11111111-1111-4111-8111-111111111111", "userId": "11111111-1111-4111-8111-111111111111", "groupId": "11111111-1111-4111-8111-111111111111", "name": "Test", "index": "a1", "publish": false, "emoji": "👍", "query": "test", "dataType": "string"}
	for _, tool := range tools {
		args := map[string]any{}
		for _, name := range tool.Schema.Required {
			v, ok := values[name]
			if !ok {
				t.Fatalf("fixture missing %s", name)
			}
			args[name] = v
		}
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: tool.Name, Arguments: args})
		if err != nil {
			t.Fatalf("%s: %v", tool.Name, err)
		}
		if result.IsError {
			b, _ := json.Marshal(result)
			t.Fatalf("%s: %s", tool.Name, b)
		}
		mu.Lock()
		got := paths[len(paths)-1]
		mu.Unlock()
		if got != "/api/"+tool.Endpoint {
			t.Fatalf("wrong route %s for %s", got, tool.Name)
		}
	}
	invalid, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "outline_documents_update", Arguments: map[string]any{"text": "missing id"}})
	if err != nil || !invalid.IsError {
		t.Fatalf("invalid args did not return tool error: %v", err)
	}
	mu.Lock()
	count := len(paths)
	mu.Unlock()
	if count != 74 {
		t.Fatalf("invalid request reached API: %d", count)
	}
	resources, err := session.ListResources(ctx, nil)
	if err != nil || len(resources.Resources) != 1 {
		t.Fatalf("resources: %v %v", resources, err)
	}
	historyURI := resources.Resources[0].URI
	resource, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: historyURI})
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
	if len(recap.Entries) != 74 {
		t.Fatalf("running server lost receipts: %d", len(recap.Entries))
	}
	for _, entry := range recap.Entries {
		if entry.Outcome != "completed" {
			t.Fatalf("wrong outcome: %s", entry.Outcome)
		}
	}
	if err = session.Close(); err != nil {
		t.Fatal(err)
	}
	// The process must not create history files beside its config.
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 1 || files[0].Name() != "config.json" {
		t.Fatalf("unexpected filesystem writes: %v %v", files, err)
	}
	restarted := start()
	defer restarted.Close()
	resources, err = restarted.ListResources(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resources.Resources[0].URI == historyURI {
		t.Fatal("restart reused volatile resource identity")
	}
	if _, err = restarted.ReadResource(ctx, &mcp.ReadResourceParams{URI: historyURI}); err == nil {
		t.Fatal("old resource URI survived restart")
	}
	resource, err = restarted.ReadResource(ctx, &mcp.ReadResourceParams{URI: resources.Resources[0].URI})
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal([]byte(resource.Contents[0].Text), &recap); err != nil {
		t.Fatal(err)
	}
	if len(recap.Entries) != 0 {
		t.Fatal("history survived process restart")
	}

}

func TestCLIRejectsOperationCommands(t *testing.T) {
	for _, cmd := range []string{"call", "tools", "schema", "history", "documents.list"} {
		var out, errOut bytes.Buffer
		if err := execute(context.Background(), []string{cmd}, &out, &errOut); err == nil {
			t.Errorf("accepted %s", cmd)
		}
	}
}
