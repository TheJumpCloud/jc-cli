package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/klaassen-consulting/jc/internal/api"
)

// maxAgentPages bounds a full read.
//
// The loop's real terminating condition is an absent nextPageToken, which the
// server does supply on the last page. This bound is for the case where it
// stops doing that: an endpoint that keeps handing back a token would
// otherwise spin forever, appending rows, until the context is cancelled or
// the process runs out of memory. At MaxAgentsPageSize this allows 100,000
// agents, which is far past any plausible tenant.
const maxAgentPages = 1000

// AgentsPage is one page of the agents listing.
type AgentsPage struct {
	Rows []json.RawMessage
	// TotalSize is the server's count of matches, which is not len(Rows)
	// whenever a page size is in play.
	TotalSize int
	// NextPageToken is empty on the last page.
	NextPageToken string
}

// GetAgentsPage reads one page of /agents.
//
// filters are already-validated field:op:value expressions; see
// ValidateAgentFilter. Nothing here passes limit, skip or sort: /agents
// ignores all three in silence, and a surface that sent them would be
// offering an operator a knob that does nothing.
func GetAgentsPage(ctx context.Context, v2 *api.V2Client, filters []string, pageSize int, pageToken string) (AgentsPage, error) {
	q := AgentsPageQuery(pageSize, pageToken)
	for _, f := range filters {
		q.Add("filter", f)
	}

	raw, err := v2.Get(ctx, WithQuery(AgentsEndpoint, q))
	if err != nil {
		return AgentsPage{}, err
	}
	rows, total, next, err := ParseAgents(raw)
	if err != nil {
		return AgentsPage{}, err
	}
	return AgentsPage{Rows: rows, TotalSize: total, NextPageToken: next}, nil
}

// ListAgents reads every page of /agents by following nextPageToken.
//
// This deliberately does not use api.V2Client.ListAll. ListAll follows a Link
// header and, when there is none, falls back to incrementing skip whenever a
// page came back full. /agents emits no Link header and ignores skip, so that
// fallback would re-request the first page indefinitely, appending the same
// rows each time — an unbounded duplicating read rather than a truncated one.
// See AgentsPagingIsCursorOnly.
func ListAgents(ctx context.Context, v2 *api.V2Client, filters []string) ([]json.RawMessage, int, error) {
	var (
		all   []json.RawMessage
		total int
		token string
	)
	seen := map[string]bool{}

	for page := 0; ; page++ {
		if page >= maxAgentPages {
			return nil, 0, fmt.Errorf("the agents listing did not stop paging after %d pages "+
				"(%d records so far): refusing to keep reading rather than return a result "+
				"that may repeat records", maxAgentPages, len(all))
		}
		if err := ctx.Err(); err != nil {
			return nil, 0, err
		}

		p, err := GetAgentsPage(ctx, v2, filters, MaxAgentsPageSize, token)
		if err != nil {
			return nil, 0, err
		}
		all = append(all, p.Rows...)
		total = p.TotalSize

		if p.NextPageToken == "" {
			break
		}
		if seen[p.NextPageToken] {
			// A cursor that repeats would otherwise be an infinite loop
			// returning duplicates. Reporting it beats both.
			return nil, 0, fmt.Errorf("the agents listing returned a repeating page cursor "+
				"after %d records: jc stopped rather than return duplicates", len(all))
		}
		seen[p.NextPageToken] = true
		token = p.NextPageToken
	}
	return all, total, nil
}

// ListGroups reads /agentgroups, which pages by offset and emits a bare
// array — both of which the generic v2 list helper already handles.
func ListGroups(ctx context.Context, v2 *api.V2Client, filters []string, limit int, sortBy string) ([]json.RawMessage, error) {
	opts := api.V2ListOptions{Limit: limit, Filter: filters, Sort: sortBy}
	result, err := v2.ListAll(ctx, GroupsEndpoint, opts)
	if err != nil {
		return nil, err
	}
	return result.Data, nil
}

// GetBareArray reads one of the four association endpoints, each of which
// returns a bare array small enough to come back whole.
func GetBareArray(ctx context.Context, v2 *api.V2Client, endpoint string, q url.Values) (json.RawMessage, error) {
	return v2.Get(ctx, WithQuery(endpoint, q))
}
