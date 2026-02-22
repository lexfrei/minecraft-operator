package plugins

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/lexfrei/go-hangar/pkg/hangar"
)

// HangarClient implements PluginClient using the go-hangar library.
type HangarClient struct {
	client  *hangar.Client
	baseURL string
}

// NewHangarClient creates a new Hangar API client.
func NewHangarClient() *HangarClient {
	client := hangar.NewClient(hangar.Config{
		BaseURL: hangar.DefaultBaseURL,
		Timeout: hangar.DefaultTimeout,
	})

	return &HangarClient{
		client:  client,
		baseURL: hangar.DefaultBaseURL,
	}
}

// GetVersions retrieves all available versions for a plugin from Hangar.
func (c *HangarClient) GetVersions(ctx context.Context, project string) ([]PluginVersion, error) {
	// Get project info first to obtain owner
	proj, err := c.client.GetProject(ctx, project)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get project")
	}

	// Fetch all versions (paginated)
	const maxVersions = 500
	versionsList, err := c.client.ListVersions(ctx, proj.Namespace.Owner, proj.Namespace.Slug, hangar.ListOptions{
		Limit: maxVersions,
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to list versions")
	}

	owner := proj.Namespace.Owner
	slug := proj.Namespace.Slug

	versions := make([]PluginVersion, 0, len(versionsList.Result))
	for _, v := range versionsList.Result {
		downloadURL, hash := c.extractPaperDownload(v, owner, slug)

		// Extract Paper versions from platform dependencies
		var paperVersions []string
		if v.PlatformDependencies != nil {
			if deps, ok := v.PlatformDependencies["PAPER"]; ok {
				paperVersions = deps
			}
		}

		minecraftVersions := v.GameVersions
		if len(minecraftVersions) == 0 {
			minecraftVersions = paperVersions
		}

		versions = append(versions, PluginVersion{
			Version:           v.Name,
			ReleaseDate:       v.CreatedAt,
			PaperVersions:     paperVersions,
			MinecraftVersions: minecraftVersions,
			DownloadURL:       downloadURL,
			Hash:              hash,
		})
	}

	return versions, nil
}

// extractPaperDownload resolves the PAPER download URL and hash for a version.
// Falls back to Hangar download API endpoint for externally-hosted plugins.
func (c *HangarClient) extractPaperDownload(v hangar.Version, owner, slug string) (string, string) {
	downloadURL := ""
	hash := ""

	if downloadInfo, ok := v.Downloads["PAPER"]; ok {
		if downloadInfo.DownloadURL != "" {
			downloadURL = downloadInfo.DownloadURL
		} else if downloadInfo.ExternalURL != "" && isDirectDownloadURL(downloadInfo.ExternalURL) {
			downloadURL = downloadInfo.ExternalURL
		}

		if downloadInfo.FileInfo != nil {
			hash = downloadInfo.FileInfo.SHA256Hash
		}
	}

	// Fallback: use Hangar download API endpoint for externally-hosted plugins.
	if downloadURL == "" && v.Downloads != nil {
		if _, ok := v.Downloads["PAPER"]; ok {
			downloadURL = fmt.Sprintf("%s/projects/%s/%s/versions/%s/PAPER/download",
				c.baseURL, owner, slug, v.Name)
		}
	}

	return downloadURL, hash
}

// GetCompatibility retrieves compatibility information for a specific version.
func (c *HangarClient) GetCompatibility(
	ctx context.Context,
	project,
	version string,
) (CompatibilityInfo, error) {
	v, err := c.client.GetVersion(ctx, project, version)
	if err != nil {
		return CompatibilityInfo{}, errors.Wrap(err, "failed to get version")
	}

	// Extract Paper versions from platform dependencies
	var paperVersions []string
	if v.PlatformDependencies != nil {
		if deps, ok := v.PlatformDependencies["PAPER"]; ok {
			paperVersions = deps
		}
	}

	// Use GameVersions for Minecraft versions (may be nil/empty)
	// If GameVersions is empty, fallback to platformDependencies.PAPER
	minecraftVersions := v.GameVersions
	if len(minecraftVersions) == 0 {
		minecraftVersions = paperVersions
	}

	return CompatibilityInfo{
		MinecraftVersions: minecraftVersions,
		PaperVersions:     paperVersions,
	}, nil
}

// isDirectDownloadURL checks if a URL points to a direct file download
// rather than a web page (e.g., GitHub release page).
func isDirectDownloadURL(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}

	path := strings.ToLower(parsed.Path)

	// Direct download file extensions
	directExtensions := []string{".jar", ".zip", ".tar.gz", ".tgz"}
	for _, ext := range directExtensions {
		if strings.HasSuffix(path, ext) {
			return true
		}
	}

	// Common download path patterns
	if strings.HasSuffix(path, "/download") || strings.Contains(path, "/download/") {
		return true
	}

	return false
}
