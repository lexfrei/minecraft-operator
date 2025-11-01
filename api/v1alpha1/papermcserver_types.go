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

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// UpdateSchedule defines when to check and apply updates.
type UpdateSchedule struct {
	// CheckCron is the cron schedule for checking updates (e.g., "0 3 * * *").
	CheckCron string `json:"checkCron"`

	// MaintenanceWindow defines when updates can be applied.
	MaintenanceWindow MaintenanceWindow `json:"maintenanceWindow"`
}

// MaintenanceWindow defines the time window for applying updates.
type MaintenanceWindow struct {
	// Cron is the schedule for applying updates (e.g., "0 4 * * 0").
	Cron string `json:"cron"`

	// Enabled determines if the maintenance window is active.
	Enabled bool `json:"enabled"`
}

// GracefulShutdown defines graceful shutdown configuration.
type GracefulShutdown struct {
	// Timeout is the shutdown timeout (must match terminationGracePeriodSeconds).
	Timeout metav1.Duration `json:"timeout"`
}

// SecretKeyRef references a key in a Secret.
type SecretKeyRef struct {
	// Name is the Secret name.
	Name string `json:"name"`

	// Key is the key within the Secret.
	Key string `json:"key"`
}

// RCONConfig defines RCON configuration for the server.
type RCONConfig struct {
	// Enabled determines if RCON is enabled.
	Enabled bool `json:"enabled"`

	// PasswordSecret references the Secret containing the RCON password.
	PasswordSecret SecretKeyRef `json:"passwordSecret"`

	// Port is the RCON port.
	// +optional
	// +kubebuilder:default=25575
	Port int32 `json:"port,omitempty"`
}

// PaperMCServerSpec defines the desired state of PaperMCServer.
type PaperMCServerSpec struct {
	// UpdateStrategy defines the update strategy for Paper version.
	// Valid values: "latest", "auto", "pin", "build-pin".
	// - latest: always use latest available version from Docker Hub
	// - auto: use solver to find best version compatible with plugins
	// - pin: pin to specific version, auto-update to latest build (requires paperVersion)
	// - build-pin: pin to specific version and build (requires paperVersion and paperBuild)
	// +kubebuilder:validation:Enum=latest;auto;pin;build-pin
	UpdateStrategy string `json:"updateStrategy"`

	// PaperVersion specifies the Paper version when using "pin" or "build-pin" strategy.
	// +optional
	PaperVersion string `json:"paperVersion,omitempty"`

	// PaperBuild specifies the exact Paper build number when using "build-pin" strategy.
	// +optional
	PaperBuild *int `json:"paperBuild,omitempty"`

	// UpdateDelay is the grace period before applying Paper updates.
	// +optional
	UpdateDelay *metav1.Duration `json:"updateDelay,omitempty"`

	// UpdateSchedule defines when to check and apply updates.
	UpdateSchedule UpdateSchedule `json:"updateSchedule"`

	// GracefulShutdown configures graceful server shutdown.
	GracefulShutdown GracefulShutdown `json:"gracefulShutdown"`

	// RCON configures RCON for graceful shutdown.
	RCON RCONConfig `json:"rcon"`

	// PodTemplate is the template for the StatefulSet pod.
	PodTemplate corev1.PodTemplateSpec `json:"podTemplate"`
}

// PluginRef references a Plugin resource.
type PluginRef struct {
	// Name is the Plugin name.
	Name string `json:"name"`

	// Namespace is the Plugin namespace.
	Namespace string `json:"namespace"`
}

// ServerPluginStatus represents a plugin's status for this server.
type ServerPluginStatus struct {
	// PluginRef references the Plugin resource.
	PluginRef PluginRef `json:"pluginRef"`

	// ResolvedVersion is the plugin version resolved for this server.
	ResolvedVersion string `json:"resolvedVersion"`

	// CurrentVersion is the currently installed plugin version.
	// +optional
	CurrentVersion string `json:"currentVersion,omitempty"`

	// DesiredVersion is the target plugin version the operator wants to install.
	// +optional
	DesiredVersion string `json:"desiredVersion,omitempty"`

	// Compatible indicates if this version is compatible with the server.
	Compatible bool `json:"compatible"`

	// Source is the plugin repository type.
	Source string `json:"source"`
}

// PluginVersionPair pairs a plugin with its version.
type PluginVersionPair struct {
	// PluginRef references the Plugin resource.
	PluginRef PluginRef `json:"pluginRef"`

	// Version is the plugin version.
	Version string `json:"version"`
}

// AvailableUpdate represents an available server update.
type AvailableUpdate struct {
	// PaperVersion is the available Paper version.
	PaperVersion string `json:"paperVersion"`

	// PaperBuild is the available Paper build number.
	PaperBuild int `json:"paperBuild"`

	// ReleasedAt is when this Paper version was released.
	ReleasedAt metav1.Time `json:"releasedAt"`

	// Plugins lists plugin versions for this update.
	Plugins []PluginVersionPair `json:"plugins"`

	// FoundAt is when this update was discovered.
	FoundAt metav1.Time `json:"foundAt"`
}

// UpdateHistory records the last update attempt.
type UpdateHistory struct {
	// AppliedAt is when the update was applied.
	AppliedAt metav1.Time `json:"appliedAt"`

	// PreviousPaperVersion is the Paper version before the update.
	PreviousPaperVersion string `json:"previousPaperVersion"`

	// Successful indicates if the update succeeded.
	Successful bool `json:"successful"`
}

// UpdateBlockedStatus indicates if updates are blocked due to compatibility issues.
type UpdateBlockedStatus struct {
	// Blocked indicates if updates are currently blocked.
	Blocked bool `json:"blocked"`

	// Reason provides a human-readable explanation for the block.
	// +optional
	Reason string `json:"reason,omitempty"`

	// BlockedBy contains details about which plugin is blocking the update.
	// +optional
	BlockedBy *BlockedByInfo `json:"blockedBy,omitempty"`
}

// BlockedByInfo contains details about which plugin is blocking an update.
type BlockedByInfo struct {
	// Plugin is the name of the Plugin resource blocking the update.
	Plugin string `json:"plugin"`

	// Version is the current/desired version of the blocking plugin.
	Version string `json:"version"`

	// SupportedPaperVersions lists Paper versions this plugin supports.
	// +optional
	SupportedPaperVersions []string `json:"supportedPaperVersions,omitempty"`
}

// PaperMCServerStatus defines the observed state of PaperMCServer.
type PaperMCServerStatus struct {
	// CurrentPaperVersion is the currently running Paper version.
	// +optional
	CurrentPaperVersion string `json:"currentPaperVersion,omitempty"`

	// CurrentPaperBuild is the currently running Paper build number.
	// +optional
	CurrentPaperBuild int `json:"currentPaperBuild,omitempty"`

	// DesiredPaperVersion is the target Paper version the operator wants to run.
	// +optional
	DesiredPaperVersion string `json:"desiredPaperVersion,omitempty"`

	// DesiredPaperBuild is the target Paper build number the operator wants to run.
	// +optional
	DesiredPaperBuild int `json:"desiredPaperBuild,omitempty"`

	// Plugins lists matched Plugin resources and their versions.
	// +optional
	Plugins []ServerPluginStatus `json:"plugins,omitempty"`

	// AvailableUpdate contains the next available update if any.
	// +optional
	AvailableUpdate *AvailableUpdate `json:"availableUpdate,omitempty"`

	// LastUpdate records the most recent update attempt.
	// +optional
	LastUpdate *UpdateHistory `json:"lastUpdate,omitempty"`

	// UpdateBlocked indicates if updates are blocked due to compatibility issues.
	// +optional
	UpdateBlocked *UpdateBlockedStatus `json:"updateBlocked,omitempty"`

	// Conditions represent the current state of the PaperMCServer resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// PaperMCServer is the Schema for the papermcservers API.
type PaperMCServer struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of PaperMCServer
	// +required
	Spec PaperMCServerSpec `json:"spec"`

	// status defines the observed state of PaperMCServer
	// +optional
	Status PaperMCServerStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// PaperMCServerList contains a list of PaperMCServer.
type PaperMCServerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PaperMCServer `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PaperMCServer{}, &PaperMCServerList{})
}
