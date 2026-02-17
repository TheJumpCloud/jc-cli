package fetch

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/filter"
)

// Default TTLs for cache entries.
const (
	ListTTL   = 30 * time.Second
	DetailTTL = 60 * time.Second
)

// Generation is a monotonically increasing counter used to discard stale responses.
var generation atomic.Int64

// NextGeneration returns a new unique generation value.
func NextGeneration() int64 {
	return generation.Add(1)
}

// ListResultMsg is sent when a list fetch completes.
type ListResultMsg struct {
	ResourceKey string
	Data        []json.RawMessage
	TotalCount  int
	Generation  int64
	Err         error
}

// DetailResultMsg is sent when a detail fetch completes.
type DetailResultMsg struct {
	ResourceKey string
	ID          string
	Data        json.RawMessage
	Generation  int64
	Err         error
}

// V1ClientFactory creates a V1 client.
type V1ClientFactory func() (*api.V1Client, error)

// V2ClientFactory creates a V2 client.
type V2ClientFactory func() (*api.V2Client, error)

// InsightsClientFactory creates an Insights client.
type InsightsClientFactory func() (*api.InsightsClient, error)

// Fetcher handles async data fetching for the TUI.
type Fetcher struct {
	Cache          *Cache
	NewV1Client    V1ClientFactory
	NewV2Client    V2ClientFactory
	NewInsights    InsightsClientFactory
}

// NewFetcher creates a Fetcher with default client factories.
func NewFetcher() *Fetcher {
	return &Fetcher{
		Cache:       NewCache(),
		NewV1Client: api.NewV1Client,
		NewV2Client: api.NewV2Client,
		NewInsights: api.NewInsightsClient,
	}
}

// FetchV1List fetches a V1 list as a tea.Cmd.
func (f *Fetcher) FetchV1List(resourceKey, endpoint string, opts api.ListOptions, gen int64) tea.Cmd {
	return func() tea.Msg {
		cacheKey := fmt.Sprintf("v1:%s:%s:%v", resourceKey, endpoint, opts)

		if data, ok := f.Cache.Get(cacheKey); ok {
			return ListResultMsg{
				ResourceKey: resourceKey,
				Data:        data,
				TotalCount:  len(data),
				Generation:  gen,
			}
		}

		client, err := f.NewV1Client()
		if err != nil {
			return ListResultMsg{ResourceKey: resourceKey, Generation: gen, Err: err}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		result, err := client.ListAll(ctx, endpoint, opts)
		if err != nil {
			return ListResultMsg{ResourceKey: resourceKey, Generation: gen, Err: err}
		}

		f.Cache.Set(cacheKey, result.Data, ListTTL)

		return ListResultMsg{
			ResourceKey: resourceKey,
			Data:        result.Data,
			TotalCount:  result.TotalCount,
			Generation:  gen,
		}
	}
}

// FetchV1Search uses POST /search/ endpoints for more powerful case-insensitive
// multi-field search. Only users and devices have these endpoints.
func (f *Fetcher) FetchV1Search(resourceKey, searchEndpoint, term string, fields []string, sort string, filters []filter.Expression, gen int64) tea.Cmd {
	return func() tea.Msg {
		cacheKey := fmt.Sprintf("v1search:%s:%s:%s:%v", resourceKey, searchEndpoint, term, filters)

		if data, ok := f.Cache.Get(cacheKey); ok {
			return ListResultMsg{
				ResourceKey: resourceKey,
				Data:        data,
				TotalCount:  len(data),
				Generation:  gen,
			}
		}

		client, err := f.NewV1Client()
		if err != nil {
			return ListResultMsg{ResourceKey: resourceKey, Generation: gen, Err: err}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		searchBody := map[string]any{
			"searchFilter": map[string]any{
				"searchTerm": term,
				"fields":     fields,
			},
		}
		if len(filters) > 0 {
			searchBody["filter"] = filter.ToV1Queries(filters)
		}

		result, err := client.Search(ctx, searchEndpoint, searchBody, api.SearchOptions{Sort: sort})
		if err != nil {
			return ListResultMsg{ResourceKey: resourceKey, Generation: gen, Err: err}
		}

		f.Cache.Set(cacheKey, result.Data, ListTTL)

		return ListResultMsg{
			ResourceKey: resourceKey,
			Data:        result.Data,
			TotalCount:  result.TotalCount,
			Generation:  gen,
		}
	}
}

// FetchV2List fetches a V2 list as a tea.Cmd.
func (f *Fetcher) FetchV2List(resourceKey, endpoint string, opts api.V2ListOptions, gen int64) tea.Cmd {
	return func() tea.Msg {
		cacheKey := fmt.Sprintf("v2:%s:%s:%v", resourceKey, endpoint, opts)

		if data, ok := f.Cache.Get(cacheKey); ok {
			return ListResultMsg{
				ResourceKey: resourceKey,
				Data:        data,
				TotalCount:  len(data),
				Generation:  gen,
			}
		}

		client, err := f.NewV2Client()
		if err != nil {
			return ListResultMsg{ResourceKey: resourceKey, Generation: gen, Err: err}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		result, err := client.ListAll(ctx, endpoint, opts)
		if err != nil {
			return ListResultMsg{ResourceKey: resourceKey, Generation: gen, Err: err}
		}

		f.Cache.Set(cacheKey, result.Data, ListTTL)

		return ListResultMsg{
			ResourceKey: resourceKey,
			Data:        result.Data,
			TotalCount:  len(result.Data),
			Generation:  gen,
		}
	}
}

// InsightsResultMsg is sent when an Insights query completes.
type InsightsResultMsg struct {
	ResourceKey string
	Data        []json.RawMessage
	Generation  int64
	Err         error
}

// FetchInsightsList fetches Insights events as a tea.Cmd.
func (f *Fetcher) FetchInsightsList(resourceKey string, query api.InsightsQuery, opts api.InsightsQueryOptions, gen int64) tea.Cmd {
	return func() tea.Msg {
		client, err := f.NewInsights()
		if err != nil {
			return InsightsResultMsg{ResourceKey: resourceKey, Generation: gen, Err: err}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		result, err := client.QueryEvents(ctx, query, opts)
		if err != nil {
			return InsightsResultMsg{ResourceKey: resourceKey, Generation: gen, Err: err}
		}

		return InsightsResultMsg{
			ResourceKey: resourceKey,
			Data:        result.Data,
			Generation:  gen,
		}
	}
}

// AssociationsResultMsg is sent when an associations fetch completes.
type AssociationsResultMsg struct {
	ResourceKey string
	TargetType  string
	Data        []json.RawMessage
	Generation  int64
	Err         error
}

// FetchAssociations fetches V2 graph associations for a resource.
func (f *Fetcher) FetchAssociations(resourceKey, graphEndpoint, id, targetType string, gen int64) tea.Cmd {
	return func() tea.Msg {
		client, err := f.NewV2Client()
		if err != nil {
			return AssociationsResultMsg{ResourceKey: resourceKey, TargetType: targetType, Generation: gen, Err: err}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		endpoint := fmt.Sprintf("%s/%s/associations?targets=%s", graphEndpoint, id, targetType)
		result, err := client.ListAll(ctx, endpoint, api.V2ListOptions{})
		if err != nil {
			return AssociationsResultMsg{ResourceKey: resourceKey, TargetType: targetType, Generation: gen, Err: err}
		}

		// Flatten nested {"to":{"type":"...","id":"..."}} to top-level.
		flattened := make([]json.RawMessage, 0, len(result.Data))
		for _, item := range result.Data {
			flat := flattenAssociation(item)
			flattened = append(flattened, flat)
		}

		return AssociationsResultMsg{
			ResourceKey: resourceKey,
			TargetType:  targetType,
			Data:        flattened,
			Generation:  gen,
		}
	}
}

// FetchMembership fetches group members via the dedicated membership endpoint.
// User groups use /usergroups/{id}/members, device groups use /systemgroups/{id}/membership.
// The response is a bare array of {id, type, ...} objects (no "to" wrapper).
func (f *Fetcher) FetchMembership(resourceKey, memberEndpoint, id, memberType string, gen int64) tea.Cmd {
	return func() tea.Msg {
		client, err := f.NewV2Client()
		if err != nil {
			return AssociationsResultMsg{ResourceKey: resourceKey, TargetType: memberType, Generation: gen, Err: err}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		endpoint := fmt.Sprintf("%s/%s/%s", memberEndpoint, id, "members")
		if memberType == "system" {
			endpoint = fmt.Sprintf("%s/%s/%s", memberEndpoint, id, "membership")
		}
		result, err := client.ListAll(ctx, endpoint, api.V2ListOptions{})
		if err != nil {
			return AssociationsResultMsg{ResourceKey: resourceKey, TargetType: memberType, Generation: gen, Err: err}
		}

		// Inject "type" field so the association table can display and drill-down.
		enriched := make([]json.RawMessage, 0, len(result.Data))
		for _, item := range result.Data {
			var obj map[string]json.RawMessage
			if err := json.Unmarshal(item, &obj); err != nil {
				enriched = append(enriched, item)
				continue
			}
			typeBytes, _ := json.Marshal(memberType)
			obj["type"] = typeBytes
			out, _ := json.Marshal(obj)
			enriched = append(enriched, out)
		}

		return AssociationsResultMsg{
			ResourceKey: resourceKey,
			TargetType:  memberType,
			Data:        enriched,
			Generation:  gen,
		}
	}
}

// flattenAssociation extracts "to.type" and "to.id" to top level.
func flattenAssociation(data json.RawMessage) json.RawMessage {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		return data
	}

	toRaw, ok := obj["to"]
	if !ok {
		return data
	}

	var to map[string]json.RawMessage
	if err := json.Unmarshal(toRaw, &to); err != nil {
		return data
	}

	result := make(map[string]json.RawMessage)
	// Copy non-"to" fields.
	for k, v := range obj {
		if k != "to" {
			result[k] = v
		}
	}
	// Promote "to" sub-fields.
	for k, v := range to {
		result[k] = v
	}

	out, err := json.Marshal(result)
	if err != nil {
		return data
	}
	return out
}

// FetchV1Detail fetches a single V1 resource.
func (f *Fetcher) FetchV1Detail(resourceKey, endpoint, id string, gen int64) tea.Cmd {
	return func() tea.Msg {
		client, err := f.NewV1Client()
		if err != nil {
			return DetailResultMsg{ResourceKey: resourceKey, ID: id, Generation: gen, Err: err}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		fullEndpoint := fmt.Sprintf("%s/%s", endpoint, id)
		data, err := client.Get(ctx, fullEndpoint)
		if err != nil {
			return DetailResultMsg{ResourceKey: resourceKey, ID: id, Generation: gen, Err: err}
		}

		return DetailResultMsg{
			ResourceKey: resourceKey,
			ID:          id,
			Data:        data,
			Generation:  gen,
		}
	}
}

// FetchV2Detail fetches a single V2 resource.
func (f *Fetcher) FetchV2Detail(resourceKey, endpoint, id string, gen int64) tea.Cmd {
	return func() tea.Msg {
		client, err := f.NewV2Client()
		if err != nil {
			return DetailResultMsg{ResourceKey: resourceKey, ID: id, Generation: gen, Err: err}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		fullEndpoint := fmt.Sprintf("%s/%s", endpoint, id)
		data, err := client.Get(ctx, fullEndpoint)
		if err != nil {
			return DetailResultMsg{ResourceKey: resourceKey, ID: id, Generation: gen, Err: err}
		}

		return DetailResultMsg{
			ResourceKey: resourceKey,
			ID:          id,
			Data:        data,
			Generation:  gen,
		}
	}
}

// AssocNameReq describes a single resource whose name should be resolved.
type AssocNameReq struct {
	ID        string
	V1        bool   // true for V1 client, false for V2
	Endpoint  string // list endpoint (e.g. "/policies")
	NameField string // JSON field containing the name (e.g. "name")
}

// AssocNamesResolvedMsg carries resolved id→name mappings.
type AssocNamesResolvedMsg struct {
	Names      map[string]string // id → resolved name
	Generation int64
}

// ResolveAssocNames concurrently fetches resources by ID to extract their names.
func (f *Fetcher) ResolveAssocNames(reqs []AssocNameReq, gen int64) tea.Cmd {
	return func() tea.Msg {
		names := make(map[string]string)
		var mu sync.Mutex
		var wg sync.WaitGroup
		sem := make(chan struct{}, 10) // concurrency limit

		for _, req := range reqs {
			wg.Add(1)
			go func(r AssocNameReq) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				ep := fmt.Sprintf("%s/%s", r.Endpoint, r.ID)
				var data json.RawMessage
				if r.V1 {
					client, err := f.NewV1Client()
					if err != nil {
						return
					}
					data, _ = client.Get(ctx, ep)
				} else {
					client, err := f.NewV2Client()
					if err != nil {
						return
					}
					data, _ = client.Get(ctx, ep)
				}

				if data == nil {
					return
				}
				name := extractNameField(data, r.NameField)
				if name != "" {
					mu.Lock()
					names[r.ID] = name
					mu.Unlock()
				}
			}(req)
		}

		wg.Wait()
		return AssocNamesResolvedMsg{Names: names, Generation: gen}
	}
}

// extractNameField extracts a string field from a JSON object.
func extractNameField(data json.RawMessage, field string) string {
	if field == "" {
		return ""
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		return ""
	}
	raw, ok := obj[field]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}
