package plugins

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/cockroachdb/errors"
)

const (
	hangarAPIBase = "https://hangar.papermc.io/api/v1"
)

// HangarClient implements PluginClient for the Hangar API.
type HangarClient struct {
	httpClient *http.Client
	baseURL    string
}

// NewHangarClient creates a new Hangar API client.
func NewHangarClient() *HangarClient {
	return &HangarClient{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		baseURL: hangarAPIBase,
	}
}

// hangarVersion represents a version from Hangar API.
type hangarVersion struct {
	Name         string                 `json:"name"`
	CreatedAt    string                 `json:"createdAt"`
	Downloads    map[string]interface{} `json:"downloads"`
	PlatformDeps map[string][]string    `json:"platformDependencies"`
	PluginDeps   map[string]interface{} `json:"pluginDependencies"`
	FileInfo     hangarFileInfo         `json:"fileInfo"`
	ReviewState  string                 `json:"reviewState"`
	Visibility   string                 `json:"visibility"`
	Stats        interface{}            `json:"stats"`
	Author       string                 `json:"author"`
	Channel      interface{}            `json:"channel"`
	PinnedStatus string                 `json:"pinnedStatus"`
	ExternalURL  string                 `json:"externalUrl"`
	Description  string                 `json:"description"`
	Platforms    []string               `json:"platforms"`
	GameVersions []string               `json:"gameVersions,omitempty"`
}

// hangarFileInfo contains file metadata.
type hangarFileInfo struct {
	Name        string `json:"name"`
	SizeBytes   int64  `json:"sizeBytes"`
	Sha256Hash  string `json:"sha256Hash"`
	DownloadURL string `json:"downloadUrl,omitempty"`
}

// hangarVersionsResponse represents the versions list response.
type hangarVersionsResponse struct {
	Result []hangarVersion `json:"result"`
}

// GetVersions retrieves all available versions for a plugin from Hangar.
func (c *HangarClient) GetVersions(ctx context.Context, project string) ([]PluginVersion, error) {
	versionsResp, err := c.fetchHangarVersions(ctx, project)
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch versions")
	}

	return c.convertHangarVersions(project, versionsResp.Result), nil
}

// fetchHangarVersions fetches raw version data from Hangar API.
func (c *HangarClient) fetchHangarVersions(
	ctx context.Context,
	project string,
) (*hangarVersionsResponse, error) {
	url := fmt.Sprintf("%s/projects/%s/versions", c.baseURL, project)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create request")
	}

	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "failed to execute request")
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, errors.Newf("hangar API returned status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read response body")
	}

	var versionsResp hangarVersionsResponse
	if err := json.Unmarshal(body, &versionsResp); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal response")
	}

	return &versionsResp, nil
}

// convertHangarVersions converts Hangar API response to PluginVersion structs.
func (c *HangarClient) convertHangarVersions(project string, hangarVersions []hangarVersion) []PluginVersion {
	versions := make([]PluginVersion, 0, len(hangarVersions))

	for _, hv := range hangarVersions {
		releaseDate, err := time.Parse(time.RFC3339, hv.CreatedAt)
		if err != nil {
			releaseDate = time.Now()
		}

		var paperVersions []string
		if deps, ok := hv.PlatformDeps["PAPER"]; ok {
			paperVersions = deps
		}

		downloadURL := fmt.Sprintf("%s/projects/%s/versions/%s/PAPER/download",
			c.baseURL, project, hv.Name)

		versions = append(versions, PluginVersion{
			Version:           hv.Name,
			ReleaseDate:       releaseDate,
			PaperVersions:     paperVersions,
			MinecraftVersions: hv.GameVersions,
			DownloadURL:       downloadURL,
			Hash:              hv.FileInfo.Sha256Hash,
		})
	}

	return versions
}

// GetCompatibility retrieves compatibility information for a specific version.
func (c *HangarClient) GetCompatibility(
	ctx context.Context,
	project,
	version string,
) (CompatibilityInfo, error) {
	url := fmt.Sprintf("%s/projects/%s/versions/%s", c.baseURL, project, version)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return CompatibilityInfo{}, errors.Wrap(err, "failed to create request")
	}

	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return CompatibilityInfo{}, errors.Wrap(err, "failed to execute request")
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return CompatibilityInfo{}, errors.Newf("hangar API returned status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return CompatibilityInfo{}, errors.Wrap(err, "failed to read response body")
	}

	var hv hangarVersion
	if err := json.Unmarshal(body, &hv); err != nil {
		return CompatibilityInfo{}, errors.Wrap(err, "failed to unmarshal response")
	}

	var paperVersions []string
	if deps, ok := hv.PlatformDeps["PAPER"]; ok {
		paperVersions = deps
	}

	return CompatibilityInfo{
		MinecraftVersions: hv.GameVersions,
		PaperVersions:     paperVersions,
	}, nil
}
