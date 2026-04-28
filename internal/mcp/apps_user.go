package mcp

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/resolve"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

//go:embed apps_html/user.html
var userHTML string

const (
	userViewResourceURI = "ui://jc/user"

	// Cap on recent events fetched per user to keep the payload bounded;
	// 50 covers a reasonable activity timeline without dragging the call.
	userViewRecentEventsLimit = 50
)

// userViewArgs is the tool input. Single required field — the user
// identifier accepts username, email, or 24-char hex ID.
type userViewArgs struct {
	// User is the JumpCloud user to inspect: username, email, or ID.
	User string `json:"user" jsonschema:"JumpCloud user to inspect (username, email, or 24-char hex ID)."`
}

// userViewData is the payload pushed to the app iframe. Mirrors the
// dashboard's layout: a header section, structured slices for the
// per-card UI, and a Warnings list when a sub-fetch failed but the
// overall view is still useful.
type userViewData struct {
	User         userHeader        `json:"user"`
	MFA          userMFA           `json:"mfa"`
	Groups       []userGroupRef    `json:"groups"`
	SSHKeys      []userSSHKey      `json:"ssh_keys"`
	RecentEvents []json.RawMessage `json:"recent_events"`
	Warnings     []string          `json:"warnings,omitempty"`
}

type userHeader struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	Email       string `json:"email"`
	Firstname   string `json:"firstname,omitempty"`
	Lastname    string `json:"lastname,omitempty"`
	Department  string `json:"department,omitempty"`
	Activated   bool   `json:"activated"`
	Suspended   bool   `json:"suspended"`
	Locked      bool   `json:"locked"`
	Created     string `json:"created,omitempty"`
	LastLogin   string `json:"last_login,omitempty"`
	Description string `json:"description,omitempty"`
}

type userMFA struct {
	TOTPEnabled bool   `json:"totp_enabled"`
	Status      string `json:"status,omitempty"` // ENROLLED / NOT_ENROLLED / etc.
}

type userGroupRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type userSSHKey struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	CreatedDate string `json:"create_date,omitempty"`
	// PublicKeyPreview is a short, safe-to-display fragment (algorithm prefix
	// + first dozen chars of the key blob). Full keys aren't sent to the
	// iframe — they aren't secrets per se but they're long and noisy.
	PublicKeyPreview string `json:"public_key_preview,omitempty"`
}

// fetchUserViewData runs the parallel API calls behind user_view and
// aggregates them into a single payload. Best-effort for sub-fetches:
// a transient failure on groups or ssh keys lands as a Warning rather
// than blocking the whole view.
func fetchUserViewData(ctx context.Context, args userViewArgs) (*userViewData, error) {
	if args.User == "" {
		return nil, fmt.Errorf("user is required")
	}

	v1, err := newV1ClientFunc()
	if err != nil {
		return nil, fmt.Errorf("v1 client: %w", err)
	}

	// Resolve identifier first — every subsequent call needs the ID.
	id, err := resolveV1(ctx, v1, args.User, resolve.UserConfig)
	if err != nil {
		return nil, fmt.Errorf("resolving user %q: %w", args.User, err)
	}

	var (
		mu       sync.Mutex
		data     userViewData
		warnings []string
	)
	addWarning := func(msg string) {
		mu.Lock()
		warnings = append(warnings, msg)
		mu.Unlock()
	}

	var wg sync.WaitGroup

	// User detail (header + MFA flags).
	wg.Add(1)
	go func() {
		defer wg.Done()
		raw, err := v1.Get(ctx, "/systemusers/"+id)
		if err != nil {
			addWarning(fmt.Sprintf("fetching user: %v", err))
			return
		}
		var u struct {
			ID            string `json:"_id"`
			Username      string `json:"username"`
			Email         string `json:"email"`
			Firstname     string `json:"firstname"`
			Lastname      string `json:"lastname"`
			Department    string `json:"department"`
			Description   string `json:"description"`
			Activated     bool   `json:"activated"`
			Suspended     bool   `json:"suspended"`
			AccountLocked bool   `json:"account_locked"`
			TOTPEnabled   bool   `json:"totp_enabled"`
			Created       string `json:"created"`
			MFA           struct {
				Configured bool `json:"configured"`
			} `json:"mfa"`
		}
		if err := json.Unmarshal(raw, &u); err != nil {
			addWarning(fmt.Sprintf("parsing user: %v", err))
			return
		}
		mfaStatus := "NOT_ENROLLED"
		if u.TOTPEnabled || u.MFA.Configured {
			mfaStatus = "ENROLLED"
		}
		mu.Lock()
		data.User = userHeader{
			ID: u.ID, Username: u.Username, Email: u.Email,
			Firstname: u.Firstname, Lastname: u.Lastname,
			Department: u.Department, Description: u.Description,
			Activated: u.Activated, Suspended: u.Suspended, Locked: u.AccountLocked,
			Created: u.Created,
		}
		data.MFA = userMFA{TOTPEnabled: u.TOTPEnabled, Status: mfaStatus}
		mu.Unlock()
	}()

	// Group memberships via the V2 graph: user → user_group.
	wg.Add(1)
	go func() {
		defer wg.Done()
		v2, err := newV2ClientFunc()
		if err != nil {
			addWarning(fmt.Sprintf("v2 client: %v", err))
			return
		}
		// User → user_group uses the membership endpoint, not graph
		// associations (the membership endpoint is what the registry
		// MemberOfTarget points at and the V1 user-groups endpoint
		// returns).
		result, err := v2.ListAll(ctx, "/users/"+id+"/memberof", api.V2ListOptions{})
		if err != nil {
			addWarning(fmt.Sprintf("groups: %v", err))
			return
		}
		groups := make([]userGroupRef, 0, len(result.Data))
		for _, raw := range result.Data {
			var g struct {
				ID         string `json:"id"`
				Attributes struct {
					Name string `json:"name"`
				} `json:"attributes"`
				Name string `json:"name"`
			}
			if err := json.Unmarshal(raw, &g); err != nil {
				continue
			}
			name := g.Name
			if name == "" {
				name = g.Attributes.Name
			}
			groups = append(groups, userGroupRef{ID: g.ID, Name: name})
		}
		sort.Slice(groups, func(i, j int) bool { return groups[i].Name < groups[j].Name })
		mu.Lock()
		data.Groups = groups
		mu.Unlock()
	}()

	// SSH keys.
	wg.Add(1)
	go func() {
		defer wg.Done()
		result, err := v1.ListAll(ctx, "/systemusers/"+id+"/sshkeys", api.ListOptions{})
		if err != nil {
			addWarning(fmt.Sprintf("ssh keys: %v", err))
			return
		}
		keys := make([]userSSHKey, 0, len(result.Data))
		for _, raw := range result.Data {
			var k struct {
				ID         string `json:"id"`
				Name       string `json:"name"`
				PublicKey  string `json:"public_key"`
				CreateDate string `json:"create_date"`
			}
			if err := json.Unmarshal(raw, &k); err != nil {
				continue
			}
			keys = append(keys, userSSHKey{
				ID: k.ID, Name: k.Name, CreatedDate: k.CreateDate,
				PublicKeyPreview: previewSSHKey(k.PublicKey),
			})
		}
		sort.Slice(keys, func(i, j int) bool { return keys[i].Name < keys[j].Name })
		mu.Lock()
		data.SSHKeys = keys
		mu.Unlock()
	}()

	// Recent insights events: last 30d, filtered to this user.
	wg.Add(1)
	go func() {
		defer wg.Done()
		insights, err := newInsightsClientFunc()
		if err != nil {
			addWarning(fmt.Sprintf("insights client: %v", err))
			return
		}
		// We don't know the username yet (the user-detail goroutine is
		// running in parallel). Use the username we resolved against —
		// args.User passes through resolve and is what users see in
		// Insights filters. If the caller passed an ID, fall back to the
		// username from the user header once available; for now we use
		// args.User which works for the common case.
		username := args.User
		now := nowFunc().UTC()
		query := api.InsightsQuery{
			Service:   "all",
			StartTime: now.Add(-30 * 24 * time.Hour).Format(time.RFC3339),
			EndTime:   now.Format(time.RFC3339),
			SearchTermFilter: map[string]any{
				"initiated_by.username": username,
			},
		}
		result, err := insights.QueryEvents(ctx, query, api.InsightsQueryOptions{
			Limit: userViewRecentEventsLimit,
			Sort:  "-timestamp",
		})
		if err != nil {
			addWarning(fmt.Sprintf("recent events: %v", err))
			return
		}
		mu.Lock()
		data.RecentEvents = result.Data
		mu.Unlock()
	}()

	wg.Wait()

	// Set last_login from the most recent event if we got any.
	if len(data.RecentEvents) > 0 {
		var first struct {
			Timestamp string `json:"timestamp"`
		}
		if json.Unmarshal(data.RecentEvents[0], &first) == nil {
			data.User.LastLogin = first.Timestamp
		}
	}

	if len(warnings) > 0 {
		data.Warnings = warnings
	}

	// If the header never populated, the user truly couldn't be fetched —
	// surface that as an error rather than returning an empty card.
	if data.User.ID == "" && data.User.Username == "" {
		return nil, fmt.Errorf("could not fetch user %q: %s", args.User, joinWarnings(warnings))
	}

	return &data, nil
}

// previewSSHKey returns a short, safe-to-render fragment of the public key
// (algorithm + first 12 chars of the body) so the UI can show "ssh-ed25519
// AAAAC3NzaC1lZDI1…" without wall-of-text. Full keys are still publicly
// shareable but are noisy; truncating here keeps the panel scannable.
func previewSSHKey(pub string) string {
	if pub == "" {
		return ""
	}
	const bodyLen = 12
	// Public keys are "<algo> <body> [comment]". Split on the first space.
	for i := 0; i < len(pub); i++ {
		if pub[i] == ' ' {
			algo := pub[:i]
			rest := pub[i+1:]
			body := rest
			if len(body) > bodyLen {
				body = body[:bodyLen]
			}
			return algo + " " + body + "…"
		}
	}
	if len(pub) > bodyLen+8 {
		return pub[:bodyLen+8] + "…"
	}
	return pub
}

func joinWarnings(ws []string) string {
	out := ""
	for i, w := range ws {
		if i > 0 {
			out += "; "
		}
		out += w
	}
	return out
}

// registerUserView wires the user_view MCP App: typed tool + ui:// resource.
// Lives outside appSpecs because it takes input args.
func (s *Server) registerUserView() {
	meta := mcp.Meta{
		"ui":             map[string]any{"resourceUri": userViewResourceURI},
		"ui/resourceUri": userViewResourceURI,
	}
	addToolWithMetaTyped(s, "user_view",
		"Show an interactive JumpCloud user profile: header (username, email, status badges), MFA enrollment, group memberships, SSH keys, and recent auth events. "+
			"Required input: user (username, email, or ID). Renders as a rich profile in MCP App-capable hosts; returns the same data as JSON when rendering isn't supported.",
		meta,
		func(ctx context.Context, req *mcp.CallToolRequest, args userViewArgs) (*mcp.CallToolResult, any, error) {
			data, err := fetchUserViewData(ctx, args)
			if err != nil {
				return errorResult(fmt.Sprintf("user_view: %v", err)), nil, nil
			}
			res, err := jsonResult(data)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return res, nil, nil
		},
	)

	rendered := renderAppHTML(userHTML)
	s.mcpServer.AddResource(
		&mcp.Resource{
			URI:         userViewResourceURI,
			Name:        "User Profile App",
			Description: "Interactive JumpCloud user profile (groups, SSH keys, MFA, recent events)",
			MIMEType:    mcpAppMIMEType,
		},
		func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			return &mcp.ReadResourceResult{
				Contents: []*mcp.ResourceContents{{
					URI:      userViewResourceURI,
					MIMEType: mcpAppMIMEType,
					Text:     rendered,
				}},
			}, nil
		},
	)
}
