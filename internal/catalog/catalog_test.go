package catalog

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestExactCoverageOfPinnedAPI(t *testing.T) {
	tools, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	wantCounts := map[string]int{"Collections": 16, "Comments": 9, "DataAttributes": 5, "Documents": 27, "Pins": 5, "Reactions": 1, "Revisions": 4, "Templates": 7}
	counts := map[string]int{}
	endpoints := map[string]bool{}
	names := map[string]bool{}
	for _, tool := range tools {
		if endpoints[tool.Endpoint] || names[tool.Name] {
			t.Fatalf("duplicate: %s", tool.Endpoint)
		}
		endpoints[tool.Endpoint] = true
		names[tool.Name] = true
		counts[tool.Category]++
	}
	if !reflect.DeepEqual(counts, wantCounts) {
		t.Fatalf("counts: %v", counts)
	}
	b, err := os.ReadFile("../../api/outline.openapi.yml")
	if err != nil {
		t.Fatal(err)
	}
	var spec map[string]any
	if err = yaml.Unmarshal(b, &spec); err != nil {
		t.Fatal(err)
	}
	excluded := map[string]bool{}
	for _, e := range strings.Fields("collections.import collections.export collections.export_all documents.import documents.export revisions.export documents.empty_trash") {
		excluded[e] = true
	}
	for path, raw := range spec["paths"].(map[string]any) {
		post, ok := raw.(map[string]any)["post"].(map[string]any)
		if !ok {
			continue
		}
		category := post["tags"].([]any)[0].(string)
		endpoint := strings.TrimPrefix(path, "/")
		want := wantCounts[category] > 0 && !excluded[endpoint]
		if endpoints[endpoint] != want {
			t.Errorf("endpoint coverage %s = %v; want %v", endpoint, endpoints[endpoint], want)
		}
	}
}

func TestValidation(t *testing.T) {
	tools, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	lookup := map[string]Tool{}
	for _, tool := range tools {
		lookup[tool.Endpoint] = tool
	}
	for _, tt := range []struct {
		endpoint, args string
		valid          bool
	}{
		{"documents.update", `{"id":"doc","text":"new text"}`, true},
		{"documents.update", `{"text":"new text"}`, false},
		{"documents.update", `{"id":"doc","publish":"yes"}`, false},
		{"documents.update", `{"id":"doc","inventedFlag":true}`, false},
		{"documents.list", `{"limit":10,"offset":0}`, true},
		{"documents.list", `{"limit":"10"}`, false},
		{"documents.list", `null`, false},
		{"documents.list", `[]`, false},
		{"comments.add_reaction", `{"id":"comment","emoji":"👍"}`, true},
		{"comments.add_reaction", `{"id":"comment"}`, false},
		{"dataAttributes.create", `{"name":"Status","dataType":"not-a-type"}`, false},
	} {
		t.Run(tt.endpoint+tt.args, func(t *testing.T) {
			_, err := lookup[tt.endpoint].Validate([]byte(tt.args))
			if (err == nil) != tt.valid {
				t.Fatalf("valid=%v: %v", tt.valid, err)
			}
		})
	}
	// OpenAPI nullable becomes a JSON Schema union without dropping enum restrictions.
	for _, tool := range tools {
		raw, _ := json.Marshal(tool.Schema)
		if strings.Contains(string(raw), "#/components/") || strings.Contains(string(raw), `"nullable"`) {
			t.Fatalf("unconverted OpenAPI in %s", tool.Name)
		}
	}
}
