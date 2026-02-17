package ask

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestParseResponse_SingleCommand(t *testing.T) {
	text := "users list --filter \"suspended=true\" -t\n\nExplanation: Lists suspended users in table format."
	result := parseResponse(text, 10)

	if len(result.Commands) != 1 {
		t.Fatalf("expected 1 command, got %d", len(result.Commands))
	}
	if result.Commands[0] != "users list --filter \"suspended=true\" -t" {
		t.Errorf("unexpected command: %s", result.Commands[0])
	}
	if !strings.Contains(result.Explanation, "suspended users") {
		t.Errorf("unexpected explanation: %s", result.Explanation)
	}
}

func TestParseResponse_MultipleCommands(t *testing.T) {
	text := "users list --filter \"activated=false\" --ids\nusers delete --stdin --force\n\nExplanation: Find and delete inactive users."
	result := parseResponse(text, 10)

	if len(result.Commands) != 2 {
		t.Fatalf("expected 2 commands, got %d", len(result.Commands))
	}
	if result.Commands[0] != "users list --filter \"activated=false\" --ids" {
		t.Errorf("unexpected command[0]: %s", result.Commands[0])
	}
}

func TestParseResponse_MaxCommandsCapped(t *testing.T) {
	text := "users list\ndevices list\ngroups user list\npolicies list\n\nExplanation: Lists everything."
	result := parseResponse(text, 2)

	if len(result.Commands) != 2 {
		t.Fatalf("expected 2 commands (capped), got %d", len(result.Commands))
	}
}

func TestParseResponse_StripsNumberedPrefix(t *testing.T) {
	text := "1. users list\n2. devices list\n\nExplanation: Two lists."
	result := parseResponse(text, 10)

	if len(result.Commands) != 2 {
		t.Fatalf("expected 2 commands, got %d", len(result.Commands))
	}
	if result.Commands[0] != "users list" {
		t.Errorf("expected numbering stripped, got: %s", result.Commands[0])
	}
}

func TestParseResponse_StripsBulletPrefix(t *testing.T) {
	text := "- users list\n- devices list\n\nExplanation: Two lists."
	result := parseResponse(text, 10)

	if len(result.Commands) != 2 {
		t.Fatalf("expected 2 commands, got %d", len(result.Commands))
	}
	if result.Commands[0] != "users list" {
		t.Errorf("expected bullet stripped, got: %s", result.Commands[0])
	}
}

func TestParseResponse_StripsCodeFences(t *testing.T) {
	text := "```\nusers list\n```\n\nExplanation: Lists users."
	result := parseResponse(text, 10)

	if len(result.Commands) != 1 {
		t.Fatalf("expected 1 command, got %d", len(result.Commands))
	}
	if result.Commands[0] != "users list" {
		t.Errorf("expected code fences removed, got: %s", result.Commands[0])
	}
}

func TestParseResponse_StripsJCPrefix(t *testing.T) {
	text := "jc users list\n\nExplanation: Lists users."
	result := parseResponse(text, 10)

	if len(result.Commands) != 1 {
		t.Fatalf("expected 1 command, got %d", len(result.Commands))
	}
	if result.Commands[0] != "users list" {
		t.Errorf("expected jc prefix stripped, got: %s", result.Commands[0])
	}
}

func TestParseResponse_StripsBackticks(t *testing.T) {
	text := "`users list`\n\nExplanation: Lists users."
	result := parseResponse(text, 10)

	if len(result.Commands) != 1 {
		t.Fatalf("expected 1 command, got %d", len(result.Commands))
	}
	if result.Commands[0] != "users list" {
		t.Errorf("expected backticks stripped, got: %s", result.Commands[0])
	}
}

func TestParseResponse_EmptyInput(t *testing.T) {
	result := parseResponse("", 10)
	if len(result.Commands) != 0 {
		t.Fatalf("expected 0 commands, got %d", len(result.Commands))
	}
}

func TestStripFormatting(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"1. users list", "users list"},
		{"2) devices list", "devices list"},
		{"- users list", "users list"},
		{"* devices list", "devices list"},
		{"```bash", ""},
		{"`users list`", "users list"},
		{"users list", "users list"},
		{"10. admins list", "admins list"},
	}
	for _, tt := range tests {
		got := stripFormatting(tt.input)
		if got != tt.want {
			t.Errorf("stripFormatting(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestBuildSystemPrompt(t *testing.T) {
	prompt := buildSystemPrompt(5)
	if !strings.Contains(prompt, "at most 5 commands") {
		t.Error("prompt should mention max commands")
	}
	if !strings.Contains(prompt, "CLI Schema:") {
		t.Error("prompt should include CLI schema")
	}
	if !strings.Contains(prompt, "jc") {
		t.Error("prompt should mention jc CLI")
	}
}

func TestNewClient_Providers(t *testing.T) {
	tests := []struct {
		provider Provider
		apiKey   string
		wantErr  bool
	}{
		{ProviderAnthropic, "sk-test", false},
		{ProviderAnthropic, "", true},
		{ProviderOpenAI, "sk-test", false},
		{ProviderOpenAI, "", true},
		{ProviderOllama, "", false},
		{ProviderDisabled, "", true},
		{"unknown", "", true},
	}
	for _, tt := range tests {
		_, err := NewClient(tt.provider, tt.apiKey, "", "")
		if (err != nil) != tt.wantErr {
			t.Errorf("NewClient(%q, %q): error=%v, wantErr=%v", tt.provider, tt.apiKey, err, tt.wantErr)
		}
	}
}

func TestIsValidProvider(t *testing.T) {
	if !IsValidProvider("anthropic") {
		t.Error("anthropic should be valid")
	}
	if !IsValidProvider("openai") {
		t.Error("openai should be valid")
	}
	if !IsValidProvider("ollama") {
		t.Error("ollama should be valid")
	}
	if !IsValidProvider("disabled") {
		t.Error("disabled should be valid")
	}
	if IsValidProvider("gemini") {
		t.Error("gemini should not be valid")
	}
}

func TestAnthropicClient_Translate(t *testing.T) {
	// Mock the HTTP client.
	origDoFunc := httpDoFunc
	defer func() { httpDoFunc = origDoFunc }()

	httpDoFunc = func(req *http.Request) (*http.Response, error) {
		// Verify request format.
		if req.Header.Get("x-api-key") != "test-key" {
			t.Error("expected x-api-key header")
		}
		if req.Header.Get("anthropic-version") != "2023-06-01" {
			t.Error("expected anthropic-version header")
		}

		var body anthropicRequest
		data, _ := io.ReadAll(req.Body)
		if err := json.Unmarshal(data, &body); err != nil {
			t.Fatalf("failed to parse request body: %v", err)
		}
		if body.Messages[0].Content != "list suspended users" {
			t.Errorf("unexpected query: %s", body.Messages[0].Content)
		}

		resp := anthropicResponse{
			Content: []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}{{Type: "text", Text: "users list --filter \"suspended=true\" -t\n\nExplanation: Lists all suspended users."}},
		}
		respData, _ := json.Marshal(resp)
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(string(respData))),
		}, nil
	}

	client := &AnthropicClient{APIKey: "test-key"}
	result, err := client.Translate("list suspended users", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Commands) != 1 {
		t.Fatalf("expected 1 command, got %d", len(result.Commands))
	}
	if !strings.Contains(result.Commands[0], "suspended=true") {
		t.Errorf("unexpected command: %s", result.Commands[0])
	}
}

func TestAnthropicClient_APIError(t *testing.T) {
	origDoFunc := httpDoFunc
	defer func() { httpDoFunc = origDoFunc }()

	httpDoFunc = func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 401,
			Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"invalid api key"}}`)),
		}, nil
	}

	client := &AnthropicClient{APIKey: "bad-key"}
	_, err := client.Translate("test", 10)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("expected HTTP 401 in error, got: %s", err.Error())
	}
}

func TestOpenAIClient_Translate(t *testing.T) {
	origDoFunc := httpDoFunc
	defer func() { httpDoFunc = origDoFunc }()

	httpDoFunc = func(req *http.Request) (*http.Response, error) {
		if !strings.HasPrefix(req.Header.Get("Authorization"), "Bearer ") {
			t.Error("expected Bearer auth header")
		}

		resp := openAIResponse{
			Choices: []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			}{{Message: struct {
				Content string `json:"content"`
			}{Content: "users list -t\n\nExplanation: Lists all users."}}},
		}
		respData, _ := json.Marshal(resp)
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(string(respData))),
		}, nil
	}

	client := &OpenAIClient{APIKey: "sk-test"}
	result, err := client.Translate("list all users", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Commands) != 1 {
		t.Fatalf("expected 1 command, got %d", len(result.Commands))
	}
}

func TestOllamaClient_Translate(t *testing.T) {
	origDoFunc := httpDoFunc
	defer func() { httpDoFunc = origDoFunc }()

	httpDoFunc = func(req *http.Request) (*http.Response, error) {
		// Verify the URL includes /api/chat
		if !strings.HasSuffix(req.URL.Path, "/api/chat") {
			t.Errorf("expected /api/chat path, got: %s", req.URL.Path)
		}

		// Verify stream=false is set
		var body map[string]interface{}
		data, _ := io.ReadAll(req.Body)
		_ = json.Unmarshal(data, &body)
		if stream, ok := body["stream"]; !ok || stream != false {
			t.Error("expected stream=false")
		}

		resp := map[string]interface{}{
			"message": map[string]string{
				"content": "devices list -t\n\nExplanation: Lists all devices.",
			},
		}
		respData, _ := json.Marshal(resp)
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(string(respData))),
		}, nil
	}

	client := &OllamaClient{URL: "http://localhost:11434"}
	result, err := client.Translate("list devices", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Commands) != 1 {
		t.Fatalf("expected 1 command, got %d", len(result.Commands))
	}
}

func TestTruncateBody(t *testing.T) {
	short := "hello"
	if truncateBody(short, 10) != "hello" {
		t.Error("short string should not be truncated")
	}
	long := strings.Repeat("x", 300)
	result := truncateBody(long, 200)
	if len(result) != 203 { // 200 + "..."
		t.Errorf("expected 203 chars, got %d", len(result))
	}
}

// === Battle Tests: Edge Cases ===

func TestParseResponse_UnicodeBullets(t *testing.T) {
	// Unicode bullet "•" is NOT handled by stripFormatting (only "- " and "* " prefixes).
	result := parseResponse("• users list", 10)
	if len(result.Commands) != 1 {
		t.Fatalf("got %d commands, want 1", len(result.Commands))
	}
	// The "•" should remain since stripFormatting doesn't handle it.
	if result.Commands[0] != "• users list" {
		t.Errorf("command = %q, want %q", result.Commands[0], "• users list")
	}
}

func TestParseResponse_MixedNumberingFormats(t *testing.T) {
	input := "1. users list\n2) devices list\n- groups user list"
	result := parseResponse(input, 10)
	if len(result.Commands) != 3 {
		t.Fatalf("got %d commands, want 3", len(result.Commands))
	}
	if result.Commands[0] != "users list" {
		t.Errorf("commands[0] = %q, want %q", result.Commands[0], "users list")
	}
	if result.Commands[1] != "devices list" {
		t.Errorf("commands[1] = %q, want %q", result.Commands[1], "devices list")
	}
	if result.Commands[2] != "groups user list" {
		t.Errorf("commands[2] = %q, want %q", result.Commands[2], "groups user list")
	}
}

func TestParseResponse_MultipleBlankLines(t *testing.T) {
	input := "users list\n\n\n\nThis is the explanation."
	result := parseResponse(input, 10)
	if len(result.Commands) != 1 {
		t.Fatalf("got %d commands, want 1", len(result.Commands))
	}
	if result.Commands[0] != "users list" {
		t.Errorf("commands[0] = %q, want %q", result.Commands[0], "users list")
	}
	// Everything after the first blank line goes to explanation.
	if result.Explanation == "" {
		t.Error("expected non-empty explanation")
	}
}

func TestParseResponse_VeryLongCommand(t *testing.T) {
	long := "users list --filter " + strings.Repeat("x", 10000)
	result := parseResponse(long, 10)
	if len(result.Commands) != 1 {
		t.Fatalf("got %d commands, want 1", len(result.Commands))
	}
	if len(result.Commands[0]) < 10000 {
		t.Errorf("command length = %d, want >= 10000", len(result.Commands[0]))
	}
}

func TestParseResponse_EmptyAfterStripping(t *testing.T) {
	// Only code fences — stripped to empty lines.
	input := "```\n```"
	result := parseResponse(input, 10)
	if len(result.Commands) != 0 {
		t.Errorf("got %d commands, want 0", len(result.Commands))
	}
}

func TestParseResponse_NoExplanation(t *testing.T) {
	// Commands with no blank line separator → no explanation.
	input := "users list\ndevices list"
	result := parseResponse(input, 10)
	if len(result.Commands) != 2 {
		t.Fatalf("got %d commands, want 2", len(result.Commands))
	}
	if result.Explanation != "" {
		t.Errorf("explanation = %q, want empty", result.Explanation)
	}
}

func TestParseResponse_ExplanationOnly(t *testing.T) {
	// Leading blank line is removed by TrimSpace, so "Explanation:" prefix
	// is needed to route the line into explanation (not commands).
	input := "\nExplanation: This is just an explanation."
	result := parseResponse(input, 10)
	if len(result.Commands) != 0 {
		t.Errorf("got %d commands, want 0", len(result.Commands))
	}
	if result.Explanation == "" {
		t.Error("expected non-empty explanation")
	}
}

func TestParseResponse_JCPrefixVariants(t *testing.T) {
	// Only lowercase "jc " should be stripped.
	tests := []struct {
		input   string
		wantCmd string
	}{
		{"jc users list", "users list"},
		{"JC users list", "JC users list"},
	}
	for _, tt := range tests {
		result := parseResponse(tt.input, 10)
		if len(result.Commands) != 1 {
			t.Errorf("parseResponse(%q): got %d commands, want 1", tt.input, len(result.Commands))
			continue
		}
		if result.Commands[0] != tt.wantCmd {
			t.Errorf("parseResponse(%q): command = %q, want %q", tt.input, result.Commands[0], tt.wantCmd)
		}
	}
}

func TestStripFormatting_CodeFenceWithLang(t *testing.T) {
	got := stripFormatting("```bash")
	if got != "" {
		t.Errorf("stripFormatting(code fence with lang) = %q, want empty", got)
	}
}

func TestStripFormatting_NestedBackticks(t *testing.T) {
	got := stripFormatting("`users list`")
	if got != "users list" {
		t.Errorf("stripFormatting = %q, want %q", got, "users list")
	}
}
