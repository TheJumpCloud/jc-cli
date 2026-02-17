package screen

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/klaassen-consulting/jc/internal/schema"
	"github.com/klaassen-consulting/jc/internal/tui"
	"github.com/klaassen-consulting/jc/internal/tui/fetch"
	"github.com/klaassen-consulting/jc/internal/tui/style"
)

// formField holds the state for a single editable field.
type formField struct {
	def      schema.FieldDef
	input    textinput.Model
	boolVal  bool
	original string // for edit change detection
}

// FormScreen provides a create/edit form for a resource.
type FormScreen struct {
	entry      tui.ResourceEntry
	mode       string // "create" or "edit"
	editID     string // populated in edit mode
	fields     []formField
	focusIdx   int
	fetcher    *fetch.Fetcher
	generation int64
	submitting bool
	err        string
	width      int
	height     int
	spinner    spinner.Model
}

// NewFormScreen creates a form screen.
// data is nil for create mode; for edit mode it contains the current resource JSON.
func NewFormScreen(entry tui.ResourceEntry, mode string, data json.RawMessage) *FormScreen {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = style.Spinner

	// Parse existing data for edit mode.
	var obj map[string]json.RawMessage
	if data != nil {
		_ = json.Unmarshal(data, &obj)
	}

	var editID string
	if mode == "edit" && obj != nil {
		if raw, ok := obj[entry.Schema.IDField]; ok {
			var id string
			if json.Unmarshal(raw, &id) == nil {
				editID = id
			}
		}
	}

	var fields []formField
	for _, fd := range entry.Schema.Fields {
		// Skip the ID field — it's not user-editable.
		if fd.Name == entry.Schema.IDField {
			continue
		}
		// Skip complex types that can't be edited as simple text.
		if fd.Type == "array" || fd.Type == "object" {
			continue
		}

		ti := textinput.New()
		ti.Placeholder = fd.Description
		ti.CharLimit = 256

		ff := formField{
			def:   fd,
			input: ti,
		}

		// Pre-populate for edit mode.
		if obj != nil {
			if raw, ok := obj[fd.Name]; ok && string(raw) != "null" {
				switch fd.Type {
				case "bool":
					var b bool
					if json.Unmarshal(raw, &b) == nil {
						ff.boolVal = b
						if b {
							ff.original = "true"
						} else {
							ff.original = "false"
						}
					}
				default:
					val := extractStringValue(raw)
					ff.input.SetValue(val)
					ff.original = val
				}
			}
		}

		fields = append(fields, ff)
	}

	// Focus the first field.
	if len(fields) > 0 && fields[0].def.Type != "bool" {
		fields[0].input.Focus()
	}

	return &FormScreen{
		entry:   entry,
		mode:    mode,
		editID:  editID,
		fields:  fields,
		fetcher: fetch.NewFetcher(),
		spinner: s,
	}
}

// extractStringValue converts a json.RawMessage to a display string.
func extractStringValue(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	// Try number.
	var n json.Number
	if json.Unmarshal(raw, &n) == nil {
		return n.String()
	}
	// Try bool (for non-bool fields that happen to hold booleans).
	var b bool
	if json.Unmarshal(raw, &b) == nil {
		return strconv.FormatBool(b)
	}
	return strings.Trim(string(raw), `"`)
}

// TextInputActive reports whether the form has active text input,
// so the app skips single-key shortcuts (q, ?) that conflict with typing.
func (f *FormScreen) TextInputActive() bool {
	return !f.submitting
}

// SetFetcher allows injecting a custom fetcher (for tests).
func (f *FormScreen) SetFetcher(ft *fetch.Fetcher) {
	f.fetcher = ft
}

func (f *FormScreen) Title() string {
	if f.mode == "edit" {
		return "Edit " + f.entry.DisplayName
	}
	return "New " + f.entry.DisplayName
}

func (f *FormScreen) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, f.spinner.Tick)
}

func (f *FormScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		f.width = msg.Width
		f.height = msg.Height
		f.updateFieldWidths()
		return f, nil

	case fetch.MutationResultMsg:
		if msg.Generation != f.generation {
			return f, nil
		}
		f.submitting = false
		if msg.Err != nil {
			f.err = msg.Err.Error()
			return f, nil
		}
		action := "Created"
		if f.mode == "edit" {
			action = "Updated"
		}
		return f, tea.Batch(
			func() tea.Msg { return tui.FlashMsg{Text: action + " " + f.entry.DisplayName} },
			func() tea.Msg { return tui.PopScreenMsg{} },
			func() tea.Msg { return tui.RefreshListMsg{} },
		)

	case spinner.TickMsg:
		if f.submitting {
			var cmd tea.Cmd
			f.spinner, cmd = f.spinner.Update(msg)
			return f, cmd
		}
		return f, nil

	case tea.KeyMsg:
		if f.submitting {
			return f, nil
		}

		switch msg.String() {
		case "ctrl+s":
			return f, f.submit()

		case "esc":
			return f, func() tea.Msg { return tui.PopScreenMsg{} }

		case "up":
			f.moveFocus(-1)
			return f, nil

		case "k":
			// 'k' navigates unless active field is a text input (non-bool).
			if len(f.fields) > 0 && f.fields[f.focusIdx].def.Type != "bool" {
				var cmd tea.Cmd
				f.fields[f.focusIdx].input, cmd = f.fields[f.focusIdx].input.Update(msg)
				return f, cmd
			}
			f.moveFocus(-1)
			return f, nil

		case "down", "tab":
			f.moveFocus(1)
			return f, nil

		case "j":
			// 'j' navigates unless active field is a text input (non-bool).
			if len(f.fields) > 0 && f.fields[f.focusIdx].def.Type != "bool" {
				var cmd tea.Cmd
				f.fields[f.focusIdx].input, cmd = f.fields[f.focusIdx].input.Update(msg)
				return f, cmd
			}
			f.moveFocus(1)
			return f, nil

		case "enter":
			f.moveFocus(1)
			return f, nil

		case "h", "l", "left", "right", " ":
			if len(f.fields) > 0 && f.fields[f.focusIdx].def.Type == "bool" {
				f.fields[f.focusIdx].boolVal = !f.fields[f.focusIdx].boolVal
				return f, nil
			}
			// Delegate to text input for non-bool fields.
			var cmd tea.Cmd
			f.fields[f.focusIdx].input, cmd = f.fields[f.focusIdx].input.Update(msg)
			return f, cmd

		default:
			if len(f.fields) > 0 && f.fields[f.focusIdx].def.Type != "bool" {
				var cmd tea.Cmd
				f.fields[f.focusIdx].input, cmd = f.fields[f.focusIdx].input.Update(msg)
				return f, cmd
			}
		}
	}

	return f, nil
}

// updateFieldWidths recalculates textinput widths based on terminal width.
func (f *FormScreen) updateFieldWidths() {
	for i, ff := range f.fields {
		if ff.def.Type == "bool" {
			continue
		}
		w := f.width/2 - len(ff.def.Name) - 6
		if w < 20 {
			w = 20
		}
		f.fields[i].input.Width = w
	}
}

// moveFocus moves the field focus by delta and updates input focus state.
func (f *FormScreen) moveFocus(delta int) {
	if len(f.fields) == 0 {
		return
	}

	// Blur current field.
	f.fields[f.focusIdx].input.Blur()

	f.focusIdx += delta
	if f.focusIdx < 0 {
		f.focusIdx = len(f.fields) - 1
	}
	if f.focusIdx >= len(f.fields) {
		f.focusIdx = 0
	}

	// Focus new field if it's a text input.
	if f.fields[f.focusIdx].def.Type != "bool" {
		f.fields[f.focusIdx].input.Focus()
	}
}

// submit validates the form and dispatches the create/update request.
func (f *FormScreen) submit() tea.Cmd {
	// Validate required fields.
	for _, ff := range f.fields {
		if !ff.def.Required {
			continue
		}
		val := ff.input.Value()
		if ff.def.Type == "bool" {
			continue // bools always have a value
		}
		if strings.TrimSpace(val) == "" {
			f.err = fmt.Sprintf("Required field %q cannot be empty", ff.def.Name)
			return nil
		}
	}

	// Build body with only relevant fields.
	body := make(map[string]any)
	for _, ff := range f.fields {
		switch ff.def.Type {
		case "bool":
			currentVal := strconv.FormatBool(ff.boolVal)
			if f.mode == "edit" && currentVal == ff.original {
				continue
			}
			if f.mode == "create" {
				body[ff.def.Name] = ff.boolVal
			} else {
				body[ff.def.Name] = ff.boolVal
			}

		case "int":
			val := strings.TrimSpace(ff.input.Value())
			if f.mode == "edit" {
				if val == ff.original {
					continue
				}
			}
			if val == "" {
				continue
			}
			n, err := strconv.Atoi(val)
			if err != nil {
				f.err = fmt.Sprintf("Field %q must be a number", ff.def.Name)
				return nil
			}
			body[ff.def.Name] = n

		default: // string, datetime, etc.
			val := strings.TrimSpace(ff.input.Value())
			if f.mode == "edit" {
				if val == ff.original {
					continue
				}
			}
			if val == "" {
				continue
			}
			body[ff.def.Name] = val
		}
	}

	f.submitting = true
	f.err = ""
	f.generation = fetch.NextGeneration()
	gen := f.generation

	switch {
	case f.mode == "create" && f.entry.ClientType == tui.ClientV1:
		return tea.Batch(f.spinner.Tick, f.fetcher.CreateV1(f.entry.Key, f.entry.ListEndpoint, body, gen))
	case f.mode == "create" && f.entry.ClientType == tui.ClientV2:
		return tea.Batch(f.spinner.Tick, f.fetcher.CreateV2(f.entry.Key, f.entry.ListEndpoint, body, gen))
	case f.mode == "edit" && f.entry.ClientType == tui.ClientV1:
		return tea.Batch(f.spinner.Tick, f.fetcher.UpdateV1(f.entry.Key, f.entry.ListEndpoint, f.editID, body, gen))
	case f.mode == "edit" && f.entry.ClientType == tui.ClientV2:
		return tea.Batch(f.spinner.Tick, f.fetcher.UpdateV2(f.entry.Key, f.entry.ListEndpoint, f.editID, body, gen))
	default:
		f.submitting = false
		f.err = "unsupported client type"
		return nil
	}
}

func (f *FormScreen) View() string {
	var sb strings.Builder

	sb.WriteString(style.Subtitle.Render(f.Title()))
	sb.WriteString("\n\n")

	if f.submitting {
		sb.WriteString(f.spinner.View())
		sb.WriteString(" Saving...")
		sb.WriteString("\n")
		return sb.String()
	}

	if f.err != "" {
		sb.WriteString(style.Error.Render("Error: " + f.err))
		sb.WriteString("\n\n")
	}

	for i, ff := range f.fields {
		label := ff.def.Name
		if ff.def.Required {
			label += " *"
		}

		focused := i == f.focusIdx

		if ff.def.Type == "bool" {
			indicator := "  "
			if focused {
				indicator = "> "
			}
			boolStr := "false"
			if ff.boolVal {
				boolStr = "true"
			}
			if focused {
				sb.WriteString(indicator + style.FieldKey.Render(label) + "  " + style.FilterChip.Render("["+boolStr+"]"))
			} else {
				sb.WriteString(indicator + style.FieldKey.Render(label) + "  " + style.FieldValue.Render("["+boolStr+"]"))
			}
		} else {
			indicator := "  "
			if focused {
				indicator = "> "
			}
			sb.WriteString(indicator + style.FieldKey.Render(label) + "  " + f.fields[i].input.View())
		}
		sb.WriteString("\n")
	}

	sb.WriteString("\n")
	sb.WriteString(style.Help.Render("ctrl+s: save  esc: cancel  j/k: navigate  h/l/←/→/space: toggle bool"))
	sb.WriteString("\n")

	return sb.String()
}

// hasVerb checks whether a verb slice contains a specific verb.
func hasVerb(verbs []string, verb string) bool {
	for _, v := range verbs {
		if v == verb {
			return true
		}
	}
	return false
}
