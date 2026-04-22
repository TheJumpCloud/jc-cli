package screen

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/klaassen-consulting/jc/internal/recipe"
	"github.com/klaassen-consulting/jc/internal/tui"
	"github.com/klaassen-consulting/jc/internal/tui/style"
)

// recipeParamField holds the editable state for one recipe parameter.
type recipeParamField struct {
	def   recipe.Parameter
	input textinput.Model
}

// RecipeParamFormScreen is the wizard-style parameter collector for a recipe.
// On submit, it validates via recipe.ResolveParams and pushes the run screen.
type RecipeParamFormScreen struct {
	recipe   *recipe.Recipe
	fields   []recipeParamField
	focused  int
	planMode bool
	err      string
	width    int
	height   int
}

// NewRecipeParamFormScreen creates the param form for a recipe.
func NewRecipeParamFormScreen(r *recipe.Recipe) *RecipeParamFormScreen {
	s := &RecipeParamFormScreen{recipe: r}

	for _, p := range r.Parameters {
		ti := textinput.New()
		ti.CharLimit = 256
		if p.Default != "" {
			ti.SetValue(p.Default)
		}
		ti.Placeholder = p.Description
		s.fields = append(s.fields, recipeParamField{def: p, input: ti})
	}

	if len(s.fields) > 0 {
		s.fields[0].input.Focus()
	}
	return s
}

func (s *RecipeParamFormScreen) Title() string {
	return "Configure: " + s.recipe.Name
}

func (s *RecipeParamFormScreen) TextInputActive() bool {
	return len(s.fields) > 0
}

func (s *RecipeParamFormScreen) Init() tea.Cmd { return nil }

// focusField sets focus on fields[i], blurring all others.
func (s *RecipeParamFormScreen) focusField(i int) {
	for j := range s.fields {
		if j == i {
			s.fields[j].input.Focus()
		} else {
			s.fields[j].input.Blur()
		}
	}
	s.focused = i
}

func (s *RecipeParamFormScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.width = msg.Width
		s.height = msg.Height
		return s, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return s, func() tea.Msg { return tui.PopScreenMsg{} }
		case "tab", "down":
			if len(s.fields) > 0 {
				s.focusField((s.focused + 1) % len(s.fields))
			}
			return s, nil
		case "shift+tab", "up":
			if len(s.fields) > 0 {
				s.focusField((s.focused - 1 + len(s.fields)) % len(s.fields))
			}
			return s, nil
		case "ctrl+p":
			s.planMode = !s.planMode
			return s, nil
		case "enter":
			return s.submit()
		}
	}

	// Forward to the focused text input.
	if len(s.fields) > 0 {
		var cmd tea.Cmd
		s.fields[s.focused].input, cmd = s.fields[s.focused].input.Update(msg)
		return s, cmd
	}
	return s, nil
}

// submit validates input and pushes the run screen (or plan preview).
func (s *RecipeParamFormScreen) submit() (tea.Model, tea.Cmd) {
	params := make(map[string]string, len(s.fields))
	for _, f := range s.fields {
		v := strings.TrimSpace(f.input.Value())
		if v == "" {
			continue
		}
		// Validate type coercion before accepting the value.
		switch f.def.Type {
		case "int":
			if _, err := strconv.Atoi(v); err != nil {
				s.err = fmt.Sprintf("parameter %q must be an integer, got %q", f.def.Name, v)
				return s, nil
			}
		case "bool":
			switch strings.ToLower(v) {
			case "true", "false", "1", "0", "yes", "no":
				// acceptable
			default:
				s.err = fmt.Sprintf("parameter %q must be a boolean (true/false), got %q", f.def.Name, v)
				return s, nil
			}
		}
		params[f.def.Name] = v
	}

	// ResolveParams applies defaults and checks required fields.
	if _, err := s.recipe.ResolveParams(params); err != nil {
		s.err = err.Error()
		return s, nil
	}
	s.err = ""

	r := s.recipe
	planMode := s.planMode
	return s, func() tea.Msg {
		return tui.PushScreenMsg{Screen: NewRecipeRunScreen(r, params, planMode)}
	}
}

func (s *RecipeParamFormScreen) View() string {
	var sb strings.Builder
	sb.WriteString(style.Title.Render("Configure: " + s.recipe.Name))
	sb.WriteString("\n")

	if s.recipe.Description != "" {
		sb.WriteString(style.DimRow.Render(s.recipe.Description))
		sb.WriteString("\n\n")
	}

	// Parameter fields.
	if len(s.fields) == 0 {
		sb.WriteString(style.DimRow.Render("  (no parameters)"))
		sb.WriteString("\n")
	} else {
		for i, f := range s.fields {
			label := f.def.Name
			if f.def.Required {
				label += " *"
			}
			if f.def.Type != "" && f.def.Type != "string" {
				label += fmt.Sprintf(" (%s)", f.def.Type)
			}

			labelStyle := style.ResourceName
			if i == s.focused {
				labelStyle = style.SelectedRow
			}
			sb.WriteString(labelStyle.Render("  " + label))
			sb.WriteString("\n    ")
			sb.WriteString(f.input.View())
			if f.def.Description != "" {
				sb.WriteString("\n    ")
				sb.WriteString(style.DimRow.Render(f.def.Description))
			}
			sb.WriteString("\n")
		}
	}

	// Step preview.
	if len(s.recipe.Steps) > 0 {
		sb.WriteString("\n")
		sb.WriteString(style.Category.Render("Steps preview"))
		sb.WriteString("\n")
		for i, st := range s.recipe.Steps {
			marker := " "
			if st.When != "" {
				marker = "?"
			}
			sb.WriteString(style.DimRow.Render(fmt.Sprintf("  %d.%s %s", i+1, marker, st.Name)))
			sb.WriteString("\n")
		}
	}

	if s.planMode {
		sb.WriteString("\n")
		sb.WriteString(style.FilterChip.Render("plan mode"))
		sb.WriteString("\n")
	}

	if s.err != "" {
		sb.WriteString("\n")
		sb.WriteString(style.Error.Render("  " + s.err))
		sb.WriteString("\n")
	}

	sb.WriteString("\n")
	sb.WriteString(style.DimRow.Render("  tab: next  enter: run  ctrl+p: toggle plan  esc: back"))

	return sb.String()
}
