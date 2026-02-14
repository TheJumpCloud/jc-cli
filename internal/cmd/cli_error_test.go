package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/filter"
	"github.com/klaassen-consulting/jc/internal/resolve"
)

// --- CLIError type tests ---

func TestCLIError_Error(t *testing.T) {
	e := &CLIError{Code: ErrCodeGeneral, Message: "something went wrong"}
	if e.Error() != "something went wrong" {
		t.Errorf("Error() = %q, want %q", e.Error(), "something went wrong")
	}
}

func TestCLIError_Unwrap(t *testing.T) {
	inner := fmt.Errorf("inner error")
	e := &CLIError{Code: ErrCodeGeneral, Message: "outer", Err: inner}
	if !errors.Is(e, inner) {
		t.Error("Unwrap() should return the inner error")
	}
}

func TestCLIError_Unwrap_Nil(t *testing.T) {
	e := &CLIError{Code: ErrCodeGeneral, Message: "no inner"}
	if e.Unwrap() != nil {
		t.Error("Unwrap() should return nil when Err is nil")
	}
}

// --- Exit code mapping tests ---

func TestCLIError_ExitCode_AuthFailed(t *testing.T) {
	e := &CLIError{Code: ErrCodeAuthFailed}
	if e.ExitCode() != ExitAuth {
		t.Errorf("ExitCode() = %d, want %d", e.ExitCode(), ExitAuth)
	}
}

func TestCLIError_ExitCode_AuthExpired(t *testing.T) {
	e := &CLIError{Code: ErrCodeAuthExpired}
	if e.ExitCode() != ExitAuth {
		t.Errorf("ExitCode() = %d, want %d", e.ExitCode(), ExitAuth)
	}
}

func TestCLIError_ExitCode_PermissionDenied(t *testing.T) {
	e := &CLIError{Code: ErrCodePermissionDenied}
	if e.ExitCode() != ExitPermission {
		t.Errorf("ExitCode() = %d, want %d", e.ExitCode(), ExitPermission)
	}
}

func TestCLIError_ExitCode_RateLimited(t *testing.T) {
	e := &CLIError{Code: ErrCodeRateLimited}
	if e.ExitCode() != ExitRateLimit {
		t.Errorf("ExitCode() = %d, want %d", e.ExitCode(), ExitRateLimit)
	}
}

func TestCLIError_ExitCode_UsageError(t *testing.T) {
	e := &CLIError{Code: ErrCodeUsageError}
	if e.ExitCode() != ExitUsage {
		t.Errorf("ExitCode() = %d, want %d", e.ExitCode(), ExitUsage)
	}
}

func TestCLIError_ExitCode_General(t *testing.T) {
	e := &CLIError{Code: ErrCodeGeneral}
	if e.ExitCode() != ExitGeneral {
		t.Errorf("ExitCode() = %d, want %d", e.ExitCode(), ExitGeneral)
	}
}

func TestCLIError_ExitCode_HTTP401Fallback(t *testing.T) {
	e := &CLIError{Code: ErrCodeAPIError, HTTPStatus: http.StatusUnauthorized}
	if e.ExitCode() != ExitAuth {
		t.Errorf("ExitCode() = %d, want %d (HTTP 401 fallback)", e.ExitCode(), ExitAuth)
	}
}

func TestCLIError_ExitCode_HTTP403Fallback(t *testing.T) {
	e := &CLIError{Code: ErrCodeAPIError, HTTPStatus: http.StatusForbidden}
	if e.ExitCode() != ExitPermission {
		t.Errorf("ExitCode() = %d, want %d (HTTP 403 fallback)", e.ExitCode(), ExitPermission)
	}
}

func TestCLIError_ExitCode_HTTP429Fallback(t *testing.T) {
	e := &CLIError{Code: ErrCodeAPIError, HTTPStatus: http.StatusTooManyRequests}
	if e.ExitCode() != ExitRateLimit {
		t.Errorf("ExitCode() = %d, want %d (HTTP 429 fallback)", e.ExitCode(), ExitRateLimit)
	}
}

// --- JSON output tests ---

func TestCLIError_WriteJSON(t *testing.T) {
	e := &CLIError{
		Code:        ErrCodeUserNotFound,
		Message:     "user \"jdoe\" not found",
		Suggestion:  "Verify the username exists: jc users list",
		HTTPStatus:  404,
		APIEndpoint: "/api/systemusers",
	}

	var buf bytes.Buffer
	if err := e.WriteJSON(&buf); err != nil {
		t.Fatalf("WriteJSON error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("Output is not valid JSON: %v\nOutput: %s", err, buf.String())
	}

	// Check all fields present.
	if result["code"] != ErrCodeUserNotFound {
		t.Errorf("code = %v, want %v", result["code"], ErrCodeUserNotFound)
	}
	if result["message"] != "user \"jdoe\" not found" {
		t.Errorf("message = %v", result["message"])
	}
	if result["suggestion"] != "Verify the username exists: jc users list" {
		t.Errorf("suggestion = %v", result["suggestion"])
	}
	if result["http_status"] != float64(404) {
		t.Errorf("http_status = %v", result["http_status"])
	}
	if result["api_endpoint"] != "/api/systemusers" {
		t.Errorf("api_endpoint = %v", result["api_endpoint"])
	}
}

func TestCLIError_WriteJSON_OmitsEmptyOptionalFields(t *testing.T) {
	e := &CLIError{
		Code:    ErrCodeGeneral,
		Message: "something failed",
	}

	var buf bytes.Buffer
	if err := e.WriteJSON(&buf); err != nil {
		t.Fatalf("WriteJSON error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("Output is not valid JSON: %v", err)
	}

	// suggestion, http_status, api_endpoint should be omitted.
	if _, ok := result["suggestion"]; ok {
		t.Error("suggestion should be omitted when empty")
	}
	if v, ok := result["http_status"]; ok && v != float64(0) {
		t.Errorf("http_status should be omitted or zero, got %v", v)
	}
	if _, ok := result["api_endpoint"]; ok {
		t.Error("api_endpoint should be omitted when empty")
	}
}

func TestCLIError_WriteJSON_IsValidJSON(t *testing.T) {
	e := &CLIError{
		Code:       ErrCodeInvalidFilter,
		Message:    `invalid filter "foo": expected format 'field=value'`,
		Suggestion: "Use format: --filter 'field=value'",
	}

	var buf bytes.Buffer
	if err := e.WriteJSON(&buf); err != nil {
		t.Fatalf("WriteJSON error: %v", err)
	}

	// Verify the output is valid JSON (special chars properly escaped).
	if !json.Valid(buf.Bytes()) {
		t.Errorf("Output is not valid JSON: %s", buf.String())
	}
}

// --- Plain text output tests ---

func TestCLIError_WritePlain(t *testing.T) {
	e := &CLIError{
		Code:       ErrCodeAuthFailed,
		Message:    "authentication failed: invalid API key",
		Suggestion: "Run 'jc auth login' to re-authenticate",
	}

	var buf bytes.Buffer
	e.WritePlain(&buf)

	output := buf.String()
	if output == "" {
		t.Fatal("WritePlain produced no output")
	}
	if !bytes.Contains(buf.Bytes(), []byte("authentication failed: invalid API key")) {
		t.Errorf("WritePlain should contain the error message, got: %s", output)
	}
	if !bytes.Contains(buf.Bytes(), []byte("Suggestion:")) {
		t.Errorf("WritePlain should contain 'Suggestion:', got: %s", output)
	}
}

func TestCLIError_WritePlain_NoSuggestion(t *testing.T) {
	e := &CLIError{
		Code:    ErrCodeGeneral,
		Message: "something failed",
	}

	var buf bytes.Buffer
	e.WritePlain(&buf)

	output := buf.String()
	if bytes.Contains(buf.Bytes(), []byte("Suggestion:")) {
		t.Errorf("WritePlain should not contain 'Suggestion:' when empty, got: %s", output)
	}
}

// --- ToCLIError conversion tests ---

func TestToCLIError_Nil(t *testing.T) {
	if ToCLIError(nil) != nil {
		t.Error("ToCLIError(nil) should return nil")
	}
}

func TestToCLIError_AlreadyCLIError(t *testing.T) {
	original := &CLIError{Code: ErrCodeRateLimited, Message: "rate limited"}
	result := ToCLIError(original)
	if result != original {
		t.Error("ToCLIError should return the same CLIError when already a CLIError")
	}
}

func TestToCLIError_FromAPIError_401(t *testing.T) {
	apiErr := &api.APIError{
		StatusCode: http.StatusUnauthorized,
		Endpoint:   "/api/systemusers",
		Message:    "invalid API key",
	}
	result := ToCLIError(apiErr)

	if result.Code != ErrCodeAuthFailed {
		t.Errorf("Code = %q, want %q", result.Code, ErrCodeAuthFailed)
	}
	if result.HTTPStatus != http.StatusUnauthorized {
		t.Errorf("HTTPStatus = %d, want %d", result.HTTPStatus, http.StatusUnauthorized)
	}
	if result.APIEndpoint != "/api/systemusers" {
		t.Errorf("APIEndpoint = %q, want %q", result.APIEndpoint, "/api/systemusers")
	}
	if result.Suggestion == "" {
		t.Error("Suggestion should not be empty for auth errors")
	}
	if result.ExitCode() != ExitAuth {
		t.Errorf("ExitCode() = %d, want %d", result.ExitCode(), ExitAuth)
	}
}

func TestToCLIError_FromAPIError_403(t *testing.T) {
	apiErr := &api.APIError{
		StatusCode: http.StatusForbidden,
		Endpoint:   "/api/systemusers",
		Message:    "insufficient permissions",
	}
	result := ToCLIError(apiErr)

	if result.Code != ErrCodePermissionDenied {
		t.Errorf("Code = %q, want %q", result.Code, ErrCodePermissionDenied)
	}
	if result.ExitCode() != ExitPermission {
		t.Errorf("ExitCode() = %d, want %d", result.ExitCode(), ExitPermission)
	}
}

func TestToCLIError_FromAPIError_404(t *testing.T) {
	apiErr := &api.APIError{
		StatusCode: http.StatusNotFound,
		Endpoint:   "/api/systemusers/abc123",
		Message:    "resource not found",
	}
	result := ToCLIError(apiErr)

	if result.Code != ErrCodeResourceNotFound {
		t.Errorf("Code = %q, want %q", result.Code, ErrCodeResourceNotFound)
	}
}

func TestToCLIError_FromAPIError_429(t *testing.T) {
	apiErr := &api.APIError{
		StatusCode: http.StatusTooManyRequests,
		Endpoint:   "/api/systemusers",
		Message:    "rate limited",
	}
	result := ToCLIError(apiErr)

	if result.Code != ErrCodeRateLimited {
		t.Errorf("Code = %q, want %q", result.Code, ErrCodeRateLimited)
	}
	if result.ExitCode() != ExitRateLimit {
		t.Errorf("ExitCode() = %d, want %d", result.ExitCode(), ExitRateLimit)
	}
}

func TestToCLIError_FromAPIError_500(t *testing.T) {
	apiErr := &api.APIError{
		StatusCode: http.StatusInternalServerError,
		Endpoint:   "/api/systemusers",
		Message:    "server error",
	}
	result := ToCLIError(apiErr)

	if result.Code != ErrCodeAPIError {
		t.Errorf("Code = %q, want %q", result.Code, ErrCodeAPIError)
	}
}

func TestToCLIError_FromResolveError_UserNotFound(t *testing.T) {
	resolveErr := &resolve.ResolveError{
		ResourceType: "username",
		Identifier:   "jdoe",
		Message:      `username "jdoe" not found`,
	}
	result := ToCLIError(resolveErr)

	if result.Code != ErrCodeUserNotFound {
		t.Errorf("Code = %q, want %q", result.Code, ErrCodeUserNotFound)
	}
	if result.Message != `username "jdoe" not found` {
		t.Errorf("Message = %q", result.Message)
	}
	if result.Suggestion == "" {
		t.Error("Suggestion should not be empty for user not found")
	}
}

func TestToCLIError_FromResolveError_DeviceNotFound(t *testing.T) {
	resolveErr := &resolve.ResolveError{
		ResourceType: "hostname",
		Identifier:   "JDOE-MBP",
		Message:      `hostname "JDOE-MBP" not found`,
	}
	result := ToCLIError(resolveErr)

	if result.Code != ErrCodeDeviceNotFound {
		t.Errorf("Code = %q, want %q", result.Code, ErrCodeDeviceNotFound)
	}
}

func TestToCLIError_FromResolveError_GenericNotFound(t *testing.T) {
	resolveErr := &resolve.ResolveError{
		ResourceType: "name",
		Identifier:   "my-group",
		Message:      `name "my-group" not found`,
	}
	result := ToCLIError(resolveErr)

	if result.Code != ErrCodeResourceNotFound {
		t.Errorf("Code = %q, want %q", result.Code, ErrCodeResourceNotFound)
	}
}

func TestToCLIError_FromFilterError(t *testing.T) {
	filterErr := &filter.FilterError{
		Expression: "foo",
		Message:    `invalid filter "foo": expected format 'field=value'`,
	}
	result := ToCLIError(filterErr)

	if result.Code != ErrCodeInvalidFilter {
		t.Errorf("Code = %q, want %q", result.Code, ErrCodeInvalidFilter)
	}
	if result.Suggestion == "" {
		t.Error("Suggestion should not be empty for filter errors")
	}
}

func TestToCLIError_FromErrNoAPIKey(t *testing.T) {
	result := ToCLIError(api.ErrNoAPIKey)

	if result.Code != ErrCodeAuthFailed {
		t.Errorf("Code = %q, want %q", result.Code, ErrCodeAuthFailed)
	}
	if result.Suggestion == "" {
		t.Error("Suggestion should not be empty for auth errors")
	}
}

func TestToCLIError_FromGenericError(t *testing.T) {
	err := fmt.Errorf("unexpected error")
	result := ToCLIError(err)

	if result.Code != ErrCodeGeneral {
		t.Errorf("Code = %q, want %q", result.Code, ErrCodeGeneral)
	}
	if result.Message != "unexpected error" {
		t.Errorf("Message = %q, want %q", result.Message, "unexpected error")
	}
}

func TestToCLIError_WrappedAPIError(t *testing.T) {
	apiErr := &api.APIError{
		StatusCode: http.StatusUnauthorized,
		Endpoint:   "/api/organizations",
		Message:    "invalid key",
	}
	wrapped := fmt.Errorf("authentication failed: %w", apiErr)
	result := ToCLIError(wrapped)

	if result.Code != ErrCodeAuthFailed {
		t.Errorf("Code = %q, want %q (should unwrap to find APIError)", result.Code, ErrCodeAuthFailed)
	}
	if result.HTTPStatus != http.StatusUnauthorized {
		t.Errorf("HTTPStatus = %d, want %d", result.HTTPStatus, http.StatusUnauthorized)
	}
}

func TestToCLIError_WrappedResolveError(t *testing.T) {
	resolveErr := &resolve.ResolveError{
		ResourceType: "username",
		Identifier:   "jdoe",
		Message:      `username "jdoe" not found`,
	}
	wrapped := fmt.Errorf("resolving user: %w", resolveErr)
	result := ToCLIError(wrapped)

	if result.Code != ErrCodeUserNotFound {
		t.Errorf("Code = %q, want %q (should unwrap to find ResolveError)", result.Code, ErrCodeUserNotFound)
	}
}

// --- NewCLIError and WrapCLIError tests ---

func TestNewCLIError(t *testing.T) {
	e := NewCLIError(ErrCodeInvalidInput, "bad input", "fix it")
	if e.Code != ErrCodeInvalidInput {
		t.Errorf("Code = %q, want %q", e.Code, ErrCodeInvalidInput)
	}
	if e.Message != "bad input" {
		t.Errorf("Message = %q", e.Message)
	}
	if e.Suggestion != "fix it" {
		t.Errorf("Suggestion = %q", e.Suggestion)
	}
}

func TestWrapCLIError(t *testing.T) {
	inner := fmt.Errorf("root cause")
	e := WrapCLIError(ErrCodeGeneral, "outer", "suggestion", inner)
	if e.Err != inner {
		t.Error("Err should be the inner error")
	}
	if !errors.Is(e, inner) {
		t.Error("errors.Is should find the inner error")
	}
}

// --- CLIErrorFromAPI tests ---

func TestCLIErrorFromAPI_400(t *testing.T) {
	apiErr := &api.APIError{StatusCode: 400, Endpoint: "/api/systemusers", Message: "bad request"}
	e := CLIErrorFromAPI(apiErr)
	if e.Code != ErrCodeInvalidInput {
		t.Errorf("Code = %q, want %q", e.Code, ErrCodeInvalidInput)
	}
}

func TestCLIErrorFromAPI_409(t *testing.T) {
	apiErr := &api.APIError{StatusCode: 409, Endpoint: "/api/v2/usergroups", Message: "conflict"}
	e := CLIErrorFromAPI(apiErr)
	if e.Code != ErrCodeInvalidInput {
		t.Errorf("Code = %q, want %q", e.Code, ErrCodeInvalidInput)
	}
}

// --- Error code constants ---

func TestErrorCodeConstants(t *testing.T) {
	// Verify all error codes follow the RESOURCE_ERROR pattern.
	codes := []string{
		ErrCodeGeneral, ErrCodeUserNotFound, ErrCodeDeviceNotFound,
		ErrCodeGroupNotFound, ErrCodeCommandNotFound, ErrCodePolicyNotFound,
		ErrCodeAppNotFound, ErrCodeResourceNotFound, ErrCodeAuthFailed,
		ErrCodeAuthExpired, ErrCodePermissionDenied, ErrCodeRateLimited,
		ErrCodeInvalidFilter, ErrCodeInvalidInput, ErrCodeAPIError,
		ErrCodeConfigError, ErrCodeUsageError, ErrCodeValidationError,
	}
	for _, code := range codes {
		if code == "" {
			t.Error("Error code constant should not be empty")
		}
	}
	if len(codes) != 18 {
		t.Errorf("Expected 18 error codes, got %d", len(codes))
	}
}

// --- Exit code constants ---

func TestExitCodeValues(t *testing.T) {
	tests := []struct {
		name string
		code int
		want int
	}{
		{"Success", ExitSuccess, 0},
		{"General", ExitGeneral, 1},
		{"Usage", ExitUsage, 2},
		{"Auth", ExitAuth, 3},
		{"Permission", ExitPermission, 4},
		{"RateLimit", ExitRateLimit, 5},
		{"Plan", ExitPlan, 10},
		{"Interrupted", ExitInterrupted, 130},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.code != tt.want {
				t.Errorf("Exit code %s = %d, want %d", tt.name, tt.code, tt.want)
			}
		})
	}
}

// --- writeError tests ---

func TestWriteError_JSON(t *testing.T) {
	e := &CLIError{
		Code:    ErrCodeUserNotFound,
		Message: "user not found",
	}
	var buf bytes.Buffer
	writeError(&buf, e, "json")

	if !json.Valid(buf.Bytes()) {
		t.Errorf("writeError with JSON format should produce valid JSON, got: %s", buf.String())
	}
}

func TestWriteError_Plain(t *testing.T) {
	e := &CLIError{
		Code:       ErrCodeUserNotFound,
		Message:    "user not found",
		Suggestion: "check the username",
	}
	var buf bytes.Buffer
	writeError(&buf, e, "table")

	output := buf.String()
	if !bytes.Contains(buf.Bytes(), []byte("user not found")) {
		t.Errorf("writeError with non-JSON format should contain message, got: %s", output)
	}
	if !bytes.Contains(buf.Bytes(), []byte("Suggestion:")) {
		t.Errorf("writeError should show suggestion, got: %s", output)
	}
}

// --- Integration: PersistentPreRunE structured errors ---

func TestPersistentPreRunE_InvalidOutputFormat(t *testing.T) {
	setupUsersTest(t)
	rootCmd := NewRootCmd()
	rootCmd.SetArgs([]string{"--output", "invalid", "users", "list"})
	var errBuf bytes.Buffer
	rootCmd.SetErr(&errBuf)

	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("Expected error for invalid output format")
	}

	var cliErr *CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("Expected CLIError, got %T: %v", err, err)
	}
	if cliErr.Code != ErrCodeValidationError {
		t.Errorf("Code = %q, want %q", cliErr.Code, ErrCodeValidationError)
	}
}

func TestPersistentPreRunE_FieldsAndExcludeMutualExclusion(t *testing.T) {
	setupUsersTest(t)
	rootCmd := NewRootCmd()
	rootCmd.SetArgs([]string{"--fields", "username", "--exclude", "email", "users", "list"})

	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("Expected error for --fields + --exclude")
	}

	var cliErr *CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("Expected CLIError, got %T: %v", err, err)
	}
	if cliErr.Code != ErrCodeValidationError {
		t.Errorf("Code = %q, want %q", cliErr.Code, ErrCodeValidationError)
	}
}
