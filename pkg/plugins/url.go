/*
Copyright 2026, Aleksei Sviridkin.

SPDX-License-Identifier: BSD-3-Clause
*/

package plugins

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"gopkg.in/yaml.v3"
)

const (
	// MaxJARSize is the maximum JAR file size we will download for metadata extraction (100 MB).
	// Also used by the update controller to set curl --max-filesize.
	MaxJARSize = 100 * 1024 * 1024

	// maxPluginYMLSize is the maximum decompressed size for plugin.yml entries (10 MB).
	// Limits decompressed entry size to prevent zip bomb attacks.
	maxPluginYMLSize = 10 * 1024 * 1024

	// safeHTTPTimeout is the default timeout for HTTP operations.
	// Set to 120s to accommodate large JARs (up to MaxJARSize=100MB) on slower connections.
	safeHTTPTimeout = 120 * time.Second

	// MaxRedirects is the maximum number of HTTP redirects to follow.
	// Also used by the update controller to set curl --max-redirs.
	MaxRedirects = 10
)

// JARMetadata contains plugin metadata extracted from plugin.yml or paper-plugin.yml inside a JAR.
type JARMetadata struct {
	// Name is the plugin name from plugin.yml.
	Name string
	// Version is the plugin version string.
	Version string
	// APIVersion is the Minecraft API version (e.g. "1.21").
	APIVersion string
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
			if len(via) >= MaxRedirects {
				return errors.Newf("stopped after %d redirects", MaxRedirects)
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

	// Block non-standard IP encodings that bypass net.ParseIP but are resolved
	// by curl and some HTTP clients: decimal integers (2130706433 = 127.0.0.1),
	// octal octets (0177.0.0.01 = 127.0.0.1), hex octets (0x7f.0.0.1 = 127.0.0.1).
	if ip := parseDecimalIP(hostname); ip != nil {
		return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
			ip.IsLinkLocalMulticast() || ip.IsUnspecified()
	}

	if looksLikeNonStandardIP(hostname) {
		return true
	}

	// Block known dangerous hostnames.
	lower := strings.ToLower(hostname)

	if lower == "localhost" {
		return true
	}

	// Kubernetes internal DNS suffixes: services (.svc), pods (.pod.cluster.local),
	// and all cluster-local names (.cluster.local).
	if strings.HasSuffix(lower, ".svc") || strings.HasSuffix(lower, ".cluster.local") {
		return true
	}

	// Cloud provider metadata endpoints.
	if lower == "metadata.google.internal" ||
		lower == "instance-data.ec2.internal" {
		return true
	}

	// Block reserved TLDs per RFC 6761 (.local) and RFC 6762 (.internal).
	// These are used for mDNS, cloud metadata, and internal service discovery.
	if strings.HasSuffix(lower, ".local") || strings.HasSuffix(lower, ".internal") {
		return true
	}

	return false
}

// looksLikeNonStandardIP detects hostnames that look like octal or hex-encoded IPs
// (e.g., "0177.0.0.01", "0x7f.0.0.1"). These bypass net.ParseIP but are resolved
// by curl and browsers. We reject them outright rather than trying to parse them.
// Only checks 4-part hostnames because dotted-octal/hex notation is only meaningful
// for IPv4 addresses (exactly 4 octets). Valid domain names with leading zeros or
// hex prefixes in their labels are not rejected since they don't resolve as IPs.
func looksLikeNonStandardIP(hostname string) bool {
	parts := strings.Split(hostname, ".")
	if len(parts) != 4 { //nolint:mnd // IPv4 has exactly 4 octets.
		return false
	}

	for _, part := range parts {
		if part == "" {
			return false
		}

		// Hex prefix in any octet.
		if strings.HasPrefix(part, "0x") || strings.HasPrefix(part, "0X") {
			return true
		}

		// Leading zero in a multi-digit octet indicates octal.
		if len(part) > 1 && part[0] == '0' {
			allDigits := true

			for _, c := range part {
				if c < '0' || c > '9' {
					allDigits = false

					break
				}
			}

			if allDigits {
				return true
			}
		}
	}

	return false
}

// parseIntegerIP checks if a hostname is a single-value integer-encoded IPv4 address.
// Supports decimal (e.g., "2130706433" for 127.0.0.1) and hexadecimal with "0x"/"0X"
// prefix (e.g., "0x7f000001" for 127.0.0.1). Returns the parsed IP or nil.
func parseDecimalIP(hostname string) net.IP {
	if hostname == "" {
		return nil
	}

	n := new(big.Int)

	// Detect hex prefix (0x/0X).
	if strings.HasPrefix(hostname, "0x") || strings.HasPrefix(hostname, "0X") {
		hexPart := hostname[2:]
		if hexPart == "" {
			return nil
		}

		if _, ok := n.SetString(hexPart, 16); !ok {
			return nil
		}
	} else {
		// Must be all ASCII digits for decimal encoding.
		for _, c := range hostname {
			if c < '0' || c > '9' {
				return nil
			}
		}

		if _, ok := n.SetString(hostname, 10); !ok {
			return nil
		}
	}

	// Valid IPv4 fits in 32 bits (0 to 4294967295).
	maxIPv4 := new(big.Int).SetUint64(1<<32 - 1)
	if n.Sign() < 0 || n.Cmp(maxIPv4) > 0 {
		return nil
	}

	v := n.Uint64()
	//nolint:gosec // Value is bounded to uint32 range by the check above.
	return net.IPv4(byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}

// DownloadJAR downloads a JAR from the given URL and returns the raw bytes.
// Uses SafeHTTPClient if httpClient is nil.
// Note: the entire JAR (up to MaxJARSize) is held in memory. With concurrent
// URL plugin reconciliations, memory usage scales with num_plugins * jar_size.
func DownloadJAR(ctx context.Context, jarURL string, httpClient *http.Client) ([]byte, error) {
	if httpClient == nil {
		httpClient = SafeHTTPClient()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jarURL, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create HTTP request")
	}

	req.Header.Set("User-Agent", "minecraft-operator")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "failed to download JAR")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, errors.Newf("HTTP %d downloading JAR from %s", resp.StatusCode, jarURL)
	}

	// Early rejection based on Content-Length header to avoid reading a large
	// response body into memory when the server advertises a size exceeding our limit.
	if resp.ContentLength > int64(MaxJARSize) {
		return nil, errors.Newf("JAR exceeds maximum size of %d bytes (Content-Length: %d)", MaxJARSize, resp.ContentLength)
	}

	// Read body with size limit (Content-Length can be absent or lie).
	limitedReader := io.LimitReader(resp.Body, MaxJARSize+1)

	body, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read JAR body")
	}

	if len(body) > MaxJARSize {
		return nil, errors.Newf("JAR exceeds maximum size of %d bytes", MaxJARSize)
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
