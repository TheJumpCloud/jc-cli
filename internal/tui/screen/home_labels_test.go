package screen

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/TheJumpCloud/jc-cli/internal/api"
	"github.com/TheJumpCloud/jc-cli/internal/schema"
	"github.com/TheJumpCloud/jc-cli/internal/tui"
)

func TestHomeScreen_EntrySuffixes(t *testing.T) {
	h := NewHomeScreen(nil)
	for _, tc := range []struct {
		name  string
		entry tui.ResourceEntry
		want  string
	}{
		{"writable", tui.ResourceEntry{DisplayName: "Users", Schema: schema.ResourceSchema{Verbs: []string{"list", "get", "create"}}}, "Users (3 ops)"},
		{"read-only verbs", tui.ResourceEntry{DisplayName: "Insights", Schema: schema.ResourceSchema{Verbs: []string{"query", "count", "distinct"}}}, "Insights (read-only)"},
		{"custom area", tui.ResourceEntry{DisplayName: "Agents", Schema: schema.ResourceSchema{Verbs: []string{"list", "get", "create"}},
			ListFunc: func(context.Context, *api.V2Client) ([]json.RawMessage, int, error) { return nil, 0, nil }}, "Agents (read-only)"},
		{"authoring", tui.ResourceEntry{DisplayName: "Workflow Templates", Authoring: true, Schema: schema.ResourceSchema{Verbs: []string{"list", "get"}}}, "Workflow Templates (2 ops)"},
		{"associations", tui.ResourceEntry{DisplayName: "Software", GraphSourceType: "software_app", Schema: schema.ResourceSchema{Verbs: []string{"list", "get"}}}, "Software (2 ops)"},
		{"no schema", tui.ResourceEntry{DisplayName: "MFA Overview"}, "MFA Overview"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := strings.TrimSpace(h.renderEntryLine(tc.entry, false))
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// Every home entry labelled read-only must open screens that offer no
// write: no n on the list, no E or d on the detail, no associations tab.
func TestReadOnlyLabelMatchesTheScreens(t *testing.T) {
	var walk func([]tui.ResourceEntry)
	labelled := 0
	walk = func(es []tui.ResourceEntry) {
		for _, e := range es {
			if len(e.SubMenu) > 0 {
				walk(e.SubMenu)
				continue
			}
			if !e.ReadOnlyInTUI() || len(e.Schema.Verbs) == 0 {
				continue
			}
			labelled++
			l := NewListScreen(e)
			d := NewDetailScreen(e, "x", "x")
			d.data = json.RawMessage(`{"_id":"x"}`)
			keys := l.HelpKeys() + "  " + d.HelpKeys()
			for _, w := range []string{"n:", "E:", "d:", "tab:"} {
				if strings.Contains(keys, w) {
					t.Errorf("%s is labelled read-only, but its screens offer %q (%s)", e.Key, w, keys)
				}
			}
		}
	}
	walk(tui.BuildRegistry())
	if labelled < 10 {
		t.Fatalf("only %d entries labelled read-only; the walk has gone blind", labelled)
	}
}

// The real list and detail footers, through the app: esc first, ? last, the
// screen's own keys in between.
func TestScreenFooters(t *testing.T) {
	users := tui.ResourceEntry{Key: "users", DisplayName: "Users", GraphSourceType: "user", Schema: schema.Resources["users"]}
	for _, tc := range []struct {
		name   string
		screen tui.Screen
		want   []string
	}{
		{"list", NewListScreen(users), []string{"n:new", "esc:back  /:filter  s:sort  c:copy id  e:export  a:all fields  r:refresh  ?:help"}},
		{"detail", func() tui.Screen {
			d := NewDetailScreen(users, "x", "x")
			d.data = json.RawMessage(`{"_id":"x"}`)
			return d
		}(), []string{"E:edit", "d:delete", "esc:back  c:copy id  e:export  a:all fields  r:refresh  ?:help"}},
		{"recipes", NewRecipeListScreen(), []string{"n:new  e:edit", "esc:back  /:filter  r:reload  ?:help"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := tui.NewApp(NewHomeScreen(nil))
			app.Update(tea.WindowSizeMsg{Width: 200, Height: 40})
			app.Update(tui.PushScreenMsg{Screen: tc.screen})
			got := app.View()
			for _, w := range tc.want {
				if !strings.Contains(got, w) {
					t.Errorf("footer lacks %q:\n%s", w, got[strings.LastIndex(got, "\n")+1:])
				}
			}
		})
	}
}
