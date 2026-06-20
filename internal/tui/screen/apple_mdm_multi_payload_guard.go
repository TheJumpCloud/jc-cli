package screen

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/klaassen-consulting/jc/internal/apple_mdm"
	"github.com/klaassen-consulting/jc/internal/tui"
	"github.com/klaassen-consulting/jc/internal/tui/style"
)

// AppleMDMMultiPayloadGuardScreen is shown when the operator drills
// into a Custom MDM policy whose mobileconfig bundles more than one
// inner Apple payload (e.g. the CIS Level 1 bundles 11 payloads in
// one envelope). v1 of the TUI edit flow doesn't support editing
// individual inner payloads from a multi-payload bundle — picking
// which one to edit requires its own UX that's deferred to a v2
// ticket. Until then this screen explains what's going on and points
// the operator at the Admin Portal.
type AppleMDMMultiPayloadGuardScreen struct {
	decoded apple_mdm.DecodedPolicy
}

// NewAppleMDMMultiPayloadGuardScreen builds the guard from a decoded
// policy. We keep the decoded reference so a future v2 can offer
// "pick which inner payload to edit" without re-fetching.
func NewAppleMDMMultiPayloadGuardScreen(d apple_mdm.DecodedPolicy) *AppleMDMMultiPayloadGuardScreen {
	return &AppleMDMMultiPayloadGuardScreen{decoded: d}
}

func (s *AppleMDMMultiPayloadGuardScreen) Title() string {
	return "Multi-payload profile: " + s.decoded.PolicyName
}

func (s *AppleMDMMultiPayloadGuardScreen) Init() tea.Cmd { return nil }

func (s *AppleMDMMultiPayloadGuardScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m, ok := msg.(tea.KeyMsg); ok {
		switch m.String() {
		case "esc", "enter", "q":
			return s, func() tea.Msg { return tui.PopScreenMsg{} }
		}
	}
	return s, nil
}

func (s *AppleMDMMultiPayloadGuardScreen) View() string {
	var b strings.Builder
	fmt.Fprintln(&b, style.Error.Render("Multi-payload profile — editing not yet supported."))
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "This policy bundles multiple Apple payloads in one mobileconfig.")
	fmt.Fprintln(&b, "The TUI's v1 edit flow only handles single-payload profiles.")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "  Policy ID:   "+s.decoded.PolicyID)
	fmt.Fprintln(&b, "  Policy name: "+s.decoded.PolicyName)
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "To edit this policy:")
	fmt.Fprintln(&b, "  - Use the JumpCloud Admin Portal (Policy Management →")
	fmt.Fprintln(&b, "    select the policy → edit the mobileconfig)")
	fmt.Fprintln(&b, "  - Or regenerate the full bundle with ProfileCreator")
	fmt.Fprintln(&b, "    and re-upload via the Admin Portal.")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Multi-payload edit support is tracked as a follow-up.")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, style.Subtitle.Render("Esc / Enter to go back"))
	return b.String()
}
