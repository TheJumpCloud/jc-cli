package fetch

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/klaassen-consulting/jc/internal/api"
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
