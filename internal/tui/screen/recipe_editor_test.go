package screen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/klaassen-consulting/jc/internal/recipe"
	"github.com/klaassen-consulting/jc/internal/tui"
)

// withTempRecipesDir points recipe.RecipesDir at a per-test tmpdir so
// edit-flow tests don't touch the operator's real ~/.config/jc/recipes.
func withTempRecipesDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	orig := recipe.RecipesDir
	recipe.RecipesDir = func() string { return dir }
	t.Cleanup(func() { recipe.RecipesDir = orig })
	return dir
}

// withFakeEditor installs an execEditor that mutates the file before
// completing — letting tests drive the after-edit path without actually
// suspending bubbletea or invoking an external program.
func withFakeEditor(t *testing.T, mutate func(path string)) {
	t.Helper()
	orig := execEditor
	execEditor = func(path string) tea.Cmd {
		return func() tea.Msg {
			if mutate != nil {
				mutate(path)
			}
			return editorFinishedMsg{path: path}
		}
	}
	t.Cleanup(func() { execEditor = orig })
}

func TestSlugForFilename(t *testing.T) {
	cases := []struct{ in, want string }{
		{"simple", "simple"},
		{"Mixed Case", "mixed-case"},
		{"with.dots and/slashes", "with-dots-and-slashes"},
		{"hyphenated-name", "hyphenated-name"},
		{"___underscores___", "underscores"},
		{"!!!", "untitled"},
		{"", "untitled"},
		{"  spaces  ", "spaces"},
		{"123-numeric", "123-numeric"},
	}
	for _, tc := range cases {
		got := slugForFilename(tc.in)
		if got != tc.want {
			t.Errorf("slugForFilename(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestUniquePath_AppendsSuffixOnCollision(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "rec.yaml")

	// No collision → returns the original path.
	if got := uniquePath(base); got != base {
		t.Errorf("uniquePath(non-existent) = %q, want %q", got, base)
	}

	// First collision → -2 suffix.
	if err := os.WriteFile(base, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	first := uniquePath(base)
	want := filepath.Join(dir, "rec-2.yaml")
	if first != want {
		t.Errorf("first collision = %q, want %q", first, want)
	}

	// Second collision → -3 suffix.
	if err := os.WriteFile(first, []byte("y"), 0o600); err != nil {
		t.Fatal(err)
	}
	second := uniquePath(base)
	want = filepath.Join(dir, "rec-3.yaml")
	if second != want {
		t.Errorf("second collision = %q, want %q", second, want)
	}
}

func TestWriteStarterRecipe_ProducesParseableSkeleton(t *testing.T) {
	dir := withTempRecipesDir(t)

	path, err := writeStarterRecipe()
	if err != nil {
		t.Fatalf("writeStarterRecipe: %v", err)
	}
	if !strings.HasPrefix(path, dir) {
		t.Errorf("starter path %q not under RecipesDir %q", path, dir)
	}
	r, err := recipe.ParseFile(path)
	if err != nil {
		t.Fatalf("starter YAML must parse: %v", err)
	}
	if err := r.Validate(); err != nil {
		t.Errorf("starter recipe must validate: %v", err)
	}
}

func TestWriteStarterRecipe_AvoidsClobberingExisting(t *testing.T) {
	withTempRecipesDir(t)

	first, err := writeStarterRecipe()
	if err != nil {
		t.Fatal(err)
	}
	second, err := writeStarterRecipe()
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Errorf("second writeStarterRecipe overwrote the first at %q", first)
	}
}

func TestSaveBuiltinAsUserCopy_WritesValidYAML(t *testing.T) {
	dir := withTempRecipesDir(t)
	r := &recipe.Recipe{
		Name:        "Audit MFA",
		Description: "Check MFA coverage",
		Steps:       []recipe.Step{{Name: "list", Command: "jc users list --filter mfa:eq:false"}},
	}

	path, err := saveBuiltinAsUserCopy(r)
	if err != nil {
		t.Fatalf("saveBuiltinAsUserCopy: %v", err)
	}
	if filepath.Dir(path) != dir {
		t.Errorf("copy written to %q, want under %q", path, dir)
	}
	if !strings.HasSuffix(path, "audit-mfa.yaml") {
		t.Errorf("copy filename = %q, want slugified", path)
	}
	loaded, err := recipe.ParseFile(path)
	if err != nil {
		t.Fatalf("user copy must parse: %v", err)
	}
	if loaded.Name != "Audit MFA" {
		t.Errorf("loaded.Name = %q, want %q", loaded.Name, "Audit MFA")
	}
}

func TestValidateEditedFile(t *testing.T) {
	dir := t.TempDir()

	// Valid recipe → empty string (no error message).
	good := filepath.Join(dir, "good.yaml")
	if err := os.WriteFile(good, []byte("name: ok\nsteps:\n  - name: s\n    command: jc users list\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if msg := validateEditedFile(good); msg != "" {
		t.Errorf("valid recipe should produce empty message; got %q", msg)
	}

	// Malformed YAML → "YAML parse failed" message.
	bad := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(bad, []byte("name: : :\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if msg := validateEditedFile(bad); !strings.Contains(msg, "parse failed") {
		t.Errorf("malformed YAML should mention parse failure; got %q", msg)
	}

	// Parseable but invalid (no steps) → "recipe is invalid" message.
	invalid := filepath.Join(dir, "invalid.yaml")
	if err := os.WriteFile(invalid, []byte("name: empty\nsteps: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if msg := validateEditedFile(invalid); !strings.Contains(msg, "invalid") {
		t.Errorf("validation failure should mention invalid; got %q", msg)
	}
}

func TestRecipeListScreen_NewKeyOpensEditorOnStarter(t *testing.T) {
	withTempRecipesDir(t)
	withRecipeLoaders(t, sampleRecipes(), sampleRecipes())

	var calledPath string
	withFakeEditor(t, func(path string) { calledPath = path })

	s := NewRecipeListScreen()
	s.Init()
	s.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if cmd == nil {
		t.Fatal("n should produce a command")
	}
	msg := cmd()
	if _, ok := msg.(editorFinishedMsg); !ok {
		t.Fatalf("n should fire editor; cmd returned %T", msg)
	}
	if !strings.HasSuffix(calledPath, ".yaml") {
		t.Errorf("editor invoked with %q, want a .yaml file", calledPath)
	}
}

func TestRecipeListScreen_EditUserRecipeOpensEditor(t *testing.T) {
	dir := withTempRecipesDir(t)

	// Write a user recipe to disk and have the loader return it as user-authored.
	userPath := filepath.Join(dir, "onboard-user.yaml")
	yaml := "name: onboard-user\nsteps:\n  - name: create\n    command: jc users create\n"
	if err := os.WriteFile(userPath, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	user, err := recipe.ParseFile(userPath)
	if err != nil {
		t.Fatal(err)
	}
	withRecipeLoaders(t, []*recipe.Recipe{user}, nil) // empty builtin → user-authored

	var calledPath string
	withFakeEditor(t, func(path string) { calledPath = path })

	s := NewRecipeListScreen()
	s.Init()
	s.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})
	if cmd == nil {
		t.Fatal("e should produce a command on a user recipe")
	}
	msg := cmd()
	if _, ok := msg.(editorFinishedMsg); !ok {
		t.Fatalf("e should fire editor; cmd returned %T", msg)
	}
	if calledPath != userPath {
		t.Errorf("editor invoked with %q, want %q", calledPath, userPath)
	}
}

func TestRecipeListScreen_EditBuiltinPromptsForCopy(t *testing.T) {
	withTempRecipesDir(t)
	// Same recipe in both 'all' and 'builtin' loaders → flagged builtin.
	all := sampleRecipes()
	withRecipeLoaders(t, all, all)
	withFakeEditor(t, nil)

	s := NewRecipeListScreen()
	s.Init()
	s.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	// `e` on a builtin should set the confirm prompt rather than
	// immediately launching the editor.
	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})
	if cmd != nil {
		t.Errorf("e on builtin should defer editor until confirm; got cmd %T", cmd())
	}
	if s.confirmPrompt == "" {
		t.Error("e on builtin should set a confirm prompt")
	}
	if !strings.Contains(s.confirmPrompt, "user copy") {
		t.Errorf("confirm prompt should mention 'user copy'; got %q", s.confirmPrompt)
	}
}

func TestRecipeListScreen_ConfirmYTriggersCopyAndEdit(t *testing.T) {
	dir := withTempRecipesDir(t)
	all := sampleRecipes()
	withRecipeLoaders(t, all, all)

	var calledPath string
	withFakeEditor(t, func(path string) { calledPath = path })

	s := NewRecipeListScreen()
	s.Init()
	s.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	// Trigger the prompt, then accept.
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})
	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if cmd == nil {
		t.Fatal("y on confirm should launch editor")
	}
	if _, ok := cmd().(editorFinishedMsg); !ok {
		t.Fatalf("y on confirm should fire editor; got %T", cmd())
	}
	if !strings.HasPrefix(calledPath, dir) {
		t.Errorf("editor path %q not under user RecipesDir %q", calledPath, dir)
	}
	if s.confirmPrompt != "" {
		t.Errorf("confirm prompt should clear after y; still %q", s.confirmPrompt)
	}
}

func TestRecipeListScreen_ConfirmNCancelsWithoutEditing(t *testing.T) {
	withTempRecipesDir(t)
	all := sampleRecipes()
	withRecipeLoaders(t, all, all)

	editorFired := false
	withFakeEditor(t, func(string) { editorFired = true })

	s := NewRecipeListScreen()
	s.Init()
	s.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})
	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if cmd != nil {
		// A nil cmd is fine; if non-nil, draining it must NOT fire the editor.
		msg := cmd()
		if _, ok := msg.(editorFinishedMsg); ok {
			t.Errorf("n on confirm should NOT trigger editor; got editorFinishedMsg")
		}
	}
	if editorFired {
		t.Error("n on confirm should leave editor unfired")
	}
	if s.confirmPrompt != "" {
		t.Errorf("confirm prompt should clear after n; still %q", s.confirmPrompt)
	}
}

func TestRecipeListScreen_EditorFinishedReloadsAndFlashes(t *testing.T) {
	dir := withTempRecipesDir(t)

	user := &recipe.Recipe{
		Name:  "before-edit",
		Steps: []recipe.Step{{Name: "s", Command: "jc users list"}},
	}
	withRecipeLoaders(t, []*recipe.Recipe{user}, nil)

	s := NewRecipeListScreen()
	s.Init()
	s.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	// Now swap the loader so the post-edit reload sees a different
	// recipe — simulating the editor having created/renamed something.
	after := &recipe.Recipe{
		Name:  "after-edit",
		Steps: []recipe.Step{{Name: "s", Command: "jc users list"}},
	}
	recipeLoader = func() ([]*recipe.Recipe, error) { return []*recipe.Recipe{after}, nil }

	// Drop a valid file at the path the editor "edited" so
	// validateEditedFile returns the success message.
	finished := editorFinishedMsg{path: filepath.Join(dir, "x.yaml")}
	if err := os.WriteFile(finished.path,
		[]byte("name: ok\nsteps:\n  - name: s\n    command: jc users list\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, cmd := s.Update(finished)
	if cmd == nil {
		t.Fatal("editorFinishedMsg should yield a flash command")
	}
	if _, ok := cmd().(tui.FlashMsg); !ok {
		t.Fatalf("expected FlashMsg, got %T", cmd())
	}
	// Reload must have picked up the new loader's recipe.
	view := s.View()
	if !strings.Contains(view, "after-edit") {
		t.Errorf("view after reload missing 'after-edit'; got:\n%s", view)
	}
}

func TestRecipeListScreen_EditorFinishedSurfacesValidationError(t *testing.T) {
	dir := withTempRecipesDir(t)
	withRecipeLoaders(t, nil, nil)

	s := NewRecipeListScreen()
	s.Init()
	s.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	bad := filepath.Join(dir, "broken.yaml")
	if err := os.WriteFile(bad, []byte("name: : :\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, cmd := s.Update(editorFinishedMsg{path: bad})
	if cmd == nil {
		t.Fatal("expected flash command")
	}
	flash, ok := cmd().(tui.FlashMsg)
	if !ok {
		t.Fatalf("expected FlashMsg, got %T", cmd())
	}
	if !strings.Contains(flash.Text, "parse failed") {
		t.Errorf("flash should describe parse failure; got %q", flash.Text)
	}
}

func TestResolveEditor_VisualBeatsEditor(t *testing.T) {
	t.Setenv("VISUAL", "code --wait")
	t.Setenv("EDITOR", "vi")
	if got := resolveEditor(); got != "code --wait" {
		t.Errorf("resolveEditor() = %q, want %q (VISUAL wins)", got, "code --wait")
	}
}

func TestResolveEditor_EditorWhenNoVisual(t *testing.T) {
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "nano")
	if got := resolveEditor(); got != "nano" {
		t.Errorf("resolveEditor() = %q, want nano", got)
	}
}

func TestResolveEditor_FallsBackToPlatformDefault(t *testing.T) {
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
	got := resolveEditor()
	// Default is "vi" on unix, "notepad" on windows. Both are
	// acceptable — the contract is just "non-empty, predictable".
	if got != "vi" && got != "notepad" {
		t.Errorf("resolveEditor() = %q, want vi or notepad", got)
	}
}
