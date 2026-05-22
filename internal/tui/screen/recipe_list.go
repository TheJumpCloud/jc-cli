package screen

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/klaassen-consulting/jc/internal/recipe"
	"github.com/klaassen-consulting/jc/internal/tui"
	"github.com/klaassen-consulting/jc/internal/tui/style"
)

// recipeLoader loads the set of available recipes. Overridable for tests.
var recipeLoader = func() ([]*recipe.Recipe, error) {
	return recipe.LoadAll()
}

// builtInLoader is used to distinguish user vs built-in recipes in the list.
// Overridable for tests.
var builtInLoader = func() ([]*recipe.Recipe, error) {
	return recipe.LoadBuiltIn()
}

// RecipeListScreen displays the merged built-in + user recipe catalog.
type RecipeListScreen struct {
	all         []*recipe.Recipe
	filtered    []*recipe.Recipe
	userRecipes map[string]bool // name → true for user-authored
	cursor      int
	filter      textinput.Model
	filtering   bool
	width       int
	height      int
	err         string
	loaded      bool

	// confirmPrompt is non-empty when the screen is asking the operator
	// to confirm a one-off action (currently: "save builtin X as user
	// copy before editing?"). y/Y triggers confirmAction; anything else
	// dismisses without running it. Inline state instead of a sub-screen
	// keeps the `e` flow on builtins to a single keypress.
	confirmPrompt string
	confirmAction func() (tea.Model, tea.Cmd)
}

// NewRecipeListScreen creates a recipe list screen.
func NewRecipeListScreen() *RecipeListScreen {
	ti := textinput.New()
	ti.Placeholder = "Type to filter recipes..."
	ti.CharLimit = 64

	return &RecipeListScreen{
		filter: ti,
	}
}

func (s *RecipeListScreen) Title() string { return "Recipes" }

func (s *RecipeListScreen) TextInputActive() bool { return s.filtering }

func (s *RecipeListScreen) Init() tea.Cmd {
	s.loadRecipes()
	return nil
}

// loadRecipes pulls the full catalog and marks which entries are user-authored.
func (s *RecipeListScreen) loadRecipes() {
	all, err := recipeLoader()
	if err != nil {
		s.err = err.Error()
		return
	}
	builtin, _ := builtInLoader()

	builtInNames := make(map[string]bool, len(builtin))
	for _, r := range builtin {
		builtInNames[r.Name] = true
	}

	// A recipe is user-authored when it either is not built-in, or is a
	// built-in overridden by a user file. LoadAll doesn't preserve source
	// info, so we infer: names that appear in user dir (== not-builtin OR
	// override) are treated as user. For the override case, we conservatively
	// treat overrides as user-authored to enable edit/delete semantics.
	userDir, _ := recipe.LoadFromDir(recipe.RecipesDir())
	userNames := make(map[string]bool, len(userDir))
	for _, r := range userDir {
		userNames[r.Name] = true
	}

	s.all = all
	s.userRecipes = make(map[string]bool, len(all))
	for _, r := range all {
		s.userRecipes[r.Name] = userNames[r.Name] || !builtInNames[r.Name]
	}
	s.applyFilter()
	s.loaded = true
}

// applyFilter narrows s.filtered based on the current filter input.
func (s *RecipeListScreen) applyFilter() {
	q := strings.ToLower(strings.TrimSpace(s.filter.Value()))
	if q == "" {
		s.filtered = s.all
		s.clampCursor()
		return
	}
	s.filtered = nil
	for _, r := range s.all {
		hay := strings.ToLower(r.Name + " " + r.Description + " " + strings.Join(r.Tags, " "))
		if strings.Contains(hay, q) {
			s.filtered = append(s.filtered, r)
		}
	}
	sort.Slice(s.filtered, func(i, j int) bool { return s.filtered[i].Name < s.filtered[j].Name })
	s.clampCursor()
}

func (s *RecipeListScreen) clampCursor() {
	if s.cursor >= len(s.filtered) {
		s.cursor = len(s.filtered) - 1
	}
	if s.cursor < 0 {
		s.cursor = 0
	}
}

func (s *RecipeListScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.width = msg.Width
		s.height = msg.Height
		return s, nil

	case editorFinishedMsg:
		return s.handleEditorFinished(msg)

	case tea.KeyMsg:
		if s.confirmPrompt != "" {
			return s.updateConfirmMode(msg)
		}
		if s.filtering {
			return s.updateFilterMode(msg)
		}
		return s.updateBrowseMode(msg)
	}
	return s, nil
}

// updateConfirmMode handles the y/n response to a confirmation prompt.
// y/Y runs the deferred action; everything else dismisses without action.
func (s *RecipeListScreen) updateConfirmMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		action := s.confirmAction
		s.confirmPrompt = ""
		s.confirmAction = nil
		if action != nil {
			return action()
		}
		return s, nil
	default:
		s.confirmPrompt = ""
		s.confirmAction = nil
		return s, nil
	}
}

// handleEditorFinished re-validates the edited file, reloads the
// catalog so the list reflects the new recipe (or the now-valid edits),
// and surfaces parse/validation errors as a flash. The file is left on
// disk in both success and failure paths — the operator's work is never
// deleted just because they introduced a typo.
func (s *RecipeListScreen) handleEditorFinished(msg editorFinishedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		return s, func() tea.Msg {
			return tui.FlashMsg{Text: fmt.Sprintf("Editor exited with error: %v", msg.err)}
		}
	}
	flashText := validateEditedFile(msg.path)
	if flashText == "" {
		flashText = fmt.Sprintf("Saved %s", filepath.Base(msg.path))
	}
	s.loadRecipes()
	return s, func() tea.Msg { return tui.FlashMsg{Text: flashText} }
}

func (s *RecipeListScreen) updateFilterMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		s.filtering = false
		s.filter.Blur()
		s.filter.SetValue("")
		s.applyFilter()
		return s, nil
	case "enter":
		s.filtering = false
		s.filter.Blur()
		return s.openSelected()
	case "up", "ctrl+p":
		if s.cursor > 0 {
			s.cursor--
		}
		return s, nil
	case "down", "ctrl+n":
		if s.cursor < len(s.filtered)-1 {
			s.cursor++
		}
		return s, nil
	}
	var cmd tea.Cmd
	s.filter, cmd = s.filter.Update(msg)
	s.applyFilter()
	return s, cmd
}

func (s *RecipeListScreen) updateBrowseMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		return s.openSelected()
	case "esc":
		// Note: "q" is not handled here because the app-level GlobalKeyMap.Quit
		// intercepts single-key "q" before screens see it (TextInputActive() is
		// false in browse mode). Users quit with "q" globally and navigate back
		// with "esc".
		return s, func() tea.Msg { return tui.PopScreenMsg{} }
	case "j", "down":
		if s.cursor < len(s.filtered)-1 {
			s.cursor++
		}
	case "k", "up":
		if s.cursor > 0 {
			s.cursor--
		}
	case "g":
		s.cursor = 0
	case "G":
		s.cursor = len(s.filtered) - 1
		s.clampCursor()
	case "/":
		s.filtering = true
		return s, s.filter.Focus()
	case "r":
		s.loadRecipes()
		return s, func() tea.Msg { return tui.FlashMsg{Text: "Recipes reloaded"} }
	case "n":
		return s.startNewRecipe()
	case "e":
		return s.editSelected()
	}
	return s, nil
}

// startNewRecipe writes the starter template to RecipesDir() under a
// unique untitled-*.yaml filename and hands the file to $EDITOR. After
// the editor returns, handleEditorFinished re-parses, validates, and
// reloads the catalog so the new recipe appears in the list.
func (s *RecipeListScreen) startNewRecipe() (tea.Model, tea.Cmd) {
	path, err := writeStarterRecipe()
	if err != nil {
		return s, func() tea.Msg {
			return tui.FlashMsg{Text: fmt.Sprintf("Could not create recipe: %v", err)}
		}
	}
	return s, execEditor(path)
}

// editSelected opens the currently-highlighted recipe in $EDITOR. For
// user-authored recipes it edits the file in place; for built-ins, we
// can't edit the embedded source, so prompt to save a user copy first
// — the operator's choice keeps the path explicit instead of silently
// shadowing a builtin.
func (s *RecipeListScreen) editSelected() (tea.Model, tea.Cmd) {
	if s.cursor < 0 || s.cursor >= len(s.filtered) {
		return s, nil
	}
	selected := s.filtered[s.cursor]
	if s.userRecipes[selected.Name] {
		path := userRecipeFile(selected.Name)
		if path == "" {
			return s, func() tea.Msg {
				return tui.FlashMsg{Text: fmt.Sprintf("Could not locate %q in %s", selected.Name, recipe.RecipesDir())}
			}
		}
		return s, execEditor(path)
	}
	// Builtin: queue a save-as-copy confirmation. The deferred action
	// runs MarshalYAML → write → edit when the operator presses 'y'.
	s.confirmPrompt = fmt.Sprintf("Save %q as user copy before editing? [y/N]", selected.Name)
	s.confirmAction = func() (tea.Model, tea.Cmd) {
		path, err := saveBuiltinAsUserCopy(selected)
		if err != nil {
			return s, func() tea.Msg {
				return tui.FlashMsg{Text: fmt.Sprintf("Could not save user copy: %v", err)}
			}
		}
		return s, execEditor(path)
	}
	return s, nil
}

// openSelected pushes the param-form screen for the currently selected recipe.
func (s *RecipeListScreen) openSelected() (tea.Model, tea.Cmd) {
	if s.cursor < 0 || s.cursor >= len(s.filtered) {
		return s, nil
	}
	selected := s.filtered[s.cursor]
	return s, func() tea.Msg {
		return tui.PushScreenMsg{Screen: NewRecipeParamFormScreen(selected)}
	}
}

func (s *RecipeListScreen) View() string {
	var sb strings.Builder
	sb.WriteString(style.Title.Render("Recipes"))
	sb.WriteString("\n")

	if s.err != "" {
		sb.WriteString(style.Error.Render("Error: " + s.err))
		sb.WriteString("\n")
		return sb.String()
	}

	if s.confirmPrompt != "" {
		sb.WriteString(style.FilterInput.Render(s.confirmPrompt))
		sb.WriteString("\n")
	} else if s.filtering {
		s.filter.Width = s.width - 4
		sb.WriteString(style.FilterInput.Render(s.filter.View()))
		sb.WriteString("\n")
	}

	if !s.loaded {
		sb.WriteString(style.DimRow.Render("Loading..."))
		return sb.String()
	}

	if len(s.filtered) == 0 {
		if s.filter.Value() != "" {
			sb.WriteString(style.DimRow.Render("  No matching recipes"))
		} else {
			sb.WriteString(style.DimRow.Render("  No recipes available. Create user recipes in ~/.config/jc/recipes/"))
		}
		return sb.String()
	}

	// Determine how many rows we can show.
	maxRows := s.height - 6
	if maxRows < 3 {
		maxRows = 3
	}
	// Scroll window so cursor stays visible.
	offset := 0
	if s.cursor >= maxRows {
		offset = s.cursor - maxRows + 1
	}
	end := offset + maxRows
	if end > len(s.filtered) {
		end = len(s.filtered)
	}

	for i := offset; i < end; i++ {
		r := s.filtered[i]
		sb.WriteString(s.renderRow(r, i == s.cursor))
		sb.WriteString("\n")
	}

	if end < len(s.filtered) {
		sb.WriteString(style.DimRow.Render(fmt.Sprintf("  … %d more", len(s.filtered)-end)))
		sb.WriteString("\n")
	}

	return sb.String()
}

// renderRow renders one recipe row: name (selected highlight), a compact
// metadata strip, and a truncated description on the next line.
func (s *RecipeListScreen) renderRow(r *recipe.Recipe, selected bool) string {
	source := "builtin"
	if s.userRecipes[r.Name] {
		source = "user"
	}
	paramCount := len(r.Parameters)
	stepCount := len(r.Steps)

	prefix := "  "
	nameStyle := style.ResourceName
	if selected {
		prefix = "> "
		nameStyle = style.SelectedRow
	}

	tagsStr := ""
	if len(r.Tags) > 0 {
		tagsStr = "  [" + strings.Join(r.Tags, ",") + "]"
	}

	head := nameStyle.Render(fmt.Sprintf("%s%s", prefix, r.Name)) +
		style.ResourceVerbs.Render(fmt.Sprintf("  (%s, %d params, %d steps)%s", source, paramCount, stepCount, tagsStr))

	desc := ""
	if r.Description != "" {
		maxLen := s.width - 4
		if maxLen < 20 {
			maxLen = 20
		}
		d := r.Description
		if len(d) > maxLen {
			d = d[:maxLen-1] + "~"
		}
		desc = "\n    " + style.DimRow.Render(d)
	}
	return head + desc
}
