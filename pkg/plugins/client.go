// Package plugins provides clients for plugin repository APIs.
package plugins

import (
	"context"
	"time"
)

// PluginVersion contains metadata about a plugin version.
type PluginVersion struct {
	// Version is the plugin version string.
	Version string
	// ReleaseDate is when this version was released.
	ReleaseDate time.Time
	// PaperVersions lists compatible Paper versions.
	PaperVersions []string
	// MinecraftVersions lists compatible Minecraft versions.
	MinecraftVersions []string
	// DownloadURL is the URL to download this version's JAR.
	DownloadURL string
	// Hash is the SHA256 hash of the JAR file.
	Hash string
}

// CompatibilityInfo contains compatibility information for a plugin version.
type CompatibilityInfo struct {
	// MinecraftVersions lists supported Minecraft versions.
	MinecraftVersions []string
	// PaperVersions lists supported Paper versions.
	PaperVersions []string
}

// PluginClient defines the interface for plugin repository clients.
type PluginClient interface {
	// GetVersions retrieves all available versions for a plugin.
	GetVersions(ctx context.Context, project string) ([]PluginVersion, error)

	// GetCompatibility retrieves compatibility information for a specific version.
	GetCompatibility(ctx context.Context, project, version string) (CompatibilityInfo, error)
}
