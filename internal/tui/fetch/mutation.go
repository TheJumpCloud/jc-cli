package fetch

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// MutationResultMsg is sent when a create/update/delete completes.
type MutationResultMsg struct {
	ResourceKey string
	Data        json.RawMessage // nil for delete
	Generation  int64
	Err         error
}

// DeleteV1 deletes a V1 resource and invalidates the cache.
func (f *Fetcher) DeleteV1(resourceKey, endpoint, id string, gen int64) tea.Cmd {
	return func() tea.Msg {
		client, err := f.NewV1Client()
		if err != nil {
			return MutationResultMsg{ResourceKey: resourceKey, Generation: gen, Err: err}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		_, err = client.Delete(ctx, fmt.Sprintf("%s/%s", endpoint, id))
		if err != nil {
			return MutationResultMsg{ResourceKey: resourceKey, Generation: gen, Err: err}
		}

		f.Cache.InvalidateResource(resourceKey)

		return MutationResultMsg{ResourceKey: resourceKey, Generation: gen}
	}
}

// DeleteV2 deletes a V2 resource and invalidates the cache.
func (f *Fetcher) DeleteV2(resourceKey, endpoint, id string, gen int64) tea.Cmd {
	return func() tea.Msg {
		client, err := f.NewV2Client()
		if err != nil {
			return MutationResultMsg{ResourceKey: resourceKey, Generation: gen, Err: err}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		_, err = client.Delete(ctx, fmt.Sprintf("%s/%s", endpoint, id))
		if err != nil {
			return MutationResultMsg{ResourceKey: resourceKey, Generation: gen, Err: err}
		}

		f.Cache.InvalidateResource(resourceKey)

		return MutationResultMsg{ResourceKey: resourceKey, Generation: gen}
	}
}

// CreateV1 creates a V1 resource and invalidates the cache.
func (f *Fetcher) CreateV1(resourceKey, endpoint string, body map[string]any, gen int64) tea.Cmd {
	return func() tea.Msg {
		client, err := f.NewV1Client()
		if err != nil {
			return MutationResultMsg{ResourceKey: resourceKey, Generation: gen, Err: err}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		data, err := client.Create(ctx, endpoint, body)
		if err != nil {
			return MutationResultMsg{ResourceKey: resourceKey, Generation: gen, Err: err}
		}

		f.Cache.InvalidateResource(resourceKey)

		return MutationResultMsg{ResourceKey: resourceKey, Data: data, Generation: gen}
	}
}

// CreateV2 creates a V2 resource and invalidates the cache.
func (f *Fetcher) CreateV2(resourceKey, endpoint string, body map[string]any, gen int64) tea.Cmd {
	return func() tea.Msg {
		client, err := f.NewV2Client()
		if err != nil {
			return MutationResultMsg{ResourceKey: resourceKey, Generation: gen, Err: err}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		data, err := client.Create(ctx, endpoint, body)
		if err != nil {
			return MutationResultMsg{ResourceKey: resourceKey, Generation: gen, Err: err}
		}

		f.Cache.InvalidateResource(resourceKey)

		return MutationResultMsg{ResourceKey: resourceKey, Data: data, Generation: gen}
	}
}

// UpdateV1 updates a V1 resource and invalidates the cache.
func (f *Fetcher) UpdateV1(resourceKey, endpoint, id string, body map[string]any, gen int64) tea.Cmd {
	return func() tea.Msg {
		client, err := f.NewV1Client()
		if err != nil {
			return MutationResultMsg{ResourceKey: resourceKey, Generation: gen, Err: err}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		data, err := client.Update(ctx, fmt.Sprintf("%s/%s", endpoint, id), body)
		if err != nil {
			return MutationResultMsg{ResourceKey: resourceKey, Generation: gen, Err: err}
		}

		f.Cache.InvalidateResource(resourceKey)

		return MutationResultMsg{ResourceKey: resourceKey, Data: data, Generation: gen}
	}
}

// UpdateV2 updates a V2 resource and invalidates the cache.
func (f *Fetcher) UpdateV2(resourceKey, endpoint, id string, body map[string]any, gen int64) tea.Cmd {
	return func() tea.Msg {
		client, err := f.NewV2Client()
		if err != nil {
			return MutationResultMsg{ResourceKey: resourceKey, Generation: gen, Err: err}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		data, err := client.Update(ctx, fmt.Sprintf("%s/%s", endpoint, id), body)
		if err != nil {
			return MutationResultMsg{ResourceKey: resourceKey, Generation: gen, Err: err}
		}

		f.Cache.InvalidateResource(resourceKey)

		return MutationResultMsg{ResourceKey: resourceKey, Data: data, Generation: gen}
	}
}
