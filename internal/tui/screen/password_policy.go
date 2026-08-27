package screen

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/pwpolicy"
	"github.com/klaassen-consulting/jc/internal/schema"
	"github.com/klaassen-consulting/jc/internal/tui"
	"github.com/klaassen-consulting/jc/internal/tui/style"
)

// newV1ClientForPasswordPolicy is overridable for tests.
var newV1ClientForPasswordPolicy = api.NewV1Client

// newV2ClientForPasswordPolicy is overridable for tests. The screen reads two
// different APIs: the org-wide default lives in V1 /organizations settings,
// while the group-bound policies are a V2 resource.
var newV2ClientForPasswordPolicy = api.NewV2Client

// passwordPolicyField describes one editable policy knob, in display
// order with group headers.
type passwordPolicyField struct {
	Key   string
	Label string
	Group string // rendered as a section header when it changes
	Bool  bool   // true = toggle, false = integer input
}

// passwordPolicyFields is the curated display order for the fields
// verified live on 2026-07-16. Unknown keys the server adds later are
// appended after these (editable when bool/number) so the screen never
// silently drops policy state on save.
var passwordPolicyFields = []passwordPolicyField{
	{Key: "enableMinLength", Label: "Enforce minimum length", Group: "Complexity", Bool: true},
	{Key: "minLength", Label: "Minimum length", Group: "Complexity"},
	{Key: "needsLowercase", Label: "Require lowercase", Group: "Complexity", Bool: true},
	{Key: "needsUppercase", Label: "Require uppercase", Group: "Complexity", Bool: true},
	{Key: "needsNumeric", Label: "Require number", Group: "Complexity", Bool: true},
	{Key: "needsSymbolic", Label: "Require symbol", Group: "Complexity", Bool: true},
	{Key: "allowUsernameSubstring", Label: "Allow username inside password", Group: "Complexity", Bool: true},
	{Key: "disallowCommonlyUsedPasswords", Label: "Block commonly used passwords", Group: "Complexity", Bool: true},
	{Key: "disallowSequentialOrRepetitiveChars", Label: "Block sequential/repetitive chars", Group: "Complexity", Bool: true},
	{Key: "displayComplexityOnResetScreen", Label: "Show complexity rules on reset screen", Group: "Complexity", Bool: true},

	{Key: "enablePasswordExpirationInDays", Label: "Enable password expiration", Group: "Expiration", Bool: true},
	{Key: "passwordExpirationInDays", Label: "Expiration (days)", Group: "Expiration"},
	{Key: "enableDaysBeforeExpirationToForceReset", Label: "Force reset before expiry", Group: "Expiration", Bool: true},
	{Key: "daysBeforeExpirationToForceReset", Label: "Days before expiry to force reset", Group: "Expiration"},
	{Key: "enableDaysAfterExpirationToSelfRecover", Label: "Allow self-recovery after expiry", Group: "Expiration", Bool: true},
	{Key: "daysAfterExpirationToSelfRecover", Label: "Days after expiry to self-recover", Group: "Expiration"},

	{Key: "enableMaxLoginAttempts", Label: "Enable max login attempts", Group: "Lockout", Bool: true},
	{Key: "maxLoginAttempts", Label: "Max login attempts", Group: "Lockout"},
	{Key: "enableLockoutTimeInSeconds", Label: "Enable lockout time", Group: "Lockout", Bool: true},
	{Key: "lockoutTimeInSeconds", Label: "Lockout time (seconds)", Group: "Lockout"},
	{Key: "enableResetLockoutCounter", Label: "Enable lockout counter reset", Group: "Lockout", Bool: true},
	{Key: "resetLockoutCounterMinutes", Label: "Reset lockout counter (minutes)", Group: "Lockout"},

	{Key: "enableMaxHistory", Label: "Enforce password history", Group: "Reuse", Bool: true},
	{Key: "maxHistory", Label: "History depth", Group: "Reuse"},
	{Key: "enableMinChangePeriodInDays", Label: "Enforce min change period", Group: "Reuse", Bool: true},
	{Key: "minChangePeriodInDays", Label: "Min change period (days)", Group: "Reuse"},

	{Key: "enableRecoveryEmail", Label: "Allow recovery email", Group: "Recovery", Bool: true},
	{Key: "allowUnenrolledMFAPasswordReset", Label: "Allow reset without MFA enrollment", Group: "Recovery", Bool: true},
}

// passwordPolicyReadOnlyKeys are server-managed values shown but never
// edited or diffed (effectiveDate updates on every save).
var passwordPolicyReadOnlyKeys = map[string]bool{"effectiveDate": true}

// ppStage tracks the screen's state machine.
type ppStage int

const (
	ppStageLoading ppStage = iota
	ppStageEdit
	ppStageEditingValue // inline int input open
	ppStageConfirm
	ppStageSaving
)

// PasswordPolicyScreen views and edits the org's password policy —
// backed by GET/PUT /organizations/{id} (KLA-480). Saves are strict
// read-modify-write: the FULL settings object from load goes back with
// only the edited passwordPolicy keys changed (round-trip contract
// verified live 2026-07-16).
type PasswordPolicyScreen struct {
	stage   ppStage
	spinner spinner.Model
	input   textinput.Model

	orgID    string
	orgName  string
	settings map[string]any // full settings object, mutated in place on save
	original map[string]any // pristine passwordPolicy copy for diffing
	policy   map[string]any // working passwordPolicy copy
	rows     []passwordPolicyField
	cursor   int

	// groupPolicies are the V2 /passwordpolicies entries: policies bound to
	// user groups and ordered by precedence. They are a different API from
	// the org default above, but an admin thinks of both as "password
	// policy", so both live on this screen.
	groupPolicies []pwpolicy.ListItem
	groupErr      string

	err   string
	flash string

	width, height int
}

// ppLoadedMsg carries the load result.
type ppLoadedMsg struct {
	orgID, orgName string
	settings       map[string]any
	err            error

	groupPolicies []pwpolicy.ListItem
	// groupErr is kept separate from err: failing to read the group-bound
	// policies must not stop the org default from loading and being edited.
	groupErr string
}

// ppSavedMsg carries the save result.
type ppSavedMsg struct{ err error }

func NewPasswordPolicyScreen() *PasswordPolicyScreen {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = style.Spinner
	ti := textinput.New()
	ti.CharLimit = 10
	return &PasswordPolicyScreen{spinner: sp, input: ti, stage: ppStageLoading}
}

func (s *PasswordPolicyScreen) Title() string { return "Password Policies" }

func (s *PasswordPolicyScreen) TextInputActive() bool { return s.stage == ppStageEditingValue }

func (s *PasswordPolicyScreen) Init() tea.Cmd {
	return tea.Batch(s.spinner.Tick, s.loadCmd())
}

func (s *PasswordPolicyScreen) loadCmd() tea.Cmd {
	return func() tea.Msg {
		client, err := newV1ClientForPasswordPolicy()
		if err != nil {
			return ppLoadedMsg{err: fmt.Errorf("building v1 client: %w", err)}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		raw, err := client.Get(ctx, "/organizations")
		if err != nil {
			return ppLoadedMsg{err: fmt.Errorf("listing organizations: %w", err)}
		}
		var list struct {
			Results []struct {
				ID          string `json:"id"`
				DisplayName string `json:"displayName"`
			} `json:"results"`
		}
		if err := json.Unmarshal(raw, &list); err != nil || len(list.Results) == 0 {
			return ppLoadedMsg{err: fmt.Errorf("no organizations visible to this API key")}
		}
		org := list.Results[0]

		detail, err := client.Get(ctx, "/organizations/"+org.ID)
		if err != nil {
			return ppLoadedMsg{err: fmt.Errorf("fetching organization: %w", err)}
		}
		var parsed struct {
			Settings map[string]any `json:"settings"`
		}
		if err := json.Unmarshal(detail, &parsed); err != nil || parsed.Settings == nil {
			return ppLoadedMsg{err: fmt.Errorf("organization response carried no settings")}
		}
		msg := ppLoadedMsg{orgID: org.ID, orgName: org.DisplayName, settings: parsed.Settings}
		msg.groupPolicies, msg.groupErr = loadGroupPolicies(ctx)
		return msg
	}
}

// loadGroupPolicies reads the V2 group-bound policies. Its failure is
// reported separately so the org default stays usable on its own.
func loadGroupPolicies(ctx context.Context) ([]pwpolicy.ListItem, string) {
	client, err := newV2ClientForPasswordPolicy()
	if err != nil {
		return nil, "building v2 client: " + err.Error()
	}
	raw, err := client.Get(ctx, pwpolicy.Endpoint)
	if err != nil {
		return nil, "reading group-bound policies: " + err.Error()
	}
	items, err := pwpolicy.ParseListItems(raw)
	if err != nil {
		return nil, err.Error()
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Precedence < items[j].Precedence })
	return items, ""
}

// passwordPolicyEntry synthesizes the registry entry used to drill into the
// group-bound policies. It is not a registered schema resource: the list
// projection is flat and sparse while a single read nests everything under
// "policy", so the two cannot share one field list.
func passwordPolicyEntry() tui.ResourceEntry {
	return tui.ResourceEntry{
		Key:          "password-policies-group",
		DisplayName:  "Group Password Policies",
		Category:     tui.CategorySecurity,
		ClientType:   tui.ClientV2,
		ListEndpoint: pwpolicy.Endpoint,
		ResponseKey:  "results",
		Schema: schema.ResourceSchema{
			Resource:      "password-policies-group",
			APIVersion:    "v2",
			Verbs:         []string{"list", "get"},
			DefaultFields: pwpolicy.ListDefaultFields,
			IDField:       "objectId",
			NameField:     "name",
		},
	}
}

func (s *PasswordPolicyScreen) saveCmd() tea.Cmd {
	orgID := s.orgID
	settings := s.settings
	policy := s.policy
	return func() tea.Msg {
		client, err := newV1ClientForPasswordPolicy()
		if err != nil {
			return ppSavedMsg{err: err}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		// Read-modify-write: full settings object with the edited
		// policy swapped in. Never a sparse body.
		settings["passwordPolicy"] = policy
		if _, err := client.Update(ctx, "/organizations/"+orgID, map[string]any{"settings": settings}); err != nil {
			return ppSavedMsg{err: err}
		}
		return ppSavedMsg{}
	}
}

// buildRows composes the display order: curated fields that exist in
// the response first, then any unknown keys (alphabetical would churn
// on map order — sort for stability).
func (s *PasswordPolicyScreen) buildRows() {
	s.rows = s.rows[:0]
	seen := map[string]bool{}
	for _, f := range passwordPolicyFields {
		if _, ok := s.policy[f.Key]; ok {
			s.rows = append(s.rows, f)
			seen[f.Key] = true
		}
	}
	var extras []string
	for k := range s.policy {
		if !seen[k] && !passwordPolicyReadOnlyKeys[k] {
			extras = append(extras, k)
		}
	}
	sortStrings(extras)
	for _, k := range extras {
		_, isBool := s.policy[k].(bool)
		s.rows = append(s.rows, passwordPolicyField{Key: k, Label: k, Group: "Other", Bool: isBool})
	}
}

func sortStrings(ss []string) {
	for i := 1; i < len(ss); i++ {
		for j := i; j > 0 && ss[j] < ss[j-1]; j-- {
			ss[j], ss[j-1] = ss[j-1], ss[j]
		}
	}
}

// changedFields diffs working policy against the pristine copy.
func (s *PasswordPolicyScreen) changedFields() []string {
	var out []string
	for _, f := range s.rows {
		if fmt.Sprintf("%v", s.policy[f.Key]) != fmt.Sprintf("%v", s.original[f.Key]) {
			out = append(out, fmt.Sprintf("%s: %v → %v", f.Label, s.original[f.Key], s.policy[f.Key]))
		}
	}
	return out
}

func (s *PasswordPolicyScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		s.width, s.height = m.Width, m.Height
		return s, nil

	case spinner.TickMsg:
		if s.stage == ppStageLoading || s.stage == ppStageSaving {
			var cmd tea.Cmd
			s.spinner, cmd = s.spinner.Update(m)
			return s, cmd
		}
		return s, nil

	case ppLoadedMsg:
		// The group-bound half is independent of the org default: keep
		// whatever loaded even if the other half failed.
		s.groupPolicies, s.groupErr = m.groupPolicies, m.groupErr
		if m.err != nil {
			s.stage = ppStageEdit
			s.err = m.err.Error()
			return s, nil
		}
		s.orgID, s.orgName, s.settings = m.orgID, m.orgName, m.settings
		policy, _ := m.settings["passwordPolicy"].(map[string]any)
		if policy == nil {
			s.stage = ppStageEdit
			s.err = "organization settings carry no passwordPolicy block"
			return s, nil
		}
		s.policy = policy
		s.original = make(map[string]any, len(policy))
		for k, v := range policy {
			s.original[k] = v
		}
		s.buildRows()
		s.cursor = clampScroll(s.cursor, len(s.rows))
		s.stage = ppStageEdit
		s.err = ""
		return s, nil

	case ppSavedMsg:
		if m.err != nil {
			s.stage = ppStageEdit
			s.err = "save failed: " + m.err.Error()
			return s, nil
		}
		// Re-baseline: what we saved is the new pristine state.
		for k, v := range s.policy {
			s.original[k] = v
		}
		s.stage = ppStageEdit
		s.flash = "Saved."
		return s, nil

	case tea.KeyMsg:
		s.flash = ""
		switch s.stage {
		case ppStageEdit:
			return s.updateEdit(m)
		case ppStageEditingValue:
			switch m.String() {
			case "esc":
				s.stage = ppStageEdit
				return s, nil
			case "enter":
				f := s.rows[s.cursor]
				n, err := strconv.Atoi(strings.TrimSpace(s.input.Value()))
				if err != nil {
					s.err = fmt.Sprintf("%s: %q is not a whole number", f.Label, s.input.Value())
					s.stage = ppStageEdit
					return s, nil
				}
				s.err = ""
				// JSON numbers decode as float64; store the same type
				// back so the diff and the wire value stay consistent.
				s.policy[f.Key] = float64(n)
				s.stage = ppStageEdit
				return s, nil
			default:
				var cmd tea.Cmd
				s.input, cmd = s.input.Update(m)
				return s, cmd
			}
		case ppStageConfirm:
			switch m.String() {
			case "y":
				s.stage = ppStageSaving
				return s, tea.Batch(s.spinner.Tick, s.saveCmd())
			case "esc", "n":
				s.stage = ppStageEdit
				return s, nil
			}
		}
	}
	return s, nil
}

func (s *PasswordPolicyScreen) updateEdit(m tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.String() {
	case "esc":
		return s, func() tea.Msg { return tui.PopScreenMsg{} }
	case "r":
		s.stage = ppStageLoading
		s.err = ""
		s.groupErr = ""
		return s, tea.Batch(s.spinner.Tick, s.loadCmd())
	case "g":
		// Open the group-bound policies as a normal list, which gives them
		// the generic detail view. Nothing to open when there are none.
		if len(s.groupPolicies) == 0 {
			return s, nil
		}
		return s, func() tea.Msg {
			return tui.PushScreenMsg{Screen: NewListScreen(passwordPolicyEntry())}
		}
	case "up", "k":
		if s.cursor > 0 {
			s.cursor--
		}
	case "down", "j":
		if s.cursor < len(s.rows)-1 {
			s.cursor++
		}
	case " ", "h", "l", "enter":
		if s.cursor >= len(s.rows) {
			return s, nil
		}
		f := s.rows[s.cursor]
		if f.Bool {
			if b, ok := s.policy[f.Key].(bool); ok {
				s.policy[f.Key] = !b
			}
			return s, nil
		}
		if m.String() == "enter" {
			s.input.SetValue(fmt.Sprintf("%v", s.policy[f.Key]))
			s.input.Focus()
			s.stage = ppStageEditingValue
			return s, textinput.Blink
		}
	case "ctrl+s":
		if len(s.changedFields()) == 0 {
			s.flash = "No changes."
			return s, nil
		}
		s.stage = ppStageConfirm
	}
	return s, nil
}

func (s *PasswordPolicyScreen) View() string {
	var b strings.Builder
	switch s.stage {
	case ppStageLoading:
		fmt.Fprintln(&b, s.spinner.View()+" Loading organization password policy...")
		return b.String()
	case ppStageSaving:
		fmt.Fprintln(&b, s.spinner.View()+" Saving (read-modify-write of the full settings object)...")
		return b.String()
	case ppStageConfirm:
		fmt.Fprintln(&b, style.SectionHeader.Render("Apply these password policy changes?"))
		fmt.Fprintln(&b)
		for _, c := range s.changedFields() {
			fmt.Fprintln(&b, "  "+c)
		}
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, style.Subtitle.Render("y save · n/Esc back"))
		return b.String()
	}

	header := "Organization password policy"
	if s.orgName != "" {
		header += " — " + s.orgName
	}
	fmt.Fprintln(&b, style.Subtitle.Render(header))
	chrome := 4 // header + blank + footer blank + footer
	if s.err != "" {
		fmt.Fprintln(&b, style.Error.Render("Error: "+s.err))
		chrome++
	}
	if s.flash != "" {
		fmt.Fprintln(&b, style.Success.Render(s.flash))
		chrome++
	}
	fmt.Fprintln(&b)

	// Build body lines with the cursor's line index tracked, then
	// window so the selected row is always on screen.
	var lines []string
	focusLine := 0
	lastGroup := ""
	for i, f := range s.rows {
		if f.Group != lastGroup {
			lines = append(lines, style.SectionHeader.Render(f.Group))
			lastGroup = f.Group
		}
		val := fmt.Sprintf("%v", s.policy[f.Key])
		if f.Bool {
			if s.policy[f.Key] == true {
				val = "on"
			} else {
				val = "off"
			}
		} else if fl, ok := s.policy[f.Key].(float64); ok {
			val = strconv.FormatFloat(fl, 'f', -1, 64)
		}
		changed := ""
		if fmt.Sprintf("%v", s.policy[f.Key]) != fmt.Sprintf("%v", s.original[f.Key]) {
			changed = style.Error.Render(" *")
		}
		line := fmt.Sprintf("%-40s %s%s", f.Label, val, changed)
		if i == s.cursor {
			if s.stage == ppStageEditingValue {
				line = fmt.Sprintf("%-40s %s", f.Label, s.input.View())
			}
			lines = append(lines, style.SelectedRow.Render("> "+line))
			focusLine = len(lines) - 1
		} else {
			lines = append(lines, "  "+line)
		}
	}
	lines = append(lines, s.groupPolicyLines()...)
	fmt.Fprintln(&b, renderWindowed(lines, focusLine, s.height, chrome))

	fmt.Fprintln(&b)
	footer := "space toggle · Enter edit number · Ctrl+S save · r reload · Esc back"
	if len(s.groupPolicies) > 0 {
		footer = "space toggle · Enter edit number · g group policies · Ctrl+S save · r reload · Esc back"
	}
	fmt.Fprintln(&b, style.Subtitle.Render(footer))
	return b.String()
}

// groupPolicyLines renders the V2 group-bound policies beneath the org
// default. They are shown rather than edited here: the two halves are
// different APIs with different shapes, and `g` opens the group-bound list
// where they can be inspected properly.
func (s *PasswordPolicyScreen) groupPolicyLines() []string {
	var out []string
	out = append(out, "", style.SectionHeader.Render("Group-bound policies"))

	switch {
	case s.groupErr != "":
		out = append(out, "  "+style.Error.Render(s.groupErr))
		return out
	case len(s.groupPolicies) == 0:
		out = append(out, "  "+style.Subtitle.Render(
			"none — every user falls under the organization default above"))
		return out
	}

	for _, p := range s.groupPolicies {
		name := p.Name
		if name == "" {
			name = style.Subtitle.Render("(unnamed)")
		}
		marker := " "
		if p.Default {
			marker = "*"
		}
		out = append(out, fmt.Sprintf("  %s %-32s precedence %-4d %s",
			marker, name, p.Precedence, pluralGroups(p.GroupCount)))
	}
	out = append(out, "  "+style.Subtitle.Render(
		"* organization default · lowest precedence wins · g to open"))
	return out
}

func pluralGroups(n int) string {
	if n == 1 {
		return "1 group"
	}
	return fmt.Sprintf("%d groups", n)
}
