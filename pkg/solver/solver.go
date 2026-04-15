// Package solver implements constraint solving for Minecraft plugin version compatibility.
package solver

import (
	"context"

	mcv1beta1 "github.com/lexfrei/minecraft-operator/api/v1beta1"
	"github.com/lexfrei/minecraft-operator/pkg/plugins"
)

// PaperCandidate describes a Paper version and which release channels have builds for it.
// Channels are upstream Paper channel identifiers, e.g. "STABLE", "BETA", "ALPHA".
type PaperCandidate struct {
	// Version is the Paper version string (e.g. "1.21.11").
	Version string
	// Channels lists distinct channels that have at least one build for this version.
	Channels []string
}

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

	// FindBestPaperVersion finds the maximum Paper version compatible with ALL matched plugins
	// AND with the server's channel constraint (server.Spec.Channel).
	// This implements two constraints simultaneously:
	//   - ∀ plugin ∈ plugins: ∃ plugin_version compatible with paper_version
	//   - candidate.Channels ∩ AllowedChannels(server.Spec.Channel) ≠ ∅
	FindBestPaperVersion(
		ctx context.Context,
		server *mcv1beta1.PaperMCServer,
		matchedPlugins []mcv1beta1.Plugin,
		candidates []PaperCandidate,
	) (string, error)
}
