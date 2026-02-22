// Package solver implements constraint solving for Minecraft plugin version compatibility.
package solver

import (
	"context"

	mcv1beta1 "github.com/lexfrei/minecraft-operator/api/v1beta1"
	"github.com/lexfrei/minecraft-operator/pkg/plugins"
)

// Solver defines the interface for finding compatible versions.
// Implementations include simple linear search (MVP) and future SAT-based solvers.
type Solver interface {
	// FindBestPluginVersion finds the maximum plugin version compatible with ALL matched servers.
	// This implements the constraint: ∀ server ∈ servers: compatible(plugin_version, server.paperVersion).
	FindBestPluginVersion(
		ctx context.Context,
		plugin *mcv1beta1.Plugin,
		servers []mcv1beta1.PaperMCServer,
		allVersions []plugins.PluginVersion,
	) (string, error)

	// FindBestPaperVersion finds the maximum Paper version compatible with ALL matched plugins.
	// This implements the constraint: ∀ plugin ∈ plugins: ∃ plugin_version compatible with paper_version.
	FindBestPaperVersion(
		ctx context.Context,
		server *mcv1beta1.PaperMCServer,
		matchedPlugins []mcv1beta1.Plugin,
		paperVersions []string,
	) (string, error)
}
