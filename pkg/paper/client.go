// Package paper provides client for PaperMC API.
package paper

import (
	"context"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/lexfrei/goPaperMC/pkg/api"
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

// Client provides access to PaperMC API using goPaperMC library.
type Client struct {
	paperClient *api.Client
	httpClient  *http.Client
}

// NewClient creates a new Paper API client.
func NewClient() *Client {
	return &Client{
		paperClient: api.NewClient().WithTimeout(60 * time.Second),
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// GetPaperVersions retrieves all available Paper versions.
func (c *Client) GetPaperVersions(ctx context.Context) ([]string, error) {
	project, err := c.paperClient.GetProject(ctx, "paper")
	if err != nil {
		return nil, errors.Wrap(err, "failed to get Paper project")
	}

	return project.FlattenVersions(), nil
}

// GetPaperBuild retrieves build information for a specific Paper version.
// Returns the latest build for the given version.
func (c *Client) GetPaperBuild(ctx context.Context, version string) (*BuildInfo, error) {
	builds, err := c.paperClient.GetBuilds(ctx, "paper", version)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get builds")
	}

	return c.extractBuildInfo(ctx, version, builds)
}

// GetBuilds retrieves all build numbers for a specific Paper version.
// Returns a slice of build numbers in ascending order.
func (c *Client) GetBuilds(ctx context.Context, version string) ([]int, error) {
	builds, err := c.paperClient.GetBuilds(ctx, "paper", version)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get builds")
	}

	if len(builds) == 0 {
		return nil, errors.Newf("no builds available for version %s", version)
	}

	// Extract build numbers from the response
	buildNumbers := make([]int, 0, len(builds))
	for _, build := range builds {
		buildNumbers = append(buildNumbers, int(build.ID))
	}

	return buildNumbers, nil
}

// extractBuildInfo extracts build info from builds response.
func (c *Client) extractBuildInfo(
	_ context.Context,
	version string,
	builds []api.BuildV3Response,
) (*BuildInfo, error) {
	if len(builds) == 0 {
		return nil, errors.Newf("no builds available for version %s", version)
	}

	// Get latest build (last in the list)
	latestBuild := builds[len(builds)-1]

	return &BuildInfo{
		Version:     version,
		Build:       int(latestBuild.ID),
		DownloadURL: latestBuild.GetDownloadURL(),
		SHA256:      latestBuild.GetDownloadSHA256(),
	}, nil
}

// DownloadPaperJAR downloads Paper JAR to the specified path.
func (c *Client) DownloadPaperJAR(ctx context.Context, version, targetPath string) error {
	buildInfo, err := c.GetPaperBuild(ctx, version)
	if err != nil {
		return errors.Wrap(err, "failed to get build info")
	}

	return c.downloadFile(ctx, buildInfo.DownloadURL, targetPath)
}

// downloadFile downloads a file from URL to target path.
func (c *Client) downloadFile(ctx context.Context, url, targetPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return errors.Wrap(err, "failed to create request")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return errors.Wrap(err, "failed to execute request")
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return errors.Newf("unexpected status code: %d", resp.StatusCode)
	}

	return c.saveToFile(resp.Body, targetPath)
}

// saveToFile saves response body to file.
func (c *Client) saveToFile(body io.Reader, targetPath string) error {
	out, err := os.Create(targetPath)
	if err != nil {
		return errors.Wrap(err, "failed to create file")
	}
	defer func() {
		_ = out.Close()
	}()

	_, err = io.Copy(out, body)
	if err != nil {
		return errors.Wrap(err, "failed to write file")
	}

	return nil
}
