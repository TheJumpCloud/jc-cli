package api

import (
	"fmt"
	"net/http"
)

// APIError represents a structured error from the JumpCloud API.
type APIError struct {
	StatusCode int    // HTTP status code
	Endpoint   string // API endpoint that was called
	Message    string // Error message from API or generated
}

func (e *APIError) Error() string {
	return fmt.Sprintf("JumpCloud API error (HTTP %d) %s: %s", e.StatusCode, e.Endpoint, e.Message)
}

// IsRetryable returns true if this error represents a transient failure
// that should be retried.
func (e *APIError) IsRetryable() bool {
	switch e.StatusCode {
	case http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	}
	return false
}

// IsRateLimit returns true if this error is a rate limit (429) response.
func (e *APIError) IsRateLimit() bool {
	return e.StatusCode == http.StatusTooManyRequests
}

// NewAPIError creates an APIError from an HTTP response and endpoint.
func NewAPIError(statusCode int, endpoint string, body []byte) *APIError {
	msg := httpStatusMessage(statusCode)
	if len(body) > 0 {
		msg = truncateBody(body, 500)
	}
	return &APIError{
		StatusCode: statusCode,
		Endpoint:   endpoint,
		Message:    msg,
	}
}

// httpStatusMessage returns a human-readable message for common HTTP error codes.
func httpStatusMessage(code int) string {
	switch code {
	case http.StatusBadRequest:
		return "bad request"
	case http.StatusUnauthorized:
		return "invalid API key — run: jc auth login"
	case http.StatusForbidden:
		return "insufficient permissions"
	case http.StatusNotFound:
		return "resource not found"
	case http.StatusConflict:
		return "resource conflict"
	case http.StatusTooManyRequests:
		return "rate limited — too many requests"
	case http.StatusInternalServerError:
		return "server error"
	case http.StatusBadGateway:
		return "bad gateway"
	case http.StatusServiceUnavailable:
		return "service unavailable"
	case http.StatusGatewayTimeout:
		return "gateway timeout"
	default:
		return fmt.Sprintf("HTTP %d", code)
	}
}
