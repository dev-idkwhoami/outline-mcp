package outline

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"unicode"
)

// APIError is both a human-readable explanation and a stable recovery contract.
// Detail is untrusted server text; NextStep is generated locally.
type APIError struct {
	Endpoint   string `json:"endpoint"`
	Status     int    `json:"status,omitempty"`
	Code       string `json:"code"`
	Message    string `json:"message"`
	Detail     string `json:"detail,omitempty"`
	NextStep   string `json:"next_step"`
	RetryAfter string `json:"retry_after,omitempty"`
	Uncertain  bool   `json:"outcome_unknown"`
}

func (e *APIError) Error() string { return e.Message }

// ForOperation makes retry guidance explicit about reads versus mutations.
func (e *APIError) ForOperation(readOnly bool) *APIError {
	copy := *e
	if copy.Uncertain {
		if readOnly {
			copy.Uncertain = false
			copy.NextStep += " This operation is read-only; retry only after addressing the cause, with a bounded retry count."
		} else {
			copy.NextStep += " The write may already have succeeded. Read current Outline state before repeating it; do not blindly replay the write."
		}
	}
	return &copy
}

func httpFailure(endpoint string, status int, upstreamCode, detail, retryAfter string) *APIError {
	code, reason, next := "api_error", "Outline reported an unsuccessful operation", "Inspect the API detail and tool schema. Do not repeat the same request unchanged without understanding the cause."
	switch {
	case status == 401:
		code, reason, next = "authentication_failed", "Outline rejected the API key", "Run outline-mcp setup or update OUTLINE_API_KEY locally, then restart the MCP process. Check whether the key expired or was revoked. Never paste the key into chat."
	case status == 403:
		code, reason, next = "permission_denied", "The API key or its owner is not permitted to perform this operation", "Check the key's scopes and the user's access to this collection/document. Ask an administrator to grant the required access; do not retry unchanged or attempt to bypass permissions."
	case status == 404:
		code, reason, next = "not_found", "The resource or API route was not found or is not visible to this key", "Verify the resource ID and Outline instance. Use an appropriate list/search tool to locate the object; check access and server-version support. A 404 does not prove the object was deleted."
	case status == 400 || status == 422:
		code, reason, next = "validation_failed", "Outline rejected the request parameters", "Use the API detail and tool schema to correct missing, invalid, or incompatible fields, then retry the corrected request."
	case status == 409 || status == 412:
		code, reason, next = "conflict", "The operation conflicts with current server state", "Fetch the latest object, reconcile the requested change with current state, and use current revision/precondition values when supported. Do not overwrite blindly."
	case status == 429:
		code, reason, next = "rate_limited", "Outline rate-limited this request", "Honor retry_after before a bounded retry. It may be seconds or an HTTP date. If absent, use a bounded backoff; do not loop immediately."
	case status == 408 || status == 504:
		code, reason, next = "request_timeout", "Outline or its proxy timed out while processing the request", "Check Outline/proxy availability and request size."
	case status >= 500:
		code, reason, next = "server_error", "Outline or an upstream proxy encountered a server error", "Check Outline/proxy health and retry only after the service recovers."
	case status >= 300 && status < 400:
		code, reason, next = "redirect_refused", "Outline redirected the request; this client did not follow it", "Check OUTLINE_BASE_URL or rerun setup with the canonical Outline URL. Redirects are not followed to avoid forwarding the API key to another destination."
	case status == 413:
		code, reason, next = "request_too_large", "Outline or its proxy rejected the request size", "Reduce the submitted content or adjust an authorized server/proxy size limit; do not resend the same oversized payload."
	case status == 405 || status == 415:
		code, reason, next = "endpoint_incompatible", "The server rejected this endpoint's method or content type", "Check the Outline base URL, reverse proxy, and deployed API version. This integration sends JSON POST requests."
	}
	e := &APIError{Endpoint: endpoint, Status: status, Code: code, Message: fmt.Sprintf("%s: %s (HTTP %d).", endpoint, reason, status), Detail: detail, NextStep: next, RetryAfter: retryAfter, Uncertain: status == 408 || status >= 500}
	if upstreamCode != "" {
		if e.Detail != "" {
			e.Detail = upstreamCode + ": " + e.Detail
		} else {
			e.Detail = upstreamCode
		}
	}
	return e
}

func transportFailure(endpoint string, err error) *APIError {
	code, reason, next := "transport_error", "The connection failed before a complete response was received", "Check the configured host, network, proxy, and Outline availability."
	var dns *net.DNSError
	var cert *tls.CertificateVerificationError
	var unknownCA x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var network net.Error
	switch {
	case errors.Is(err, context.Canceled):
		code, reason, next = "request_cancelled", "The request was cancelled", "Do not retry automatically after cancellation. Continue only if the task still calls for this operation."
	case errors.Is(err, context.DeadlineExceeded):
		code, reason, next = "request_timeout", "The request exceeded its timeout", "Check Outline availability and request size. Adjust OUTLINE_TIMEOUT_SECONDS only if a longer request is expected."
	case errors.As(err, &dns):
		code, reason, next = "dns_error", "The Outline hostname could not be resolved", "Check OUTLINE_BASE_URL, DNS, VPN, and network connectivity."
	case errors.As(err, &cert) || errors.As(err, &unknownCA) || errors.As(err, &hostname):
		code, reason, next = "tls_error", "TLS certificate verification failed", "Check the configured hostname and the server certificate/trust chain. Do not disable certificate verification."
	case errors.As(err, &network) && network.Timeout():
		code, reason, next = "request_timeout", "The network request timed out", "Check Outline/network availability and OUTLINE_TIMEOUT_SECONDS."
	}
	return &APIError{Endpoint: endpoint, Code: code, Message: endpoint + ": " + reason + ".", NextStep: next, Uncertain: true}
}

// SafeDetail bounds untrusted diagnostics and redacts the configured credential.
func SafeDetail(value, key string) string {
	if key != "" {
		value = strings.ReplaceAll(value, key, "[REDACTED]")
	}
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, value)
	runes := []rune(value)
	if len(runes) > 2000 {
		return string(runes[:2000]) + "…"
	}
	return value
}

func responseFailure(endpoint, code, message, next string, status int) *APIError {
	return &APIError{Endpoint: endpoint, Status: status, Code: code, Message: endpoint + ": " + message, NextStep: next, Uncertain: status >= 200 && status < 300 || status == http.StatusRequestTimeout || status >= 500}
}
