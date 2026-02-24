/*
Copyright 2026.

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
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"gopkg.in/yaml.v3"
)

const (
	// maxJARSize is the maximum JAR file size we will download for metadata extraction (100 MB).
	maxJARSize = 100 * 1024 * 1024

	// maxPluginYMLSize is the maximum decompressed size for plugin.yml entries (10 MB).
	// Limits decompressed entry size to prevent zip bomb attacks.
	maxPluginYMLSize = 10 * 1024 * 1024

	// safeHTTPTimeout is the default timeout for HTTP operations.
	safeHTTPTimeout = 30 * time.Second

	// maxRedirects is the maximum number of HTTP redirects to follow.
	maxRedirects = 10
)

// JARMetadata contains plugin metadata extracted from plugin.yml or paper-plugin.yml inside a JAR.
type JARMetadata struct {
	// Name is the plugin name from plugin.yml.
	Name string
	// Version is the plugin version string.
	Version string
	// APIVersion is the Minecraft API version (e.g. "1.21").
	APIVersion string
	// SHA256 is the computed hash of the downloaded JAR.
	// Only set by FetchJARMetadata; ParseJARMetadata leaves this empty.
	SHA256 string
}

// pluginYML represents the relevant fields from a Minecraft plugin.yml or paper-plugin.yml.
type pluginYML struct {
	Name       string `yaml:"name"`
	Version    string `yaml:"version"`
	APIVersion string `yaml:"api-version"`
}

// SafeHTTPClient creates an HTTP client with timeout and SSRF protection.
// It blocks redirects to private/internal hosts to prevent SSRF via open redirect,
// while allowing cross-host redirects to public hosts (e.g., GitHub → CDN).
//
// Known limitation: DNS rebinding attacks are not prevented. ValidateDownloadURL
// checks the hostname at URL parse time, but a malicious DNS record could resolve
// to a private IP at connection time. This is mitigated by the fact that modifying
// Plugin CRDs requires Kubernetes RBAC access.
func SafeHTTPClient() *http.Client {
	return &http.Client{
		Timeout: safeHTTPTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return errors.Newf("stopped after %d redirects", maxRedirects)
			}

			// Block redirects to private/internal hosts.
			if isBlockedHost(req.URL.Host) {
				return errors.Newf("redirect to blocked host: %s", req.URL.Host)
			}

			return nil
		},
	}
}

// ValidateDownloadURL checks that a URL is valid and safe for plugin downloads.
// Blocks non-HTTPS URLs, missing hosts, and private/internal network addresses.
// Note: This validates the hostname at parse time. DNS rebinding (where a hostname
// resolves to a private IP at connection time) is a known limitation.
func ValidateDownloadURL(rawURL string) error {
	if rawURL == "" {
		return errors.New("URL is required for url source type")
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return errors.Wrap(err, "failed to parse URL")
	}

	if parsed.Scheme != "https" {
		return errors.Newf("URL must use HTTPS, got %s", parsed.Scheme)
	}

	if parsed.Host == "" {
		return errors.New("URL must have a valid host")
	}

	if isBlockedHost(parsed.Host) {
		return errors.Newf("URL host %s is blocked (private/internal address)", parsed.Hostname())
	}

	return nil
}

// isBlockedHost checks whether a host (with optional port) points to a private,
// loopback, or otherwise restricted network address. This prevents SSRF attacks
// where a malicious URL could probe internal cluster services or cloud metadata.
func isBlockedHost(host string) bool {
	hostname := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		hostname = h
	}

	// Strip IPv6 brackets (e.g., "[::1]" → "::1").
	hostname = strings.TrimPrefix(hostname, "[")
	hostname = strings.TrimSuffix(hostname, "]")

	// Block literal IPs in restricted ranges.
	if ip := net.ParseIP(hostname); ip != nil {
		return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
			ip.IsLinkLocalMulticast() || ip.IsUnspecified()
	}

	// Block known dangerous hostnames.
	lower := strings.ToLower(hostname)

	if lower == "localhost" {
		return true
	}

	// Kubernetes internal DNS suffixes.
	if strings.HasSuffix(lower, ".svc") || strings.HasSuffix(lower, ".svc.cluster.local") {
		return true
	}

	// Cloud provider metadata endpoints.
	if lower == "metadata.google.internal" {
		return true
	}

	return false
}

// DownloadJAR downloads a JAR from the given URL and returns the raw bytes.
// Uses SafeHTTPClient if httpClient is nil.
func DownloadJAR(ctx context.Context, jarURL string, httpClient *http.Client) ([]byte, error) {
	if httpClient == nil {
		httpClient = SafeHTTPClient()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jarURL, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create HTTP request")
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "failed to download JAR")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, errors.Newf("HTTP %d downloading JAR from %s", resp.StatusCode, jarURL)
	}

	// Read body with size limit.
	limitedReader := io.LimitReader(resp.Body, maxJARSize+1)

	body, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read JAR body")
	}

	if len(body) > maxJARSize {
		return nil, errors.Newf("JAR exceeds maximum size of %d bytes", maxJARSize)
	}

	return body, nil
}

// ParseJARMetadata parses a JAR file (ZIP archive) and extracts plugin metadata
// from plugin.yml or paper-plugin.yml. The SHA256 field is NOT set by this function;
// callers should compute it separately if needed.
func ParseJARMetadata(jarBytes []byte) (*JARMetadata, error) {
	zipReader, err := zip.NewReader(bytes.NewReader(jarBytes), int64(len(jarBytes)))
	if err != nil {
		return nil, errors.Wrap(err, "failed to open JAR as ZIP archive")
	}

	return extractPluginYML(zipReader)
}

// FetchJARMetadata downloads a JAR from the given URL and extracts plugin metadata.
// This is a convenience function that calls DownloadJAR and ParseJARMetadata,
// and also computes the SHA256 hash of the downloaded JAR.
func FetchJARMetadata(ctx context.Context, jarURL string, httpClient *http.Client) (*JARMetadata, error) {
	body, err := DownloadJAR(ctx, jarURL, httpClient)
	if err != nil {
		return nil, err
	}

	meta, err := ParseJARMetadata(body)
	if err != nil {
		return nil, err
	}

	hash := sha256.Sum256(body)
	meta.SHA256 = fmt.Sprintf("%x", hash)

	return meta, nil
}

// extractPluginYML finds and parses plugin.yml or paper-plugin.yml from a ZIP archive.
// Prefers paper-plugin.yml over plugin.yml if both exist.
// Limits decompressed entry size to prevent zip bomb attacks.
func extractPluginYML(zipReader *zip.Reader) (*JARMetadata, error) {
	var pluginFile, paperPluginFile *zip.File

	for _, f := range zipReader.File {
		switch f.Name {
		case "plugin.yml":
			pluginFile = f
		case "paper-plugin.yml":
			paperPluginFile = f
		}
	}

	// Prefer paper-plugin.yml.
	target := paperPluginFile
	if target == nil {
		target = pluginFile
	}

	if target == nil {
		return nil, errors.New("JAR contains neither plugin.yml nor paper-plugin.yml")
	}

	rc, err := target.Open()
	if err != nil {
		return nil, errors.Wrapf(err, "failed to open %s in JAR", target.Name)
	}
	defer func() { _ = rc.Close() }()

	// Limit decompressed size to prevent zip bomb attacks.
	limitedReader := io.LimitReader(rc, maxPluginYMLSize+1)

	data, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read %s from JAR", target.Name)
	}

	if len(data) > maxPluginYMLSize {
		return nil, errors.Newf("%s exceeds maximum size of %d bytes", target.Name, maxPluginYMLSize)
	}

	var yml pluginYML
	if err := yaml.Unmarshal(data, &yml); err != nil {
		return nil, errors.Wrapf(err, "failed to parse %s", target.Name)
	}

	return &JARMetadata{
		Name:       yml.Name,
		Version:    yml.Version,
		APIVersion: yml.APIVersion,
	}, nil
}
