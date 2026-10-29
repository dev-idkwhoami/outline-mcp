package outline

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"outline-mcp/internal/config"
)

const MaxResponseBytes = 16 << 20

type Client struct {
	baseURL, key string
	http         *http.Client
}

func New(c config.Config) *Client {
	return &Client{c.BaseURL, c.APIKey, &http.Client{
		Timeout:       time.Duration(c.TimeoutSeconds) * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

func (c *Client) Call(ctx context.Context, endpoint string, args map[string]any) (json.RawMessage, error) {
	payload, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/"+endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "outline-mcp/0.1.0")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, transportFailure(endpoint, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBytes+1))
	if err != nil {
		return nil, responseFailure(endpoint, "response_read_failed", "The response body was interrupted or unreadable.", "Check the connection and Outline/proxy health.", resp.StatusCode)
	}
	if len(data) > MaxResponseBytes {
		return nil, responseFailure(endpoint, "response_too_large", "The response exceeded the 16 MiB client limit.", "For list/search operations, request a smaller page with limit/offset. For a single large object, inspect its size and the integration response limit.", resp.StatusCode)
	}
	data = bytes.ReplaceAll(data, []byte(c.key), []byte("[REDACTED]"))
	var envelope map[string]json.RawMessage
	valid := json.Unmarshal(data, &envelope) == nil && envelope != nil
	ok := true
	if valid {
		if b, exists := envelope["ok"]; exists {
			if json.Unmarshal(b, &ok) != nil {
				valid = false
			}
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || !ok {
		var code, message string
		if valid {
			_ = json.Unmarshal(envelope["error"], &code)
			_ = json.Unmarshal(envelope["message"], &message)
		}
		e := httpFailure(endpoint, resp.StatusCode, SafeDetail(code, c.key), SafeDetail(message, c.key), SafeDetail(resp.Header.Get("Retry-After"), c.key))
		e.Detail = SafeDetail(e.Detail, c.key)
		return nil, e
	}

	if !valid {
		return nil, responseFailure(endpoint, "invalid_response", "The server returned an invalid JSON API response.", "Check the Outline base URL and reverse proxy; an HTML login/error page is not an API response.", resp.StatusCode)
	}
	return data, nil
}
