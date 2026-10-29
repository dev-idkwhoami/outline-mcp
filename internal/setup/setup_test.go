package setup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"outline-mcp/internal/config"
	"outline-mcp/skills"
)

func TestInteractiveHarnessInstallation(t *testing.T) {
	for _, harness := range []string{"codex", "claude", "generic"} {
		t.Run(harness, func(t *testing.T) {
			dir := t.TempDir()
			cfg := filepath.Join(dir, "config.json")
			bin := filepath.Join(dir, "path with spaces", "outline-mcp")
			var out bytes.Buffer
			var command string
			var argv []string
			input := "https://outline.example\nsecret-key\ny\n"
			if harness == "generic" {
				input += filepath.Join(dir, "custom-skill") + "\n"
			}
			input += "y\n"
			i := Installer{In: strings.NewReader(input), Out: &out, Home: dir, LookPath: func(s string) (string, error) { return s, nil }, RunCommand: func(_ context.Context, s string, args ...string) error { command = s; argv = args; return nil }}
			if err := i.Run(context.Background(), Options{ConfigPath: cfg, Executable: bin, Harness: harness}); err != nil {
				t.Fatal(err)
			}
			c, err := config.Load(cfg)
			if err != nil || c.APIKey != "secret-key" || !c.History {
				t.Fatalf("config: %+v %v", c, err)
			}
			if strings.Contains(out.String(), "secret-key") {
				t.Fatal("key leaked to output")
			}
			var skill string
			switch harness {
			case "codex":
				skill = filepath.Join(dir, ".agents", "skills", "outline", "SKILL.md")
				if command != "codex" || !reflect.DeepEqual(argv, []string{"mcp", "add", "outline", "--", bin, "run", "--config", cfg}) {
					t.Fatalf("%s %v", command, argv)
				}
			case "claude":
				skill = filepath.Join(dir, ".claude", "skills", "outline", "SKILL.md")
				if command != "claude" || !reflect.DeepEqual(argv[:5], []string{"mcp", "add-json", "--scope", "user", "outline"}) {
					t.Fatalf("%s %v", command, argv)
				}
				var entry ServerConfig
				if err = json.Unmarshal([]byte(argv[5]), &entry); err != nil || entry.Command != bin {
					t.Fatal("invalid registration JSON")
				}
			default:
				skill = filepath.Join(dir, "custom-skill", "SKILL.md")
				if command != "" {
					t.Fatal("ran a command for generic client")
				}
			}
			b, err := os.ReadFile(skill)
			if err != nil || !bytes.Equal(b, skills.Outline) {
				t.Fatalf("skill install: %v", err)
			}
		})
	}
}

func TestNoHarnessFallbackAndCancellation(t *testing.T) {
	for _, answer := range []string{"n\n", ""} {
		dir := t.TempDir()
		cfg := filepath.Join(dir, "config.json")
		var out bytes.Buffer
		i := Installer{In: strings.NewReader("https://example.com\nkey\nn\n\n\n" + answer), Out: &out, Home: dir, LookPath: func(string) (string, error) { return "", errors.New("missing") }}
		err := i.Run(context.Background(), Options{ConfigPath: cfg, Executable: "outline-mcp"})
		if answer == "" && err == nil {
			t.Fatal("EOF accepted as approval")
		}
		if _, err = os.Stat(cfg); !os.IsNotExist(err) {
			t.Fatal("wrote config without approval")
		}
		if !strings.Contains(out.String(), "Generic setup") {
			t.Fatal("no generic fallback")
		}
	}
}

func TestPrintConfigDoesNotReadSecretsOrWrite(t *testing.T) {
	var out bytes.Buffer
	path := filepath.Join(t.TempDir(), "missing.json")
	i := Installer{Out: &out}
	if err := i.Run(context.Background(), Options{ConfigPath: path, Executable: "/some path/outline-mcp", PrintConfig: true}); err != nil {
		t.Fatal(err)
	}
	var doc map[string]map[string]ServerConfig
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if doc["mcpServers"]["outline"].Args[0] != "run" {
		t.Fatal("wrong launch command")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("created config")
	}
}

func TestBackupAndRegistrationFailure(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.json")
	old := []byte(`{"base_url":"https://old.example","api_key":"old-key","timeout_seconds":60,"history_limit":500}`)
	os.WriteFile(cfg, old, 0600)
	var out bytes.Buffer
	i := Installer{In: strings.NewReader("\nnew-key\nn\ny\n"), Out: &out, Home: dir, LookPath: func(s string) (string, error) { return s, nil }, RunCommand: func(context.Context, string, ...string) error { return errors.New("test registration failure") }}
	err := i.Run(context.Background(), Options{ConfigPath: cfg, Executable: "outline-mcp", Harness: "codex"})
	if err == nil || !strings.Contains(out.String(), "Manual MCP JSON") {
		t.Fatalf("missing failure fallback: %v %s", err, &out)
	}
	matches, _ := filepath.Glob(cfg + ".bak-*")
	if len(matches) != 1 {
		t.Fatal("no backup")
	}
	b, _ := os.ReadFile(matches[0])
	if !bytes.Equal(b, old) {
		t.Fatal("backup changed")
	}
}
