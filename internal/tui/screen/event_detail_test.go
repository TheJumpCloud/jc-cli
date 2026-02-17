package screen

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/klaassen-consulting/jc/internal/ask"
	"github.com/klaassen-consulting/jc/internal/tui"
)

var testEvent = json.RawMessage(`{
	"timestamp": "2024-01-15T10:30:00Z",
	"event_type": "user_login",
	"initiated_by": "admin@example.com",
	"client_ip": "192.168.1.1",
	"success": true,
	"organization": "test-org",
	"mfa": false
}`)

func TestEventDetailScreen_Title(t *testing.T) {
	e := NewEventDetailScreen(testEvent)
	if e.Title() != "user_login" {
		t.Errorf("Title = %q, want 'user_login'", e.Title())
	}
}

func TestEventDetailScreen_TitleFallback(t *testing.T) {
	data := json.RawMessage(`{"success": true}`)
	e := NewEventDetailScreen(data)
	// ExtractName returns "" for missing field; Title() returns "".
	if e.Title() != "" {
		t.Errorf("Title = %q, want empty string", e.Title())
	}
}

func TestEventDetailScreen_RendersAllFields(t *testing.T) {
	e := NewEventDetailScreen(testEvent)
	e.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	view := e.View()
	for _, field := range []string{"timestamp", "event_type", "initiated_by", "client_ip", "success", "organization", "mfa"} {
		if !strings.Contains(view, field) {
			t.Errorf("view should contain field %q", field)
		}
	}
}

func TestEventDetailScreen_ViewHeader(t *testing.T) {
	e := NewEventDetailScreen(testEvent)
	e.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	view := e.View()
	if !strings.Contains(view, "user_login") {
		t.Error("view header should contain event_type")
	}
	if !strings.Contains(view, "2024-01-15") {
		t.Error("view header should contain timestamp")
	}
}

func TestEventDetailScreen_EscPops(t *testing.T) {
	e := NewEventDetailScreen(testEvent)
	e.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	_, cmd := e.Update(tea.KeyMsg{Type: tea.KeyEscape})
	if cmd == nil {
		t.Fatal("Esc should produce a command")
	}
	msg := cmd()
	if _, ok := msg.(tui.PopScreenMsg); !ok {
		t.Fatalf("expected PopScreenMsg, got %T", msg)
	}
}

func TestEventDetailScreen_CopyJSON(t *testing.T) {
	var copied string
	origClip := clipboardWriteFunc
	clipboardWriteFunc = func(s string) error {
		copied = s
		return nil
	}
	t.Cleanup(func() { clipboardWriteFunc = origClip })

	e := NewEventDetailScreen(testEvent)
	e.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	_, cmd := e.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	if cmd == nil {
		t.Fatal("c should produce a command")
	}
	msg := cmd()
	flash, ok := msg.(tui.FlashMsg)
	if !ok {
		t.Fatalf("expected FlashMsg, got %T", msg)
	}
	if !strings.Contains(flash.Text, "Copied event JSON") {
		t.Errorf("flash = %q, want 'Copied event JSON'", flash.Text)
	}
	if copied == "" {
		t.Error("clipboard should have been written to")
	}
	if !strings.Contains(copied, "user_login") {
		t.Error("clipboard should contain event data")
	}
}

func TestEventDetailScreen_ExportMode(t *testing.T) {
	e := NewEventDetailScreen(testEvent)
	e.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	// Press e to enter export mode.
	e.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})
	if !e.exporting {
		t.Error("e should enter export mode")
	}

	view := e.View()
	if !strings.Contains(view, "Export") {
		t.Error("view should show export options")
	}

	// Press Esc to cancel export.
	e.Update(tea.KeyMsg{Type: tea.KeyEscape})
	if e.exporting {
		t.Error("Esc should cancel export mode")
	}
}

func TestEventDetailScreen_HelpLine(t *testing.T) {
	e := NewEventDetailScreen(testEvent)
	e.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	view := e.View()
	for _, hint := range []string{"esc:back", "c:copy", "e:export", "x:explain"} {
		if !strings.Contains(view, hint) {
			t.Errorf("view should contain help hint %q", hint)
		}
	}
}

// mockAskClient implements ask.Client for testing.
type mockAskClient struct {
	result *ask.TranslateResult
	err    error
}

func (m *mockAskClient) Translate(query string, maxCommands int) (*ask.TranslateResult, error) {
	return m.result, m.err
}

func TestEventDetailScreen_ExplainSuccess(t *testing.T) {
	origClient := newAskClientFunc
	newAskClientFunc = func() (ask.Client, error) {
		return &mockAskClient{
			result: &ask.TranslateResult{
				Explanation: "An admin logged in from a private IP address. This appears to be normal activity.",
			},
		}, nil
	}
	t.Cleanup(func() { newAskClientFunc = origClient })

	e := NewEventDetailScreen(testEvent)
	e.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	// Press x to trigger explain.
	_, cmd := e.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	if cmd == nil {
		t.Fatal("x should produce a command")
	}
	if !e.explaining {
		t.Error("explaining should be true")
	}

	// Execute the batched commands to find the explain result.
	msgs := executeBatchCmd(cmd)
	var explainResult *ExplainResultMsg
	for _, m := range msgs {
		if er, ok := m.(ExplainResultMsg); ok {
			explainResult = &er
			break
		}
	}
	if explainResult == nil {
		t.Fatal("batch should contain ExplainResultMsg")
	}

	// Send the result back.
	e.Update(*explainResult)

	if e.explaining {
		t.Error("explaining should be false after result")
	}
	if e.explanation == "" {
		t.Error("explanation should be set")
	}

	view := e.View()
	if !strings.Contains(view, "AI Explanation") {
		t.Error("view should contain AI Explanation section header")
	}
	if !strings.Contains(view, "admin logged in") {
		t.Error("view should contain the explanation text")
	}
}

func TestEventDetailScreen_ExplainNotConfigured(t *testing.T) {
	origClient := newAskClientFunc
	newAskClientFunc = func() (ask.Client, error) {
		return nil, fmt.Errorf("conversational mode is disabled. Set ask.provider in config to enable")
	}
	t.Cleanup(func() { newAskClientFunc = origClient })

	e := NewEventDetailScreen(testEvent)
	e.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	_, cmd := e.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	if cmd == nil {
		t.Fatal("x should produce a command")
	}

	msgs := executeBatchCmd(cmd)
	var explainResult *ExplainResultMsg
	for _, m := range msgs {
		if er, ok := m.(ExplainResultMsg); ok {
			explainResult = &er
			break
		}
	}
	if explainResult == nil {
		t.Fatal("batch should contain ExplainResultMsg")
	}

	e.Update(*explainResult)

	if e.explainErr == "" {
		t.Error("explainErr should be set when provider not configured")
	}

	view := e.View()
	if !strings.Contains(view, "disabled") {
		t.Error("view should show disabled provider error")
	}
}

func TestEventDetailScreen_ExplainError(t *testing.T) {
	origClient := newAskClientFunc
	newAskClientFunc = func() (ask.Client, error) {
		return &mockAskClient{
			err: fmt.Errorf("LLM API error (HTTP 500): internal server error"),
		}, nil
	}
	t.Cleanup(func() { newAskClientFunc = origClient })

	e := NewEventDetailScreen(testEvent)
	e.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	_, cmd := e.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	msgs := executeBatchCmd(cmd)

	var explainResult *ExplainResultMsg
	for _, m := range msgs {
		if er, ok := m.(ExplainResultMsg); ok {
			explainResult = &er
			break
		}
	}
	if explainResult == nil {
		t.Fatal("batch should contain ExplainResultMsg")
	}

	e.Update(*explainResult)

	if e.explainErr == "" {
		t.Error("explainErr should be set on API error")
	}
	if !strings.Contains(e.explainErr, "HTTP 500") {
		t.Errorf("explainErr = %q, should mention HTTP 500", e.explainErr)
	}

	view := e.View()
	if !strings.Contains(view, "Error") {
		t.Error("view should show error in explanation section")
	}
}

func TestEventDetailScreen_StaleExplainIgnored(t *testing.T) {
	e := NewEventDetailScreen(testEvent)
	e.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	e.explaining = true
	e.explainGen = 5

	// Send a stale result.
	e.Update(ExplainResultMsg{
		Generation:  3, // stale
		Explanation: "should be ignored",
	})

	if !e.explaining {
		t.Error("explaining should still be true (stale result ignored)")
	}
	if e.explanation != "" {
		t.Error("explanation should be empty (stale result ignored)")
	}
}

func TestEventDetailScreen_ExplainCommandsField(t *testing.T) {
	// Simulate the real-world case where parseResponse puts the explanation
	// text into Commands (first line) rather than Explanation.
	origClient := newAskClientFunc
	newAskClientFunc = func() (ask.Client, error) {
		return &mockAskClient{
			result: &ask.TranslateResult{
				Commands:    []string{"A disk usage alert fired on macos.shared."},
				Explanation: "",
			},
		}, nil
	}
	t.Cleanup(func() { newAskClientFunc = origClient })

	e := NewEventDetailScreen(testEvent)
	e.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	_, cmd := e.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	msgs := executeBatchCmd(cmd)
	for _, m := range msgs {
		if er, ok := m.(ExplainResultMsg); ok {
			e.Update(er)
			break
		}
	}

	if e.explanation == "" {
		t.Error("explanation should be set from Commands field")
	}
	if !strings.Contains(e.explanation, "disk usage alert") {
		t.Errorf("explanation = %q, should contain text from Commands", e.explanation)
	}
}

// executeBatchCmd executes a tea.Cmd and collects all messages from a tea.BatchMsg.
func executeBatchCmd(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return []tea.Msg{msg}
	}
	var msgs []tea.Msg
	for _, c := range batch {
		if c != nil {
			msgs = append(msgs, c())
		}
	}
	return msgs
}
