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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lexfrei/go-hangar/pkg/hangar"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestHangarClient creates a HangarClient pointing to a test server.
func newTestHangarClient(serverURL string, httpClient *http.Client) *HangarClient {
	client := hangar.NewClient(hangar.Config{
		BaseURL:    serverURL,
		HTTPClient: httpClient,
		Timeout:    5 * time.Second,
	})

	return &HangarClient{
		client: client,
	}
}

// --- Test fixtures ---

func makeTestProject(slug string) hangar.Project {
	return hangar.Project{
		ID:   1,
		Name: "Test Plugin",
		Namespace: hangar.Namespace{
			Owner: "TestOwner",
			Slug:  slug,
		},
		Category:    "gameplay",
		Description: "A test plugin",
	}
}

func makeTestVersion(name string, gameVersions, paperVersions []string, downloadURL, hash string) hangar.Version {
	downloads := map[string]hangar.DownloadInfo{}
	if downloadURL != "" || hash != "" {
		fileInfo := &hangar.FileInfo{}
		if hash != "" {
			fileInfo.SHA256Hash = hash
		}
		downloads["PAPER"] = hangar.DownloadInfo{
			DownloadURL: downloadURL,
			FileInfo:    fileInfo,
		}
	}

	platformDeps := map[string][]string{}
	if len(paperVersions) > 0 {
		platformDeps["PAPER"] = paperVersions
	}

	return hangar.Version{
		ID:                   1,
		Name:                 name,
		GameVersions:         gameVersions,
		PlatformDependencies: platformDeps,
		Downloads:            downloads,
		CreatedAt:            time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

func makeVersionsList(versions ...hangar.Version) hangar.VersionsList {
	return hangar.VersionsList{
		Pagination: hangar.Pagination{
			Limit:  500,
			Offset: 0,
			Count:  int64(len(versions)),
		},
		Result: versions,
	}
}

// --- GetVersions tests ---

func TestHangarClient_GetVersions_Success(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/projects/test-plugin":
			// GetProject call
			project := makeTestProject("test-plugin")
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(project)
		case "/projects/TestOwner/test-plugin/versions":
			// ListVersions call
			versions := makeVersionsList(
				makeTestVersion("1.0.0", []string{"1.21.1"}, []string{"1.21.1"}, "https://example.com/v1.jar", "abc123"),
				makeTestVersion("1.1.0", []string{"1.21.1", "1.21.2"}, []string{"1.21.1", "1.21.2"},
					"https://example.com/v2.jar", "def456"),
			)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(versions)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newTestHangarClient(server.URL, server.Client())

	versions, err := client.GetVersions(context.Background(), "test-plugin")

	require.NoError(t, err)
	require.Len(t, versions, 2)

	// Check first version
	assert.Equal(t, "1.0.0", versions[0].Version)
	assert.Equal(t, []string{"1.21.1"}, versions[0].MinecraftVersions)
	assert.Equal(t, []string{"1.21.1"}, versions[0].PaperVersions)
	assert.Equal(t, "https://example.com/v1.jar", versions[0].DownloadURL)
	assert.Equal(t, "abc123", versions[0].Hash)

	// Check second version
	assert.Equal(t, "1.1.0", versions[1].Version)
	assert.Len(t, versions[1].MinecraftVersions, 2)
}

func TestHangarClient_GetVersions_EmptyResult(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/projects/empty-plugin":
			project := makeTestProject("empty-plugin")
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(project)
		case "/projects/TestOwner/empty-plugin/versions":
			versions := makeVersionsList() // Empty
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(versions)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newTestHangarClient(server.URL, server.Client())

	versions, err := client.GetVersions(context.Background(), "empty-plugin")

	require.NoError(t, err)
	assert.Empty(t, versions)
}

func TestHangarClient_GetVersions_ExternalURLFallback(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/projects/external-plugin":
			project := makeTestProject("external-plugin")
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(project)
		case "/projects/TestOwner/external-plugin/versions":
			// Version with ExternalURL instead of DownloadURL
			version := hangar.Version{
				ID:           1,
				Name:         "1.0.0",
				GameVersions: []string{"1.21.1"},
				Downloads: map[string]hangar.DownloadInfo{
					"PAPER": {
						ExternalURL: "https://github.com/example/plugin/releases/v1.0.0.jar",
						FileInfo:    &hangar.FileInfo{SHA256Hash: "ext123"},
					},
				},
				CreatedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			}
			versions := makeVersionsList(version)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(versions)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newTestHangarClient(server.URL, server.Client())

	versions, err := client.GetVersions(context.Background(), "external-plugin")

	require.NoError(t, err)
	require.Len(t, versions, 1)
	assert.Equal(t, "https://github.com/example/plugin/releases/v1.0.0.jar", versions[0].DownloadURL)
	assert.Equal(t, "ext123", versions[0].Hash)
}

func TestHangarClient_GetVersions_GameVersionsFallback(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/projects/fallback-plugin":
			project := makeTestProject("fallback-plugin")
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(project)
		case "/projects/TestOwner/fallback-plugin/versions":
			// Version with empty GameVersions - should fall back to PlatformDependencies.PAPER
			version := hangar.Version{
				ID:           1,
				Name:         "1.0.0",
				GameVersions: nil, // Empty!
				PlatformDependencies: map[string][]string{
					"PAPER": {"1.21.1", "1.21.2"},
				},
				Downloads: map[string]hangar.DownloadInfo{
					"PAPER": {
						DownloadURL: "https://example.com/plugin.jar",
					},
				},
				CreatedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			}
			versions := makeVersionsList(version)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(versions)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newTestHangarClient(server.URL, server.Client())

	versions, err := client.GetVersions(context.Background(), "fallback-plugin")

	require.NoError(t, err)
	require.Len(t, versions, 1)
	// Should use PlatformDependencies.PAPER as fallback
	assert.Equal(t, []string{"1.21.1", "1.21.2"}, versions[0].MinecraftVersions)
	assert.Equal(t, []string{"1.21.1", "1.21.2"}, versions[0].PaperVersions)
}

func TestHangarClient_GetVersions_ProjectNotFound(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	client := newTestHangarClient(server.URL, server.Client())

	versions, err := client.GetVersions(context.Background(), "nonexistent")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get project")
	assert.Nil(t, versions)
}

func TestHangarClient_GetVersions_APIError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/projects/error-plugin":
			project := makeTestProject("error-plugin")
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(project)
		case "/projects/TestOwner/error-plugin/versions":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newTestHangarClient(server.URL, server.Client())

	versions, err := client.GetVersions(context.Background(), "error-plugin")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to list versions")
	assert.Nil(t, versions)
}

func TestHangarClient_GetVersions_NoPaperDownloads(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/projects/no-paper-plugin":
			project := makeTestProject("no-paper-plugin")
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(project)
		case "/projects/TestOwner/no-paper-plugin/versions":
			// Version without PAPER downloads (e.g., only Velocity)
			version := hangar.Version{
				ID:           1,
				Name:         "1.0.0",
				GameVersions: []string{"1.21.1"},
				Downloads: map[string]hangar.DownloadInfo{
					"VELOCITY": {
						DownloadURL: "https://example.com/velocity.jar",
					},
				},
				CreatedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			}
			versions := makeVersionsList(version)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(versions)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newTestHangarClient(server.URL, server.Client())

	versions, err := client.GetVersions(context.Background(), "no-paper-plugin")

	require.NoError(t, err)
	require.Len(t, versions, 1)
	// Should have empty download URL and hash for PAPER
	assert.Empty(t, versions[0].DownloadURL)
	assert.Empty(t, versions[0].Hash)
}

// --- GetCompatibility tests ---

func TestHangarClient_GetCompatibility_Success(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/projects/test-plugin/versions/1.0.0" {
			version := makeTestVersion("1.0.0", []string{"1.21.1", "1.21.2"}, []string{"1.21.1", "1.21.2"}, "", "")
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(version)
		} else {
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newTestHangarClient(server.URL, server.Client())

	compat, err := client.GetCompatibility(context.Background(), "test-plugin", "1.0.0")

	require.NoError(t, err)
	assert.Equal(t, []string{"1.21.1", "1.21.2"}, compat.MinecraftVersions)
	assert.Equal(t, []string{"1.21.1", "1.21.2"}, compat.PaperVersions)
}

func TestHangarClient_GetCompatibility_VersionNotFound(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	client := newTestHangarClient(server.URL, server.Client())

	compat, err := client.GetCompatibility(context.Background(), "test-plugin", "nonexistent")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get version")
	assert.Empty(t, compat.MinecraftVersions)
}

func TestHangarClient_GetCompatibility_FallbackVersions(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/projects/test-plugin/versions/1.0.0" {
			// Version with empty GameVersions
			version := hangar.Version{
				ID:           1,
				Name:         "1.0.0",
				GameVersions: nil, // Empty
				PlatformDependencies: map[string][]string{
					"PAPER": {"1.21.1", "1.21.2"},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(version)
		} else {
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newTestHangarClient(server.URL, server.Client())

	compat, err := client.GetCompatibility(context.Background(), "test-plugin", "1.0.0")

	require.NoError(t, err)
	// Should fallback to PlatformDependencies.PAPER
	assert.Equal(t, []string{"1.21.1", "1.21.2"}, compat.MinecraftVersions)
	assert.Equal(t, []string{"1.21.1", "1.21.2"}, compat.PaperVersions)
}

// --- Constructor test ---

func TestNewHangarClient_CreatesClient(t *testing.T) {
	t.Parallel()

	client := NewHangarClient()

	assert.NotNil(t, client)
	assert.NotNil(t, client.client)
}

// --- External URL validation test ---

func TestHangarClient_GetVersions_ExternalURL_GitHubReleasePage(t *testing.T) {
	t.Parallel()

	// Bug 12: ExternalURL pointing to GitHub release PAGE (not JAR)
	// should either be resolved to an actual JAR download URL
	// or marked as non-downloadable.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/projects/github-plugin":
			project := makeTestProject("github-plugin")
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(project)
		case "/projects/TestOwner/github-plugin/versions":
			version := hangar.Version{
				ID:           1,
				Name:         "2.21.2",
				GameVersions: []string{"1.21.8"},
				Downloads: map[string]hangar.DownloadInfo{
					"PAPER": {
						// This is a release PAGE, not a direct JAR download!
						ExternalURL: "https://github.com/EssentialsX/Essentials/releases/tags/2.21.2",
					},
				},
				CreatedAt: time.Date(2025, 8, 3, 0, 0, 0, 0, time.UTC),
			}
			versions := makeVersionsList(version)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(versions)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newTestHangarClient(server.URL, server.Client())
	versions, err := client.GetVersions(context.Background(), "github-plugin")

	require.NoError(t, err)
	require.Len(t, versions, 1)

	// The download URL should either:
	// a) be empty (not downloadable via simple curl), or
	// b) be resolved to an actual JAR asset URL
	// Currently it returns the release page URL, which is wrong.
	assert.NotEqual(t,
		"https://github.com/EssentialsX/Essentials/releases/tags/2.21.2",
		versions[0].DownloadURL,
		"ExternalURL pointing to GitHub release page should NOT be used as DownloadURL")
}
