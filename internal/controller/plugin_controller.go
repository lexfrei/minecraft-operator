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

package controller

import (
	"context"
	"time"

	"github.com/cockroachdb/errors"
	mcv1alpha1 "github.com/lexfrei/minecraft-operator/api/v1alpha1"
	"github.com/lexfrei/minecraft-operator/pkg/plugins"
	"github.com/lexfrei/minecraft-operator/pkg/selector"
	"github.com/lexfrei/minecraft-operator/pkg/solver"
	"github.com/lexfrei/minecraft-operator/pkg/version"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	conditionTypeReady               = "Ready"
	conditionTypeRepositoryAvailable = "RepositoryAvailable"
	conditionTypeVersionResolved     = "VersionResolved"

	reasonReconcileSuccess    = "ReconcileSuccess"
	reasonReconcileError      = "ReconcileError"
	reasonAvailable           = "Available"
	reasonUnavailable         = "Unavailable"
	reasonResolved            = "Resolved"
	reasonNoCompatibleVersion = "NoCompatibleVersion"
	reasonPinnedNotFound      = "PinnedVersionNotFound"

	repositoryStatusAvailable   = "available"
	repositoryStatusUnavailable = "unavailable"
	repositoryStatusOrphaned    = "orphaned"
)

// PluginReconciler reconciles a Plugin object.
type PluginReconciler struct {
	client.Client
	Scheme       *runtime.Scheme
	PluginClient plugins.PluginClient
	Solver       solver.Solver
}

//+kubebuilder:rbac:groups=mc.k8s.lex.la,resources=plugins,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=mc.k8s.lex.la,resources=plugins/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=mc.k8s.lex.la,resources=plugins/finalizers,verbs=update
//+kubebuilder:rbac:groups=mc.k8s.lex.la,resources=papermcservers,verbs=get;list;watch
//nolint:revive // kubebuilder markers require no space after //

// Reconcile implements the reconciliation loop for Plugin resources.
func (r *PluginReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)

	// Fetch the Plugin resource
	var plugin mcv1alpha1.Plugin
	if err := r.Get(ctx, req.NamespacedName, &plugin); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("Plugin resource not found, ignoring")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get Plugin resource")
		return ctrl.Result{}, errors.Wrap(err, "failed to get plugin")
	}

	// Store original status for comparison
	originalStatus := plugin.Status.DeepCopy()

	// Run reconciliation logic
	result, err := r.doReconcile(ctx, &plugin)

	// Update status if changed
	if err != nil || !statusEqual(&plugin.Status, originalStatus) {
		if updateErr := r.Status().Update(ctx, &plugin); updateErr != nil {
			log.Error(updateErr, "Failed to update Plugin status")
			if err == nil {
				err = updateErr
			}
		}
	}

	// Set conditions based on result
	if err != nil {
		log.Error(err, "Reconciliation failed")
		r.setCondition(&plugin, conditionTypeReady, metav1.ConditionFalse, reasonReconcileError, err.Error())
	} else {
		r.setCondition(&plugin, conditionTypeReady, metav1.ConditionTrue, reasonReconcileSuccess, "Plugin reconciled successfully")
	}

	return result, err
}

// doReconcile performs the actual reconciliation logic.
func (r *PluginReconciler) doReconcile(ctx context.Context, plugin *mcv1alpha1.Plugin) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)

	// Step 1: Fetch and cache plugin metadata
	allVersions, result, err := r.syncPluginMetadata(ctx, plugin)
	if err != nil {
		return result, err
	}

	// Step 2: Find matched servers
	matchedServers, err := r.findMatchedServers(ctx, plugin)
	if err != nil {
		return ctrl.Result{}, err
	}

	log.Info("Found matching servers", "count", len(matchedServers))

	// Step 3: Resolve plugin version
	resolvedVersion := r.resolveVersion(ctx, plugin, matchedServers, allVersions)

	// Step 4: Update status
	plugin.Status.ResolvedVersion = resolvedVersion
	plugin.Status.MatchedInstances = buildMatchedInstances(matchedServers, resolvedVersion, allVersions)

	// Step 5: Trigger reconciliation for matched PaperMCServer instances
	if err := r.enqueueMatchedServers(ctx, matchedServers); err != nil {
		log.Error(err, "Failed to enqueue server reconciliations")
	}

	return ctrl.Result{RequeueAfter: 15 * time.Minute}, nil
}

// syncPluginMetadata fetches plugin metadata and updates cache in status.
func (r *PluginReconciler) syncPluginMetadata(
	ctx context.Context,
	plugin *mcv1alpha1.Plugin,
) ([]plugins.PluginVersion, ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)

	allVersions, repoErr := r.fetchPluginMetadata(ctx, plugin)

	if repoErr != nil {
		log.Error(repoErr, "Failed to fetch plugin metadata, using cached versions")
		return r.handleRepositoryError(plugin, repoErr)
	}

	// Repository available - update cache
	plugin.Status.RepositoryStatus = repositoryStatusAvailable
	r.setCondition(plugin, conditionTypeRepositoryAvailable, metav1.ConditionTrue,
		reasonAvailable, "Repository accessible")

	now := metav1.Now()
	plugin.Status.LastFetched = &now
	plugin.Status.AvailableVersions = convertToPluginVersionInfo(allVersions)

	return allVersions, ctrl.Result{}, nil
}

// handleRepositoryError handles repository fetch errors by falling back to cached data.
func (r *PluginReconciler) handleRepositoryError(
	plugin *mcv1alpha1.Plugin,
	repoErr error,
) ([]plugins.PluginVersion, ctrl.Result, error) {
	if len(plugin.Status.AvailableVersions) > 0 {
		// Use orphaned status with cached data
		plugin.Status.RepositoryStatus = repositoryStatusOrphaned
		r.setCondition(plugin, conditionTypeRepositoryAvailable, metav1.ConditionFalse,
			reasonUnavailable, "Repository unavailable, using cached data")
		return convertCachedVersions(plugin.Status.AvailableVersions), ctrl.Result{}, nil
	}

	// No cached data available
	plugin.Status.RepositoryStatus = repositoryStatusUnavailable
	r.setCondition(plugin, conditionTypeRepositoryAvailable, metav1.ConditionFalse,
		reasonUnavailable, repoErr.Error())
	return nil, ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
}

// findMatchedServers finds all PaperMCServer instances matching the plugin selector.
func (r *PluginReconciler) findMatchedServers(
	ctx context.Context,
	plugin *mcv1alpha1.Plugin,
) ([]mcv1alpha1.PaperMCServer, error) {
	servers, err := selector.FindMatchingServers(
		ctx,
		r.Client,
		plugin.Namespace,
		plugin.Spec.InstanceSelector,
	)
	if err != nil {
		return nil, errors.Wrap(err, "failed to find matching servers")
	}
	return servers, nil
}

// resolveVersion resolves the plugin version based on matched servers and policy.
func (r *PluginReconciler) resolveVersion(
	ctx context.Context,
	plugin *mcv1alpha1.Plugin,
	matchedServers []mcv1alpha1.PaperMCServer,
	allVersions []plugins.PluginVersion,
) string {
	log := ctrl.LoggerFrom(ctx)

	if len(matchedServers) == 0 {
		log.Info("No matched servers, skipping solver")
		r.setCondition(plugin, conditionTypeVersionResolved, metav1.ConditionTrue,
			reasonResolved, "No servers matched")
		return ""
	}

	resolvedVersion, err := r.resolvePluginVersion(ctx, plugin, matchedServers, allVersions)
	if err != nil {
		log.Error(err, "Failed to resolve plugin version")
		r.setCondition(plugin, conditionTypeVersionResolved, metav1.ConditionFalse,
			reasonNoCompatibleVersion, err.Error())
		return ""
	}

	log.Info("Resolved plugin version", "version", resolvedVersion)
	r.setCondition(plugin, conditionTypeVersionResolved, metav1.ConditionTrue,
		reasonResolved, "Version resolved successfully")
	return resolvedVersion
}

// fetchPluginMetadata fetches plugin metadata from the repository.
func (r *PluginReconciler) fetchPluginMetadata(
	ctx context.Context,
	plugin *mcv1alpha1.Plugin,
) ([]plugins.PluginVersion, error) {
	if plugin.Spec.Source.Type != "hangar" {
		return nil, errors.Newf("unsupported source type: %s", plugin.Spec.Source.Type)
	}

	versions, err := r.PluginClient.GetVersions(ctx, plugin.Spec.Source.Project)
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch versions from repository")
	}

	return versions, nil
}

// resolvePluginVersion runs the constraint solver to find the best plugin version.
func (r *PluginReconciler) resolvePluginVersion(
	ctx context.Context,
	plugin *mcv1alpha1.Plugin,
	matchedServers []mcv1alpha1.PaperMCServer,
	allVersions []plugins.PluginVersion,
) (string, error) {
	log := ctrl.LoggerFrom(ctx)

	// Handle pinned version policy
	if plugin.Spec.VersionPolicy == versionPolicyPinned {
		if plugin.Spec.PinnedVersion == "" {
			return "", errors.New("versionPolicy is pinned but pinnedVersion is not set")
		}

		// Verify pinned version exists
		found := false
		for _, v := range allVersions {
			if v.Version == plugin.Spec.PinnedVersion {
				found = true
				break
			}
		}

		if !found {
			r.setCondition(plugin, conditionTypeVersionResolved, metav1.ConditionFalse,
				reasonPinnedNotFound, "Pinned version not found in repository")
			return "", errors.Newf("pinned version %s not found", plugin.Spec.PinnedVersion)
		}

		log.Info("Using pinned version", "version", plugin.Spec.PinnedVersion)
		return plugin.Spec.PinnedVersion, nil
	}

	// Apply update delay filter
	filteredVersions := applyUpdateDelay(allVersions, plugin.Spec.UpdateDelay)
	log.Info("Applied update delay filter",
		"original", len(allVersions),
		"filtered", len(filteredVersions))

	if len(filteredVersions) == 0 {
		return "", errors.New("no versions available after applying update delay")
	}

	// Run solver
	resolvedVersion, err := r.Solver.FindBestPluginVersion(ctx, plugin, matchedServers, filteredVersions)
	if err != nil {
		return "", errors.Wrap(err, "solver failed to find compatible version")
	}

	if resolvedVersion == "" {
		return "", errors.New("no compatible version found for all matched servers")
	}

	return resolvedVersion, nil
}

// applyUpdateDelay filters versions based on the update delay policy.
func applyUpdateDelay(versions []plugins.PluginVersion, updateDelay *metav1.Duration) []plugins.PluginVersion {
	if updateDelay == nil {
		return versions
	}

	// Convert to version.VersionInfo for filtering
	versionInfos := make([]version.VersionInfo, len(versions))
	for i, v := range versions {
		versionInfos[i] = version.VersionInfo{
			Version:     v.Version,
			ReleaseDate: v.ReleaseDate,
		}
	}

	filtered := version.FilterByUpdateDelay(versionInfos, updateDelay.Duration)

	// Convert back to plugins.PluginVersion
	result := make([]plugins.PluginVersion, 0, len(filtered))
	for _, f := range filtered {
		for _, v := range versions {
			if v.Version == f.Version {
				result = append(result, v)
				break
			}
		}
	}

	return result
}

// buildMatchedInstances constructs the list of matched instances with compatibility info.
func buildMatchedInstances(
	servers []mcv1alpha1.PaperMCServer,
	resolvedVersion string,
	allVersions []plugins.PluginVersion,
) []mcv1alpha1.MatchedInstance {
	instances := make([]mcv1alpha1.MatchedInstance, 0, len(servers))

	for _, server := range servers {
		compatible := false
		if resolvedVersion != "" {
			// Check compatibility
			for _, v := range allVersions {
				if v.Version == resolvedVersion {
					compatible = isVersionCompatible(server.Status.CurrentPaperVersion, v.MinecraftVersions)
					break
				}
			}
		}

		instances = append(instances, mcv1alpha1.MatchedInstance{
			Name:         server.Name,
			Namespace:    server.Namespace,
			PaperVersion: server.Status.CurrentPaperVersion,
			Compatible:   compatible,
		})
	}

	return instances
}

// isVersionCompatible checks if a Paper version is compatible with the plugin.
func isVersionCompatible(paperVersion string, minecraftVersions []string) bool {
	if paperVersion == "" {
		return false
	}

	if len(minecraftVersions) == 0 {
		return true
	}

	for _, mcVersion := range minecraftVersions {
		if mcVersion == paperVersion {
			return true
		}
	}

	return false
}

// convertToPluginVersionInfo converts plugin versions to status PluginVersionInfo.
func convertToPluginVersionInfo(versions []plugins.PluginVersion) []mcv1alpha1.PluginVersionInfo {
	infos := make([]mcv1alpha1.PluginVersionInfo, len(versions))
	now := metav1.Now()

	for i, v := range versions {
		infos[i] = mcv1alpha1.PluginVersionInfo{
			Version:           v.Version,
			MinecraftVersions: v.MinecraftVersions,
			DownloadURL:       v.DownloadURL,
			Hash:              v.Hash,
			CachedAt:          now,
			ReleasedAt:        metav1.NewTime(v.ReleaseDate),
		}
	}

	return infos
}

// convertCachedVersions converts cached PluginVersionInfo back to PluginVersion.
func convertCachedVersions(cached []mcv1alpha1.PluginVersionInfo) []plugins.PluginVersion {
	versions := make([]plugins.PluginVersion, len(cached))

	for i, c := range cached {
		versions[i] = plugins.PluginVersion{
			Version:           c.Version,
			ReleaseDate:       c.ReleasedAt.Time,
			MinecraftVersions: c.MinecraftVersions,
			DownloadURL:       c.DownloadURL,
			Hash:              c.Hash,
		}
	}

	return versions
}

// enqueueMatchedServers triggers reconciliation for matched PaperMCServer instances.
func (r *PluginReconciler) enqueueMatchedServers(
	ctx context.Context,
	servers []mcv1alpha1.PaperMCServer,
) error {
	// This is handled by the watch in SetupWithManager
	// The servers will be reconciled automatically when Plugin status changes
	return nil
}

// setCondition sets or updates a condition in the Plugin status.
func (r *PluginReconciler) setCondition(
	plugin *mcv1alpha1.Plugin,
	conditionType string,
	status metav1.ConditionStatus,
	reason,
	message string,
) {
	condition := metav1.Condition{
		Type:               conditionType,
		Status:             status,
		ObservedGeneration: plugin.Generation,
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	}

	meta.SetStatusCondition(&plugin.Status.Conditions, condition)
}

// statusEqual compares two Plugin statuses for equality.
func statusEqual(a, b *mcv1alpha1.PluginStatus) bool {
	if a.ResolvedVersion != b.ResolvedVersion {
		return false
	}
	if a.RepositoryStatus != b.RepositoryStatus {
		return false
	}
	if len(a.MatchedInstances) != len(b.MatchedInstances) {
		return false
	}
	if len(a.AvailableVersions) != len(b.AvailableVersions) {
		return false
	}
	return true
}

// SetupWithManager sets up the controller with the Manager.
func (r *PluginReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&mcv1alpha1.Plugin{}).
		Watches(
			&mcv1alpha1.PaperMCServer{},
			handler.EnqueueRequestsFromMapFunc(r.findPluginsForServer),
		).
		Named("plugin").
		Complete(r)
}

// findPluginsForServer maps PaperMCServer changes to Plugin reconciliation requests.
// This ensures Plugins are reconciled when server labels change.
func (r *PluginReconciler) findPluginsForServer(ctx context.Context, obj client.Object) []reconcile.Request {
	server, ok := obj.(*mcv1alpha1.PaperMCServer)
	if !ok {
		return nil
	}

	log := ctrl.LoggerFrom(ctx)

	// Find all plugins in the same namespace that might match this server
	var pluginList mcv1alpha1.PluginList
	if err := r.List(ctx, &pluginList, client.InNamespace(server.Namespace)); err != nil {
		log.Error(err, "Failed to list plugins for server watch")
		return nil
	}

	var requests []reconcile.Request
	for i := range pluginList.Items {
		plugin := &pluginList.Items[i]

		// Check if this plugin's selector might match the server
		matches, err := selector.MatchesSelector(server.Labels, plugin.Spec.InstanceSelector)
		if err != nil {
			log.Error(err, "Failed to check selector match", "plugin", plugin.Name)
			continue
		}

		if matches {
			requests = append(requests, reconcile.Request{
				NamespacedName: client.ObjectKey{
					Name:      plugin.Name,
					Namespace: plugin.Namespace,
				},
			})
		}
	}

	log.Info("Server change triggered plugin reconciliations",
		"server", server.Name,
		"plugins", len(requests))

	return requests
}
