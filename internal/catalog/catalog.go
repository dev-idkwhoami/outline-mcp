// Package catalog embeds the reviewed Outline tool surface.
package catalog

import (
	_ "embed"
	"encoding/json"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
)

//go:embed tools.json
var data []byte

type Tool struct {
	Name        string             `json:"name"`
	Endpoint    string             `json:"endpoint"`
	Category    string             `json:"category"`
	Description string             `json:"description"`
	ReadOnly    bool               `json:"readOnly"`
	Schema      *jsonschema.Schema `json:"schema"`
	resolved    *jsonschema.Resolved
}

func Load() ([]Tool, error) {
	var tools []Tool
	if err := json.Unmarshal(data, &tools); err != nil {
		return nil, err
	}
	for i := range tools {
		r, err := tools[i].Schema.Resolve(nil)
		if err != nil {
			return nil, fmt.Errorf("%s schema: %w", tools[i].Endpoint, err)
		}
		tools[i].resolved = r
	}
	return tools, nil
}

func (t Tool) Validate(raw []byte) (map[string]any, error) {
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	var args map[string]any
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, fmt.Errorf("arguments must be a JSON object: %w", err)
	}
	if args == nil {
		return nil, fmt.Errorf("arguments must be a JSON object")
	}
	if err := t.resolved.Validate(args); err != nil {
		return nil, fmt.Errorf("invalid %s arguments: %w", t.Endpoint, err)
	}
	return args, nil
}
