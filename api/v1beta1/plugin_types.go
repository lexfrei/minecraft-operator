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

package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PluginSource defines the source of a plugin.
type PluginSource struct {
	// Type specifies the plugin repository type.
	// +kubebuilder:validation:Enum=hangar;modrinth;spigot;url
	Type string `json:"type"`

	// Project is the plugin project identifier (for hangar/modrinth/spigot).
	// +optional
	Project string `json:"project,omitempty"`

	// URL is the direct download URL (for type: url).
	// +optional
	URL string `json:"url,omitempty"`

	// Checksum is the expected SHA256 hash of the JAR file (for type: url).
	// If not provided, the operator logs a warning about unverified downloads.
	// +optional
	// +kubebuilder:validation:Pattern=`^[a-f0-9]{64}$`
	Checksum string `json:"checksum,omitempty"`
}

// CompatibilityOverride allows manual compatibility specification.
type CompatibilityOverride struct {
	// Enabled determines if the override replaces API metadata.
	Enabled bool `json:"enabled"`

	// MinecraftVersions lists supported Minecraft versions.
	// +optional
	MinecraftVersions []string `json:"minecraftVersions,omitempty"`
}

// PluginSpec defines the desired state of Plugin.
type PluginSpec struct {
	// Source specifies where to fetch the plugin.
	Source PluginSource `json:"source"`

	// UpdateStrategy defines how plugin versions are managed.
	// Valid values: "latest", "auto", "pin", "build-pin".
	// - latest: always use the latest available version from the repository
	// - auto: use constraint solver to find best version compatible with matched servers
	// - pin: pin to specific version (requires version field), auto-update to latest build
	// - build-pin: pin to specific version and build (requires version and build fields)
	// +kubebuilder:validation:Enum=latest;auto;pin;build-pin
	// +kubebuilder:default=latest
	UpdateStrategy string `json:"updateStrategy,omitempty"`

	// Version specifies the target plugin version (required for pin and build-pin strategies).
	// Example: "2.5.0" for a semantic version.
	// +optional
	Version string `json:"version,omitempty"`

	// Build specifies the target plugin build number (optional, only used with build-pin strategy).
	// If set, the operator will pin to this exact build of the specified version.
	// +optional
	Build *int `json:"build,omitempty"`

	// UpdateDelay is the grace period before auto-applying new versions.
	// +optional
	UpdateDelay *metav1.Duration `json:"updateDelay,omitempty"`

	// InstanceSelector selects which PaperMCServer instances to apply this plugin to.
	InstanceSelector metav1.LabelSelector `json:"instanceSelector"`

	// Port is the network port that this plugin exposes (optional).
	// If specified, this port will be added to the Service of all matched servers (TCP+UDP).
	// Example: 8123 for Dynmap web interface.
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	Port *int32 `json:"port,omitempty"`

	// CompatibilityOverride allows manual compatibility specification for edge cases.
	// +optional
	CompatibilityOverride *CompatibilityOverride `json:"compatibilityOverride,omitempty"`
}

// PluginVersionInfo contains metadata about a specific plugin version.
type PluginVersionInfo struct {
	// Version is the plugin version string.
	Version string `json:"version"`

	// MinecraftVersions lists compatible Minecraft versions.
	MinecraftVersions []string `json:"minecraftVersions"`

	// DownloadURL is the URL to download this version.
	DownloadURL string `json:"downloadURL"`

	// Hash is the SHA256 hash of the plugin JAR.
	Hash string `json:"hash"`

	// CachedAt is when this metadata was cached.
	CachedAt metav1.Time `json:"cachedAt"`

	// ReleasedAt is when this version was released.
	ReleasedAt metav1.Time `json:"releasedAt"`
}

// MatchedInstance represents a PaperMCServer instance matched by the selector.
type MatchedInstance struct {
	// Name is the server name.
	Name string `json:"name"`

	// Namespace is the server namespace.
	Namespace string `json:"namespace"`

	// Version is the server's current Paper version.
	Version string `json:"version"`

	// Compatible indicates whether the plugin has a resolved version compatible with this server.
	// This value is read from PaperMCServer.status.plugins during Plugin reconciliation.
	Compatible bool `json:"compatible"`
}

// DeletionProgressEntry tracks JAR cleanup status for one server during plugin deletion.
type DeletionProgressEntry struct {
	// ServerName is the PaperMCServer name.
	ServerName string `json:"serverName"`

	// Namespace is the PaperMCServer namespace.
	Namespace string `json:"namespace"`

	// JARDeleted indicates the JAR file has been removed from disk.
	JARDeleted bool `json:"jarDeleted"`

	// DeletedAt is when the JAR was actually deleted.
	// +optional
	DeletedAt *metav1.Time `json:"deletedAt,omitempty"`

	// DeletionRequestedAt is when the JAR deletion was first requested.
	// Used for timeout-based forced completion to prevent deadlocks.
	// +optional
	DeletionRequestedAt *metav1.Time `json:"deletionRequestedAt,omitempty"`
}

// PluginStatus defines the observed state of Plugin.
type PluginStatus struct {
	// AvailableVersions contains cached metadata from the plugin repository.
	// +optional
	AvailableVersions []PluginVersionInfo `json:"availableVersions,omitempty"`

	// MatchedInstances lists servers this plugin applies to with per-server resolved versions.
	// +optional
	MatchedInstances []MatchedInstance `json:"matchedInstances,omitempty"`

	// RepositoryStatus indicates the plugin repository availability.
	// +optional
	// +kubebuilder:validation:Enum=available;unavailable;orphaned
	RepositoryStatus string `json:"repositoryStatus,omitempty"`

	// LastFetched is the timestamp of the last API fetch.
	// +optional
	LastFetched *metav1.Time `json:"lastFetched,omitempty"`

	// DeletionProgress tracks JAR cleanup per server during plugin deletion.
	// This field is populated when the plugin is being deleted (has DeletionTimestamp).
	// +optional
	DeletionProgress []DeletionProgressEntry `json:"deletionProgress,omitempty"`

	// Conditions represent the current state of the Plugin resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:storageversion

// Plugin is the Schema for the plugins API.
type Plugin struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of Plugin
	// +required
	Spec PluginSpec `json:"spec"`

	// status defines the observed state of Plugin
	// +optional
	Status PluginStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// PluginList contains a list of Plugin.
type PluginList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Plugin `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Plugin{}, &PluginList{})
}
