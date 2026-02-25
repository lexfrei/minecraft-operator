/*
Copyright 2026, Aleksei Sviridkin.

SPDX-License-Identifier: BSD-3-Clause
*/

package v1beta1

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

// GatewayConfig defines optional Gateway API TCPRoute/UDPRoute for game traffic.
type GatewayConfig struct {
	// Enabled determines if Gateway API routes are created for this server.
	Enabled bool `json:"enabled"`

	// ParentRefs are references to the Gateway(s) that routes should attach to.
	// +optional
	ParentRefs []GatewayParentRef `json:"parentRefs,omitempty"`

	// TCPRoute configures the TCPRoute resource for game traffic.
	// +optional
	TCPRoute *RouteConfig `json:"tcpRoute,omitempty"`

	// UDPRoute configures the UDPRoute resource for game traffic.
	// +optional
	UDPRoute *RouteConfig `json:"udpRoute,omitempty"`
}

// GatewayParentRef references a Gateway that routes should attach to.
type GatewayParentRef struct {
	// Name is the name of the Gateway.
	Name string `json:"name"`

	// Namespace is the namespace of the Gateway.
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// SectionName is the name of a specific listener on the Gateway to attach to.
	// +optional
	SectionName string `json:"sectionName,omitempty"`
}

// RouteConfig configures an individual Gateway API route.
type RouteConfig struct {
	// Enabled determines if this route type is created.
	Enabled bool `json:"enabled"`
}

// NetworkConfig defines network policy configuration for a PaperMCServer.
type NetworkConfig struct {
	// NetworkPolicy configures the NetworkPolicy for this server.
	// +optional
	NetworkPolicy *ServerNetworkPolicy `json:"networkPolicy,omitempty"`
}

// ServerNetworkPolicy configures a Kubernetes NetworkPolicy for a PaperMCServer.
type ServerNetworkPolicy struct {
	// Enabled determines if NetworkPolicy is created for this server.
	Enabled bool `json:"enabled"`

	// AllowFrom defines additional ingress sources for the Minecraft port.
	// Each entry is a standard Kubernetes NetworkPolicyPeer.
	// +optional
	AllowFrom []NetworkPolicySource `json:"allowFrom,omitempty"`

	// RestrictEgress restricts outbound traffic to DNS only.
	// +optional
	// +kubebuilder:default=true
	RestrictEgress *bool `json:"restrictEgress,omitempty"`

	// AllowEgressTo defines additional egress destinations when restrictEgress is true.
	// +optional
	AllowEgressTo []NetworkPolicyDestination `json:"allowEgressTo,omitempty"`
}

// NetworkPolicySource defines an ingress source (CIDR or pod/namespace selector).
type NetworkPolicySource struct {
	// CIDR is an IPv4 block in CIDR notation (e.g., "10.0.0.0/8").
	// IPv6 CIDRs are not supported. Semantic validation (valid octets, prefix length)
	// is performed by the Kubernetes NetworkPolicy API at apply time.
	// +optional
	// +kubebuilder:validation:Pattern=`^([0-9]{1,3}\.){3}[0-9]{1,3}/[0-9]{1,2}$`
	CIDR string `json:"cidr,omitempty"`

	// PodSelector matches pods in the same namespace.
	// +optional
	PodSelector *metav1.LabelSelector `json:"podSelector,omitempty"`

	// NamespaceSelector matches pods in selected namespaces.
	// +optional
	NamespaceSelector *metav1.LabelSelector `json:"namespaceSelector,omitempty"`
}

// NetworkPolicyDestination defines an egress destination (CIDR or port).
type NetworkPolicyDestination struct {
	// CIDR is an IPv4 block in CIDR notation (e.g., "203.0.113.0/24").
	// IPv6 CIDRs are not supported. Semantic validation (valid octets, prefix length)
	// is performed by the Kubernetes NetworkPolicy API at apply time.
	// +optional
	// +kubebuilder:validation:Pattern=`^([0-9]{1,3}\.){3}[0-9]{1,3}/[0-9]{1,2}$`
	CIDR string `json:"cidr,omitempty"`

	// Port is the destination port number.
	// +optional
	Port *int32 `json:"port,omitempty"`

	// Protocol is the protocol (TCP or UDP). Defaults to TCP.
	// +optional
	// +kubebuilder:validation:Enum=TCP;UDP
	Protocol *corev1.Protocol `json:"protocol,omitempty"`
}

// ServiceConfig defines configuration for the Kubernetes Service.
type ServiceConfig struct {
	// Type is the Service type (LoadBalancer, NodePort, or ClusterIP).
	// +optional
	// +kubebuilder:default=LoadBalancer
	// +kubebuilder:validation:Enum=LoadBalancer;NodePort;ClusterIP
	Type corev1.ServiceType `json:"type,omitempty"`

	// Annotations are custom annotations for the Service (e.g., for LoadBalancer configuration).
	// Examples: service.cilium.io/global, metallb.universe.tf/loadBalancerIPs
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`

	// LoadBalancerIP is the IP address for LoadBalancer type services.
	// +optional
	LoadBalancerIP string `json:"loadBalancerIP,omitempty"`
}

// BackupSpec configures VolumeSnapshot-based backups for the Minecraft server.
type BackupSpec struct {
	// Enabled determines if backups are enabled.
	Enabled bool `json:"enabled"`

	// Schedule is the cron schedule for periodic backups (e.g., "0 */6 * * *").
	// +optional
	Schedule string `json:"schedule,omitempty"`

	// BeforeUpdate creates a backup before any server update is applied.
	// +optional
	// +kubebuilder:default=true
	BeforeUpdate *bool `json:"beforeUpdate,omitempty"`

	// VolumeSnapshotClassName is the VolumeSnapshotClass to use for creating snapshots.
	// If empty, the cluster default VolumeSnapshotClass is used.
	// +optional
	VolumeSnapshotClassName string `json:"volumeSnapshotClassName,omitempty"`

	// Retention defines how many backup snapshots to keep.
	// +optional
	Retention BackupRetention `json:"retention,omitempty"`
}

// BackupRetention defines backup retention policy.
type BackupRetention struct {
	// MaxCount is the maximum number of VolumeSnapshots to retain per server.
	// Oldest snapshots are deleted first when the limit is exceeded.
	// +optional
	// +kubebuilder:default=10
	// +kubebuilder:validation:Minimum=1
	MaxCount int32 `json:"maxCount,omitempty"`
}

// BackupStatus represents the observed backup state for the server.
type BackupStatus struct {
	// LastBackup records the most recent backup attempt.
	// +optional
	LastBackup *BackupRecord `json:"lastBackup,omitempty"`

	// BackupCount is the current number of retained VolumeSnapshots.
	BackupCount int32 `json:"backupCount"`
}

// BackupRecord records information about a single backup.
type BackupRecord struct {
	// SnapshotName is the name of the VolumeSnapshot resource.
	// Empty when the backup failed before snapshot creation.
	// +optional
	SnapshotName string `json:"snapshotName,omitempty"`

	// StartedAt is when the backup process started.
	StartedAt metav1.Time `json:"startedAt"`

	// CompletedAt is when the backup process completed.
	// +optional
	CompletedAt *metav1.Time `json:"completedAt,omitempty"`

	// Successful indicates if the backup completed successfully.
	Successful bool `json:"successful"`

	// Trigger describes what triggered the backup (scheduled, before-update, manual).
	Trigger string `json:"trigger"`
}

// PaperMCServerSpec defines the desired state of PaperMCServer.
type PaperMCServerSpec struct {
	// UpdateStrategy defines the update strategy for Paper version.
	// Valid values: "latest", "auto", "pin", "build-pin".
	// - latest: always use latest available version from Docker Hub
	// - auto: use constraint solver to find best version compatible with plugins
	// - pin: pin to specific version, auto-update to latest build (requires version field)
	// - build-pin: pin to specific version and build (requires version and build fields)
	// +kubebuilder:validation:Enum=latest;auto;pin;build-pin
	UpdateStrategy string `json:"updateStrategy"`

	// Version specifies the target Paper version (required for pin and build-pin strategies).
	// Example: "1.21.1" for a Minecraft version.
	// +optional
	Version string `json:"version,omitempty"`

	// Build specifies the target Paper build number (required for build-pin strategy, optional for pin).
	// If set with pin strategy, it serves as a minimum build; operator will still auto-update to newer builds.
	// If set with build-pin strategy, operator will pin to this exact build.
	// +optional
	Build *int `json:"build,omitempty"`

	// UpdateDelay is the grace period before applying Paper updates.
	// +optional
	UpdateDelay *metav1.Duration `json:"updateDelay,omitempty"`

	// UpdateSchedule defines when to check and apply updates.
	UpdateSchedule UpdateSchedule `json:"updateSchedule"`

	// GracefulShutdown configures graceful server shutdown.
	GracefulShutdown GracefulShutdown `json:"gracefulShutdown"`

	// RCON configures RCON for graceful shutdown.
	RCON RCONConfig `json:"rcon"`

	// Service configures the Kubernetes Service for this server.
	// +optional
	Service ServiceConfig `json:"service,omitempty"`

	// Gateway configures optional Gateway API TCPRoute/UDPRoute for game traffic.
	// Requires Gateway API CRDs (experimental channel) installed in the cluster.
	// +optional
	Gateway *GatewayConfig `json:"gateway,omitempty"`

	// Backup configures VolumeSnapshot-based backups.
	// +optional
	Backup *BackupSpec `json:"backup,omitempty"`

	// Network configures network policies for this server.
	// +optional
	Network *NetworkConfig `json:"network,omitempty"`

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

	// PendingDeletion marks the plugin for removal on next server restart.
	// This is set when the Plugin CRD is deleted but the JAR is still on disk.
	// +optional
	PendingDeletion bool `json:"pendingDeletion,omitempty"`

	// InstalledJARName is the filename of the installed JAR in /data/plugins/.
	// Example: "BlueMap-5.4-paper.jar". Used for targeted deletion.
	// +optional
	InstalledJARName string `json:"installedJarName,omitempty"`
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
	// Version is the available Paper version.
	Version string `json:"version"`

	// Build is the available Paper build number.
	Build int `json:"build"`

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

	// PreviousVersion is the Paper version before the update.
	PreviousVersion string `json:"previousVersion"`

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

	// SupportedVersions lists Paper versions this plugin supports.
	// +optional
	SupportedVersions []string `json:"supportedVersions,omitempty"`
}

// PaperMCServerStatus defines the observed state of PaperMCServer.
type PaperMCServerStatus struct {
	// CurrentVersion is the currently running Paper version.
	// +optional
	CurrentVersion string `json:"currentVersion,omitempty"`

	// CurrentBuild is the currently running Paper build number.
	// +optional
	CurrentBuild int `json:"currentBuild,omitempty"`

	// DesiredVersion is the target Paper version the operator wants to run.
	// +optional
	DesiredVersion string `json:"desiredVersion,omitempty"`

	// DesiredBuild is the target Paper build number the operator wants to run.
	// +optional
	DesiredBuild int `json:"desiredBuild,omitempty"`

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

	// Backup represents the observed backup state.
	// +optional
	Backup *BackupStatus `json:"backup,omitempty"`

	// Conditions represent the current state of the PaperMCServer resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:storageversion

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
