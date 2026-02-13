package api

import (
	"net/http"
	"strings"
	"testing"
)

func TestAPIError_Error(t *testing.T) {
	err := &APIError{
		StatusCode: 404,
		Endpoint:   "/systemusers/abc",
		Message:    "Not Found",
	}

	msg := err.Error()
	if !strings.Contains(msg, "404") {
		t.Errorf("error should contain status code, got: %q", msg)
	}
	if !strings.Contains(msg, "/systemusers/abc") {
		t.Errorf("error should contain endpoint, got: %q", msg)
	}
	if !strings.Contains(msg, "Not Found") {
		t.Errorf("error should contain message, got: %q", msg)
	}
}

func TestAPIError_IsRetryable(t *testing.T) {
	tests := []struct {
		name     string
		code     int
		expected bool
	}{
		{"400 Bad Request", 400, false},
		{"401 Unauthorized", 401, false},
		{"403 Forbidden", 403, false},
		{"404 Not Found", 404, false},
		{"429 Too Many Requests", 429, true},
		{"500 Internal Server Error", 500, true},
		{"502 Bad Gateway", 502, true},
		{"503 Service Unavailable", 503, true},
		{"504 Gateway Timeout", 504, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := &APIError{StatusCode: tt.code}
			if got := err.IsRetryable(); got != tt.expected {
				t.Errorf("IsRetryable() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestAPIError_IsRateLimit(t *testing.T) {
	tests := []struct {
		code     int
		expected bool
	}{
		{200, false},
		{401, false},
		{429, true},
		{500, false},
	}

	for _, tt := range tests {
		t.Run(http.StatusText(tt.code), func(t *testing.T) {
			err := &APIError{StatusCode: tt.code}
			if got := err.IsRateLimit(); got != tt.expected {
				t.Errorf("IsRateLimit() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestNewAPIError_WithBody(t *testing.T) {
	body := []byte(`{"message":"User not found"}`)
	err := NewAPIError(404, "/systemusers/abc", body)

	if err.StatusCode != 404 {
		t.Errorf("StatusCode = %d, want 404", err.StatusCode)
	}
	if err.Endpoint != "/systemusers/abc" {
		t.Errorf("Endpoint = %q, want %q", err.Endpoint, "/systemusers/abc")
	}
	if !strings.Contains(err.Message, "User not found") {
		t.Errorf("Message should contain body content, got: %q", err.Message)
	}
}

func TestNewAPIError_EmptyBody(t *testing.T) {
	err := NewAPIError(401, "/systemusers", nil)

	if err.StatusCode != 401 {
		t.Errorf("StatusCode = %d, want 401", err.StatusCode)
	}
	if !strings.Contains(err.Message, "invalid API key") {
		t.Errorf("Message should contain default message for 401, got: %q", err.Message)
	}
}

func TestNewAPIError_LongBodyTruncated(t *testing.T) {
	body := make([]byte, 1000)
	for i := range body {
		body[i] = 'x'
	}

	err := NewAPIError(500, "/test", body)
	if len(err.Message) > 510 { // 500 chars + "..."
		t.Errorf("Message should be truncated, length = %d", len(err.Message))
	}
}

func TestHTTPStatusMessage(t *testing.T) {
	tests := []struct {
		code     int
		contains string
	}{
		{400, "bad request"},
		{401, "invalid API key"},
		{403, "insufficient permissions"},
		{404, "not found"},
		{429, "rate limited"},
		{500, "server error"},
		{502, "bad gateway"},
		{503, "service unavailable"},
		{504, "gateway timeout"},
		{418, "HTTP 418"}, // Unknown code.
	}

	for _, tt := range tests {
		t.Run(http.StatusText(tt.code), func(t *testing.T) {
			msg := httpStatusMessage(tt.code)
			if !strings.Contains(msg, tt.contains) {
				t.Errorf("httpStatusMessage(%d) = %q, want to contain %q", tt.code, msg, tt.contains)
			}
		})
	}
}
