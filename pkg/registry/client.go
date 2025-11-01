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

package registry

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
	defaultBaseURL     = "https://hub.docker.com/v2"
	defaultPageSize    = 100
	defaultHTTPTimeout = 10 * time.Second
)

// Client provides methods to interact with Docker Hub registry API.
type Client struct {
	httpClient *http.Client
	baseURL    string
}

// NewClient creates a new Docker Hub registry client with default settings.
func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: defaultHTTPTimeout,
		},
		baseURL: defaultBaseURL,
	}
}

// ListTags retrieves all tags for a repository.
// repository should be in the format "namespace/repo" (e.g., "lexfrei/papermc").
// pageSize controls how many tags to fetch per API call (0 uses default).
func (c *Client) ListTags(ctx context.Context, repository string, pageSize int) ([]string, error) {
	if pageSize <= 0 {
		pageSize = defaultPageSize
	}

	url := fmt.Sprintf("%s/repositories/%s/tags?page_size=%d", c.baseURL, repository, pageSize)
	tags := []string{}

	for url != "" {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, errors.Wrap(err, "failed to create request")
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, errors.Wrap(err, "failed to fetch tags")
		}

		if resp.StatusCode != http.StatusOK {
			_ = resp.Body.Close()

			return nil, errors.Newf("unexpected status code: %d", resp.StatusCode)
		}

		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		if err != nil {
			return nil, errors.Wrap(err, "failed to read response body")
		}

		var tagsResp TagsResponse
		if err := json.Unmarshal(body, &tagsResp); err != nil {
			return nil, errors.Wrap(err, "failed to parse JSON response")
		}

		for _, tag := range tagsResp.Results {
			tags = append(tags, tag.Name)
		}

		// Handle pagination
		url = tagsResp.Next
	}

	return tags, nil
}

// ImageExists checks if a specific tag exists in the repository.
// repository should be in the format "namespace/repo" (e.g., "lexfrei/papermc").
// Returns true if the tag exists, false if it doesn't exist (404), and error for other failures.
func (c *Client) ImageExists(ctx context.Context, repository, tag string) (bool, error) {
	url := fmt.Sprintf("%s/repositories/%s/tags/%s", c.baseURL, repository, tag)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, errors.Wrap(err, "failed to create request")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false, errors.Wrap(err, "failed to check tag existence")
	}

	_ = resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	default:
		return false, errors.Newf("unexpected status code: %d", resp.StatusCode)
	}
}
