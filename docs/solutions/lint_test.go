// Package solutions enforces the docs/solutions/ frontmatter
// convention (KLA-445). Every file under docs/solutions/ must carry
// the required frontmatter fields so the library stays grep-able and
// the agent-facing tags don't silently rot.
//
// The package contains no production code — it exists purely as a
// home for the lint test.
package solutions

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// requiredFields lists the YAML keys every solution file MUST set.
// Update docs/solutions/README.md in lockstep with this list.
var requiredFields = []string{"title", "date", "category", "module", "tags", "applies_when"}

// validCategories restricts the `category:` field to the three
// subdirectory names the README documents. Adding a new category
// requires updating BOTH this list AND README.md so they stay in
// sync — the lint test deliberately fails loud when they don't.
var validCategories = map[string]bool{
	"design-patterns": true,
	"conventions":     true,
	"postmortems":     true,
}

// frontmatter captures the fields the lint cares about. yaml.v3 is
// lenient about extra keys — a file with `superseded_by:` or other
// optional fields parses cleanly.
type frontmatter struct {
	Title       string   `yaml:"title"`
	Date        string   `yaml:"date"`
	Category    string   `yaml:"category"`
	Module      string   `yaml:"module"`
	Tags        []string `yaml:"tags"`
	AppliesWhen []string `yaml:"applies_when"`
}

func TestEverySolutionFileHasFrontmatter(t *testing.T) {
	files, err := walkSolutions()
	if err != nil {
		t.Fatalf("walking docs/solutions: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("found zero solution files; the library exists but is empty")
	}

	var problems []string
	for _, path := range files {
		fm, err := readFrontmatter(path)
		if err != nil {
			problems = append(problems, path+": "+err.Error())
			continue
		}
		for _, want := range requiredFields {
			if missing := isFieldMissing(fm, want); missing {
				problems = append(problems, path+": missing required field "+want)
			}
		}
		if fm.Category != "" && !validCategories[fm.Category] {
			problems = append(problems, path+": category "+fm.Category+
				" is not one of design-patterns, conventions, postmortems")
		}
		// Filename / category sanity check — a file under
		// design-patterns/ that declares category: postmortems is a
		// copy-paste error.
		dir := filepath.Base(filepath.Dir(path))
		if fm.Category != "" && dir != fm.Category {
			problems = append(problems,
				path+": category "+fm.Category+
					" disagrees with parent directory "+dir)
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		t.Errorf("docs/solutions frontmatter issues:\n  %s",
			strings.Join(problems, "\n  "))
	}
}

func walkSolutions() ([]string, error) {
	var out []string
	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		// Skip README files and the lint test itself.
		base := filepath.Base(path)
		if base == "README.md" || strings.HasSuffix(base, ".go") {
			return nil
		}
		if strings.HasSuffix(base, ".md") {
			out = append(out, path)
		}
		return nil
	})
	sort.Strings(out)
	return out, err
}

func readFrontmatter(path string) (frontmatter, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return frontmatter{}, err
	}
	// Frontmatter is the YAML block between the first two `---` lines.
	const sep = "---"
	parts := strings.SplitN(string(body), sep, 3)
	if len(parts) < 3 || strings.TrimSpace(parts[0]) != "" {
		return frontmatter{}, errMissingFrontmatter
	}
	var fm frontmatter
	if err := yaml.Unmarshal([]byte(parts[1]), &fm); err != nil {
		return frontmatter{}, err
	}
	return fm, nil
}

var errMissingFrontmatter = &lintError{msg: "no YAML frontmatter block"}

type lintError struct{ msg string }

func (e *lintError) Error() string { return e.msg }

// isFieldMissing returns true when the named required field is empty.
// Lists are considered missing only if zero-length; scalars only if
// empty string.
func isFieldMissing(fm frontmatter, field string) bool {
	switch field {
	case "title":
		return fm.Title == ""
	case "date":
		return fm.Date == ""
	case "category":
		return fm.Category == ""
	case "module":
		return fm.Module == ""
	case "tags":
		return len(fm.Tags) == 0
	case "applies_when":
		return len(fm.AppliesWhen) == 0
	}
	return false
}
