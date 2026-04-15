// Package paper provides client for PaperMC API.
package paper

import (
	"context"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/lexfrei/goPaperMC/pkg/api"
	"golang.org/x/sync/errgroup"

	"github.com/lexfrei/minecraft-operator/pkg/solver"
)

// candidateCacheTTL is how long GetPaperCandidates caches its result.
// Reconcile runs every ~15 minutes; 10 minutes absorbs bursts without risking
// stale data outlasting a full reconcile cycle.
const candidateCacheTTL = 10 * time.Minute

// candidateFetchConcurrency caps concurrent per-version /builds fetches.
const candidateFetchConcurrency = 8

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

	candidateMu sync.Mutex
	cachedList  []solver.PaperCandidate
	cachedAt    time.Time
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

// WithBaseURL overrides the upstream PaperMC API base URL. Intended for tests.
func (c *Client) WithBaseURL(baseURL string) *Client {
	c.paperClient = c.paperClient.WithBaseURL(baseURL)
	return c
}

// GetPaperCandidates retrieves all Paper versions along with the set of
// channels (STABLE/BETA/ALPHA) that have at least one build for each version.
// Results are cached for candidateCacheTTL to avoid refetching per-version
// build lists on every reconcile.
func (c *Client) GetPaperCandidates(ctx context.Context) ([]solver.PaperCandidate, error) {
	c.candidateMu.Lock()
	if c.cachedList != nil && time.Since(c.cachedAt) < candidateCacheTTL {
		cached := c.cachedList
		c.candidateMu.Unlock()
		return cached, nil
	}
	c.candidateMu.Unlock()

	project, err := c.paperClient.GetProject(ctx, "paper")
	if err != nil {
		return nil, errors.Wrap(err, "failed to get Paper project")
	}

	versions := project.FlattenVersions()
	candidates := make([]solver.PaperCandidate, len(versions))

	group, gctx := errgroup.WithContext(ctx)
	group.SetLimit(candidateFetchConcurrency)

	for i, v := range versions {
		group.Go(func() error {
			builds, err := c.paperClient.GetBuilds(gctx, "paper", v)
			if err != nil {
				return errors.Wrapf(err, "failed to get builds for version %s", v)
			}
			candidates[i] = solver.PaperCandidate{
				Version:  v,
				Channels: distinctChannels(builds),
			}
			return nil
		})
	}

	if err := group.Wait(); err != nil {
		return nil, err
	}

	c.candidateMu.Lock()
	c.cachedList = candidates
	c.cachedAt = time.Now()
	c.candidateMu.Unlock()

	return candidates, nil
}

// distinctChannels returns the set of distinct channel identifiers seen across
// the provided builds.
func distinctChannels(builds []api.BuildV3Response) []string {
	seen := make(map[string]struct{}, 2)
	for _, b := range builds {
		if b.Channel != "" {
			seen[b.Channel] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for ch := range seen {
		out = append(out, ch)
	}
	return out
}

// GetPaperBuild retrieves build information for a specific Paper version,
// filtering to the latest build whose channel is in allowedChannels.
// If allowedChannels is empty, no channel filter is applied.
func (c *Client) GetPaperBuild(
	ctx context.Context,
	version string,
	allowedChannels []string,
) (*BuildInfo, error) {
	builds, err := c.paperClient.GetBuilds(ctx, "paper", version)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get builds")
	}

	return c.extractBuildInfo(version, builds, allowedChannels)
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

	buildNumbers := make([]int, 0, len(builds))
	for _, build := range builds {
		buildNumbers = append(buildNumbers, int(build.ID))
	}

	return buildNumbers, nil
}

// extractBuildInfo picks the latest build whose channel is in allowedChannels.
// Empty allowedChannels means no filter — returns the last build as before.
func (c *Client) extractBuildInfo(
	version string,
	builds []api.BuildV3Response,
	allowedChannels []string,
) (*BuildInfo, error) {
	if len(builds) == 0 {
		return nil, errors.Newf("no builds available for version %s", version)
	}

	filtered := filterBuildsByChannel(builds, allowedChannels)
	if len(filtered) == 0 {
		return nil, errors.Newf("no builds available for version %s on channels %v", version, allowedChannels)
	}

	latestBuild := filtered[len(filtered)-1]

	return &BuildInfo{
		Version:     version,
		Build:       int(latestBuild.ID),
		DownloadURL: latestBuild.GetDownloadURL(),
		SHA256:      latestBuild.GetDownloadSHA256(),
	}, nil
}

// filterBuildsByChannel returns builds whose channel is in the allowed set.
// Empty allowedChannels means no filter (returns builds unchanged).
func filterBuildsByChannel(builds []api.BuildV3Response, allowed []string) []api.BuildV3Response {
	if len(allowed) == 0 {
		return builds
	}
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, c := range allowed {
		allowedSet[c] = struct{}{}
	}
	out := make([]api.BuildV3Response, 0, len(builds))
	for _, b := range builds {
		if _, ok := allowedSet[b.Channel]; ok {
			out = append(out, b)
		}
	}
	return out
}

// DownloadPaperJAR downloads Paper JAR to the specified path using the latest
// build for the given version regardless of channel. Intended for direct
// downloads where callers have already resolved the target version.
func (c *Client) DownloadPaperJAR(ctx context.Context, version, targetPath string) error {
	buildInfo, err := c.GetPaperBuild(ctx, version, nil)
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
