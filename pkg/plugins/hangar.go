package plugins

import (
	"context"

	"github.com/cockroachdb/errors"
	"github.com/lexfrei/go-hangar/pkg/hangar"
)

// HangarClient implements PluginClient using the go-hangar library.
type HangarClient struct {
	client *hangar.Client
}

// NewHangarClient creates a new Hangar API client.
func NewHangarClient() *HangarClient {
	client := hangar.NewClient(hangar.Config{
		BaseURL: hangar.DefaultBaseURL,
		Timeout: hangar.DefaultTimeout,
	})

	return &HangarClient{
		client: client,
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

	versions := make([]PluginVersion, 0, len(versionsList.Result))
	for _, v := range versionsList.Result {
		// Extract download URL for PAPER platform
		downloadURL := ""
		hash := ""
		if downloadInfo, ok := v.Downloads["PAPER"]; ok {
			if downloadInfo.DownloadURL != "" {
				downloadURL = downloadInfo.DownloadURL
			} else if downloadInfo.ExternalURL != "" {
				downloadURL = downloadInfo.ExternalURL
			}
			if downloadInfo.FileInfo != nil {
				hash = downloadInfo.FileInfo.SHA256Hash
			}
		}

		// Extract Paper versions from platform dependencies
		var paperVersions []string
		if v.PlatformDependencies != nil {
			if deps, ok := v.PlatformDependencies["PAPER"]; ok {
				paperVersions = deps
			}
		}

		// Use GameVersions for Minecraft versions (may be nil/empty)
		minecraftVersions := v.GameVersions
		if minecraftVersions == nil {
			minecraftVersions = []string{}
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
	minecraftVersions := v.GameVersions
	if minecraftVersions == nil {
		minecraftVersions = []string{}
	}

	return CompatibilityInfo{
		MinecraftVersions: minecraftVersions,
		PaperVersions:     paperVersions,
	}, nil
}
