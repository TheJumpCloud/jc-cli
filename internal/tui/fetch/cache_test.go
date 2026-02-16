package fetch

import (
	"encoding/json"
	"testing"
	"time"
)

func TestCache_SetAndGet(t *testing.T) {
	c := NewCache()
	data := []json.RawMessage{json.RawMessage(`{"id":"1"}`)}
	c.Set("test", data, 10*time.Second)

	got, ok := c.Get("test")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if len(got) != 1 || string(got[0]) != `{"id":"1"}` {
		t.Errorf("got %v, want %v", got, data)
	}
}

func TestCache_Miss(t *testing.T) {
	c := NewCache()
	_, ok := c.Get("nonexistent")
	if ok {
		t.Error("expected cache miss")
	}
}

func TestCache_Expired(t *testing.T) {
	c := NewCache()
	data := []json.RawMessage{json.RawMessage(`{"id":"1"}`)}
	c.Set("test", data, 1*time.Millisecond)

	time.Sleep(2 * time.Millisecond)

	_, ok := c.Get("test")
	if ok {
		t.Error("expected cache miss for expired entry")
	}
}

func TestCache_Invalidate(t *testing.T) {
	c := NewCache()
	c.Set("test", []json.RawMessage{json.RawMessage(`{}`)}, 10*time.Second)
	c.Invalidate("test")

	_, ok := c.Get("test")
	if ok {
		t.Error("expected cache miss after invalidate")
	}
}

func TestCache_Clear(t *testing.T) {
	c := NewCache()
	c.Set("a", []json.RawMessage{json.RawMessage(`{}`)}, 10*time.Second)
	c.Set("b", []json.RawMessage{json.RawMessage(`{}`)}, 10*time.Second)
	c.Clear()

	if _, ok := c.Get("a"); ok {
		t.Error("expected miss for 'a' after clear")
	}
	if _, ok := c.Get("b"); ok {
		t.Error("expected miss for 'b' after clear")
	}
}

func TestCacheEntry_IsExpired(t *testing.T) {
	entry := &CacheEntry{
		Data:      nil,
		FetchedAt: time.Now().Add(-10 * time.Second),
		TTL:       5 * time.Second,
	}
	if !entry.IsExpired() {
		t.Error("entry should be expired")
	}

	entry.FetchedAt = time.Now()
	entry.TTL = 10 * time.Second
	if entry.IsExpired() {
		t.Error("entry should not be expired")
	}
}

func TestNextGeneration_Monotonic(t *testing.T) {
	g1 := NextGeneration()
	g2 := NextGeneration()
	g3 := NextGeneration()

	if g2 <= g1 || g3 <= g2 {
		t.Errorf("generations should be monotonically increasing: %d, %d, %d", g1, g2, g3)
	}
}
