// Run from the repository root: go run ./scripts/generate.go
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

type operation struct {
	Name        string         `json:"name"`
	Endpoint    string         `json:"endpoint"`
	Category    string         `json:"category"`
	Description string         `json:"description"`
	ReadOnly    bool           `json:"readOnly"`
	Schema      map[string]any `json:"schema"`
}

func main() {
	data, err := os.ReadFile("api/outline.openapi.yml")
	must(err)
	var spec map[string]any
	must(yaml.Unmarshal(data, &spec))
	defs := spec["components"].(map[string]any)["schemas"].(map[string]any)
	categories := map[string]bool{}
	for _, c := range strings.Fields("Collections Comments DataAttributes Documents Pins Reactions Revisions Templates") {
		categories[c] = true
	}
	excluded := map[string]bool{}
	for _, p := range strings.Fields("collections.import collections.export collections.export_all documents.import documents.export revisions.export documents.empty_trash") {
		excluded[p] = true
	}
	reads := map[string]bool{}
	for _, p := range strings.Fields("info documents list memberships group_memberships insights drafts viewed answerQuestion search_titles search users archived deleted") {
		reads[p] = true
	}
	var ops []operation
	for path, raw := range spec["paths"].(map[string]any) {
		post, ok := raw.(map[string]any)["post"].(map[string]any)
		if !ok {
			continue
		}
		category := post["tags"].([]any)[0].(string)
		endpoint := strings.TrimPrefix(path, "/")
		if !categories[category] || excluded[endpoint] {
			continue
		}
		body := post["requestBody"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"]
		schema := convert(body).(map[string]any)
		used := map[string]any{}
		collect(schema, defs, used)
		// Merge root allOf object fields so clients can discover arguments directly.
		schema = flatten(schema, used)
		schema["type"] = "object"
		schema["additionalProperties"] = false
		if len(used) > 0 {
			schema["$defs"] = used
		}
		description, _ := post["description"].(string)
		description = strings.Join(strings.Fields(post["summary"].(string)+". "+description), " ")
		ops = append(ops, operation{"outline_" + snake(strings.ReplaceAll(endpoint, ".", "_")), endpoint, category, description, reads[strings.SplitN(endpoint, ".", 2)[1]], schema})
	}
	sort.Slice(ops, func(i, j int) bool { return ops[i].Endpoint < ops[j].Endpoint })
	if len(ops) != 74 {
		panic(fmt.Sprintf("expected 74 tools, got %d; review upstream scope", len(ops)))
	}
	out, err := json.MarshalIndent(ops, "", "  ")
	must(err)
	must(os.WriteFile("internal/catalog/tools.json", append(out, '\n'), 0644))
	fmt.Printf("Generated %d tools\n", len(ops))
}

func convert(v any) any {
	switch x := v.(type) {
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = convert(e)
		}
		return out
	case map[string]any:
		out := map[string]any{}
		for k, e := range x {
			switch k {
			case "nullable", "example", "discriminator", "xml":
				continue
			}
			if k == "$ref" {
				out[k] = strings.Replace(e.(string), "#/components/schemas/", "#/$defs/", 1)
			} else {
				out[k] = convert(e)
			}
		}
		if x["nullable"] == true {
			return map[string]any{"anyOf": []any{out, map[string]any{"type": "null"}}}
		}
		return out
	default:
		return v
	}
}

func collect(v any, defs, used map[string]any) {
	switch x := v.(type) {
	case []any:
		for _, e := range x {
			collect(e, defs, used)
		}
	case map[string]any:
		if ref, ok := x["$ref"].(string); ok {
			name := strings.TrimPrefix(ref, "#/$defs/")
			if _, ok := used[name]; !ok {
				raw, ok := defs[name]
				if !ok {
					panic(ref)
				}
				used[name] = convert(raw)
				collect(used[name], defs, used)
			}
		}
		for _, e := range x {
			collect(e, defs, used)
		}
	}
}

func flatten(s map[string]any, defs map[string]any) map[string]any {
	if ref, ok := s["$ref"].(string); ok {
		return flatten(defs[strings.TrimPrefix(ref, "#/$defs/")].(map[string]any), defs)
	}
	all, ok := s["allOf"].([]any)
	if !ok {
		return s
	}
	props := map[string]any{}
	var required []any
	for _, part := range all {
		p := flatten(part.(map[string]any), defs)
		if fields, ok := p["properties"].(map[string]any); ok {
			for k, v := range fields {
				props[k] = v
			}
		}
		if r, ok := p["required"].([]any); ok {
			required = append(required, r...)
		}
	}
	out := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		out["required"] = required
	}
	return out
}

func snake(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsUpper(r) {
			b.WriteByte('_')
			r = unicode.ToLower(r)
		}
		b.WriteRune(r)
	}
	return b.String()
}
func must(err error) {
	if err != nil {
		panic(err)
	}
}
