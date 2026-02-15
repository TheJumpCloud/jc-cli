package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// startCustomEmailsServer creates a mock JumpCloud V2 server that handles custom email endpoints.
func startCustomEmailsServer(t *testing.T, templates []map[string]any, configs map[string]map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// GET /customemail/templates — list template definitions.
		if r.URL.Path == "/customemail/templates" && r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(templates)
			return
		}

		// Routes under /customemails/{type}.
		if strings.HasPrefix(r.URL.Path, "/customemails/") {
			emailType := strings.TrimPrefix(r.URL.Path, "/customemails/")

			switch r.Method {
			case http.MethodGet:
				if cfg, ok := configs[emailType]; ok {
					json.NewEncoder(w).Encode(cfg)
					return
				}
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte(`{"message":"Not Found"}`))
				return

			case http.MethodPost:
				var input map[string]any
				json.NewDecoder(r.Body).Decode(&input)
				input["type"] = emailType
				w.WriteHeader(http.StatusCreated)
				json.NewEncoder(w).Encode(input)
				return

			case http.MethodPut:
				if cfg, ok := configs[emailType]; ok {
					var input map[string]any
					json.NewDecoder(r.Body).Decode(&input)
					merged := make(map[string]any)
					for k, v := range cfg {
						merged[k] = v
					}
					for k, v := range input {
						merged[k] = v
					}
					json.NewEncoder(w).Encode(merged)
					return
				}
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte(`{"message":"Not Found"}`))
				return

			case http.MethodDelete:
				if _, ok := configs[emailType]; ok {
					w.WriteHeader(http.StatusOK)
					w.Write([]byte(`{}`))
					return
				}
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte(`{"message":"Not Found"}`))
				return
			}
		}

		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Not Found"}`))
	}))
}

func sampleCustomEmailTemplates() []map[string]any {
	return []map[string]any{
		{
			"type":        "activate_user_custom",
			"displayName": "Activate User Custom",
			"description": "Custom activation email for new users",
		},
		{
			"type":        "password_expiration",
			"displayName": "Password Expiration",
			"description": "Notification sent when password is about to expire",
		},
		{
			"type":        "lockout_notice_user",
			"displayName": "Account Lockout Notice",
			"description": "Notification sent when account is locked",
		},
	}
}

func sampleCustomEmailConfigs() map[string]map[string]any {
	return map[string]map[string]any{
		"activate_user_custom": {
			"type":    "activate_user_custom",
			"subject": "Welcome to our organization",
			"title":   "Get Started",
			"body":    "Click the button below to activate your account.",
			"header":  "Welcome!",
			"button":  "Activate Now",
		},
		"password_expiration": {
			"type":    "password_expiration",
			"subject": "Your password is expiring soon",
			"title":   "Password Expiration Notice",
		},
	}
}

// --- Templates List Tests ---

func TestCustomEmailTemplatesList(t *testing.T) {
	setupUsersTest(t)
	templates := sampleCustomEmailTemplates()
	ts := startCustomEmailsServer(t, templates, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"custom-emails", "templates"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) != 3 {
		t.Errorf("got %d templates, want 3", len(result))
	}
}

// --- Get Tests ---

func TestCustomEmailGetByType(t *testing.T) {
	setupUsersTest(t)
	configs := sampleCustomEmailConfigs()
	ts := startCustomEmailsServer(t, nil, configs)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"custom-emails", "get", "activate_user_custom"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["type"] != "activate_user_custom" {
		t.Errorf("type = %q, want 'activate_user_custom'", result["type"])
	}
	if result["subject"] != "Welcome to our organization" {
		t.Errorf("subject = %q, want 'Welcome to our organization'", result["subject"])
	}
}

func TestCustomEmailGetInvalidType(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"custom-emails", "get", "invalid_type"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for invalid type, got nil")
	}
	if !strings.Contains(err.Error(), "invalid custom email type") {
		t.Errorf("error should mention 'invalid custom email type', got: %v", err)
	}
}

// --- Create Tests ---

func TestCustomEmailCreate(t *testing.T) {
	setupUsersTest(t)
	ts := startCustomEmailsServer(t, nil, sampleCustomEmailConfigs())
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"custom-emails", "create",
		"--type", "activate_user_custom",
		"--subject", "Welcome!",
		"--title", "Get Started",
		"--body", "Click below to activate.",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["subject"] != "Welcome!" {
		t.Errorf("subject = %q, want 'Welcome!'", result["subject"])
	}
	if result["type"] != "activate_user_custom" {
		t.Errorf("type = %q, want 'activate_user_custom'", result["type"])
	}
}

func TestCustomEmailCreatePlan(t *testing.T) {
	setupUsersTest(t)
	ts := startCustomEmailsServer(t, nil, sampleCustomEmailConfigs())
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"custom-emails", "create",
		"--type", "activate_user_custom",
		"--subject", "Welcome!",
		"--plan",
	})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected ExitError for plan mode, got nil")
	}
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected ExitError, got: %T: %v", err, err)
	}
	if exitErr.Code != 10 {
		t.Errorf("exit code = %d, want 10", exitErr.Code)
	}
}

// --- Update Tests ---

func TestCustomEmailUpdate(t *testing.T) {
	setupUsersTest(t)
	configs := sampleCustomEmailConfigs()
	ts := startCustomEmailsServer(t, nil, configs)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"custom-emails", "update", "activate_user_custom",
		"--subject", "Updated Subject",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["subject"] != "Updated Subject" {
		t.Errorf("subject = %q, want 'Updated Subject'", result["subject"])
	}
}

func TestCustomEmailUpdateNoFields(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"custom-emails", "update", "activate_user_custom"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for no fields, got nil")
	}
	if !strings.Contains(err.Error(), "no fields to update") {
		t.Errorf("error should mention 'no fields to update', got: %v", err)
	}
}

// --- Delete Tests ---

func TestCustomEmailDeleteForce(t *testing.T) {
	setupUsersTest(t)
	configs := sampleCustomEmailConfigs()
	ts := startCustomEmailsServer(t, nil, configs)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"custom-emails", "delete", "activate_user_custom", "--force"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "deleted successfully") {
		t.Errorf("output should confirm deletion, got: %s", out)
	}
}

func TestCustomEmailDeletePlan(t *testing.T) {
	setupUsersTest(t)
	configs := sampleCustomEmailConfigs()
	ts := startCustomEmailsServer(t, nil, configs)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"custom-emails", "delete", "activate_user_custom", "--plan"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected ExitError for plan mode, got nil")
	}
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected ExitError, got: %T: %v", err, err)
	}
	if exitErr.Code != 10 {
		t.Errorf("exit code = %d, want 10", exitErr.Code)
	}
}

// --- Help Tests ---

func TestCustomEmailHelp(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"custom-emails", "--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	out := buf.String()
	for _, sub := range []string{"templates", "get", "create", "update", "delete"} {
		if !strings.Contains(out, sub) {
			t.Errorf("help should contain subcommand %q, got:\n%s", sub, out)
		}
	}
}
