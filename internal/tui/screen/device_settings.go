package screen

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/devsettings"
	"github.com/klaassen-consulting/jc/internal/tui"
	"github.com/klaassen-consulting/jc/internal/tui/style"
)

// Organization-wide device settings (KLA-485). A virtual registry entry rather
// than a generic list: these are two singletons with no collection behind
// them, the same shape as the patch-management and mfa-overview entries.
//
// Every write goes through internal/devsettings — MergeSignInSetting in
// particular — so the TUI cannot drift from the CLI on the one thing live
// probing could NOT establish: whether the sign-in PUT replaces the whole
// settings array or merges per OS family. Sending the complete array with one
// entry changed is correct either way, and the merge helper is what guarantees
// that here as it does on the CLI.
//
// /devices/settings/ssao is deliberately absent: it returns HTTP 501 on a live
// tenant. See devsettings.SSAOEndpoint.

var newV2ClientForDeviceSettings = api.NewV2Client

type deviceSettingsStage int

const (
	dsStageLoading deviceSettingsStage = iota
	dsStageBrowse
	dsStageConfirm
	dsStageSaving
)

// dsRow is one editable setting. Sign-in contributes two rows per OS family
// (enabled, and the default permission); password sync contributes one.
type dsRow struct {
	Group string
	Label string
	// OSFamily is set for sign-in rows, empty for the password sync row.
	OSFamily string
	// Permission marks the row that cycles STANDARD/ADMIN rather than toggling.
	Permission bool
	Value      string
	Original   string
}

func (r dsRow) Changed() bool { return r.Value != r.Original }

type dsLoadedMsg struct {
	signIn devsettings.SignInSettings
	sync   bool
	err    error
}

type dsSavedMsg struct{ err error }

// DeviceSettingsScreen shows and edits both org device-settings singletons.
type DeviceSettingsScreen struct {
	stage   deviceSettingsStage
	spinner spinner.Model

	signIn devsettings.SignInSettings
	rows   []dsRow
	cursor int

	err   string
	flash string

	width, height int
}

func NewDeviceSettingsScreen() *DeviceSettingsScreen {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = style.Spinner
	return &DeviceSettingsScreen{spinner: sp, stage: dsStageLoading}
}

func (s *DeviceSettingsScreen) Title() string { return "Device Settings" }

// HelpKeys puts this screen's editing keys in the status bar.
func (s *DeviceSettingsScreen) HelpKeys() string {
	if s.stage == dsStageConfirm {
		return "y:save  n:back"
	}
	if s.stage == dsStageBrowse {
		return "space:toggle  ctrl+s:save  r:reload"
	}
	return ""
}

func (s *DeviceSettingsScreen) Init() tea.Cmd {
	return tea.Batch(s.spinner.Tick, s.loadCmd())
}

func (s *DeviceSettingsScreen) loadCmd() tea.Cmd {
	return func() tea.Msg {
		client, err := newV2ClientForDeviceSettings()
		if err != nil {
			return dsLoadedMsg{err: err}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		rawSignIn, err := client.Get(ctx, devsettings.SignInEndpoint)
		if err != nil {
			return dsLoadedMsg{err: fmt.Errorf("reading sign-in settings: %w", err)}
		}
		signIn, err := devsettings.ParseSignIn(rawSignIn)
		if err != nil {
			return dsLoadedMsg{err: err}
		}

		rawSync, err := client.Get(ctx, devsettings.PasswordSyncEndpoint)
		if err != nil {
			return dsLoadedMsg{err: fmt.Errorf("reading password sync setting: %w", err)}
		}
		var sync devsettings.PasswordSync
		if err := json.Unmarshal(rawSync, &sync); err != nil {
			return dsLoadedMsg{err: fmt.Errorf("decoding password sync setting: %w", err)}
		}

		return dsLoadedMsg{signIn: signIn, sync: sync.Enabled}
	}
}

// buildRows lays the two singletons out as one editable list.
func (s *DeviceSettingsScreen) buildRows(sync bool) {
	s.rows = s.rows[:0]
	for _, st := range s.signIn.Settings {
		enabled := "off"
		if st.Enabled {
			enabled = "on"
		}
		s.rows = append(s.rows,
			dsRow{Group: "Sign In with JumpCloud", Label: st.OSFamily + " enabled",
				OSFamily: st.OSFamily, Value: enabled, Original: enabled},
			dsRow{Group: "Sign In with JumpCloud", Label: st.OSFamily + " default permission",
				OSFamily: st.OSFamily, Permission: true,
				Value: st.DefaultPermission, Original: st.DefaultPermission},
		)
	}

	syncVal := "off"
	if sync {
		syncVal = "on"
	}
	s.rows = append(s.rows, dsRow{
		Group: "Password sync", Label: "New devices sync passwords by default",
		Value: syncVal, Original: syncVal,
	})
}

func (s *DeviceSettingsScreen) changed() []string {
	var out []string
	for _, r := range s.rows {
		if r.Changed() {
			out = append(out, fmt.Sprintf("%s: %s -> %s", r.Label, r.Original, r.Value))
		}
	}
	return out
}

// saveCmd writes only the singletons that actually changed.
func (s *DeviceSettingsScreen) saveCmd() tea.Cmd {
	rows := append([]dsRow(nil), s.rows...)
	current := append([]devsettings.SignInSetting(nil), s.signIn.Settings...)

	return func() tea.Msg {
		client, err := newV2ClientForDeviceSettings()
		if err != nil {
			return dsSavedMsg{err: err}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		signInChanged := false
		merged := current
		for _, r := range rows {
			if !r.Changed() || r.OSFamily == "" {
				continue
			}
			signInChanged = true
			if r.Permission {
				perm := r.Value
				merged = devsettings.MergeSignInSetting(merged, r.OSFamily, nil, &perm)
				continue
			}
			enabled := r.Value == "on"
			merged = devsettings.MergeSignInSetting(merged, r.OSFamily, &enabled, nil)
		}
		if signInChanged {
			// The complete array goes back with the changed entries swapped
			// in, which is correct whether the PUT replaces or merges.
			if _, err := client.Update(ctx, devsettings.SignInEndpoint,
				devsettings.SignInBody(merged)); err != nil {
				return dsSavedMsg{err: fmt.Errorf("saving sign-in settings: %w", err)}
			}
		}

		for _, r := range rows {
			if r.OSFamily != "" || !r.Changed() {
				continue
			}
			if _, err := client.Update(ctx, devsettings.PasswordSyncEndpoint,
				devsettings.PasswordSyncBody(r.Value == "on")); err != nil {
				return dsSavedMsg{err: fmt.Errorf("saving password sync setting: %w", err)}
			}
		}
		return dsSavedMsg{}
	}
}

func (s *DeviceSettingsScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		s.width, s.height = m.Width, m.Height
		return s, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		s.spinner, cmd = s.spinner.Update(m)
		return s, cmd

	case dsLoadedMsg:
		s.stage = dsStageBrowse
		if m.err != nil {
			s.err = m.err.Error()
			return s, nil
		}
		s.err = ""
		s.signIn = m.signIn
		s.buildRows(m.sync)
		return s, nil

	case dsSavedMsg:
		if m.err != nil {
			s.stage = dsStageBrowse
			s.err = m.err.Error()
			return s, nil
		}
		s.flash = "Saved."
		s.stage = dsStageLoading
		return s, tea.Batch(s.spinner.Tick, s.loadCmd())

	case tea.KeyMsg:
		return s.updateKey(m)
	}
	return s, nil
}

func (s *DeviceSettingsScreen) updateKey(m tea.KeyMsg) (tea.Model, tea.Cmd) {
	if s.stage == dsStageConfirm {
		switch m.String() {
		case "y":
			s.stage = dsStageSaving
			return s, tea.Batch(s.spinner.Tick, s.saveCmd())
		case "n", "esc":
			s.stage = dsStageBrowse
			return s, nil
		}
		return s, nil
	}
	if s.stage != dsStageBrowse {
		return s, nil
	}

	switch m.String() {
	case "esc":
		return s, func() tea.Msg { return tui.PopScreenMsg{} }
	case "r":
		s.stage = dsStageLoading
		s.err, s.flash = "", ""
		return s, tea.Batch(s.spinner.Tick, s.loadCmd())
	case "up", "k":
		if s.cursor > 0 {
			s.cursor--
		}
	case "down", "j":
		if s.cursor < len(s.rows)-1 {
			s.cursor++
		}
	case " ", "enter":
		s.toggleCurrent()
	case "ctrl+s":
		if len(s.changed()) == 0 {
			s.flash = "No changes."
			return s, nil
		}
		s.stage = dsStageConfirm
	}
	return s, nil
}

// toggleCurrent flips a boolean row or cycles a permission row.
func (s *DeviceSettingsScreen) toggleCurrent() {
	if s.cursor >= len(s.rows) {
		return
	}
	r := &s.rows[s.cursor]
	if r.Permission {
		if r.Value == "ADMIN" {
			r.Value = "STANDARD"
		} else {
			r.Value = "ADMIN"
		}
		return
	}
	if r.Value == "on" {
		r.Value = "off"
	} else {
		r.Value = "on"
	}
}

func (s *DeviceSettingsScreen) View() string {
	var b strings.Builder

	switch s.stage {
	case dsStageLoading:
		fmt.Fprintln(&b, s.spinner.View()+" Loading organization device settings...")
		return b.String()
	case dsStageSaving:
		fmt.Fprintln(&b, s.spinner.View()+" Saving...")
		return b.String()
	case dsStageConfirm:
		fmt.Fprintln(&b, style.SectionHeader.Render("Apply these organization-wide changes?"))
		fmt.Fprintln(&b)
		for _, c := range s.changed() {
			fmt.Fprintln(&b, "  "+c)
		}
		fmt.Fprintln(&b, "\n  "+style.Error.Render("This affects every device in the organization."))
		fmt.Fprintln(&b, "\n"+style.Subtitle.Render("y save · n/Esc back"))
		return b.String()
	}

	fmt.Fprintln(&b, style.Subtitle.Render("Organization-wide device defaults, not per-device configuration"))
	if s.err != "" {
		fmt.Fprintln(&b, style.Error.Render("Error: "+s.err))
	}
	if s.flash != "" {
		fmt.Fprintln(&b, style.Success.Render(s.flash))
	}
	fmt.Fprintln(&b)

	lastGroup := ""
	for i, r := range s.rows {
		if r.Group != lastGroup {
			fmt.Fprintln(&b, style.SectionHeader.Render(r.Group))
			lastGroup = r.Group
		}
		marker := ""
		if r.Changed() {
			marker = style.Error.Render(" *")
		}
		line := fmt.Sprintf("%-44s %s%s", r.Label, r.Value, marker)
		if i == s.cursor {
			fmt.Fprintln(&b, style.SelectedRow.Render("> "+line))
		} else {
			fmt.Fprintln(&b, "  "+line)
		}
	}

	fmt.Fprintln(&b, "\n"+style.Subtitle.Render("space toggle · Ctrl+S save · r reload · Esc back"))
	return b.String()
}
