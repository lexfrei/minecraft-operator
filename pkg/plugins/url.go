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
	"net/http"
	"net/url"

	"github.com/cockroachdb/errors"
	"gopkg.in/yaml.v3"
)

// maxJARSize is the maximum JAR file size we will download for metadata extraction (100 MB).
const maxJARSize = 100 * 1024 * 1024

// JARMetadata contains plugin metadata extracted from plugin.yml or paper-plugin.yml inside a JAR.
type JARMetadata struct {
	// Name is the plugin name from plugin.yml.
	Name string
	// Version is the plugin version string.
	Version string
	// APIVersion is the Minecraft API version (e.g. "1.21").
	APIVersion string
	// SHA256 is the computed hash of the downloaded JAR.
	SHA256 string
}

// pluginYML represents the relevant fields from a Minecraft plugin.yml or paper-plugin.yml.
type pluginYML struct {
	Name       string `yaml:"name"`
	Version    string `yaml:"version"`
	APIVersion string `yaml:"api-version"`
}

// ValidateDownloadURL checks that a URL is valid for plugin downloads.
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

	return nil
}

// BuildURLVersion constructs a PluginVersion from direct URL parameters.
// Used as a fallback when JAR metadata extraction fails.
func BuildURLVersion(downloadURL, version, checksum string) PluginVersion {
	if version == "" {
		version = "0.0.0"
	}

	return PluginVersion{
		Version:     version,
		DownloadURL: downloadURL,
		Hash:        checksum,
	}
}

// FetchJARMetadata downloads a JAR from the given URL and extracts plugin metadata.
// It reads plugin.yml or paper-plugin.yml from inside the JAR and computes the SHA256 hash.
// The httpClient parameter allows injecting a custom client (e.g. for TLS test servers).
// Pass nil to use http.DefaultClient.
func FetchJARMetadata(ctx context.Context, jarURL string, httpClient *http.Client) (*JARMetadata, error) {
	if httpClient == nil {
		httpClient = http.DefaultClient
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

	// Compute SHA256.
	hash := sha256.Sum256(body)
	sha256Hex := fmt.Sprintf("%x", hash)

	// Open as ZIP.
	zipReader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return nil, errors.Wrap(err, "failed to open JAR as ZIP archive")
	}

	// Find plugin.yml or paper-plugin.yml.
	meta, err := extractPluginYML(zipReader)
	if err != nil {
		return nil, err
	}

	meta.SHA256 = sha256Hex

	return meta, nil
}

// extractPluginYML finds and parses plugin.yml or paper-plugin.yml from a ZIP archive.
// Prefers paper-plugin.yml over plugin.yml if both exist.
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

	var yml pluginYML
	if err := yaml.NewDecoder(rc).Decode(&yml); err != nil {
		return nil, errors.Wrapf(err, "failed to parse %s", target.Name)
	}

	return &JARMetadata{
		Name:       yml.Name,
		Version:    yml.Version,
		APIVersion: yml.APIVersion,
	}, nil
}
