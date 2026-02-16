package fetch

import (
	"encoding/json"
	"sync"
	"time"
)

// CacheEntry holds cached data with expiry.
type CacheEntry struct {
	Data      []json.RawMessage
	FetchedAt time.Time
	TTL       time.Duration
}

// IsExpired returns true if the entry has exceeded its TTL.
func (e *CacheEntry) IsExpired() bool {
	return time.Since(e.FetchedAt) > e.TTL
}

// Cache is an in-memory session cache for API responses.
type Cache struct {
	mu      sync.RWMutex
	entries map[string]*CacheEntry
}

// NewCache creates an empty cache.
func NewCache() *Cache {
	return &Cache{entries: make(map[string]*CacheEntry)}
}

// Get retrieves a cached entry if it exists and hasn't expired.
func (c *Cache) Get(key string) ([]json.RawMessage, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.entries[key]
	if !ok || entry.IsExpired() {
		return nil, false
	}
	return entry.Data, true
}

// Set stores data in the cache with the given TTL.
func (c *Cache) Set(key string, data []json.RawMessage, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = &CacheEntry{
		Data:      data,
		FetchedAt: time.Now(),
		TTL:       ttl,
	}
}

// Invalidate removes a specific key from the cache.
func (c *Cache) Invalidate(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, key)
}

// Clear removes all entries from the cache.
func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]*CacheEntry)
}
