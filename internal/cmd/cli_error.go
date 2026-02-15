package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/filter"
	"github.com/klaassen-consulting/jc/internal/resolve"
)

// Exit codes for structured error reporting.
const (
	ExitSuccess     = 0
	ExitGeneral     = 1
	ExitUsage       = 2
	ExitAuth        = 3
	ExitPermission  = 4
	ExitRateLimit   = 5
	ExitPlan        = 10
	ExitInterrupted = 130
)

// Error code constants follow the pattern RESOURCE_ERROR.
const (
	ErrCodeGeneral         = "GENERAL_ERROR"
	ErrCodeUserNotFound    = "USER_NOT_FOUND"
	ErrCodeDeviceNotFound  = "DEVICE_NOT_FOUND"
	ErrCodeGroupNotFound   = "GROUP_NOT_FOUND"
	ErrCodeCommandNotFound = "COMMAND_NOT_FOUND"
	ErrCodePolicyNotFound  = "POLICY_NOT_FOUND"
	ErrCodeAppNotFound        = "APP_NOT_FOUND"
	ErrCodeAuthPolicyNotFound = "AUTH_POLICY_NOT_FOUND"
	ErrCodeIPListNotFound     = "IP_LIST_NOT_FOUND"
	ErrCodeAdminNotFound      = "ADMIN_NOT_FOUND"
	ErrCodeSoftwareNotFound   = "SOFTWARE_NOT_FOUND"
	ErrCodeLDAPNotFound       = "LDAP_NOT_FOUND"
	ErrCodeADNotFound           = "AD_NOT_FOUND"
	ErrCodeRADIUSNotFound       = "RADIUS_NOT_FOUND"
	ErrCodeAppleMDMNotFound     = "APPLE_MDM_NOT_FOUND"
	ErrCodePolicyGroupNotFound  = "POLICY_GROUP_NOT_FOUND"
	ErrCodeResourceNotFound     = "RESOURCE_NOT_FOUND"
	ErrCodeAuthFailed      = "AUTH_FAILED"
	ErrCodeAuthExpired     = "AUTH_EXPIRED"
	ErrCodePermissionDenied = "PERMISSION_DENIED"
	ErrCodeRateLimited     = "RATE_LIMITED"
	ErrCodeInvalidFilter   = "INVALID_FILTER"
	ErrCodeInvalidInput    = "INVALID_INPUT"
	ErrCodeAPIError        = "API_ERROR"
	ErrCodeConfigError     = "CONFIG_ERROR"
	ErrCodeUsageError      = "USAGE_ERROR"
	ErrCodeValidationError = "VALIDATION_ERROR"
)

// CLIError is a structured, machine-readable error with error code,
// message, suggestion, and optional HTTP context.
type CLIError struct {
	Code        string `json:"code"`
	Message     string `json:"message"`
	Suggestion  string `json:"suggestion,omitempty"`
	HTTPStatus  int    `json:"http_status,omitempty"`
	APIEndpoint string `json:"api_endpoint,omitempty"`
	Err         error  `json:"-"`
}

func (e *CLIError) Error() string {
	return e.Message
}

func (e *CLIError) Unwrap() error {
	return e.Err
}

// ExitCode returns the appropriate process exit code for this error.
func (e *CLIError) ExitCode() int {
	return codeToExit(e.Code, e.HTTPStatus)
}

// WriteJSON writes the error as structured JSON to w.
func (e *CLIError) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(e)
}

// WritePlain writes the error as a human-readable string to w.
func (e *CLIError) WritePlain(w io.Writer) {
	fmt.Fprintln(w, e.Message)
	if e.Suggestion != "" {
		fmt.Fprintf(w, "Suggestion: %s\n", e.Suggestion)
	}
}

// codeToExit maps error codes and HTTP status to process exit codes.
func codeToExit(code string, httpStatus int) int {
	switch code {
	case ErrCodeAuthFailed, ErrCodeAuthExpired:
		return ExitAuth
	case ErrCodePermissionDenied:
		return ExitPermission
	case ErrCodeRateLimited:
		return ExitRateLimit
	case ErrCodeUsageError:
		return ExitUsage
	}

	// Fall back to HTTP status mapping.
	switch httpStatus {
	case http.StatusUnauthorized:
		return ExitAuth
	case http.StatusForbidden:
		return ExitPermission
	case http.StatusTooManyRequests:
		return ExitRateLimit
	}

	return ExitGeneral
}

// NewCLIError creates a CLIError with the given code and message.
func NewCLIError(code, message, suggestion string) *CLIError {
	return &CLIError{
		Code:       code,
		Message:    message,
		Suggestion: suggestion,
	}
}

// WrapCLIError creates a CLIError wrapping an existing error.
func WrapCLIError(code, message, suggestion string, err error) *CLIError {
	return &CLIError{
		Code:       code,
		Message:    message,
		Suggestion: suggestion,
		Err:        err,
	}
}

// CLIErrorFromAPI converts an api.APIError to a CLIError with appropriate
// error code, message, and suggestion based on the HTTP status code.
func CLIErrorFromAPI(apiErr *api.APIError) *CLIError {
	code, suggestion := mapAPIStatus(apiErr.StatusCode)
	return &CLIError{
		Code:        code,
		Message:     apiErr.Error(),
		Suggestion:  suggestion,
		HTTPStatus:  apiErr.StatusCode,
		APIEndpoint: apiErr.Endpoint,
		Err:         apiErr,
	}
}

// mapAPIStatus returns the error code and suggestion for a given HTTP status.
func mapAPIStatus(status int) (code, suggestion string) {
	switch status {
	case http.StatusBadRequest:
		return ErrCodeInvalidInput, "Check the request parameters and try again"
	case http.StatusUnauthorized:
		return ErrCodeAuthFailed, "Run 'jc auth login' to re-authenticate"
	case http.StatusForbidden:
		return ErrCodePermissionDenied, "Check your API key permissions for this operation"
	case http.StatusNotFound:
		return ErrCodeResourceNotFound, "Verify the resource exists and the ID/name is correct"
	case http.StatusConflict:
		return ErrCodeInvalidInput, "The resource already exists or conflicts with an existing resource"
	case http.StatusTooManyRequests:
		return ErrCodeRateLimited, "Wait a moment and retry, or reduce request frequency"
	default:
		return ErrCodeAPIError, "Check JumpCloud service status and try again"
	}
}

// ToCLIError converts any error to a CLIError. If the error is already a
// CLIError, it is returned as-is. If it wraps an api.APIError, it is
// converted with appropriate HTTP context. Resolve and filter errors
// get mapped to their specific error codes. Otherwise, a generic CLIError
// is created.
func ToCLIError(err error) *CLIError {
	if err == nil {
		return nil
	}

	// Already a CLIError.
	var cliErr *CLIError
	if ok := errorAs(err, &cliErr); ok {
		return cliErr
	}

	// Wraps an api.APIError.
	var apiErr *api.APIError
	if ok := errorAs(err, &apiErr); ok {
		return CLIErrorFromAPI(apiErr)
	}

	// Wraps a resolve.ResolveError.
	var resolveErr *resolve.ResolveError
	if ok := errorAs(err, &resolveErr); ok {
		code := mapResolveResourceType(resolveErr.ResourceType)
		return &CLIError{
			Code:       code,
			Message:    resolveErr.Message,
			Suggestion: fmt.Sprintf("Verify the %s exists: jc %s list", resolveErr.ResourceType, resolveResourceCmd(resolveErr.ResourceType)),
			Err:        err,
		}
	}

	// Wraps a filter.FilterError.
	var filterErr *filter.FilterError
	if ok := errorAs(err, &filterErr); ok {
		return &CLIError{
			Code:       ErrCodeInvalidFilter,
			Message:    filterErr.Message,
			Suggestion: "Use format: --filter 'field=value' (operators: =, !=, >, <, >=, <=)",
			Err:        err,
		}
	}

	// Check for well-known error messages.
	if err == api.ErrNoAPIKey {
		return &CLIError{
			Code:       ErrCodeAuthFailed,
			Message:    err.Error(),
			Suggestion: "Run 'jc auth login' or set JC_API_KEY environment variable",
			Err:        err,
		}
	}

	// Generic error.
	return &CLIError{
		Code:    ErrCodeGeneral,
		Message: err.Error(),
		Err:     err,
	}
}

// mapResolveResourceType maps a resolve ResourceType (field name) to the
// appropriate structured error code.
func mapResolveResourceType(resourceType string) string {
	switch resourceType {
	case "username":
		return ErrCodeUserNotFound
	case "hostname":
		return ErrCodeDeviceNotFound
	case "email":
		return ErrCodeAdminNotFound
	case "displayName":
		return ErrCodeSoftwareNotFound
	case "domain":
		return ErrCodeADNotFound
	case "name":
		// Could be group, command, policy, LDAP, or app — use generic.
		return ErrCodeResourceNotFound
	default:
		return ErrCodeResourceNotFound
	}
}

// resolveResourceCmd maps a resolve ResourceType to the CLI command name
// for use in suggestion messages.
func resolveResourceCmd(resourceType string) string {
	switch resourceType {
	case "username":
		return "users"
	case "hostname":
		return "devices"
	case "email":
		return "admins"
	case "displayName":
		return "software"
	case "domain":
		return "ad"
	default:
		return "resources"
	}
}

// errorAs is a wrapper around errors.As that works with the generic pattern.
// Using a separate function avoids importing errors in the type definition.
func errorAs[T any](err error, target *T) bool {
	// Type assert through the error chain.
	for err != nil {
		if t, ok := err.(T); ok {
			*target = t
			return true
		}
		if u, ok := err.(interface{ Unwrap() error }); ok {
			err = u.Unwrap()
		} else {
			return false
		}
	}
	return false
}
