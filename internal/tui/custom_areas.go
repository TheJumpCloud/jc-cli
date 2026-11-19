package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/klaassen-consulting/jc/internal/accesscert"
	"github.com/klaassen-consulting/jc/internal/agents"
	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/config"
	"github.com/klaassen-consulting/jc/internal/googleemm"
	"github.com/klaassen-consulting/jc/internal/identityrisk"
	"github.com/klaassen-consulting/jc/internal/passwordvault"
	"github.com/klaassen-consulting/jc/internal/privaccess"
	"github.com/klaassen-consulting/jc/internal/pwm"
	"github.com/klaassen-consulting/jc/internal/schema"
	"github.com/klaassen-consulting/jc/internal/workday"
)

// ListFunc lists one resource through the same package code the CLI uses,
// for areas whose API the generic endpoint fetch cannot read: cursor paging,
// a required header, an envelope per area, an activation gate, a required
// time window. It returns the rows and the server's count.
type ListFunc func(ctx context.Context, c *api.V2Client) ([]json.RawMessage, int, error)

// customAreas are schema resources the generic BuildRegistry loop must not
// build: each has several sub-resources and its own read path, so it is
// built by customAreaEntries instead. Before this, all eight fell through to
// the generic loop with no list endpoint, landed under Settings with their
// raw key as a name, and showed "No results" on an org with data.
var customAreas = map[string]bool{
	"agents": true, "agent-groups": true, "access-cert": true, "google-emm": true,
	"identity-risk": true, "password-manager": true, "password-vault": true, "workday": true,
}

// readOnlyEntry builds a browsable, read-only entry served by fn. Detail is
// taken from the listing, matched on idField.
func readOnlyEntry(key, name string, cat Category, idField, nameField string,
	fields []string, fn ListFunc) ResourceEntry {
	return ResourceEntry{
		Key: key, DisplayName: name, Category: cat, ClientType: ClientV2, ListFunc: fn,
		Schema: schema.ResourceSchema{
			Resource: key, APIVersion: "v2", Verbs: []string{"list", "get"},
			DefaultFields: fields, IDField: idField, NameField: nameField,
		},
	}
}

func subMenu(key, name string, cat Category, children ...ResourceEntry) ResourceEntry {
	return ResourceEntry{Key: key, DisplayName: name, Category: cat, SubMenu: children}
}

// agentsClient adds the x-org-id header the agents API requires and does not
// document.
func agentsClient(c *api.V2Client) *api.V2Client {
	if org := config.OrgID(); org != "" {
		c.SetExtraHeaders(http.Header{agents.OrgHeader: []string{org}})
	}
	return c
}

// envelopeList reads an endpoint whose rows sit under key, with an optional
// count field.
func envelopeList(ctx context.Context, c *api.V2Client, endpoint, key, countKey string) ([]json.RawMessage, int, error) {
	raw, err := c.Get(ctx, endpoint)
	if err != nil {
		return nil, 0, err
	}
	var env map[string]json.RawMessage
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, 0, fmt.Errorf("the %s response is not an object: %w", endpoint, err)
	}
	var rows []json.RawMessage
	if arr, ok := env[key]; ok && string(arr) != "null" {
		if err := json.Unmarshal(arr, &rows); err != nil {
			return nil, 0, fmt.Errorf("%q in the %s response is not an array: %w", key, endpoint, err)
		}
	}
	total := len(rows)
	if countKey != "" {
		if n, ok := env[countKey]; ok {
			_ = json.Unmarshal(n, &total)
		}
	}
	return rows, total, nil
}

// vaultList reads a Password Vault listing behind its activation gate, and
// passes every row through the PAM redaction and personal-field strip (one
// backend): the TUI shows rows on screen and can export them.
func vaultList(endpoint, what string) ListFunc {
	return func(ctx context.Context, c *api.V2Client) ([]json.RawMessage, int, error) {
		raw, err := c.Get(ctx, passwordvault.StatusEndpoint)
		if err != nil {
			return nil, 0, err
		}
		st, err := passwordvault.ParseStatus(raw)
		if err != nil {
			return nil, 0, err
		}
		if !st.IsActive {
			return nil, 0, passwordvault.ErrNotActivated()
		}
		if raw, err = c.Get(ctx, endpoint); err != nil {
			return nil, 0, err
		}
		rows, total, err := passwordvault.ParseList(raw, what)
		if err != nil {
			return nil, 0, err
		}
		rows, err = redactRows(rows)
		return rows, total, err
	}
}

func redactRows(rows []json.RawMessage) ([]json.RawMessage, error) {
	rows, err := privaccess.StripPersonalFields(rows)
	if err != nil {
		return nil, err
	}
	for i, r := range rows {
		if rows[i], err = privaccess.Redact(r); err != nil {
			return nil, err
		}
	}
	return rows, nil
}

func pwmList(endpoint string) ListFunc {
	return func(ctx context.Context, c *api.V2Client) ([]json.RawMessage, int, error) {
		raw, err := c.Get(ctx, endpoint)
		if err != nil {
			// A 404 here means Password Manager is off. Say why, as the CLI
			// does, rather than let the list show "No results": on an org
			// that has moved to Password Vault the records are there.
			var apiErr *api.APIError
			if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound &&
				pwm.VaultActive(ctx, c.Get) {
				return nil, 0, pwm.MigrationHint(endpoint)
			}
			return nil, 0, err
		}
		return pwm.ParseList(raw)
	}
}

// riskWindow is the window the identity-risk aggregates require; the TUI
// uses the last 30 days.
func riskWindow() url.Values {
	now := time.Now().UTC()
	v := url.Values{}
	v.Set("start_time", now.AddDate(0, 0, -30).Format(time.RFC3339))
	v.Set("end_time", now.Format(time.RFC3339))
	return v
}

func customAreaEntries() []ResourceEntry {
	return []ResourceEntry{
		readOnlyEntry("agents", "AI Agents", CategoryAccess, "id", "name",
			[]string{"name", "email", "state", "ownerId", "id"},
			func(ctx context.Context, c *api.V2Client) ([]json.RawMessage, int, error) {
				return agents.ListAgents(ctx, agentsClient(c), nil)
			}),
		readOnlyEntry("agent-groups", "AI Agent Groups", CategoryAccess, "id", "name",
			[]string{"name", "description", "id"},
			func(ctx context.Context, c *api.V2Client) ([]json.RawMessage, int, error) {
				rows, err := agents.ListGroups(ctx, agentsClient(c), nil, 0, "")
				return rows, len(rows), err
			}),
		subMenu("access-cert", "Access Certification", CategorySecurity,
			readOnlyEntry("access-cert-campaigns", "Campaigns", CategorySecurity, "id", "name",
				[]string{"name", "status", "enabled", "scopeType", "scheduleMode", "lastRunAt"},
				func(ctx context.Context, c *api.V2Client) ([]json.RawMessage, int, error) {
					p, err := accesscert.ListAll(ctx, c, accesscert.CampaignsEndpoint, accesscert.Query{})
					if err != nil {
						return nil, 0, err
					}
					return p.Results, p.TotalCount, nil
				}),
			readOnlyEntry("access-cert-runs", "Campaign Runs", CategorySecurity, "runId", "campaignName",
				[]string{"campaignName", "runNumber", "status", "reviewerType", "reviewProgressPercent", "activatedAt", "closedAt"},
				func(ctx context.Context, c *api.V2Client) ([]json.RawMessage, int, error) {
					p, err := accesscert.ListRuns(ctx, c, "", accesscert.Query{})
					if err != nil {
						return nil, 0, err
					}
					return p.Results, p.TotalCount, nil
				}),
		),
		readOnlyEntry("google-emm", "Google EMM (Android)", CategoryDeviceMgmt, "objectId", "displayName",
			[]string{"displayName", "enterpriseType", "allowDeviceEnrollment", "objectId"},
			func(ctx context.Context, c *api.V2Client) ([]json.RawMessage, int, error) {
				return envelopeList(ctx, c, googleemm.EnterprisesEndpoint, "enterprises", "count")
			}),
		subMenu("identity-risk", "Identity Risk", CategorySecurity,
			readOnlyEntry("identity-risk-events", "Risk Detections (latest 100)", CategorySecurity, "objectId", "identityDisplayName",
				[]string{"level", "score", "identityDisplayName", "applicationDisplayName", "resolutionStatus", "lastOccurrenceAt"},
				func(ctx context.Context, c *api.V2Client) ([]json.RawMessage, int, error) {
					raw, err := c.Get(ctx, identityrisk.EventsEndpoint+"?limit=100")
					if err != nil {
						return nil, 0, err
					}
					return identityrisk.ParseEvents(raw)
				}),
			readOnlyEntry("identity-risk-identities", "Risky Identities (30 days)", CategorySecurity, "identityObjectId", "displayName",
				[]string{"displayName", "identifier", "count", "identityObjectId"},
				func(ctx context.Context, c *api.V2Client) ([]json.RawMessage, int, error) {
					raw, err := c.Get(ctx, identityrisk.IdentitiesEndpoint+"?"+riskWindow().Encode())
					if err != nil {
						return nil, 0, err
					}
					return identityrisk.ParseIdentities(raw)
				}),
		),
		subMenu("password-manager", "Password Manager", CategoryAccess,
			readOnlyEntry("password-manager-users", "Users", CategoryAccess, "id", "email",
				[]string{"name", "username", "email", "status", "id"}, pwmList(pwm.UsersEndpoint)),
			readOnlyEntry("password-manager-groups", "Groups", CategoryAccess, "id", "name",
				[]string{"name", "id"}, pwmList(pwm.GroupsEndpoint)),
		),
		subMenu("password-vault", "Password Vault", CategoryAccess,
			readOnlyEntry("password-vault-credentials", "Credentials (no secrets)", CategoryAccess, "id", "name",
				[]string{"name", "credentialType", "username", "folderName", "archived"},
				vaultList(passwordvault.CredentialsEndpoint, "credentials")),
			readOnlyEntry("password-vault-websites", "Websites", CategoryAccess, "id", "name",
				[]string{"name", "uri", "folderName", "tags", "archived"},
				vaultList(passwordvault.WebsitesEndpoint, "websites")),
			readOnlyEntry("password-vault-folders", "Folders", CategoryAccess, "id", "name",
				[]string{"name", "description", "isPrivate"},
				vaultList(passwordvault.FoldersEndpoint, "folders")),
			readOnlyEntry("password-vault-users", "Users", CategoryAccess, "id", "userName",
				[]string{"userName", "emailAddress", "name", "surname", "isActive"},
				vaultList(passwordvault.UsersEndpoint, "Password Vault users")),
		),
		readOnlyEntry("workday", "Workday", CategoryUserMgmt, "id", "name",
			[]string{"name", "id"},
			func(ctx context.Context, c *api.V2Client) ([]json.RawMessage, int, error) {
				raw, err := c.Get(ctx, workday.Endpoint)
				if err != nil {
					return nil, 0, err
				}
				rows, err := workday.ParseList(raw)
				return rows, len(rows), err
			}),
	}
}

// MatchRows keeps the rows in which any value contains term, case-blind.
// Areas served by a ListFunc have no server-side search.
func MatchRows(rows []json.RawMessage, term string) []json.RawMessage {
	term = strings.ToLower(strings.TrimSpace(term))
	if term == "" {
		return rows
	}
	out := rows[:0:0]
	for _, r := range rows {
		var m map[string]any
		if json.Unmarshal(r, &m) != nil {
			continue
		}
		for _, v := range m {
			if v != nil && strings.Contains(strings.ToLower(fmt.Sprint(v)), term) {
				out = append(out, r)
				break
			}
		}
	}
	return out
}

// RowID reads idField from a row as a string.
func RowID(row json.RawMessage, idField string) string {
	var m map[string]json.RawMessage
	if json.Unmarshal(row, &m) != nil {
		return ""
	}
	return strings.Trim(string(m[idField]), `"`)
}
