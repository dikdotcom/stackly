package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/dikdotcom/stackly/internal/metrics"
	"github.com/dikdotcom/stackly/internal/scanner"
)

// CacheEntry represents a cached scan result
type CacheEntry struct {
	URL       string             `json:"url"`
	Result    *scanner.ScanResult `json:"result"`
	CachedAt  time.Time          `json:"cached_at"`
	ExpiresAt time.Time          `json:"expires_at"`
	UserAgent string             `json:"user_agent"`
}

// Cache is a 24h result cache with optional file persistence
type Cache struct {
	mu      sync.RWMutex
	entries map[string]*CacheEntry
	ttl     time.Duration
	path    string
	maxSize int
	hits    int64
	misses  int64
}

// NewCache creates a new cache with given TTL and optional persistence path
func NewCache(ttl time.Duration, persistPath string) *Cache {
	c := &Cache{
		entries: make(map[string]*CacheEntry),
		ttl:     ttl,
		path:    persistPath,
		maxSize: 5000,
	}
	if persistPath != "" {
		c.load()
	}
	return c
}

// Get retrieves a cached result for a URL (returns nil if not cached or expired)
func (c *Cache) Get(url string) *CacheEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()

	key := urlKey(url)
	entry, ok := c.entries[key]
	if !ok {
		c.misses++
		return nil
	}
	if time.Now().After(entry.ExpiresAt) {
		c.misses++
		return nil
	}

	c.hits++
	return entry
}

// Set stores a scan result in the cache
func (c *Cache) Set(url string, result *scanner.ScanResult, ua string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	key := urlKey(url)

	entry := &CacheEntry{
		URL:       url,
		Result:    result,
		CachedAt:  now,
		ExpiresAt: now.Add(c.ttl),
		UserAgent: ua,
	}

	c.entries[key] = entry

	// Enforce max size (LRU-ish: remove oldest)
	if len(c.entries) > c.maxSize {
		c.evictOldest()
	}

	if c.path != "" {
		c.persistLocked()
	}

	metrics.CacheSize.Set(float64(len(c.entries)))
}

// evictOldest removes the oldest expired or least-recent entries
func (c *Cache) evictOldest() {
	// First pass: remove expired
	now := time.Now()
	for k, e := range c.entries {
		if now.After(e.ExpiresAt) {
			delete(c.entries, k)
		}
	}

	// If still over limit, remove oldest
	if len(c.entries) > c.maxSize {
		var oldestKey string
		var oldestTime time.Time
		first := true
		for k, e := range c.entries {
			if first || e.CachedAt.Before(oldestTime) {
				oldestKey = k
				oldestTime = e.CachedAt
				first = false
			}
		}
		if oldestKey != "" {
			delete(c.entries, oldestKey)
		}
	}
}

// Stats returns cache statistics
func (c *Cache) Stats() Stats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	valid := 0
	for _, e := range c.entries {
		if time.Now().Before(e.ExpiresAt) {
			valid++
		}
	}

	total := c.hits + c.misses
	var hitRate float64
	if total > 0 {
		hitRate = float64(c.hits) / float64(total)
	}

	return Stats{
		Size:    len(c.entries),
		Valid:   valid,
		Hits:    c.hits,
		Misses:  c.misses,
		HitRate: hitRate,
		TTL:     c.ttl,
	}
}

// Stats contains cache statistics
type Stats struct {
	Size    int           `json:"size"`
	Valid   int           `json:"valid"`
	Hits    int64         `json:"hits"`
	Misses  int64         `json:"misses"`
	HitRate float64       `json:"hit_rate"`
	TTL     time.Duration `json:"ttl"`
}

// Clear removes all entries from the cache
func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]*CacheEntry)
	if c.path != "" {
		c.persistLocked()
	}
	metrics.CacheSize.Set(0)
	metrics.CacheInvalidations.Inc()
}

// CleanupExpired removes expired entries
func (c *Cache) CleanupExpired() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	removed := 0
	for k, e := range c.entries {
		if now.After(e.ExpiresAt) {
			delete(c.entries, k)
			removed++
		}
	}
	if removed > 0 && c.path != "" {
		c.persistLocked()
	}
	return removed
}

// persistLocked saves cache to disk (must hold lock)
func (c *Cache) persistLocked() {
	if c.path == "" {
		return
	}
	dir := filepath.Dir(c.path)
	os.MkdirAll(dir, 0755)

	// Only persist valid entries to keep file small
	valid := make(map[string]*CacheEntry)
	now := time.Now()
	for k, e := range c.entries {
		if now.Before(e.ExpiresAt) {
			valid[k] = e
		}
	}

	data, err := json.Marshal(valid)
	if err != nil {
		return
	}
	os.WriteFile(c.path, data, 0644)
}

// load reads cache from disk
func (c *Cache) load() {
	if c.path == "" {
		return
	}
	data, err := os.ReadFile(c.path)
	if err != nil {
		return
	}
	var loaded map[string]*CacheEntry
	if err := json.Unmarshal(data, &loaded); err != nil {
		return
	}

	// Only keep non-expired entries
	now := time.Now()
	for k, e := range loaded {
		if now.Before(e.ExpiresAt) {
			c.entries[k] = e
		}
	}
}

// urlKey creates a normalized cache key from a URL
func urlKey(url string) string {
	// Normalize URL: lowercase scheme/host, strip trailing slash
	normalized := normalizeURL(url)
	hash := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(hash[:16])
}

// normalizeURL normalizes a URL for cache key generation
func normalizeURL(url string) string {
	// Simple normalization: strip fragment, lowercase host, strip trailing slash
	out := url
	for len(out) > 0 && out[len(out)-1] == '/' {
		out = out[:len(out)-1]
	}
	return out
}