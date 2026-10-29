package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestNormalizeURL(t *testing.T) {
	for _, tt := range []struct{ in, want string }{
		{"https://example.com/", "https://example.com/api"},
		{"https://example.com/api/", "https://example.com/api"},
		{"https://example.com/wiki", "https://example.com/wiki/api"},
		{"http://127.0.0.1:8080", "http://127.0.0.1:8080/api"},
		{"http://[::1]:8080", "http://[::1]:8080/api"},
		{"http://remote.example", ""}, {"https://user:secret@example.com", ""},
		{"https://example.com?key=secret", ""}, {"https://example.com#fragment", ""}, {"file:///etc/passwd", ""},
	} {
		got, err := NormalizeURL(tt.in)
		if tt.want == "" {
			if err == nil {
				t.Errorf("accepted %s", tt.in)
			}
		} else if err != nil || got != tt.want {
			t.Errorf("%s: %s %v", tt.in, got, err)
		}
	}
}

func TestSaveLoadAndOverrides(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	c := Config{BaseURL: "https://example.com", APIKey: "saved", HistoryLimit: 12, TimeoutSeconds: 20}
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OUTLINE_API_KEY", "override")
	t.Setenv("OUTLINE_BASE_URL", "https://other.example")
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = got.Validate(); err != nil {
		t.Fatal(err)
	}
	if got.APIKey != "override" || got.BaseURL != "https://other.example/api" {
		t.Fatalf("%+v", got)
	}
	st, _ := os.Stat(path)
	if runtime.GOOS != "windows" && st.Mode().Perm() != 0600 {
		t.Fatalf("mode: %v", st.Mode())
	}
	t.Setenv("OUTLINE_HISTORY", "not-bool")
	if _, err = Load(path); err == nil {
		t.Fatal("accepted invalid boolean")
	}
}
