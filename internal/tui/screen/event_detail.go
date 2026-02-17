package screen

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/klaassen-consulting/jc/internal/ask"
	"github.com/klaassen-consulting/jc/internal/tui"
	"github.com/klaassen-consulting/jc/internal/tui/component"
	"github.com/klaassen-consulting/jc/internal/tui/style"
	"github.com/spf13/viper"
)

// newAskClientFunc creates an ask.Client from config. Overridable in tests.
var newAskClientFunc = func() (ask.Client, error) {
	provider := ask.Provider(viper.GetString("ask.provider"))
	apiKey := resolveAskAPIKey()
	model := viper.GetString("ask.model")
	url := viper.GetString("ask.url")
	return ask.NewClient(provider, apiKey, model, url)
}

// resolveAskAPIKey returns the LLM API key from env or config.
func resolveAskAPIKey() string {
	if key := os.Getenv("JC_ASK_API_KEY"); key != "" {
		return key
	}
	return viper.GetString("ask.api_key")
}

// ExplainResultMsg carries the AI explanation result back to the screen.
type ExplainResultMsg struct {
	Explanation string
	Generation  int64
	Err         error
}

// EventDetailScreen shows all fields of a single insights event.
type EventDetailScreen struct {
	data     json.RawMessage
	viewport viewport.Model
	spinner  spinner.Model
	ready    bool
	width    int
	height   int

	exporting bool

	// AI explanation state.
	explaining  bool
	explanation string
	explainErr  string
	explainGen  int64
}

// NewEventDetailScreen creates an event detail screen from raw event JSON.
func NewEventDetailScreen(data json.RawMessage) *EventDetailScreen {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = style.Spinner

	return &EventDetailScreen{
		data:    data,
		spinner: s,
	}
}

func (e *EventDetailScreen) Title() string {
	return component.ExtractName(e.data, "event_type")
}

func (e *EventDetailScreen) Init() tea.Cmd { return nil }

func (e *EventDetailScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		e.width = msg.Width
		e.height = msg.Height
		headerHeight := 3
		if !e.ready {
			e.viewport = viewport.New(msg.Width, msg.Height-headerHeight-2)
			e.ready = true
		} else {
			e.viewport.Width = msg.Width
			e.viewport.Height = msg.Height - headerHeight - 2
		}
		e.viewport.SetContent(e.renderContent())
		return e, nil

	case ExplainResultMsg:
		if msg.Generation != e.explainGen {
			return e, nil
		}
		e.explaining = false
		if msg.Err != nil {
			e.explainErr = msg.Err.Error()
		} else {
			e.explanation = msg.Explanation
		}
		if e.ready {
			e.viewport.SetContent(e.renderContent())
		}
		return e, nil

	case spinner.TickMsg:
		if e.explaining {
			var cmd tea.Cmd
			e.spinner, cmd = e.spinner.Update(msg)
			return e, cmd
		}
		return e, nil

	case tea.KeyMsg:
		if e.exporting {
			return e, e.handleExportKey(msg)
		}

		switch {
		case msg.String() == "esc":
			return e, func() tea.Msg { return tui.PopScreenMsg{} }

		case msg.String() == "c":
			raw, err := json.MarshalIndent(json.RawMessage(e.data), "", "  ")
			if err != nil {
				return e, func() tea.Msg { return tui.FlashMsg{Text: "Copy error: " + err.Error()} }
			}
			if err := clipboardWriteFunc(string(raw)); err != nil {
				return e, func() tea.Msg { return tui.FlashMsg{Text: "Clipboard error: " + err.Error()} }
			}
			return e, func() tea.Msg { return tui.FlashMsg{Text: "Copied event JSON"} }

		case msg.String() == "e":
			e.exporting = true
			return e, nil

		case msg.String() == "x":
			if e.explaining {
				return e, nil
			}
			return e, e.triggerExplain()

		default:
			var cmd tea.Cmd
			e.viewport, cmd = e.viewport.Update(msg)
			return e, cmd
		}
	}

	return e, nil
}

func (e *EventDetailScreen) triggerExplain() tea.Cmd {
	e.explaining = true
	e.explanation = ""
	e.explainErr = ""
	e.explainGen++
	gen := e.explainGen

	if e.ready {
		e.viewport.SetContent(e.renderContent())
	}

	return tea.Batch(e.spinner.Tick, func() tea.Msg {
		client, err := newAskClientFunc()
		if err != nil {
			return ExplainResultMsg{Generation: gen, Err: err}
		}

		prompt := fmt.Sprintf(
			"Explain this JumpCloud security event in plain English. "+
				"What happened, who did it, and is it concerning?\n\nEvent:\n%s",
			string(e.data),
		)

		result, err := client.Translate(prompt, 1)
		if err != nil {
			return ExplainResultMsg{Generation: gen, Err: err}
		}

		return ExplainResultMsg{
			Generation:  gen,
			Explanation: result.Explanation,
		}
	})
}

func (e *EventDetailScreen) handleExportKey(msg tea.KeyMsg) tea.Cmd {
	e.exporting = false
	switch msg.String() {
	case "j":
		flash, err := exportSingleToClipboard(e.data)
		if err != nil {
			return func() tea.Msg { return tui.FlashMsg{Text: "Export error: " + err.Error()} }
		}
		return func() tea.Msg { return tui.FlashMsg{Text: flash} }
	case "J":
		flash, err := exportSingleToFile(e.data, "event", "json")
		if err != nil {
			return func() tea.Msg { return tui.FlashMsg{Text: "Export error: " + err.Error()} }
		}
		return func() tea.Msg { return tui.FlashMsg{Text: flash} }
	}
	return nil
}

func (e *EventDetailScreen) renderContent() string {
	var sb strings.Builder

	// Render key-value fields.
	sb.WriteString(e.renderFields())

	// Render AI explanation section.
	if e.explaining {
		sb.WriteString("\n")
		sb.WriteString(style.SectionHeader.Render("AI Explanation"))
		sb.WriteString("\n")
		sb.WriteString(e.spinner.View())
		sb.WriteString(" Analyzing event...")
		sb.WriteString("\n")
	} else if e.explainErr != "" {
		sb.WriteString("\n")
		sb.WriteString(style.SectionHeader.Render("AI Explanation"))
		sb.WriteString("\n")
		sb.WriteString(style.Error.Render("Error: " + e.explainErr))
		sb.WriteString("\n")
	} else if e.explanation != "" {
		sb.WriteString("\n")
		sb.WriteString(style.SectionHeader.Render("AI Explanation"))
		sb.WriteString("\n")
		sb.WriteString(style.FieldValue.Render(e.explanation))
		sb.WriteString("\n")
	}

	return sb.String()
}

func (e *EventDetailScreen) renderFields() string {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(e.data, &obj); err != nil {
		return style.Error.Render("Failed to parse event data")
	}

	var fieldNames []string
	for k := range obj {
		fieldNames = append(fieldNames, k)
	}
	sort.Strings(fieldNames)

	var sb strings.Builder
	maxKeyLen := 0
	for _, k := range fieldNames {
		if len(k) > maxKeyLen {
			maxKeyLen = len(k)
		}
	}

	for _, k := range fieldNames {
		v := obj[k]
		keyStr := style.FieldKey.Render(fmt.Sprintf("%-*s", maxKeyLen, k))
		val := formatDetailValue(v)
		sb.WriteString(keyStr + "  " + style.FieldValue.Render(val) + "\n")
	}

	return sb.String()
}

func (e *EventDetailScreen) View() string {
	var sb strings.Builder

	// Title header.
	eventType := component.ExtractName(e.data, "event_type")
	timestamp := component.ExtractName(e.data, "timestamp")
	title := "Event Detail"
	if eventType != "" {
		title = eventType
	}
	if timestamp != "" {
		title += " / " + timestamp
	}
	sb.WriteString(style.Subtitle.Render(title))
	sb.WriteString("\n\n")

	if e.ready {
		sb.WriteString(e.viewport.View())
	} else {
		sb.WriteString(e.renderContent())
	}

	sb.WriteString("\n")
	if e.exporting {
		sb.WriteString(style.Help.Render("Export: [j]son clipboard  [J]son file  [esc] cancel"))
	} else {
		sb.WriteString(style.Help.Render("esc:back  c:copy  e:export  x:explain  j/k:scroll"))
	}
	sb.WriteString("\n")

	return sb.String()
}
