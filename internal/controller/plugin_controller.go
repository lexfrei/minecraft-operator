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
	"log/slog"
	"time"

	"github.com/cockroachdb/errors"
	mcv1alpha1 "github.com/lexfrei/minecraft-operator/api/v1alpha1"
	"github.com/lexfrei/minecraft-operator/pkg/plugins"
	"github.com/lexfrei/minecraft-operator/pkg/selector"
	"github.com/lexfrei/minecraft-operator/pkg/solver"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	conditionTypeReady               = "Ready"
	conditionTypeRepositoryAvailable = "RepositoryAvailable"
	conditionTypeVersionResolved     = "VersionResolved"

	reasonReconcileSuccess = "ReconcileSuccess"
	reasonReconcileError   = "ReconcileError"
	reasonAvailable        = "Available"
	reasonUnavailable      = "Unavailable"
	reasonResolved         = "Resolved"

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
	// Fetch the Plugin resource
	var plugin mcv1alpha1.Plugin
	if err := r.Get(ctx, req.NamespacedName, &plugin); err != nil {
		if apierrors.IsNotFound(err) {
			slog.InfoContext(ctx, "Plugin resource not found, ignoring")
			return ctrl.Result{}, nil
		}
		slog.ErrorContext(ctx, "Failed to get Plugin resource", "error", err)
		return ctrl.Result{}, errors.Wrap(err, "failed to get plugin")
	}

	// Handle deletion
	if !plugin.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &plugin)
	}

	// Ensure finalizer is present
	if !controllerutil.ContainsFinalizer(&plugin, PluginFinalizer) {
		slog.InfoContext(ctx, "Adding finalizer to Plugin", "plugin", plugin.Name)
		controllerutil.AddFinalizer(&plugin, PluginFinalizer)
		if err := r.Update(ctx, &plugin); err != nil {
			slog.ErrorContext(ctx, "Failed to add finalizer", "error", err)
			return ctrl.Result{}, errors.Wrap(err, "failed to add finalizer")
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Store original status for comparison
	originalStatus := plugin.Status.DeepCopy()

	// Run reconciliation logic
	result, err := r.doReconcile(ctx, &plugin)

	// Set conditions based on result BEFORE status update so they are persisted
	if err != nil {
		slog.ErrorContext(ctx, "Reconciliation failed", "error", err)
		r.setCondition(&plugin, conditionTypeReady, metav1.ConditionFalse, reasonReconcileError, err.Error())
	} else {
		r.setCondition(&plugin, conditionTypeReady, metav1.ConditionTrue, reasonReconcileSuccess, "Plugin reconciled successfully")
	}

	// Update status if changed (includes conditions set above)
	if err != nil || !statusEqual(&plugin.Status, originalStatus) {
		if updateErr := r.Status().Update(ctx, &plugin); updateErr != nil {
			slog.ErrorContext(ctx, "Failed to update Plugin status", "error", updateErr)
			if err == nil {
				err = updateErr
			}
		}
	}

	return result, err
}

// doReconcile performs the actual reconciliation logic.
func (r *PluginReconciler) doReconcile(ctx context.Context, plugin *mcv1alpha1.Plugin) (ctrl.Result, error) {
	// Step 1: Fetch and cache plugin metadata
	_, result, err := r.syncPluginMetadata(ctx, plugin)
	if err != nil {
		return result, err
	}

	// Step 2: Find matched servers
	matchedServers, err := r.findMatchedServers(ctx, plugin)
	if err != nil {
		return ctrl.Result{}, err
	}

	slog.InfoContext(ctx, "Found matching servers", "count", len(matchedServers))

	// Step 3: Update status (version resolution moved to PaperMCServer controller)
	plugin.Status.MatchedInstances = buildMatchedInstances(matchedServers)

	// Update condition - metadata fetched successfully
	r.setCondition(plugin, conditionTypeVersionResolved, metav1.ConditionTrue,
		reasonResolved, "Metadata fetched and servers matched")

	// Step 4: Trigger reconciliation for matched PaperMCServer instances
	// They will resolve plugin versions individually
	if err := r.enqueueMatchedServers(ctx, matchedServers); err != nil {
		slog.ErrorContext(ctx, "Failed to enqueue server reconciliations", "error", err)
	}

	return ctrl.Result{RequeueAfter: 15 * time.Minute}, nil
}

// syncPluginMetadata fetches plugin metadata and updates cache in status.
func (r *PluginReconciler) syncPluginMetadata(
	ctx context.Context,
	plugin *mcv1alpha1.Plugin,
) ([]plugins.PluginVersion, ctrl.Result, error) {
	allVersions, repoErr := r.fetchPluginMetadata(ctx, plugin)

	if repoErr != nil {
		slog.ErrorContext(ctx, "Failed to fetch plugin metadata, using cached versions", "error", repoErr)
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

// buildMatchedInstances constructs the list of matched instances.
// Compatibility check is now done in PaperMCServer controller during version resolution.
func buildMatchedInstances(servers []mcv1alpha1.PaperMCServer) []mcv1alpha1.MatchedInstance {
	instances := make([]mcv1alpha1.MatchedInstance, 0, len(servers))

	for _, server := range servers {
		instances = append(instances, mcv1alpha1.MatchedInstance{
			Name:       server.Name,
			Namespace:  server.Namespace,
			Version:    server.Status.CurrentVersion,
			Compatible: true, // Compatibility check moved to PaperMCServer controller
		})
	}

	return instances
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
	if a.RepositoryStatus != b.RepositoryStatus {
		return false
	}

	if len(a.MatchedInstances) != len(b.MatchedInstances) {
		return false
	}

	for i := range a.MatchedInstances {
		if a.MatchedInstances[i].Name != b.MatchedInstances[i].Name ||
			a.MatchedInstances[i].Namespace != b.MatchedInstances[i].Namespace ||
			a.MatchedInstances[i].Compatible != b.MatchedInstances[i].Compatible ||
			a.MatchedInstances[i].Version != b.MatchedInstances[i].Version {
			return false
		}
	}

	if len(a.AvailableVersions) != len(b.AvailableVersions) {
		return false
	}

	for i := range a.AvailableVersions {
		if a.AvailableVersions[i].Version != b.AvailableVersions[i].Version ||
			a.AvailableVersions[i].DownloadURL != b.AvailableVersions[i].DownloadURL ||
			a.AvailableVersions[i].Hash != b.AvailableVersions[i].Hash {
			return false
		}
	}

	// Compare Conditions
	if len(a.Conditions) != len(b.Conditions) {
		return false
	}

	for i := range a.Conditions {
		if a.Conditions[i].Type != b.Conditions[i].Type ||
			a.Conditions[i].Status != b.Conditions[i].Status ||
			a.Conditions[i].Reason != b.Conditions[i].Reason ||
			a.Conditions[i].Message != b.Conditions[i].Message {
			return false
		}
	}

	return true
}

// reconcileDelete handles Plugin deletion with finalizer.
// It ensures JARs are deleted from all matched servers before removing the finalizer.
func (r *PluginReconciler) reconcileDelete(
	ctx context.Context,
	plugin *mcv1alpha1.Plugin,
) (ctrl.Result, error) {
	slog.InfoContext(ctx, "Reconciling Plugin deletion", "plugin", plugin.Name)

	// Check if finalizer is present
	if !controllerutil.ContainsFinalizer(plugin, PluginFinalizer) {
		slog.InfoContext(ctx, "Finalizer already removed, deletion can proceed")
		return ctrl.Result{}, nil
	}

	// Step 1: Initialize DeletionProgress if empty
	if err := r.initDeletionProgressIfNeeded(ctx, plugin); err != nil {
		return ctrl.Result{}, err
	}

	// Step 2: Remove entries for deleted servers (prevents deadlock)
	if err := r.cleanupDeletedServers(ctx, plugin); err != nil {
		slog.ErrorContext(ctx, "Failed to cleanup deleted servers", "error", err)
		return ctrl.Result{}, errors.Wrap(err, "failed to cleanup deleted servers")
	}

	// Step 3: Mark plugins as PendingDeletion in each server's status
	if err := r.markPluginForDeletionOnServers(ctx, plugin); err != nil {
		slog.ErrorContext(ctx, "Failed to mark plugin for deletion on servers", "error", err)
		return ctrl.Result{}, errors.Wrap(err, "failed to mark plugin for deletion")
	}

	// Step 4: Force-complete stale deletion entries to prevent deadlocks
	r.forceCompleteStaleDeletions(ctx, plugin)

	// Persist force-completion changes
	if err := r.Status().Update(ctx, plugin); err != nil {
		slog.ErrorContext(ctx, "Failed to persist force-completion changes", "error", err)
		return ctrl.Result{}, errors.Wrap(err, "failed to persist force-completion")
	}

	// Step 5: Check if all JARs have been deleted
	if !r.allJARsDeleted(plugin) {
		slog.InfoContext(ctx, "Waiting for JAR deletion on servers",
			"plugin", plugin.Name,
			"progress", plugin.Status.DeletionProgress)
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	// Step 6: All JARs deleted, remove finalizer
	return r.removeFinalizer(ctx, plugin)
}

// initDeletionProgressIfNeeded initializes DeletionProgress for all matched servers.
func (r *PluginReconciler) initDeletionProgressIfNeeded(
	ctx context.Context,
	plugin *mcv1alpha1.Plugin,
) error {
	if len(plugin.Status.DeletionProgress) > 0 {
		return nil // Already initialized
	}

	matchedServers, err := r.findMatchedServers(ctx, plugin)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to find matched servers during deletion", "error", err)
		return errors.Wrap(err, "failed to find matched servers")
	}

	now := metav1.Now()
	plugin.Status.DeletionProgress = make([]mcv1alpha1.DeletionProgressEntry, len(matchedServers))

	for i, server := range matchedServers {
		plugin.Status.DeletionProgress[i] = mcv1alpha1.DeletionProgressEntry{
			ServerName:          server.Name,
			Namespace:           server.Namespace,
			JARDeleted:          false,
			DeletionRequestedAt: &now,
		}
	}

	if err := r.Status().Update(ctx, plugin); err != nil {
		slog.ErrorContext(ctx, "Failed to initialize DeletionProgress", "error", err)
		return errors.Wrap(err, "failed to initialize deletion progress")
	}

	slog.InfoContext(ctx, "Initialized DeletionProgress",
		"plugin", plugin.Name,
		"servers", len(matchedServers))
	return nil
}

// deletionTimeout is the maximum time to wait for JAR deletion before force-completing.
const deletionTimeout = 10 * time.Minute

// forceCompleteStaleDeletions force-marks stale deletion entries as completed
// to prevent deadlocks when JAR deletion fails repeatedly.
func (r *PluginReconciler) forceCompleteStaleDeletions(ctx context.Context, plugin *mcv1alpha1.Plugin) {
	now := metav1.Now()

	for i := range plugin.Status.DeletionProgress {
		entry := &plugin.Status.DeletionProgress[i]
		if !entry.JARDeleted && entry.DeletionRequestedAt != nil &&
			time.Since(entry.DeletionRequestedAt.Time) > deletionTimeout {
			slog.WarnContext(ctx, "Force-completing stale deletion entry",
				"plugin", plugin.Name,
				"server", entry.ServerName,
				"requestedAt", entry.DeletionRequestedAt.Time)

			entry.JARDeleted = true
			entry.DeletedAt = &now
		}
	}
}

// allJARsDeleted checks if all JARs have been deleted from servers.
func (r *PluginReconciler) allJARsDeleted(plugin *mcv1alpha1.Plugin) bool {
	for _, progress := range plugin.Status.DeletionProgress {
		if !progress.JARDeleted {
			return false
		}
	}
	return true
}

// cleanupDeletedServers removes DeletionProgress entries for servers that no longer exist.
// This prevents deadlock when a server is deleted before the plugin JAR cleanup completes.
func (r *PluginReconciler) cleanupDeletedServers(
	ctx context.Context,
	plugin *mcv1alpha1.Plugin,
) error {
	if len(plugin.Status.DeletionProgress) == 0 {
		return nil
	}

	remaining := make([]mcv1alpha1.DeletionProgressEntry, 0, len(plugin.Status.DeletionProgress))

	for _, entry := range plugin.Status.DeletionProgress {
		// Keep entries that are already marked as deleted
		if entry.JARDeleted {
			remaining = append(remaining, entry)
			continue
		}

		// Check if server still exists
		var server mcv1alpha1.PaperMCServer
		err := r.Get(ctx, client.ObjectKey{Name: entry.ServerName, Namespace: entry.Namespace}, &server)
		if apierrors.IsNotFound(err) {
			// Server deleted - remove entry entirely (no cleanup needed)
			slog.InfoContext(ctx, "Server deleted, removing from DeletionProgress",
				"plugin", plugin.Name,
				"server", entry.ServerName,
				"namespace", entry.Namespace)
			continue // Don't add to remaining
		}
		if err != nil {
			// Unexpected error - keep entry and retry later
			slog.ErrorContext(ctx, "Failed to check server existence",
				"error", err,
				"server", entry.ServerName)
			remaining = append(remaining, entry)
			continue
		}

		// Server exists, keep the entry
		remaining = append(remaining, entry)
	}

	// Update status only if entries were removed
	if len(remaining) != len(plugin.Status.DeletionProgress) {
		slog.InfoContext(ctx, "Cleaned up DeletionProgress entries for deleted servers",
			"plugin", plugin.Name,
			"original", len(plugin.Status.DeletionProgress),
			"remaining", len(remaining))
		plugin.Status.DeletionProgress = remaining
		if err := r.Status().Update(ctx, plugin); err != nil {
			return errors.Wrap(err, "failed to update deletion progress after cleanup")
		}
	}

	return nil
}

// removeFinalizer removes the finalizer from the plugin after all cleanup is done.
func (r *PluginReconciler) removeFinalizer(
	ctx context.Context,
	plugin *mcv1alpha1.Plugin,
) (ctrl.Result, error) {
	slog.InfoContext(ctx, "All JARs deleted, removing finalizer", "plugin", plugin.Name)
	controllerutil.RemoveFinalizer(plugin, PluginFinalizer)
	if err := r.Update(ctx, plugin); err != nil {
		slog.ErrorContext(ctx, "Failed to remove finalizer", "error", err)
		return ctrl.Result{}, errors.Wrap(err, "failed to remove finalizer")
	}
	slog.InfoContext(ctx, "Plugin deletion completed", "plugin", plugin.Name)
	return ctrl.Result{}, nil
}

// markPluginForDeletionOnServers sets PendingDeletion=true for this plugin
// in all matched server statuses.
func (r *PluginReconciler) markPluginForDeletionOnServers(
	ctx context.Context,
	plugin *mcv1alpha1.Plugin,
) error {
	for _, progress := range plugin.Status.DeletionProgress {
		if progress.JARDeleted {
			continue // Already handled
		}

		// Get the server
		var server mcv1alpha1.PaperMCServer
		serverKey := client.ObjectKey{Name: progress.ServerName, Namespace: progress.Namespace}
		if err := r.Get(ctx, serverKey, &server); err != nil {
			if apierrors.IsNotFound(err) {
				// Server doesn't exist, mark as deleted
				if err := r.markJARAsDeleted(ctx, plugin, progress.ServerName, progress.Namespace); err != nil {
					return err
				}
				continue
			}
			return errors.Wrapf(err, "failed to get server %s/%s", progress.Namespace, progress.ServerName)
		}

		// Find and mark the plugin status in server
		updated := false
		for i := range server.Status.Plugins {
			if server.Status.Plugins[i].PluginRef.Name == plugin.Name &&
				server.Status.Plugins[i].PluginRef.Namespace == plugin.Namespace {
				if !server.Status.Plugins[i].PendingDeletion {
					server.Status.Plugins[i].PendingDeletion = true
					updated = true
				}
				break
			}
		}

		if updated {
			if err := r.Status().Update(ctx, &server); err != nil {
				return errors.Wrapf(err, "failed to mark plugin for deletion on server %s", server.Name)
			}
			slog.InfoContext(ctx, "Marked plugin for deletion on server",
				"plugin", plugin.Name,
				"server", server.Name)
		}
	}

	return nil
}

// markJARAsDeleted updates the DeletionProgress to indicate JAR was deleted.
func (r *PluginReconciler) markJARAsDeleted(
	ctx context.Context,
	plugin *mcv1alpha1.Plugin,
	serverName,
	namespace string,
) error {
	now := metav1.Now()
	for i := range plugin.Status.DeletionProgress {
		if plugin.Status.DeletionProgress[i].ServerName == serverName &&
			plugin.Status.DeletionProgress[i].Namespace == namespace {
			plugin.Status.DeletionProgress[i].JARDeleted = true
			plugin.Status.DeletionProgress[i].DeletedAt = &now
			break
		}
	}

	if err := r.Status().Update(ctx, plugin); err != nil {
		return errors.Wrap(err, "failed to update deletion progress")
	}

	slog.InfoContext(ctx, "Marked JAR as deleted",
		"plugin", plugin.Name,
		"server", serverName)
	return nil
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

	// Find all plugins in the same namespace that might match this server
	var pluginList mcv1alpha1.PluginList
	if err := r.List(ctx, &pluginList, client.InNamespace(server.Namespace)); err != nil {
		slog.ErrorContext(ctx, "Failed to list plugins for server watch", "error", err)
		return nil
	}

	var requests []reconcile.Request
	for i := range pluginList.Items {
		plugin := &pluginList.Items[i]

		// Check if this plugin's selector might match the server
		matches, err := selector.MatchesSelector(server.Labels, plugin.Spec.InstanceSelector)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to check selector match", "error", err, "plugin", plugin.Name)
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

	slog.InfoContext(ctx, "Server change triggered plugin reconciliations",
		"server", server.Name,
		"plugins", len(requests))

	return requests
}
