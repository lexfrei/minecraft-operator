package plugins

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/cockroachdb/errors"
)

// cacheEntry represents a cached value with expiration.
type cacheEntry struct {
	versions   []PluginVersion
	compat     CompatibilityInfo
	expiresAt  time.Time
	isVersions bool
}

// CachedPluginClient wraps a PluginClient with in-memory caching.
type CachedPluginClient struct {
	client PluginClient
	cache  map[string]*cacheEntry
	mu     sync.RWMutex
	ttl    time.Duration
}

// NewCachedPluginClient creates a new caching wrapper around a PluginClient.
func NewCachedPluginClient(client PluginClient, ttl time.Duration) *CachedPluginClient {
	return &CachedPluginClient{
		client: client,
		cache:  make(map[string]*cacheEntry),
		ttl:    ttl,
	}
}

// GetVersions retrieves versions from cache or fetches from the underlying client.
func (c *CachedPluginClient) GetVersions(ctx context.Context, project string) ([]PluginVersion, error) {
	cacheKey := fmt.Sprintf("versions:%s", project)

	// Check cache first
	c.mu.RLock()
	entry, exists := c.cache[cacheKey]
	c.mu.RUnlock()

	if exists && entry.isVersions && time.Now().Before(entry.expiresAt) {
		// Cache hit
		return entry.versions, nil
	}

	// Cache miss or expired, fetch from client
	versions, err := c.client.GetVersions(ctx, project)
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch versions")
	}

	// Update cache
	c.mu.Lock()
	c.cache[cacheKey] = &cacheEntry{
		versions:   versions,
		expiresAt:  time.Now().Add(c.ttl),
		isVersions: true,
	}
	c.mu.Unlock()

	return versions, nil
}

// GetCompatibility retrieves compatibility info from cache or fetches from the underlying client.
func (c *CachedPluginClient) GetCompatibility(
	ctx context.Context,
	project,
	version string,
) (CompatibilityInfo, error) {
	cacheKey := fmt.Sprintf("compat:%s:%s", project, version)

	// Check cache first
	c.mu.RLock()
	entry, exists := c.cache[cacheKey]
	c.mu.RUnlock()

	if exists && !entry.isVersions && time.Now().Before(entry.expiresAt) {
		// Cache hit
		return entry.compat, nil
	}

	// Cache miss or expired, fetch from client
	compat, err := c.client.GetCompatibility(ctx, project, version)
	if err != nil {
		return CompatibilityInfo{}, errors.Wrap(err, "failed to fetch compatibility")
	}

	// Update cache
	c.mu.Lock()
	c.cache[cacheKey] = &cacheEntry{
		compat:     compat,
		expiresAt:  time.Now().Add(c.ttl),
		isVersions: false,
	}
	c.mu.Unlock()

	return compat, nil
}

// ClearCache removes all entries from the cache.
func (c *CachedPluginClient) ClearCache() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache = make(map[string]*cacheEntry)
}

// ClearProject removes cache entries for a specific project.
func (c *CachedPluginClient) ClearProject(project string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Remove all entries related to this project
	for key := range c.cache {
		// Check if key starts with "versions:project" or "compat:project"
		versionKey := fmt.Sprintf("versions:%s", project)
		compatPrefix := fmt.Sprintf("compat:%s:", project)

		if key == versionKey || len(key) >= len(compatPrefix) && key[:len(compatPrefix)] == compatPrefix {
			delete(c.cache, key)
		}
	}
}
