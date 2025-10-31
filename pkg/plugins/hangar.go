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
// NOTE: Currently using go-hangar which doesn't expose platformDependencies and gameVersions.
// A PR will be created to add these fields to the library.
// For now, we return empty compatibility data - this will be fixed after the PR is merged.
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

		// TODO: Add platformDependencies and gameVersions after go-hangar PR is merged
		// For now, these fields will be empty
		versions = append(versions, PluginVersion{
			Version:           v.Name,
			ReleaseDate:       v.CreatedAt,
			PaperVersions:     []string{}, // TODO: Extract from v.PlatformDependencies["PAPER"]
			MinecraftVersions: []string{}, // TODO: Extract from v.GameVersions
			DownloadURL:       downloadURL,
			Hash:              hash,
		})
	}

	return versions, nil
}

// GetCompatibility retrieves compatibility information for a specific version.
// NOTE: Currently returns empty data until platformDependencies and gameVersions
// are added to go-hangar through a PR.
func (c *HangarClient) GetCompatibility(
	ctx context.Context,
	project,
	version string,
) (CompatibilityInfo, error) {
	v, err := c.client.GetVersion(ctx, project, version)
	if err != nil {
		return CompatibilityInfo{}, errors.Wrap(err, "failed to get version")
	}

	// TODO: Add platformDependencies and gameVersions after go-hangar PR is merged
	_ = v // Suppress unused variable warning

	return CompatibilityInfo{
		MinecraftVersions: []string{}, // TODO: Extract from v.GameVersions
		PaperVersions:     []string{}, // TODO: Extract from v.PlatformDependencies["PAPER"]
	}, nil
}
