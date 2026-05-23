package screen

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/klaassen-consulting/jc/internal/recipe"
	"go.yaml.in/yaml/v3"
)

// editorFinishedMsg is delivered after $EDITOR returns. path is the file
// the editor was pointed at; err is non-nil only when the child process
// itself failed to launch or returned a non-zero exit. Parse/validate
// errors live in the post-load handling — this message just signals
// "editor session ended, time to re-check the file."
type editorFinishedMsg struct {
	path string
	err  error
}

// recipeStarterTemplate is the YAML body written to a freshly-created
// recipe file. Intentionally minimal so the user's first edit replaces
// most of it. Uses YAML comments to point at reference material — those
// stick around because we're handing the file off to $EDITOR untouched,
// not round-tripping it through yaml.Marshal.
const recipeStarterTemplate = `# New jc recipe. Edit, save, and exit your editor to validate.
# Reference: built-in recipes embedded in jc (jc tui → Recipes → list).
# Spec: docs/QUICKSTART.md → Recipes.

name: untitled
description: ""
author: ""
version: 0.1.0
tags: []

parameters:
  # - name: username
  #   description: Account to operate on
  #   type: string
  #   required: true

steps:
  - name: example
    command: jc users list --filter "activated:eq:false" --ids
    # when: '{{ .username }}'
    # capture: user_ids
    # continue_on_error: false
`

// resolveEditor picks the command to launch for $EDITOR handoff. Order
// matches the de-facto convention `git commit` uses: $VISUAL beats
// $EDITOR (full-screen editors typically set VISUAL), then a platform
// default. We deliberately don't error when neither var is set — most
// systems have `vi` (or notepad on Windows), and a clear "could not
// launch editor" error from the OS is more actionable than a refusal
// here would be.
//
// editorOverride lets tests inject a known-good binary (e.g. a shell
// script that mutates the file) without touching env vars.
var editorOverride string

func resolveEditor() string {
	if editorOverride != "" {
		return editorOverride
	}
	// TrimSpace on the env-var values: a whitespace-only $VISUAL (e.g.
	// "  ") used to pass the empty check, then strings.Fields would
	// return an empty slice in execEditor and fields[0] panicked. Treat
	// whitespace-only the same as unset so we fall through to the
	// platform default.
	if v := strings.TrimSpace(os.Getenv("VISUAL")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("EDITOR")); v != "" {
		return v
	}
	if runtime.GOOS == "windows" {
		return "notepad"
	}
	return "vi"
}

// execEditor is overridable for tests. The default runs the resolved
// editor against path via tea.ExecProcess, which suspends the bubbletea
// runtime so the editor takes over the controlling terminal.
var execEditor = func(path string) tea.Cmd {
	editor := resolveEditor()
	// Some editors (VS Code's `code -w`, sublime's `subl -w`) need a
	// "wait" flag to block; we honor whatever the user put in $EDITOR
	// verbatim by splitting on whitespace — same convention git uses.
	fields := strings.Fields(editor)
	if len(fields) == 0 {
		// resolveEditor's TrimSpace + platform default should make this
		// unreachable, but belt-and-suspenders: surface an editorFinishedMsg
		// with err set so handleEditorFinished produces a sensible flash
		// instead of panicking the bubbletea program.
		return func() tea.Msg {
			return editorFinishedMsg{path: path, err: fmt.Errorf("no editor configured ($VISUAL/$EDITOR empty)")}
		}
	}
	cmd := exec.Command(fields[0], append(fields[1:], path)...)
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return editorFinishedMsg{path: path, err: err}
	})
}

// userRecipeFile returns the path on disk of a user recipe with the
// given name, or "" if not found. Scans RecipesDir() and parses each
// candidate just enough to match the internal recipe.Name. Necessary
// because recipe.Recipe doesn't carry its source path through Load*.
func userRecipeFile(name string) string {
	dir := recipe.RecipesDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(strings.ToLower(e.Name()), ".yaml") &&
			!strings.HasSuffix(strings.ToLower(e.Name()), ".yml") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		r, err := recipe.ParseFile(path)
		if err != nil {
			continue
		}
		if r.Name == name {
			return path
		}
	}
	return ""
}

// slugForFilename converts a recipe name into a filesystem-safe slug.
// Used when writing new files (e.g. saving a builtin as a user copy or
// renaming an "untitled" file after the user picked a real name in
// their editor). Keep it conservative: lowercase, letters / digits /
// hyphens / underscores only.
func slugForFilename(name string) string {
	var b strings.Builder
	prev := byte(0)
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'A' && c <= 'Z':
			c += 32
			fallthrough
		case (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_':
			b.WriteByte(c)
		default:
			// Coalesce any run of non-safe chars into a single hyphen.
			if prev != '-' {
				b.WriteByte('-')
				c = '-'
			} else {
				continue
			}
		}
		prev = c
	}
	out := strings.Trim(b.String(), "-_")
	if out == "" {
		return "untitled"
	}
	return out
}

// uniquePath returns base if it doesn't exist; otherwise appends -2,
// -3, ... before the extension until it finds a free slot. Avoids
// silently clobbering an existing file when the operator hits `n`
// twice and we'd otherwise overwrite their first attempt.
func uniquePath(base string) string {
	if _, err := os.Stat(base); os.IsNotExist(err) {
		return base
	}
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	for i := 2; i < 1000; i++ {
		candidate := fmt.Sprintf("%s-%d%s", stem, i, ext)
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
	// Astonishingly unlikely; defer to caller with the un-incremented
	// path and let os.WriteFile fail loudly if it really is taken.
	return base
}

// writeStarterRecipe creates a new YAML file under RecipesDir() with
// the starter template. Returns the actual path written (may differ
// from desired if a unique-suffix was applied). Caller is responsible
// for opening it in the editor.
func writeStarterRecipe() (string, error) {
	dir := recipe.RecipesDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("creating recipes dir: %w", err)
	}
	path := uniquePath(filepath.Join(dir, "untitled.yaml"))
	if err := os.WriteFile(path, []byte(recipeStarterTemplate), 0o600); err != nil {
		return "", fmt.Errorf("writing starter recipe: %w", err)
	}
	return path, nil
}

// saveBuiltinAsUserCopy serializes the named builtin to RecipesDir()
// so the operator can edit a copy without touching the embedded
// original. Returns the path of the new file. The MarshalYAML
// reformatting cost is documented on the prompt — operators who want
// pristine YAML start a new file with the starter template instead.
func saveBuiltinAsUserCopy(r *recipe.Recipe) (string, error) {
	dir := recipe.RecipesDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("creating recipes dir: %w", err)
	}
	path := uniquePath(filepath.Join(dir, slugForFilename(r.Name)+".yaml"))
	yaml, err := recipe.MarshalYAML(r)
	if err != nil {
		return "", fmt.Errorf("marshaling builtin recipe: %w", err)
	}
	if err := os.WriteFile(path, yaml, 0o600); err != nil {
		return "", fmt.Errorf("writing user copy: %w", err)
	}
	return path, nil
}

// validateEditedFile re-parses + re-validates a file after the editor
// returned. Returns a user-facing message — empty string when valid.
// Doesn't delete the file on invalid YAML; the operator's work is
// preserved on disk so they can re-open and fix it.
//
// Why this doesn't call recipe.ParseFile: that helper folds yaml
// unmarshal + Validate() into a single error, so a structural failure
// (no steps, unnamed step) would surface here as the YAML-parse branch
// — Bugbot flagged the mislabel on the first version. Splitting the
// two steps keeps the message accurate.
func validateEditedFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Sprintf("Could not read %s: %v", filepath.Base(path), err)
	}
	var r recipe.Recipe
	if err := yaml.Unmarshal(data, &r); err != nil {
		return fmt.Sprintf("Saved %s but YAML parse failed: %v", filepath.Base(path), err)
	}
	if err := r.Validate(); err != nil {
		return fmt.Sprintf("Saved %s but recipe is invalid: %v", filepath.Base(path), err)
	}
	return ""
}
