package screen

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/tui"
	"github.com/klaassen-consulting/jc/internal/tui/style"
)

// newV1ClientForMFAOverview is overridable for tests.
var newV1ClientForMFAOverview = api.NewV1Client

// mfaFactor names one enrollment factor, in display order. Field names
// verified live on systemusers.mfaEnrollment (2026-07-16).
var mfaFactors = []struct{ Key, Label string }{
	{"totpStatus", "TOTP (authenticator app)"},
	{"webAuthnStatus", "WebAuthn (security key / passkey)"},
	{"pushStatus", "Push (JumpCloud Protect)"},
	{"jcGoStatus", "JumpCloud Go"},
	{"smsStatus", "SMS"},
}

// mfaOverviewData is the aggregated dashboard state.
type mfaOverviewData struct {
	RequireAdminMFA    bool
	AllowUnenrolledPWR bool

	TotalUsers   int
	Enrolled     int // overallStatus == ENROLLED
	Excluded     int // mfa.exclusion
	FactorCounts map[string]int
	NotEnrolled  []string // usernames, sorted
}

// MFAOverviewScreen is a read-only MFA posture dashboard (KLA-482):
// org-level knobs + per-factor enrollment aggregation + the actionable
// not-enrolled list. JumpCloud exposes no public MFA *configuration*
// endpoint — editing TOTP/WebAuthn/Push settings stays in the Admin
// Portal, and the footer says so.
type MFAOverviewScreen struct {
	loading bool
	err     string
	data    mfaOverviewData
	spinner spinner.Model

	width, height int
}

// mfaOverviewLoadedMsg carries the aggregate.
type mfaOverviewLoadedMsg struct {
	data mfaOverviewData
	err  error
}

func NewMFAOverviewScreen() *MFAOverviewScreen {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = style.Spinner
	return &MFAOverviewScreen{spinner: sp, loading: true}
}

func (s *MFAOverviewScreen) Title() string { return "MFA Overview" }

func (s *MFAOverviewScreen) Init() tea.Cmd {
	return tea.Batch(s.spinner.Tick, s.loadCmd())
}

func (s *MFAOverviewScreen) loadCmd() tea.Cmd {
	return func() tea.Msg {
		client, err := newV1ClientForMFAOverview()
		if err != nil {
			return mfaOverviewLoadedMsg{err: fmt.Errorf("building v1 client: %w", err)}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		data := mfaOverviewData{FactorCounts: map[string]int{}}

		// Org-level knobs.
		orgsRaw, err := client.Get(ctx, "/organizations")
		if err != nil {
			return mfaOverviewLoadedMsg{err: fmt.Errorf("listing organizations: %w", err)}
		}
		var orgs struct {
			Results []struct {
				ID string `json:"id"`
			} `json:"results"`
		}
		if err := json.Unmarshal(orgsRaw, &orgs); err != nil || len(orgs.Results) == 0 {
			return mfaOverviewLoadedMsg{err: fmt.Errorf("no organizations visible to this API key")}
		}
		orgRaw, err := client.Get(ctx, "/organizations/"+orgs.Results[0].ID)
		if err != nil {
			return mfaOverviewLoadedMsg{err: fmt.Errorf("fetching organization: %w", err)}
		}
		var org struct {
			Settings struct {
				RequireAdminMFA bool `json:"requireAdminMFA"`
				PasswordPolicy  struct {
					AllowUnenrolledMFAPasswordReset bool `json:"allowUnenrolledMFAPasswordReset"`
				} `json:"passwordPolicy"`
			} `json:"settings"`
		}
		_ = json.Unmarshal(orgRaw, &org)
		data.RequireAdminMFA = org.Settings.RequireAdminMFA
		data.AllowUnenrolledPWR = org.Settings.PasswordPolicy.AllowUnenrolledMFAPasswordReset

		// Per-user enrollment aggregation.
		users, err := client.ListAll(ctx, "/systemusers", api.ListOptions{})
		if err != nil {
			return mfaOverviewLoadedMsg{err: fmt.Errorf("listing users: %w", err)}
		}
		for _, raw := range users.Data {
			var u struct {
				Username string `json:"username"`
				MFA      struct {
					Exclusion bool `json:"exclusion"`
				} `json:"mfa"`
				MFAEnrollment map[string]any `json:"mfaEnrollment"`
			}
			if err := json.Unmarshal(raw, &u); err != nil {
				continue
			}
			data.TotalUsers++
			if u.MFA.Exclusion {
				data.Excluded++
			}
			if u.MFAEnrollment["overallStatus"] == "ENROLLED" {
				data.Enrolled++
			} else {
				data.NotEnrolled = append(data.NotEnrolled, u.Username)
			}
			for _, f := range mfaFactors {
				if u.MFAEnrollment[f.Key] == "ENROLLED" {
					data.FactorCounts[f.Key]++
				}
			}
		}
		sort.Strings(data.NotEnrolled)
		return mfaOverviewLoadedMsg{data: data}
	}
}

func (s *MFAOverviewScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		s.width, s.height = m.Width, m.Height
		return s, nil
	case spinner.TickMsg:
		if s.loading {
			var cmd tea.Cmd
			s.spinner, cmd = s.spinner.Update(m)
			return s, cmd
		}
		return s, nil
	case mfaOverviewLoadedMsg:
		s.loading = false
		if m.err != nil {
			s.err = m.err.Error()
			return s, nil
		}
		s.data = m.data
		return s, nil
	case tea.KeyMsg:
		switch m.String() {
		case "esc":
			return s, func() tea.Msg { return tui.PopScreenMsg{} }
		case "r":
			if !s.loading {
				s.loading, s.err = true, ""
				return s, tea.Batch(s.spinner.Tick, s.loadCmd())
			}
		}
	}
	return s, nil
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

func (s *MFAOverviewScreen) View() string {
	var b strings.Builder
	switch {
	case s.loading:
		fmt.Fprintln(&b, s.spinner.View()+" Aggregating MFA posture (org settings + user enrollment)...")
		return b.String()
	case s.err != "":
		fmt.Fprintln(&b, style.Error.Render("Error: "+s.err))
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, style.Subtitle.Render("r retry · Esc back"))
		return b.String()
	}

	d := s.data
	fmt.Fprintln(&b, style.SectionHeader.Render("Org settings"))
	fmt.Fprintf(&b, "  %-42s %s\n", "Require MFA for administrators", onOff(d.RequireAdminMFA))
	fmt.Fprintf(&b, "  %-42s %s\n", "Password reset without MFA enrollment", onOff(d.AllowUnenrolledPWR))

	fmt.Fprintln(&b)
	fmt.Fprintln(&b, style.SectionHeader.Render("User enrollment"))
	overall := fmt.Sprintf("%d of %d users enrolled", d.Enrolled, d.TotalUsers)
	if d.Enrolled == d.TotalUsers && d.TotalUsers > 0 {
		fmt.Fprintln(&b, "  "+style.Success.Render(overall))
	} else {
		fmt.Fprintln(&b, "  "+style.Error.Render(overall))
	}
	if d.Excluded > 0 {
		fmt.Fprintf(&b, "  %d user(s) excluded from MFA requirements\n", d.Excluded)
	}
	fmt.Fprintln(&b)
	for _, f := range mfaFactors {
		fmt.Fprintf(&b, "  %-42s %d/%d\n", f.Label, d.FactorCounts[f.Key], d.TotalUsers)
	}

	if len(d.NotEnrolled) > 0 {
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, style.SectionHeader.Render(fmt.Sprintf("Not enrolled (%d)", len(d.NotEnrolled))))
		const maxNames = 15
		shown := d.NotEnrolled
		if len(shown) > maxNames {
			shown = shown[:maxNames]
		}
		for _, name := range shown {
			fmt.Fprintln(&b, "  "+name)
		}
		if len(d.NotEnrolled) > maxNames {
			fmt.Fprintf(&b, "  … and %d more\n", len(d.NotEnrolled)-maxNames)
		}
	}

	fmt.Fprintln(&b)
	fmt.Fprintln(&b, style.Subtitle.Render(
		"Factor configuration (TOTP/WebAuthn/Push settings) is Admin Portal-only — no public API. Duo lives under Access → Duo Security."))
	fmt.Fprintln(&b, style.Subtitle.Render("r refresh · Esc back"))
	return b.String()
}
