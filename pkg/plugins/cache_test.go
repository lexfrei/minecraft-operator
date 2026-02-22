/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package plugins

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockPluginClient implements PluginClient for testing.
type MockPluginClient struct {
	mu                  sync.Mutex
	GetVersionsCalls    int
	GetCompatCalls      int
	GetVersionsResult   []PluginVersion
	GetVersionsError    error
	GetCompatResult     CompatibilityInfo
	GetCompatError      error
	GetVersionsProjects []string
	GetCompatProjects   []string
	GetCompatVersions   []string
}

func (m *MockPluginClient) GetVersions(ctx context.Context, project string) ([]PluginVersion, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.GetVersionsCalls++
	m.GetVersionsProjects = append(m.GetVersionsProjects, project)

	if m.GetVersionsError != nil {
		return nil, m.GetVersionsError
	}

	return m.GetVersionsResult, nil
}

func (m *MockPluginClient) GetCompatibility(
	ctx context.Context,
	project,
	version string,
) (CompatibilityInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.GetCompatCalls++
	m.GetCompatProjects = append(m.GetCompatProjects, project)
	m.GetCompatVersions = append(m.GetCompatVersions, version)

	if m.GetCompatError != nil {
		return CompatibilityInfo{}, m.GetCompatError
	}

	return m.GetCompatResult, nil
}

func (m *MockPluginClient) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.GetVersionsCalls = 0
	m.GetCompatCalls = 0
	m.GetVersionsProjects = nil
	m.GetCompatProjects = nil
	m.GetCompatVersions = nil
	m.GetVersionsError = nil
	m.GetCompatError = nil
}

// --- Test fixtures ---

func testVersions() []PluginVersion {
	return []PluginVersion{
		{
			Version:           "1.0.0",
			ReleaseDate:       time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			MinecraftVersions: []string{"1.21.1"},
			PaperVersions:     []string{"1.21.1"},
			DownloadURL:       "https://example.com/plugin-1.0.0.jar",
			Hash:              "abc123",
		},
		{
			Version:           "1.1.0",
			ReleaseDate:       time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC),
			MinecraftVersions: []string{"1.21.1", "1.21.2"},
			PaperVersions:     []string{"1.21.1", "1.21.2"},
			DownloadURL:       "https://example.com/plugin-1.1.0.jar",
			Hash:              "def456",
		},
	}
}

func testCompatInfo() CompatibilityInfo {
	return CompatibilityInfo{
		MinecraftVersions: []string{"1.21.1", "1.21.2"},
		PaperVersions:     []string{"1.21.1", "1.21.2"},
	}
}

// --- GetVersions tests ---

func TestCachedPluginClient_GetVersions_CacheMiss(t *testing.T) {
	t.Parallel()

	mock := &MockPluginClient{GetVersionsResult: testVersions()}
	client := NewCachedPluginClient(mock, time.Hour)

	versions, err := client.GetVersions(context.Background(), "test-plugin")

	require.NoError(t, err)
	assert.Len(t, versions, 2)
	assert.Equal(t, "1.0.0", versions[0].Version)
	assert.Equal(t, 1, mock.GetVersionsCalls, "should call underlying client on cache miss")
}

func TestCachedPluginClient_GetVersions_CacheHit(t *testing.T) {
	t.Parallel()

	mock := &MockPluginClient{GetVersionsResult: testVersions()}
	client := NewCachedPluginClient(mock, time.Hour)

	// First call - cache miss
	_, err := client.GetVersions(context.Background(), "test-plugin")
	require.NoError(t, err)
	assert.Equal(t, 1, mock.GetVersionsCalls)

	// Second call - should be cache hit
	versions, err := client.GetVersions(context.Background(), "test-plugin")
	require.NoError(t, err)
	assert.Len(t, versions, 2)
	assert.Equal(t, 1, mock.GetVersionsCalls, "should NOT call underlying client on cache hit")
}

func TestCachedPluginClient_GetVersions_CacheExpiry(t *testing.T) {
	t.Parallel()

	mock := &MockPluginClient{GetVersionsResult: testVersions()}
	// Use very short TTL
	client := NewCachedPluginClient(mock, 10*time.Millisecond)

	// First call
	_, err := client.GetVersions(context.Background(), "test-plugin")
	require.NoError(t, err)
	assert.Equal(t, 1, mock.GetVersionsCalls)

	// Wait for cache to expire
	time.Sleep(20 * time.Millisecond)

	// Second call - should hit underlying after expiry
	_, err = client.GetVersions(context.Background(), "test-plugin")
	require.NoError(t, err)
	assert.Equal(t, 2, mock.GetVersionsCalls, "should call underlying client after TTL expires")
}

func TestCachedPluginClient_GetVersions_UnderlyingError(t *testing.T) {
	t.Parallel()

	expectedErr := errors.New("network error")
	mock := &MockPluginClient{GetVersionsError: expectedErr}
	client := NewCachedPluginClient(mock, time.Hour)

	versions, err := client.GetVersions(context.Background(), "test-plugin")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "network error")
	assert.Nil(t, versions)
	assert.Equal(t, 1, mock.GetVersionsCalls)
}

func TestCachedPluginClient_GetVersions_DifferentProjects(t *testing.T) {
	t.Parallel()

	mock := &MockPluginClient{GetVersionsResult: testVersions()}
	client := NewCachedPluginClient(mock, time.Hour)

	// Call for project A
	_, err := client.GetVersions(context.Background(), "project-a")
	require.NoError(t, err)

	// Call for project B - different cache key
	_, err = client.GetVersions(context.Background(), "project-b")
	require.NoError(t, err)

	// Both should have triggered underlying calls
	assert.Equal(t, 2, mock.GetVersionsCalls)
	assert.Contains(t, mock.GetVersionsProjects, "project-a")
	assert.Contains(t, mock.GetVersionsProjects, "project-b")
}

// --- GetCompatibility tests ---

func TestCachedPluginClient_GetCompatibility_CacheMiss(t *testing.T) {
	t.Parallel()

	mock := &MockPluginClient{GetCompatResult: testCompatInfo()}
	client := NewCachedPluginClient(mock, time.Hour)

	compat, err := client.GetCompatibility(context.Background(), "test-plugin", "1.0.0")

	require.NoError(t, err)
	assert.Len(t, compat.MinecraftVersions, 2)
	assert.Equal(t, 1, mock.GetCompatCalls, "should call underlying client on cache miss")
}

func TestCachedPluginClient_GetCompatibility_CacheHit(t *testing.T) {
	t.Parallel()

	mock := &MockPluginClient{GetCompatResult: testCompatInfo()}
	client := NewCachedPluginClient(mock, time.Hour)

	// First call
	_, err := client.GetCompatibility(context.Background(), "test-plugin", "1.0.0")
	require.NoError(t, err)
	assert.Equal(t, 1, mock.GetCompatCalls)

	// Second call - cache hit
	compat, err := client.GetCompatibility(context.Background(), "test-plugin", "1.0.0")
	require.NoError(t, err)
	assert.Len(t, compat.MinecraftVersions, 2)
	assert.Equal(t, 1, mock.GetCompatCalls, "should NOT call underlying client on cache hit")
}

func TestCachedPluginClient_GetCompatibility_DifferentVersions(t *testing.T) {
	t.Parallel()

	mock := &MockPluginClient{GetCompatResult: testCompatInfo()}
	client := NewCachedPluginClient(mock, time.Hour)

	// Call for version 1.0.0
	_, err := client.GetCompatibility(context.Background(), "test-plugin", "1.0.0")
	require.NoError(t, err)

	// Call for version 1.1.0 - different cache key
	_, err = client.GetCompatibility(context.Background(), "test-plugin", "1.1.0")
	require.NoError(t, err)

	assert.Equal(t, 2, mock.GetCompatCalls)
}

func TestCachedPluginClient_GetCompatibility_SeparateFromVersions(t *testing.T) {
	t.Parallel()

	mock := &MockPluginClient{
		GetVersionsResult: testVersions(),
		GetCompatResult:   testCompatInfo(),
	}
	client := NewCachedPluginClient(mock, time.Hour)

	// Get versions
	_, err := client.GetVersions(context.Background(), "test-plugin")
	require.NoError(t, err)

	// Get compatibility - should NOT be cached (different key)
	_, err = client.GetCompatibility(context.Background(), "test-plugin", "1.0.0")
	require.NoError(t, err)

	assert.Equal(t, 1, mock.GetVersionsCalls)
	assert.Equal(t, 1, mock.GetCompatCalls, "versions and compat should use separate cache keys")
}

func TestCachedPluginClient_GetCompatibility_UnderlyingError(t *testing.T) {
	t.Parallel()

	expectedErr := errors.New("API error")
	mock := &MockPluginClient{GetCompatError: expectedErr}
	client := NewCachedPluginClient(mock, time.Hour)

	compat, err := client.GetCompatibility(context.Background(), "test-plugin", "1.0.0")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "API error")
	assert.Empty(t, compat.MinecraftVersions)
}

// --- ClearCache tests ---

func TestCachedPluginClient_ClearCache_ClearsAll(t *testing.T) {
	t.Parallel()

	mock := &MockPluginClient{
		GetVersionsResult: testVersions(),
		GetCompatResult:   testCompatInfo(),
	}
	client := NewCachedPluginClient(mock, time.Hour)

	// Populate cache
	_, _ = client.GetVersions(context.Background(), "project-a")
	_, _ = client.GetVersions(context.Background(), "project-b")
	_, _ = client.GetCompatibility(context.Background(), "project-a", "1.0.0")
	assert.Equal(t, 2, mock.GetVersionsCalls)
	assert.Equal(t, 1, mock.GetCompatCalls)

	// Clear cache
	client.ClearCache()

	// All calls should hit underlying again
	_, _ = client.GetVersions(context.Background(), "project-a")
	_, _ = client.GetVersions(context.Background(), "project-b")
	_, _ = client.GetCompatibility(context.Background(), "project-a", "1.0.0")

	assert.Equal(t, 4, mock.GetVersionsCalls, "all version requests should hit underlying after clear")
	assert.Equal(t, 2, mock.GetCompatCalls, "all compat requests should hit underlying after clear")
}

// --- ClearProject tests ---

func TestCachedPluginClient_ClearProject_ClearsOnlyProject(t *testing.T) {
	t.Parallel()

	mock := &MockPluginClient{
		GetVersionsResult: testVersions(),
		GetCompatResult:   testCompatInfo(),
	}
	client := NewCachedPluginClient(mock, time.Hour)

	// Populate cache for two projects
	_, _ = client.GetVersions(context.Background(), "project-a")
	_, _ = client.GetVersions(context.Background(), "project-b")
	_, _ = client.GetCompatibility(context.Background(), "project-a", "1.0.0")
	_, _ = client.GetCompatibility(context.Background(), "project-b", "1.0.0")
	assert.Equal(t, 2, mock.GetVersionsCalls)
	assert.Equal(t, 2, mock.GetCompatCalls)

	// Clear only project-a
	client.ClearProject("project-a")

	// Project-a should hit underlying
	_, _ = client.GetVersions(context.Background(), "project-a")
	_, _ = client.GetCompatibility(context.Background(), "project-a", "1.0.0")

	// Project-b should still be cached
	_, _ = client.GetVersions(context.Background(), "project-b")
	_, _ = client.GetCompatibility(context.Background(), "project-b", "1.0.0")

	assert.Equal(t, 3, mock.GetVersionsCalls, "only project-a versions should hit underlying")
	assert.Equal(t, 3, mock.GetCompatCalls, "only project-a compat should hit underlying")
}

func TestCachedPluginClient_ClearProject_NonExistent(t *testing.T) {
	t.Parallel()

	mock := &MockPluginClient{GetVersionsResult: testVersions()}
	client := NewCachedPluginClient(mock, time.Hour)

	// Populate cache
	_, _ = client.GetVersions(context.Background(), "project-a")

	// Clear non-existent project - should not panic
	client.ClearProject("non-existent")

	// Project-a should still be cached
	_, _ = client.GetVersions(context.Background(), "project-a")
	assert.Equal(t, 1, mock.GetVersionsCalls)
}

// --- Concurrency tests ---

func TestCachedPluginClient_Concurrency_Safe(t *testing.T) {
	t.Parallel()

	mock := &MockPluginClient{
		GetVersionsResult: testVersions(),
		GetCompatResult:   testCompatInfo(),
	}
	client := NewCachedPluginClient(mock, time.Hour)

	var wg sync.WaitGroup
	var successCount atomic.Int32

	// Run concurrent operations
	for i := range 100 {
		wg.Add(3)

		go func() {
			defer wg.Done()
			_, err := client.GetVersions(context.Background(), "project")
			if err == nil {
				successCount.Add(1)
			}
		}()

		go func() {
			defer wg.Done()
			_, err := client.GetCompatibility(context.Background(), "project", "1.0.0")
			if err == nil {
				successCount.Add(1)
			}
		}()

		go func() {
			defer wg.Done()
			if i%10 == 0 {
				client.ClearCache()
			}
		}()
	}

	wg.Wait()

	// All non-clear operations should succeed
	assert.GreaterOrEqual(t, successCount.Load(), int32(180), "most operations should succeed")
}
