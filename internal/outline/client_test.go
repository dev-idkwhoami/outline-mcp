package outline

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"outline-mcp/internal/config"
)

func TestRequestAndErrorHandling(t *testing.T) {
	for _, tt := range []struct {
		name               string
		status             int
		body               string
		wantErr, uncertain bool
	}{
		{"success", 200, `{"ok":true,"data":{"id":"doc"},"pagination":{"nextPath":"next"}}`, false, false},
		{"permission", 403, `{"ok":false,"error":"authorization_error","message":"denied"}`, true, false},
		{"rate", 429, `{"ok":false,"error":"rate_limit","message":"wait"}`, true, false},
		{"server", 503, `{"ok":false,"error":"unavailable"}`, true, true},
		{"invalid-json", 200, `<html>wrong endpoint</html>`, true, true},
		{"false-ok", 200, `{"ok":false,"error":"validation_error"}`, true, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "POST" || r.URL.Path != "/api/documents.info" {
					t.Errorf("request: %s %s", r.Method, r.URL)
				}
				if r.Header.Get("Authorization") != "Bearer secret" {
					t.Error("missing key")
				}
				if r.Header.Get("Content-Type") != "application/json" {
					t.Error("wrong content type")
				}
				b, _ := io.ReadAll(r.Body)
				var args map[string]any
				if json.Unmarshal(b, &args) != nil || args["id"] != "doc" {
					t.Error("wrong body")
				}
				w.Header().Set("Retry-After", "7")
				w.WriteHeader(tt.status)
				io.WriteString(w, tt.body)
			}))
			defer srv.Close()
			c := New(config.Config{BaseURL: srv.URL + "/api", APIKey: "secret", TimeoutSeconds: 2})
			data, err := c.Call(context.Background(), "documents.info", map[string]any{"id": "doc"})
			if (err != nil) != tt.wantErr {
				t.Fatalf("error: %v", err)
			}
			if calls != 1 {
				t.Fatalf("unexpected retries: %d", calls)
			}
			if err == nil && string(data) != tt.body {
				t.Error("response was not preserved")
			}
			if err != nil {
				var e *APIError
				if !errors.As(err, &e) || e.Uncertain != tt.uncertain {
					t.Fatalf("wrong error: %+v", err)
				}
				if tt.status == 429 && e.RetryAfter != "7" {
					t.Error("missing retry-after")
				}
			}
		})
	}
}

func TestNoCredentialRedirect(t *testing.T) {
	reached := false
	dst := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true }))
	defer dst.Close()
	src := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, dst.URL, http.StatusTemporaryRedirect)
	}))
	defer src.Close()
	_, err := New(config.Config{BaseURL: src.URL, APIKey: "secret", TimeoutSeconds: 2}).Call(context.Background(), "documents.info", map[string]any{})
	if reached || err == nil {
		t.Fatal("followed redirect or did not report it")
	}
}

func TestSecretRedactionAndResponseBound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"ok":false,"message":"private-key"}`)
	}))
	defer srv.Close()
	_, err := New(config.Config{BaseURL: srv.URL, APIKey: "private-key", TimeoutSeconds: 2}).Call(context.Background(), "x", nil)
	if err == nil || strings.Contains(err.Error(), "private-key") {
		t.Fatalf("unredacted: %v", err)
	}
	big := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, strings.Repeat("x", MaxResponseBytes+1))
	}))
	defer big.Close()
	_, err = New(config.Config{BaseURL: big.URL, APIKey: "private-key", TimeoutSeconds: 2}).Call(context.Background(), "x", nil)
	if err == nil {
		t.Fatal("unbounded response")
	}
}

func TestInformativeHTTPFailures(t *testing.T) {
	for _, tt := range []struct {
		status     int
		code, hint string
	}{
		{401, "authentication_failed", "setup"}, {403, "permission_denied", "scopes"},
		{404, "not_found", "resource ID"}, {400, "validation_failed", "correct"},
		{422, "validation_failed", "correct"}, {409, "conflict", "latest"},
		{429, "rate_limited", "retry_after"}, {408, "request_timeout", "current Outline state"},
		{503, "server_error", "current Outline state"}, {302, "redirect_refused", "canonical"},
		{413, "request_too_large", "Reduce"}, {405, "endpoint_incompatible", "API version"},
	} {
		t.Run(tt.code, func(t *testing.T) {
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Retry-After", "12")
				w.WriteHeader(tt.status)
				io.WriteString(w, `{"ok":false,"error":"upstream_error","message":"specific diagnostic private-key"}`)
			}))
			defer api.Close()
			_, err := New(config.Config{BaseURL: api.URL, APIKey: "private-key", TimeoutSeconds: 2}).Call(context.Background(), "documents.update", map[string]any{"id": "doc"})
			var e *APIError
			if !errors.As(err, &e) {
				t.Fatalf("not a structured error: %v", err)
			}
			e = e.ForOperation(false)
			if e.Code != tt.code || e.Endpoint != "documents.update" || e.Status != tt.status || !strings.Contains(e.NextStep, tt.hint) {
				t.Fatalf("unhelpful error: %+v", e)
			}
			raw, _ := json.Marshal(e)
			if strings.Contains(string(raw), "private-key") || !strings.Contains(e.Detail, "specific diagnostic") {
				t.Fatalf("lost or unredacted detail: %s", raw)
			}
			if tt.status == 429 && e.RetryAfter != "12" {
				t.Fatal("missing delay")
			}
		})
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestTransportDiagnosticsAndReadWriteAdvice(t *testing.T) {
	for _, tt := range []struct {
		name, code string
		err        error
	}{
		{"cancelled", "request_cancelled", context.Canceled},
		{"timeout", "request_timeout", context.DeadlineExceeded},
		{"dns", "dns_error", &net.DNSError{Err: "no such host", Name: "outline.invalid"}},
		{"tls", "tls_error", &tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}}},
		{"network", "transport_error", errors.New("connection reset private-key")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := New(config.Config{BaseURL: "https://outline.invalid/api", APIKey: "private-key", TimeoutSeconds: 2})
			c.http.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, tt.err })
			_, err := c.Call(context.Background(), "documents.update", map[string]any{"id": "doc"})
			var e *APIError
			if !errors.As(err, &e) || e.Code != tt.code {
				t.Fatalf("wrong diagnosis: %+v", err)
			}
			write := e.ForOperation(false)
			read := e.ForOperation(true)
			if !write.Uncertain || !strings.Contains(write.NextStep, "Read current Outline state") {
				t.Fatalf("unsafe write guidance: %+v", write)
			}
			if read.Uncertain || !strings.Contains(read.NextStep, "read-only") {
				t.Fatalf("read advice: %+v", read)
			}
			b, _ := json.Marshal(write)
			if strings.Contains(string(b), "private-key") {
				t.Fatal("raw transport error leaked key")
			}
		})
	}
}
