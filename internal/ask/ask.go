// Package ask provides LLM-powered natural language to jc CLI command
// translation. It sends user queries along with the CLI schema to an LLM
// provider and parses the response into executable command strings.
package ask

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/klaassen-consulting/jc/internal/schema"
)

// Provider identifies which LLM service to use.
type Provider string

const (
	ProviderAnthropic Provider = "anthropic"
	ProviderOpenAI    Provider = "openai"
	ProviderOllama    Provider = "ollama"
	ProviderDisabled  Provider = "disabled"
)

// ValidProviders lists all recognized provider values.
var ValidProviders = []string{
	string(ProviderAnthropic),
	string(ProviderOpenAI),
	string(ProviderOllama),
	string(ProviderDisabled),
}

// IsValidProvider returns true if the given provider name is recognized.
func IsValidProvider(p string) bool {
	for _, v := range ValidProviders {
		if p == v {
			return true
		}
	}
	return false
}

// TranslateResult holds the LLM's response: one or more jc command strings.
type TranslateResult struct {
	Commands    []string `json:"commands"`
	Explanation string   `json:"explanation"`
}

// Client is the interface for LLM translation. Implementations call the
// appropriate LLM API and parse the response into TranslateResult.
type Client interface {
	Translate(query string, maxCommands int) (*TranslateResult, error)
}

// httpDoFunc abstracts http.Client.Do for test injection.
var httpDoFunc = func(req *http.Request) (*http.Response, error) {
	c := &http.Client{Timeout: 60 * time.Second}
	return c.Do(req)
}

// buildSystemPrompt creates the system prompt containing the CLI schema context.
func buildSystemPrompt(maxCommands int) string {
	manifest := schema.BuildCommandManifest()
	manifestJSON, _ := json.Marshal(manifest)

	return fmt.Sprintf(`You are a JumpCloud CLI assistant. Your job is to translate natural language queries into jc CLI commands.

IMPORTANT RULES:
1. Only output valid jc CLI commands. Do NOT include the "jc" prefix — just the subcommand and flags.
2. Output at most %d commands.
3. Each command must be a valid jc subcommand with proper flags.
4. Do NOT make up flags or subcommands that don't exist.
5. For time-based queries, use --filter with comparison operators or --last for insights.
6. Output commands one per line, no numbering, no bullets, no code fences.
7. After the commands, add a blank line and then a brief explanation starting with "Explanation: ".

CLI Schema:
%s`, maxCommands, string(manifestJSON))
}

// --- Anthropic provider ---

// AnthropicClient calls the Anthropic Messages API.
type AnthropicClient struct {
	APIKey string
	Model  string
	URL    string // overridable for testing
}

// anthropicRequest is the Anthropic Messages API request body.
type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system"`
	Messages  []anthropicMessage `json:"messages"`
}

// anthropicMessage is a single message in the Anthropic API.
type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// anthropicResponse is the Anthropic Messages API response body.
type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

func (c *AnthropicClient) Translate(query string, maxCommands int) (*TranslateResult, error) {
	url := c.URL
	if url == "" {
		url = "https://api.anthropic.com/v1/messages"
	}
	model := c.Model
	if model == "" {
		model = "claude-sonnet-4-20250514"
	}

	body := anthropicRequest{
		Model:     model,
		MaxTokens: 1024,
		System:    buildSystemPrompt(maxCommands),
		Messages: []anthropicMessage{
			{Role: "user", Content: query},
		},
	}

	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := httpDoFunc(req)
	if err != nil {
		return nil, fmt.Errorf("LLM API request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read LLM response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("LLM API error (HTTP %d): %s", resp.StatusCode, truncateBody(string(respBody), 200))
	}

	var apiResp anthropicResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse LLM response: %w", err)
	}

	if apiResp.Error != nil {
		return nil, fmt.Errorf("LLM API error: %s", apiResp.Error.Message)
	}

	if len(apiResp.Content) == 0 {
		return nil, fmt.Errorf("LLM returned empty response")
	}

	return parseResponse(apiResp.Content[0].Text, maxCommands), nil
}

// --- OpenAI provider ---

// OpenAIClient calls the OpenAI Chat Completions API.
type OpenAIClient struct {
	APIKey string
	Model  string
	URL    string // overridable for testing
}

// openAIRequest is the OpenAI Chat Completions API request body.
type openAIRequest struct {
	Model    string             `json:"model"`
	Messages []openAIMessage    `json:"messages"`
	MaxTokens int               `json:"max_tokens"`
}

// openAIMessage is a single message in the OpenAI API.
type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// openAIResponse is the OpenAI Chat Completions API response body.
type openAIResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (c *OpenAIClient) Translate(query string, maxCommands int) (*TranslateResult, error) {
	url := c.URL
	if url == "" {
		url = "https://api.openai.com/v1/chat/completions"
	}
	model := c.Model
	if model == "" {
		model = "gpt-4o"
	}

	body := openAIRequest{
		Model:     model,
		MaxTokens: 1024,
		Messages: []openAIMessage{
			{Role: "system", Content: buildSystemPrompt(maxCommands)},
			{Role: "user", Content: query},
		},
	}

	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	resp, err := httpDoFunc(req)
	if err != nil {
		return nil, fmt.Errorf("LLM API request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read LLM response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("LLM API error (HTTP %d): %s", resp.StatusCode, truncateBody(string(respBody), 200))
	}

	var apiResp openAIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse LLM response: %w", err)
	}

	if apiResp.Error != nil {
		return nil, fmt.Errorf("LLM API error: %s", apiResp.Error.Message)
	}

	if len(apiResp.Choices) == 0 {
		return nil, fmt.Errorf("LLM returned empty response")
	}

	return parseResponse(apiResp.Choices[0].Message.Content, maxCommands), nil
}

// --- Ollama provider ---

// OllamaClient calls a local Ollama instance.
type OllamaClient struct {
	Model string
	URL   string // overridable; defaults to http://localhost:11434
}

func (c *OllamaClient) Translate(query string, maxCommands int) (*TranslateResult, error) {
	url := c.URL
	if url == "" {
		url = "http://localhost:11434"
	}
	url = strings.TrimRight(url, "/") + "/api/chat"
	model := c.Model
	if model == "" {
		model = "llama3"
	}

	// Ollama uses the same chat format as OpenAI.
	body := openAIRequest{
		Model: model,
		Messages: []openAIMessage{
			{Role: "system", Content: buildSystemPrompt(maxCommands)},
			{Role: "user", Content: query},
		},
	}

	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Add stream=false to get a single response.
	var bodyMap map[string]interface{}
	_ = json.Unmarshal(data, &bodyMap)
	bodyMap["stream"] = false
	data, _ = json.Marshal(bodyMap)

	req, err := http.NewRequest("POST", url, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpDoFunc(req)
	if err != nil {
		return nil, fmt.Errorf("Ollama API request failed (is Ollama running?): %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read Ollama response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Ollama API error (HTTP %d): %s", resp.StatusCode, truncateBody(string(respBody), 200))
	}

	// Ollama response has message.content
	var ollamaResp struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(respBody, &ollamaResp); err != nil {
		return nil, fmt.Errorf("failed to parse Ollama response: %w", err)
	}

	if ollamaResp.Message.Content == "" {
		return nil, fmt.Errorf("Ollama returned empty response")
	}

	return parseResponse(ollamaResp.Message.Content, maxCommands), nil
}

// --- Response parsing ---

// parseResponse extracts commands and explanation from the LLM's text output.
func parseResponse(text string, maxCommands int) *TranslateResult {
	lines := strings.Split(strings.TrimSpace(text), "\n")

	var commands []string
	var explanationLines []string
	inExplanation := false

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			if len(commands) > 0 {
				inExplanation = true
			}
			continue
		}

		if inExplanation || strings.HasPrefix(line, "Explanation:") {
			inExplanation = true
			explanationLines = append(explanationLines, strings.TrimPrefix(line, "Explanation: "))
			continue
		}

		// Strip common LLM formatting artifacts.
		line = stripFormatting(line)
		if line == "" {
			continue
		}

		// Strip "jc " prefix if the LLM included it.
		line = strings.TrimPrefix(line, "jc ")

		if len(commands) < maxCommands {
			commands = append(commands, line)
		}
	}

	return &TranslateResult{
		Commands:    commands,
		Explanation: strings.Join(explanationLines, " "),
	}
}

// stripFormatting removes common LLM formatting from a line.
func stripFormatting(line string) string {
	// Remove numbered list prefixes: "1. ", "1) "
	if len(line) > 2 && line[0] >= '0' && line[0] <= '9' {
		for i := 0; i < len(line); i++ {
			if line[i] == '.' || line[i] == ')' {
				if i+1 < len(line) && line[i+1] == ' ' {
					line = strings.TrimSpace(line[i+2:])
					break
				}
			}
			if line[i] < '0' || line[i] > '9' {
				break
			}
		}
	}
	// Remove bullet prefixes: "- ", "* "
	line = strings.TrimPrefix(line, "- ")
	line = strings.TrimPrefix(line, "* ")
	// Remove code fences
	if strings.HasPrefix(line, "```") {
		return ""
	}
	// Remove inline backticks
	line = strings.Trim(line, "`")
	return strings.TrimSpace(line)
}

// truncateBody shortens a string for error messages.
func truncateBody(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// NewClient creates an LLM client for the given provider and API key.
func NewClient(provider Provider, apiKey, model, url string) (Client, error) {
	switch provider {
	case ProviderAnthropic:
		if apiKey == "" {
			return nil, fmt.Errorf("Anthropic API key required. Set via 'jc config set ask.api_key <key>' or JC_ASK_API_KEY env var")
		}
		return &AnthropicClient{APIKey: apiKey, Model: model, URL: url}, nil
	case ProviderOpenAI:
		if apiKey == "" {
			return nil, fmt.Errorf("OpenAI API key required. Set via 'jc config set ask.api_key <key>' or JC_ASK_API_KEY env var")
		}
		return &OpenAIClient{APIKey: apiKey, Model: model, URL: url}, nil
	case ProviderOllama:
		return &OllamaClient{Model: model, URL: url}, nil
	case ProviderDisabled:
		return nil, fmt.Errorf("conversational mode is disabled. Set ask.provider in config to enable")
	default:
		return nil, fmt.Errorf("unknown LLM provider %q. Valid providers: %s", provider, strings.Join(ValidProviders, ", "))
	}
}
