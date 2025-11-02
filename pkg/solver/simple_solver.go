package solver

import (
	"context"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/cockroachdb/errors"
	mcv1alpha1 "github.com/lexfrei/minecraft-operator/api/v1alpha1"
	"github.com/lexfrei/minecraft-operator/pkg/plugins"
	"github.com/lexfrei/minecraft-operator/pkg/version"
)

// SimpleSolver implements a linear search constraint solver for MVP.
// Per ADR-010, this is sufficient for small numbers of plugins/servers.
type SimpleSolver struct{}

// NewSimpleSolver creates a new simple linear search solver.
func NewSimpleSolver() *SimpleSolver {
	return &SimpleSolver{}
}

// FindBestPluginVersion finds the maximum plugin version compatible with ALL servers.
// Algorithm:
//  1. Filter versions by updateDelay (skip too new versions)
//  2. Sort versions in descending order
//  3. For each version (highest first), check if compatible with ALL servers
//  4. Return first version that satisfies all constraints
func (s *SimpleSolver) FindBestPluginVersion(
	ctx context.Context,
	plugin *mcv1alpha1.Plugin,
	servers []mcv1alpha1.PaperMCServer,
	allVersions []plugins.PluginVersion,
) (string, error) {
	if len(allVersions) == 0 {
		return "", errors.New("no versions available")
	}

	if len(servers) == 0 {
		return "", errors.New("no servers matched by selector")
	}

	// Handle pinned version policy
	if plugin.Spec.VersionPolicy == "pinned" {
		if plugin.Spec.PinnedVersion == "" {
			return "", errors.New("versionPolicy is pinned but pinnedVersion is not set")
		}
		return plugin.Spec.PinnedVersion, nil
	}

	// Filter by updateDelay
	var delay time.Duration
	if plugin.Spec.UpdateDelay != nil {
		delay = plugin.Spec.UpdateDelay.Duration
	}

	filteredVersions := filterByDelay(allVersions, delay)
	if len(filteredVersions) == 0 {
		return "", errors.New("no versions available after updateDelay filtering")
	}

	// Sort versions in descending order (newest first)
	sortedVersions := sortVersionsDesc(filteredVersions)

	// Linear search: find max version compatible with ALL servers
	for _, pv := range sortedVersions {
		compatible := true

		for i := range servers {
			server := &servers[i]

			// Check if this plugin version is compatible with server's Paper version
			if !isPluginCompatibleWithServer(pv, server, plugin) {
				compatible = false
				break
			}
		}

		if compatible {
			return pv.Version, nil
		}
	}

	return "", errors.New("no plugin version compatible with all matched servers")
}

// FindBestPaperVersion finds the maximum Paper version compatible with ALL plugins.
// Algorithm:
//  1. Filter Paper versions by updateDelay
//  2. Sort versions in descending order
//  3. For each Paper version (highest first), check if ALL plugins have a compatible version
//  4. Return first Paper version that satisfies all constraints
func (s *SimpleSolver) FindBestPaperVersion(
	ctx context.Context,
	server *mcv1alpha1.PaperMCServer,
	matchedPlugins []mcv1alpha1.Plugin,
	paperVersions []string,
) (string, error) {
	if len(paperVersions) == 0 {
		return "", errors.New("no Paper versions available")
	}

	// Handle specific version
	if server.Spec.PaperVersion != "latest" {
		return server.Spec.PaperVersion, nil
	}

	sortedVersions, err := s.filterAndSortPaperVersions(server, paperVersions)
	if err != nil {
		return "", errors.Wrap(err, "failed to filter versions")
	}

	// Linear search: find max Paper version where ALL plugins have compatible version
	for _, paperVer := range sortedVersions {
		if s.isPaperVersionCompatibleWithAllPlugins(paperVer, matchedPlugins) {
			return paperVer, nil
		}
	}

	return "", errors.New("no Paper version compatible with all matched plugins")
}

// filterAndSortPaperVersions filters Paper versions by updateDelay and sorts them descending.
func (s *SimpleSolver) filterAndSortPaperVersions(
	server *mcv1alpha1.PaperMCServer,
	paperVersions []string,
) ([]string, error) {
	var delay time.Duration
	if server.Spec.UpdateDelay != nil {
		delay = server.Spec.UpdateDelay.Duration
	}

	// Convert to version.VersionInfo for filtering
	paperVersionInfos := make([]version.VersionInfo, len(paperVersions))
	for i, v := range paperVersions {
		paperVersionInfos[i] = version.VersionInfo{
			Version:     v,
			ReleaseDate: time.Now(), // Paper API doesn't provide release dates, assume current
		}
	}

	filteredVersions := version.FilterByUpdateDelay(paperVersionInfos, delay)
	if len(filteredVersions) == 0 {
		return nil, errors.New("no Paper versions available after updateDelay filtering")
	}

	// Extract version strings and sort descending
	versionStrings := make([]string, len(filteredVersions))
	for i, v := range filteredVersions {
		versionStrings[i] = v.Version
	}

	return sortPaperVersionsDesc(versionStrings), nil
}

// isPaperVersionCompatibleWithAllPlugins checks if a Paper version is compatible with all plugins.
func (s *SimpleSolver) isPaperVersionCompatibleWithAllPlugins(
	paperVer string,
	matchedPlugins []mcv1alpha1.Plugin,
) bool {
	for i := range matchedPlugins {
		plugin := &matchedPlugins[i]

		// For each plugin, check if it has any version compatible with paperVer
		if len(plugin.Status.AvailableVersions) == 0 {
			// No cached versions, assume compatible (logged as warning elsewhere)
			continue
		}

		hasCompatibleVersion := false
		for _, pvInfo := range plugin.Status.AvailableVersions {
			// Check if this plugin version supports the Paper version
			if containsVersion(pvInfo.MinecraftVersions, paperVer) {
				hasCompatibleVersion = true
				break
			}
		}

		if !hasCompatibleVersion {
			return false
		}
	}

	return true
}

// filterByDelay filters plugin versions based on updateDelay.
func filterByDelay(versions []plugins.PluginVersion, delay time.Duration) []plugins.PluginVersion {
	if delay == 0 {
		return versions
	}

	cutoff := time.Now().Add(-delay)
	filtered := make([]plugins.PluginVersion, 0)

	for _, v := range versions {
		if v.ReleaseDate.Before(cutoff) || v.ReleaseDate.Equal(cutoff) {
			filtered = append(filtered, v)
		}
	}

	return filtered
}

// sortVersionsDesc sorts plugin versions in descending order (newest/highest first).
func sortVersionsDesc(versions []plugins.PluginVersion) []plugins.PluginVersion {
	sorted := make([]plugins.PluginVersion, len(versions))
	copy(sorted, versions)

	// Bubble sort (simple for MVP, can optimize later)
	for i := 0; i < len(sorted)-1; i++ {
		for j := i + 1; j < len(sorted); j++ {
			v1, err1 := semver.NewVersion(sorted[i].Version)
			v2, err2 := semver.NewVersion(sorted[j].Version)

			if err1 == nil && err2 == nil && v2.GreaterThan(v1) {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	return sorted
}

// sortPaperVersionsDesc sorts Paper version strings in descending order.
func sortPaperVersionsDesc(versions []string) []string {
	sorted := make([]string, len(versions))
	copy(sorted, versions)

	// Bubble sort
	for i := 0; i < len(sorted)-1; i++ {
		for j := i + 1; j < len(sorted); j++ {
			v1, err1 := semver.NewVersion(sorted[i])
			v2, err2 := semver.NewVersion(sorted[j])

			if err1 == nil && err2 == nil && v2.GreaterThan(v1) {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	return sorted
}

// isPluginCompatibleWithServer checks if a plugin version is compatible with a server's Paper version.
func isPluginCompatibleWithServer(
	pv plugins.PluginVersion,
	server *mcv1alpha1.PaperMCServer,
	plugin *mcv1alpha1.Plugin,
) bool {
	// Use the current Paper version from status, fallback to spec
	paperVersion := server.Status.CurrentPaperVersion
	if paperVersion == "" {
		paperVersion = server.Spec.PaperVersion
	}

	// Check if compatibilityOverride is enabled
	if plugin.Spec.CompatibilityOverride != nil && plugin.Spec.CompatibilityOverride.Enabled {
		// Use override versions instead of API metadata
		if len(plugin.Spec.CompatibilityOverride.MinecraftVersions) > 0 {
			return containsVersion(plugin.Spec.CompatibilityOverride.MinecraftVersions, paperVersion)
		}
		// If override is enabled but no versions specified, assume compatible
		return true
	}

	// Per DESIGN.md: "Plugin without version metadata: assume compatibility, log warning"
	// If both MinecraftVersions and PaperVersions are empty, assume compatible
	if len(pv.MinecraftVersions) == 0 && len(pv.PaperVersions) == 0 {
		return true
	}

	// Check if the plugin's compatible versions include this Paper version
	// For MVP, we check MinecraftVersions which should include the Minecraft version
	// corresponding to the Paper version
	return containsVersion(pv.MinecraftVersions, paperVersion) ||
		containsVersion(pv.PaperVersions, paperVersion)
}

// containsVersion checks if a version string is in the list.
func containsVersion(versions []string, target string) bool {
	for _, v := range versions {
		if v == target {
			return true
		}
		// Also support version ranges like "1.21.x"
		if matchesVersionPattern(v, target) {
			return true
		}
	}
	return false
}

// matchesVersionPattern checks if a target version matches a pattern (e.g., "1.21.x" matches "1.21.1").
func matchesVersionPattern(pattern, target string) bool {
	// Simple implementation: if pattern ends with .x, match major.minor
	if len(pattern) > 2 && pattern[len(pattern)-2:] == ".x" {
		prefix := pattern[:len(pattern)-2]
		return len(target) >= len(prefix) && target[:len(prefix)] == prefix
	}
	return false
}
