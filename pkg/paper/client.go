// Package paper provides client for PaperMC API.
package paper

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/cockroachdb/errors"
)

const (
	paperAPIBase = "https://api.papermc.io/v2"
)

// BuildInfo contains information about a Paper build.
type BuildInfo struct {
	// Version is the Minecraft version.
	Version string
	// Build is the build number.
	Build int
	// DownloadURL is the URL to download this build.
	DownloadURL string
	// SHA256 is the checksum of the JAR file.
	SHA256 string
}

// Client provides access to PaperMC API.
type Client struct {
	httpClient *http.Client
	baseURL    string
}

// NewClient creates a new Paper API client.
func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
		baseURL: paperAPIBase,
	}
}

// paperVersionsResponse represents the versions response.
type paperVersionsResponse struct {
	ProjectID   string   `json:"project_id"`
	ProjectName string   `json:"project_name"`
	Versions    []string `json:"versions"`
}

// paperBuildsResponse represents the builds response.
type paperBuildsResponse struct {
	ProjectID   string `json:"project_id"`
	ProjectName string `json:"project_name"`
	Version     string `json:"version"`
	Builds      []int  `json:"builds"`
}

// paperBuildResponse represents a specific build response.
type paperBuildResponse struct {
	ProjectID   string                   `json:"project_id"`
	ProjectName string                   `json:"project_name"`
	Version     string                   `json:"version"`
	Build       int                      `json:"build"`
	Time        string                   `json:"time"`
	Channel     string                   `json:"channel"`
	Promoted    bool                     `json:"promoted"`
	Changes     []interface{}            `json:"changes"`
	Downloads   map[string]paperDownload `json:"downloads"`
}

// paperDownload represents a download entry.
type paperDownload struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}

// GetPaperVersions retrieves all available Paper versions.
func (c *Client) GetPaperVersions(ctx context.Context) ([]string, error) {
	url := fmt.Sprintf("%s/projects/paper", c.baseURL)

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
		return nil, errors.Newf("paper API returned status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read response body")
	}

	var versionsResp paperVersionsResponse
	if err := json.Unmarshal(body, &versionsResp); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal response")
	}

	return versionsResp.Versions, nil
}

// GetPaperBuild retrieves the latest build information for a specific version.
func (c *Client) GetPaperBuild(ctx context.Context, version string) (*BuildInfo, error) {
	latestBuild, err := c.getLatestBuildNumber(ctx, version)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get latest build number")
	}

	buildInfo, err := c.getBuildDetails(ctx, version, latestBuild)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get build details")
	}

	return buildInfo, nil
}

// getLatestBuildNumber retrieves the latest build number for a version.
func (c *Client) getLatestBuildNumber(ctx context.Context, version string) (int, error) {
	buildsURL := fmt.Sprintf("%s/projects/paper/versions/%s", c.baseURL, version)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, buildsURL, nil)
	if err != nil {
		return 0, errors.Wrap(err, "failed to create builds request")
	}

	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, errors.Wrap(err, "failed to execute builds request")
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return 0, errors.Newf("paper API returned status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, errors.Wrap(err, "failed to read builds response body")
	}

	var buildsResp paperBuildsResponse
	if err := json.Unmarshal(body, &buildsResp); err != nil {
		return 0, errors.Wrap(err, "failed to unmarshal builds response")
	}

	if len(buildsResp.Builds) == 0 {
		return 0, errors.New("no builds found for version")
	}

	return buildsResp.Builds[len(buildsResp.Builds)-1], nil
}

// getBuildDetails retrieves detailed information about a specific build.
func (c *Client) getBuildDetails(ctx context.Context, version string, build int) (*BuildInfo, error) {
	buildURL := fmt.Sprintf("%s/projects/paper/versions/%s/builds/%d",
		c.baseURL, version, build)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, buildURL, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create build request")
	}

	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "failed to execute build request")
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, errors.Newf("paper API returned status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read build response body")
	}

	var buildResp paperBuildResponse
	if err := json.Unmarshal(body, &buildResp); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal build response")
	}

	appDownload, ok := buildResp.Downloads["application"]
	if !ok {
		return nil, errors.New("no application download found")
	}

	downloadURL := fmt.Sprintf("%s/projects/paper/versions/%s/builds/%d/downloads/%s",
		c.baseURL, version, build, appDownload.Name)

	return &BuildInfo{
		Version:     version,
		Build:       build,
		DownloadURL: downloadURL,
		SHA256:      appDownload.SHA256,
	}, nil
}

// DownloadPaperJAR downloads a Paper JAR file to the specified path.
func (c *Client) DownloadPaperJAR(ctx context.Context, version, targetPath string) error {
	buildInfo, err := c.GetPaperBuild(ctx, version)
	if err != nil {
		return errors.Wrap(err, "failed to get build info")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, buildInfo.DownloadURL, nil)
	if err != nil {
		return errors.Wrap(err, "failed to create download request")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return errors.Wrap(err, "failed to execute download request")
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return errors.Newf("download returned status %d", resp.StatusCode)
	}

	// Create the target file
	out, err := os.Create(targetPath)
	if err != nil {
		return errors.Wrap(err, "failed to create target file")
	}
	defer func() {
		_ = out.Close()
	}()

	// Copy the response body to the file
	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return errors.Wrap(err, "failed to write JAR file")
	}

	return nil
}
