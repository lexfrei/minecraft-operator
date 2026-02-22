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
	"crypto/sha256"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/cockroachdb/errors"
	mcv1alpha1 "github.com/lexfrei/minecraft-operator/api/v1alpha1"
	mccron "github.com/lexfrei/minecraft-operator/pkg/cron"
	"github.com/lexfrei/minecraft-operator/pkg/paper"
	"github.com/lexfrei/minecraft-operator/pkg/plugins"
	"github.com/lexfrei/minecraft-operator/pkg/rcon"
	"github.com/robfig/cron/v3"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	conditionTypeUpdating  = "Updating"
	reasonUpdateInProgress = "UpdateInProgress"
	reasonUpdateComplete   = "UpdateComplete"
	containerNamePaperMC   = "papermc"
)

// UpdateReconciler reconciles PaperMCServer resources for scheduled updates.
type UpdateReconciler struct {
	client.Client
	Scheme       *runtime.Scheme
	PaperClient  *paper.Client
	PluginClient plugins.PluginClient
	PodExecutor  PodExecutor
	cron         mccron.Scheduler

	// nowFunc returns current time; override in tests for deterministic behavior.
	nowFunc func() time.Time

	// Track cron entries per server
	cronEntriesMu sync.RWMutex
	cronEntries   map[string]cronEntryInfo
}

// cronEntryInfo stores both the cron entry ID and the spec string
// to avoid needing to retrieve the spec from the scheduler.
type cronEntryInfo struct {
	ID   cron.EntryID
	Spec string
}

//+kubebuilder:rbac:groups=mc.k8s.lex.la,resources=papermcservers,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=mc.k8s.lex.la,resources=papermcservers/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=mc.k8s.lex.la,resources=plugins,verbs=get;list;watch
//+kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;delete
//+kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch
//nolint:revive // kubebuilder markers require no space after //

// Reconcile reconciles PaperMCServer resources for update management.
//
//nolint:funlen // Complex update orchestration logic, hard to simplify further
func (r *UpdateReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// Initialize cronEntries if nil
	if r.cronEntries == nil {
		r.cronEntriesMu.Lock()
		if r.cronEntries == nil {
			r.cronEntries = make(map[string]cronEntryInfo)
		}
		r.cronEntriesMu.Unlock()
	}

	// Fetch the PaperMCServer resource
	var server mcv1alpha1.PaperMCServer
	if err := r.Get(ctx, req.NamespacedName, &server); err != nil {
		if apierrors.IsNotFound(err) {
			// Resource deleted - remove cron job if exists
			r.removeCronJob(req.String())
			slog.InfoContext(ctx, "PaperMCServer resource not found, removed cron job if existed")
			return ctrl.Result{}, nil
		}
		slog.ErrorContext(ctx, "Failed to get PaperMCServer resource", "error", err)
		return ctrl.Result{}, errors.Wrap(err, "failed to get server")
	}

	// Manage cron schedule for this server
	if err := r.manageCronSchedule(ctx, &server); err != nil {
		slog.ErrorContext(ctx, "Failed to manage cron schedule", "error", err)
		return ctrl.Result{}, err
	}

	// Check for immediate apply annotation (bypasses maintenance window and updateDelay)
	applyNow := r.shouldApplyNow(ctx, &server)
	if applyNow {
		slog.InfoContext(ctx, "Apply-now annotation detected, triggering immediate update",
			"server", server.Name)
		// Remove annotation first to prevent loops
		if err := r.removeApplyNowAnnotation(ctx, &server); err != nil {
			slog.ErrorContext(ctx, "Failed to remove apply-now annotation", "error", err)
			return ctrl.Result{}, errors.Wrap(err, "failed to remove apply-now annotation")
		}
		// Continue with update (skip shouldApplyUpdate check)
	} else {
		// Check if update is ready to apply based on updateDelay
		shouldApply, remainingDelay := r.shouldApplyUpdate(&server)
		if !shouldApply {
			slog.InfoContext(ctx, "Update not ready to apply",
				"server", server.Name,
				"remainingDelay", remainingDelay)
			// Requeue after remaining delay
			if remainingDelay > 0 {
				return ctrl.Result{RequeueAfter: remainingDelay}, nil
			}
			return ctrl.Result{}, nil
		}

		// Check if we are within the maintenance window
		if !r.isInMaintenanceWindow(&server) {
			slog.InfoContext(ctx, "Outside maintenance window, deferring update",
				"server", server.Name,
				"maintenanceWindowCron", server.Spec.UpdateSchedule.MaintenanceWindow.Cron)

			return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
		}
	}

	// Check if there's an available update
	if server.Status.AvailableUpdate == nil {
		slog.DebugContext(ctx, "No available update", "server", server.Name)
		return ctrl.Result{}, nil
	}

	// Set updating condition
	r.setUpdatingCondition(&server, true, "Update in progress")

	// Determine update type
	paperChanged := server.Status.DesiredVersion != server.Status.CurrentVersion ||
		server.Status.DesiredBuild != server.Status.CurrentBuild

	var updateErr error
	if paperChanged {
		// Combined update: Paper + plugins
		slog.InfoContext(ctx, "Starting Paper and plugins update",
			"server", server.Name,
			"currentVersion", server.Status.CurrentVersion,
			"currentBuild", server.Status.CurrentBuild,
			"desiredVersion", server.Status.DesiredVersion,
			"desiredBuild", server.Status.DesiredBuild)

		updateErr = r.performCombinedUpdate(ctx, &server)
	} else {
		// Plugin-only update
		slog.InfoContext(ctx, "Starting plugin-only update", "server", server.Name)
		updateErr = r.performPluginOnlyUpdate(ctx, &server)
	}

	// Update status based on result
	successful := updateErr == nil
	r.updateServerStatus(&server, successful)
	r.setUpdatingCondition(&server, false, "Update completed")

	// Update the server resource status
	if err := r.Status().Update(ctx, &server); err != nil {
		slog.ErrorContext(ctx, "Failed to update server status", "error", err)
		return ctrl.Result{}, errors.Wrap(err, "failed to update status")
	}

	if updateErr != nil {
		slog.ErrorContext(ctx, "Update failed", "error", updateErr)
		return ctrl.Result{}, updateErr
	}

	slog.InfoContext(ctx, "Update completed successfully", "server", server.Name)
	return ctrl.Result{}, nil
}

// shouldApplyUpdate checks if an update should be applied based on updateDelay.
// Returns (shouldApply bool, remainingDelay time.Duration).
func (r *UpdateReconciler) shouldApplyUpdate(server *mcv1alpha1.PaperMCServer) (bool, time.Duration) {
	// No available update - nothing to apply
	if server.Status.AvailableUpdate == nil {
		return true, 0
	}

	// No updateDelay configured - apply immediately
	if server.Spec.UpdateDelay == nil {
		return true, 0
	}

	delay := server.Spec.UpdateDelay.Duration
	releasedAt := server.Status.AvailableUpdate.ReleasedAt.Time
	timeSinceRelease := time.Since(releasedAt)

	// Check if delay satisfied
	if timeSinceRelease >= delay {
		return true, 0
	}

	// Delay not satisfied - return remaining time
	remaining := delay - timeSinceRelease
	return false, remaining
}

// maintenanceWindowDuration is the duration after cron trigger during which updates are allowed.
const maintenanceWindowDuration = 1 * time.Hour

// now returns the current time, using nowFunc if set (for testing).
func (r *UpdateReconciler) now() time.Time {
	if r.nowFunc != nil {
		return r.nowFunc()
	}

	return time.Now()
}

// isInMaintenanceWindow checks if the current time is within the maintenance window.
// The maintenance window starts at the cron trigger time and lasts for maintenanceWindowDuration.
// Returns true if maintenance window is disabled (updates always allowed).
func (r *UpdateReconciler) isInMaintenanceWindow(server *mcv1alpha1.PaperMCServer) bool {
	mw := server.Spec.UpdateSchedule.MaintenanceWindow
	if !mw.Enabled || mw.Cron == "" {
		return true // No maintenance window configured, always allow
	}

	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

	schedule, err := parser.Parse(mw.Cron)
	if err != nil {
		slog.Error("Failed to parse maintenance window cron, blocking update",
			"cron", mw.Cron, "error", err)

		return false // Can't parse cron, block updates as safe default
	}

	now := r.now()

	// Find the last trigger time by checking when the schedule would have fired
	// before now. We check backwards up to 7 days.
	lastTrigger := findLastCronTrigger(schedule, now)
	if lastTrigger.IsZero() {
		return false // No trigger found within lookback period
	}

	windowEnd := lastTrigger.Add(maintenanceWindowDuration)

	return now.After(lastTrigger) && now.Before(windowEnd)
}

// findLastCronTrigger finds the most recent time the cron schedule triggered before now.
func findLastCronTrigger(schedule cron.Schedule, now time.Time) time.Time {
	// Walk backwards in 1-hour steps up to 7 days to find a window
	lookback := 7 * 24 * time.Hour

	candidate := now.Add(-lookback)
	var lastTrigger time.Time

	for {
		next := schedule.Next(candidate)
		if next.After(now) {
			break
		}

		lastTrigger = next
		candidate = next
	}

	return lastTrigger
}

// manageCronSchedule adds, updates, or removes cron jobs based on server spec.
func (r *UpdateReconciler) manageCronSchedule(ctx context.Context, server *mcv1alpha1.PaperMCServer) error {
	serverKey := types.NamespacedName{
		Name:      server.Name,
		Namespace: server.Namespace,
	}.String()

	// Check if maintenance window is enabled
	if !server.Spec.UpdateSchedule.MaintenanceWindow.Enabled {
		// Remove cron job if exists
		r.removeCronJob(serverKey)
		slog.InfoContext(ctx, "Maintenance window disabled, removed cron job")
		return nil
	}

	cronSpec := server.Spec.UpdateSchedule.MaintenanceWindow.Cron

	// Check if cron job already exists with same spec
	r.cronEntriesMu.RLock()
	existing, exists := r.cronEntries[serverKey]
	r.cronEntriesMu.RUnlock()

	if exists {
		if existing.Spec == cronSpec {
			// Spec unchanged - nothing to do
			return nil
		}
		// Spec changed - remove old and add new
		r.removeCronJob(serverKey)
		slog.InfoContext(ctx, "Cron spec changed, updating job", "old", existing.Spec, "new", cronSpec)
	}

	// Add new cron job
	entryID, err := r.cron.AddFunc(cronSpec, func() {
		// Trigger reconciliation on cron schedule
		slog.InfoContext(ctx, "Maintenance window triggered by cron", "server", serverKey)
	})

	if err != nil {
		return errors.Wrap(err, "failed to add cron job")
	}

	// Store entry ID and spec together
	r.cronEntriesMu.Lock()
	r.cronEntries[serverKey] = cronEntryInfo{ID: entryID, Spec: cronSpec}
	r.cronEntriesMu.Unlock()

	slog.InfoContext(ctx, "Added cron job", "server", serverKey, "spec", cronSpec, "entryID", entryID)
	return nil
}

// removeCronJob removes the cron job for a server.
func (r *UpdateReconciler) removeCronJob(serverKey string) {
	r.cronEntriesMu.Lock()
	defer r.cronEntriesMu.Unlock()

	if entry, exists := r.cronEntries[serverKey]; exists {
		r.cron.Remove(entry.ID)
		delete(r.cronEntries, serverKey)
	}
}

// downloadFile downloads a file from URL to targetPath with context support.
func (r *UpdateReconciler) downloadFile(ctx context.Context, url, targetPath string) error {
	// Create HTTP request with context
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return errors.Wrap(err, "failed to create download request")
	}

	// Execute request
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return errors.Wrap(err, "failed to download file")
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			err = errors.CombineErrors(err, errors.Wrap(closeErr, "failed to close response body"))
		}
	}()

	// Check status code
	if resp.StatusCode != http.StatusOK {
		return errors.Newf("download failed with status %d", resp.StatusCode)
	}

	// Create target file
	outFile, err := os.Create(targetPath)
	if err != nil {
		return errors.Wrap(err, "failed to create target file")
	}
	defer func() {
		if closeErr := outFile.Close(); closeErr != nil {
			err = errors.CombineErrors(err, errors.Wrap(closeErr, "failed to close output file"))
		}
	}()

	// Copy data
	_, err = io.Copy(outFile, resp.Body)
	if err != nil {
		return errors.Wrap(err, "failed to write file")
	}

	return nil
}

// verifyChecksum verifies the SHA256 checksum of a file.
func (r *UpdateReconciler) verifyChecksum(filePath, expectedHash string) error {
	// Open file
	file, err := os.Open(filePath)
	if err != nil {
		return errors.Wrap(err, "failed to open file for checksum")
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			err = errors.CombineErrors(err, errors.Wrap(closeErr, "failed to close file"))
		}
	}()

	// Compute SHA256
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return errors.Wrap(err, "failed to compute checksum")
	}

	actualHash := fmt.Sprintf("%x", hash.Sum(nil))

	// Compare
	if actualHash != expectedHash {
		return errors.Newf("checksum mismatch: expected %s, got %s", expectedHash, actualHash)
	}

	return nil
}

// downloadPluginToServer downloads a plugin JAR to the server's /data/plugins/update/ directory.
func (r *UpdateReconciler) downloadPluginToServer(
	ctx context.Context,
	server *mcv1alpha1.PaperMCServer,
	pluginName string,
	downloadURL string,
	expectedHash string,
) error {
	podName := server.Name + "-0" // StatefulSet pod naming convention
	namespace := server.Namespace
	container := containerNamePaperMC

	// Build curl command to download directly to /data/plugins/update/
	curlCmd := fmt.Sprintf(
		"curl -fsSL -o /data/plugins/update/%s.jar '%s'",
		pluginName,
		downloadURL,
	)

	slog.InfoContext(ctx, "Downloading plugin to server",
		"server", server.Name,
		"plugin", pluginName,
		"url", downloadURL)

	_, err := r.PodExecutor.ExecInPod(ctx, namespace, podName, container,
		[]string{"sh", "-c", curlCmd})
	if err != nil {
		return errors.Wrapf(err, "failed to download plugin %s", pluginName)
	}

	// Verify checksum if provided
	if expectedHash != "" {
		checksumCmd := fmt.Sprintf(
			"sha256sum /data/plugins/update/%s.jar | awk '{print $1}'",
			pluginName,
		)

		output, execErr := r.PodExecutor.ExecInPod(ctx, namespace, podName, container,
			[]string{"sh", "-c", checksumCmd})
		if execErr != nil {
			return errors.Wrapf(execErr, "failed to verify checksum for plugin %s", pluginName)
		}

		actualHash := strings.TrimSpace(string(output))
		if actualHash != expectedHash {
			return errors.Newf("checksum mismatch: expected %s, got %s",
				expectedHash, actualHash)
		}

		slog.InfoContext(ctx, "Plugin checksum verified", "plugin", pluginName)
	}

	slog.InfoContext(ctx, "Plugin downloaded successfully",
		"server", server.Name,
		"plugin", pluginName)

	return nil
}

// applyPluginUpdates downloads and applies plugin updates for the server.
//
//nolint:funlen // Complex plugin download orchestration with error handling
func (r *UpdateReconciler) applyPluginUpdates(
	ctx context.Context,
	server *mcv1alpha1.PaperMCServer,
) error {
	// Get current Plugin CRDs to access download URLs
	var pluginList mcv1alpha1.PluginList
	if err := r.List(ctx, &pluginList, client.InNamespace(server.Namespace)); err != nil {
		return errors.Wrap(err, "failed to list plugins")
	}

	// Build map of plugin name -> Plugin CRD for quick lookup
	pluginMap := make(map[string]*mcv1alpha1.Plugin)
	for i := range pluginList.Items {
		plugin := &pluginList.Items[i]
		pluginMap[plugin.Name] = plugin
	}

	// Download each plugin that needs update
	updatedCount := 0
	var downloadErrors []error

	for i := range server.Status.Plugins {
		pluginStatus := &server.Status.Plugins[i]
		pluginName := pluginStatus.PluginRef.Name

		plugin, exists := pluginMap[pluginName]
		if !exists {
			slog.InfoContext(ctx, "Plugin not found in cluster, skipping",
				"plugin", pluginName)
			continue
		}

		if pluginStatus.ResolvedVersion == "" {
			slog.InfoContext(ctx, "No resolved version for plugin, skipping",
				"plugin", pluginName)
			continue
		}

		// Find download URL and hash for resolved version
		var downloadURL, hash string
		for _, v := range plugin.Status.AvailableVersions {
			if v.Version == pluginStatus.ResolvedVersion {
				downloadURL = v.DownloadURL
				hash = v.Hash
				break
			}
		}

		if downloadURL == "" {
			slog.InfoContext(ctx, "Plugin has no download URL, skipping",
				"plugin", pluginName, "version", pluginStatus.ResolvedVersion)
			continue
		}

		// Download plugin
		if err := r.downloadPluginToServer(ctx, server, pluginName, downloadURL, hash); err != nil {
			downloadErrors = append(downloadErrors,
				errors.Wrapf(err, "plugin %s", pluginName))
			continue
		}

		// Track installed JAR name and version
		pluginStatus.InstalledJARName = pluginName + ".jar"
		pluginStatus.CurrentVersion = pluginStatus.ResolvedVersion
		updatedCount++
	}

	slog.InfoContext(ctx, "Plugin updates applied",
		"server", server.Name,
		"updated", updatedCount,
		"total", len(server.Status.Plugins),
		"errors", len(downloadErrors))

	// Return aggregate error if any downloads failed
	if len(downloadErrors) > 0 {
		return errors.Newf("failed to download %d plugins: %v", len(downloadErrors), downloadErrors)
	}

	return nil
}

// waitForPodReady waits for the server pod to become ready after restart.
func (r *UpdateReconciler) waitForPodReady(
	ctx context.Context,
	server *mcv1alpha1.PaperMCServer,
) error {
	podName := server.Name + "-0"
	namespace := server.Namespace

	slog.InfoContext(ctx, "Waiting for pod to become ready", "pod", podName)

	// Timeout after 10 minutes
	ctxTimeout, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctxTimeout.Done():
			return errors.New("timeout waiting for pod to become ready")

		case <-ticker.C:
			var pod corev1.Pod
			if err := r.Get(ctxTimeout, client.ObjectKey{
				Name:      podName,
				Namespace: namespace,
			}, &pod); err != nil {
				slog.InfoContext(ctxTimeout, "Pod not found yet", "pod", podName)
				continue
			}

			// Check if pod is ready
			for _, condition := range pod.Status.Conditions {
				if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
					slog.InfoContext(ctxTimeout, "Pod is ready", "pod", podName)
					return nil
				}
			}

			slog.InfoContext(ctxTimeout, "Pod not ready yet", "pod", podName, "phase", pod.Status.Phase)
		}
	}
}

// createRCONClient creates an RCON client for the server by fetching Pod IP and password from Secret.
func (r *UpdateReconciler) createRCONClient(
	ctx context.Context,
	server *mcv1alpha1.PaperMCServer,
) (*rcon.RCONClient, error) {
	podName := server.Name + "-0"
	namespace := server.Namespace

	// Get Pod to obtain IP address
	var pod corev1.Pod
	if err := r.Get(ctx, client.ObjectKey{
		Name:      podName,
		Namespace: namespace,
	}, &pod); err != nil {
		return nil, errors.Wrap(err, "failed to get pod for RCON connection")
	}

	if pod.Status.PodIP == "" {
		return nil, errors.New("pod IP not available")
	}

	// Get password from Secret
	var secret corev1.Secret
	if err := r.Get(ctx, client.ObjectKey{
		Name:      server.Spec.RCON.PasswordSecret.Name,
		Namespace: namespace,
	}, &secret); err != nil {
		return nil, errors.Wrap(err, "failed to get RCON password secret")
	}

	passwordBytes, exists := secret.Data[server.Spec.RCON.PasswordSecret.Key]
	if !exists {
		return nil, errors.Newf("key %s not found in secret %s",
			server.Spec.RCON.PasswordSecret.Key,
			server.Spec.RCON.PasswordSecret.Name)
	}

	password := string(passwordBytes)

	// Get RCON port (use default if not specified)
	port := server.Spec.RCON.Port
	if port == 0 {
		port = 25575 // Default RCON port
	}

	slog.InfoContext(ctx, "Creating RCON client",
		"host", pod.Status.PodIP,
		"port", port)

	// Create RCON client
	rconClient, err := rcon.NewRCONClient(pod.Status.PodIP, int(port), password)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create RCON client")
	}

	return rconClient, nil
}

// performPluginOnlyUpdate handles updates when only plugins changed (Paper version unchanged).
func (r *UpdateReconciler) performPluginOnlyUpdate(
	ctx context.Context,
	server *mcv1alpha1.PaperMCServer,
) error {
	slog.InfoContext(ctx, "Starting plugin-only update", "server", server.Name)

	// Step 1: Delete plugins marked for deletion (PendingDeletion=true)
	if err := r.deleteMarkedPlugins(ctx, server); err != nil {
		return errors.Wrap(err, "failed to delete marked plugins")
	}

	// Step 2: Download plugins to /data/plugins/update/
	if err := r.applyPluginUpdates(ctx, server); err != nil {
		return errors.Wrap(err, "failed to download plugins")
	}

	// Step 3: Create RCON client
	rconClient, err := r.createRCONClient(ctx, server)
	if err != nil {
		return errors.Wrap(err, "failed to create RCON client")
	}

	// Step 4: Execute graceful shutdown via RCON
	if err := r.executeGracefulShutdownWithClient(ctx, server, rconClient); err != nil {
		return errors.Wrap(err, "failed to execute graceful shutdown")
	}

	// Step 5: Delete pod to trigger StatefulSet recreation
	podName := server.Name + "-0"
	if err := r.deletePod(ctx, podName, server.Namespace); err != nil {
		return errors.Wrap(err, "failed to delete pod for recreation")
	}

	// Step 6: Wait for pod to restart (StatefulSet will recreate it)
	if err := r.waitForPodReady(ctx, server); err != nil {
		return errors.Wrap(err, "failed to wait for pod ready")
	}

	slog.InfoContext(ctx, "Plugin-only update completed successfully", "server", server.Name)

	return nil
}

// updateStatefulSetImage updates the Paper container image in the StatefulSet.
func (r *UpdateReconciler) updateStatefulSetImage(
	ctx context.Context,
	server *mcv1alpha1.PaperMCServer,
	newImage string,
) error {
	slog.InfoContext(ctx, "Updating StatefulSet image",
		"server", server.Name,
		"newImage", newImage)

	var sts appsv1.StatefulSet
	if err := r.Get(ctx, client.ObjectKey{
		Name:      server.Name,
		Namespace: server.Namespace,
	}, &sts); err != nil {
		return errors.Wrap(err, "failed to get StatefulSet")
	}

	// Update container image
	updated := false
	for i := range sts.Spec.Template.Spec.Containers {
		if sts.Spec.Template.Spec.Containers[i].Name == containerNamePaperMC {
			sts.Spec.Template.Spec.Containers[i].Image = newImage
			updated = true
			break
		}
	}

	if !updated {
		return errors.New("papermc container not found in StatefulSet")
	}

	// Apply update
	if err := r.Update(ctx, &sts); err != nil {
		return errors.Wrap(err, "failed to update StatefulSet")
	}

	slog.InfoContext(ctx, "StatefulSet image updated successfully",
		"server", server.Name,
		"newImage", newImage)

	return nil
}

// performCombinedUpdate handles updates when both Paper and plugins need updating.
func (r *UpdateReconciler) performCombinedUpdate(
	ctx context.Context,
	server *mcv1alpha1.PaperMCServer,
) error {
	slog.InfoContext(ctx, "Starting combined Paper and plugins update", "server", server.Name)

	// Step 1: Delete plugins marked for deletion (PendingDeletion=true)
	if err := r.deleteMarkedPlugins(ctx, server); err != nil {
		return errors.Wrap(err, "failed to delete marked plugins")
	}

	// Step 2: Download plugins to /data/plugins/update/ (BEFORE pod restart)
	if err := r.applyPluginUpdates(ctx, server); err != nil {
		return errors.Wrap(err, "failed to download plugins")
	}

	// Step 3: RCON graceful shutdown (warn players, save world)
	rconClient, err := r.createRCONClient(ctx, server)
	if err != nil {
		return errors.Wrap(err, "failed to create RCON client")
	}

	if err := r.executeGracefulShutdownWithClient(ctx, server, rconClient); err != nil {
		return errors.Wrap(err, "failed to execute graceful shutdown")
	}

	// Step 4: Update StatefulSet image to new Paper version
	newImage := fmt.Sprintf("lexfrei/papermc:%s-%d",
		server.Status.DesiredVersion,
		server.Status.DesiredBuild)

	if err := r.updateStatefulSetImage(ctx, server, newImage); err != nil {
		return errors.Wrap(err, "failed to update StatefulSet image")
	}

	// Step 5: Delete pod to trigger StatefulSet recreation with new image
	podName := server.Name + "-0"
	if err := r.deletePod(ctx, podName, server.Namespace); err != nil {
		return errors.Wrap(err, "failed to delete pod for recreation")
	}

	// Step 6: Wait for pod to restart with new image
	if err := r.waitForPodReady(ctx, server); err != nil {
		return errors.Wrap(err, "failed to wait for pod ready")
	}

	slog.InfoContext(ctx, "Combined update completed successfully", "server", server.Name)

	return nil
}

// executeGracefulShutdownWithClient performs graceful shutdown using provided RCON client.
// This allows for testing with mock clients.
func (r *UpdateReconciler) executeGracefulShutdownWithClient(
	ctx context.Context,
	server *mcv1alpha1.PaperMCServer,
	rconClient interface {
		Connect(ctx context.Context) error
		GracefulShutdown(ctx context.Context, warnings []string, warningInterval time.Duration) error
		Close() error
	},
) error {
	// Connect to RCON
	if err := rconClient.Connect(ctx); err != nil {
		return errors.Wrap(err, "failed to connect to RCON")
	}
	defer func() {
		if err := rconClient.Close(); err != nil {
			slog.ErrorContext(ctx, "Failed to close RCON connection", "error", err)
		}
	}()

	// Prepare warning messages
	warnings := []string{
		"Server will restart for update in 5 minutes",
		"Server will restart for update in 2 minutes",
		"Server will restart for update in 1 minute",
		"Server will restart for update in 30 seconds",
		"Server restarting now for update",
	}

	// Calculate warning interval based on graceful shutdown timeout
	timeout := server.Spec.GracefulShutdown.Timeout.Duration
	warningInterval := timeout / time.Duration(len(warnings))

	// Execute graceful shutdown
	if err := rconClient.GracefulShutdown(ctx, warnings, warningInterval); err != nil {
		return errors.Wrap(err, "graceful shutdown failed")
	}

	slog.InfoContext(ctx, "Graceful shutdown completed successfully")
	return nil
}

// updateServerStatus updates the server status after an update attempt.
func (r *UpdateReconciler) updateServerStatus(
	server *mcv1alpha1.PaperMCServer,
	successful bool,
) {
	now := metav1.Now()

	// Record update history
	server.Status.LastUpdate = &mcv1alpha1.UpdateHistory{
		AppliedAt:       now,
		PreviousVersion: server.Status.CurrentVersion,
		Successful:      successful,
	}

	// Update versions and clear availableUpdate if successful
	if successful {
		server.Status.AvailableUpdate = nil

		// Update current version to match desired (update completed)
		server.Status.CurrentVersion = server.Status.DesiredVersion
		server.Status.CurrentBuild = server.Status.DesiredBuild

		// Update plugin current versions to match resolved versions
		for i := range server.Status.Plugins {
			if server.Status.Plugins[i].ResolvedVersion != "" {
				server.Status.Plugins[i].CurrentVersion = server.Status.Plugins[i].ResolvedVersion
			}
		}
	}
}

// setUpdatingCondition sets the Updating condition on the server.
func (r *UpdateReconciler) setUpdatingCondition(server *mcv1alpha1.PaperMCServer, updating bool, message string) {
	var status metav1.ConditionStatus
	var reason string

	if updating {
		status = metav1.ConditionTrue
		reason = reasonUpdateInProgress
	} else {
		status = metav1.ConditionFalse
		reason = reasonUpdateComplete
	}

	condition := metav1.Condition{
		Type:               conditionTypeUpdating,
		Status:             status,
		ObservedGeneration: server.Generation,
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	}

	meta.SetStatusCondition(&server.Status.Conditions, condition)
}

// getPluginsToDelete returns all plugins marked for deletion.
func (r *UpdateReconciler) getPluginsToDelete(
	server *mcv1alpha1.PaperMCServer,
) []mcv1alpha1.ServerPluginStatus {
	var result []mcv1alpha1.ServerPluginStatus

	for _, plugin := range server.Status.Plugins {
		if plugin.PendingDeletion {
			result = append(result, plugin)
		}
	}

	return result
}

// markJARAsDeleted updates the Plugin's DeletionProgress to mark a JAR as deleted.
func (r *UpdateReconciler) markJARAsDeleted(
	ctx context.Context,
	pluginName, pluginNamespace string,
	serverName, serverNamespace string,
) error {
	// Fetch the Plugin resource
	var plugin mcv1alpha1.Plugin
	if err := r.Get(ctx, client.ObjectKey{
		Name:      pluginName,
		Namespace: pluginNamespace,
	}, &plugin); err != nil {
		return errors.Wrap(err, "failed to get plugin")
	}

	// Find and update the DeletionProgress entry
	now := metav1.Now()
	updated := false

	for i := range plugin.Status.DeletionProgress {
		entry := &plugin.Status.DeletionProgress[i]
		if entry.ServerName == serverName && entry.Namespace == serverNamespace {
			entry.JARDeleted = true
			entry.DeletedAt = &now
			updated = true

			break
		}
	}

	if !updated {
		slog.WarnContext(ctx, "DeletionProgress entry not found",
			"plugin", pluginName,
			"server", serverName)

		return nil
	}

	// Update the Plugin status
	if err := r.Status().Update(ctx, &plugin); err != nil {
		return errors.Wrap(err, "failed to update plugin status")
	}

	slog.InfoContext(ctx, "Marked JAR as deleted in Plugin status",
		"plugin", pluginName,
		"server", serverName)

	return nil
}

// deleteMarkedPlugins deletes plugin JARs that are marked for deletion from the server.
func (r *UpdateReconciler) deleteMarkedPlugins(
	ctx context.Context,
	server *mcv1alpha1.PaperMCServer,
) error {
	pluginsToDelete := r.getPluginsToDelete(server)

	if len(pluginsToDelete) == 0 {
		slog.DebugContext(ctx, "No plugins marked for deletion", "server", server.Name)

		return nil
	}

	slog.InfoContext(ctx, "Deleting marked plugins",
		"server", server.Name,
		"count", len(pluginsToDelete))

	var errs []error

	for _, plugin := range pluginsToDelete {
		if err := r.deletePluginJAR(ctx, server, plugin); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return errors.CombineErrors(errs[0], errors.Newf("and %d more errors", len(errs)-1))
	}

	return nil
}

// deletePluginJAR deletes a single plugin JAR from the server.
// Handles three cases: plugin never installed (empty InstalledJARName),
// plugin in plugins/ only, or plugin in both plugins/ and update/ folders.
func (r *UpdateReconciler) deletePluginJAR(
	ctx context.Context,
	server *mcv1alpha1.PaperMCServer,
	plugin mcv1alpha1.ServerPluginStatus,
) error {
	if plugin.InstalledJARName == "" {
		slog.InfoContext(ctx, "Plugin was never installed, marking as deleted",
			"server", server.Name,
			"plugin", plugin.PluginRef.Name)

		return r.markJARAsDeleted(ctx,
			plugin.PluginRef.Name, plugin.PluginRef.Namespace,
			server.Name, server.Namespace)
	}

	podName := server.Name + "-0"
	namespace := server.Namespace
	container := containerNamePaperMC

	// Delete from both plugins/ and update/ directories
	rmCmd := fmt.Sprintf("rm -f /data/plugins/%s /data/plugins/update/%s",
		plugin.InstalledJARName, plugin.InstalledJARName)

	slog.InfoContext(ctx, "Deleting plugin JAR",
		"server", server.Name,
		"plugin", plugin.PluginRef.Name,
		"jar", plugin.InstalledJARName)

	_, err := r.PodExecutor.ExecInPod(ctx, namespace, podName, container,
		[]string{"sh", "-c", rmCmd})
	if err != nil {
		slog.ErrorContext(ctx, "Failed to delete plugin JAR",
			"error", err,
			"plugin", plugin.PluginRef.Name)

		return errors.Wrapf(err, "failed to delete JAR for plugin %s", plugin.PluginRef.Name)
	}

	// Mark JAR as deleted in Plugin status
	if err := r.markJARAsDeleted(ctx,
		plugin.PluginRef.Name, plugin.PluginRef.Namespace,
		server.Name, server.Namespace); err != nil {
		slog.ErrorContext(ctx, "Failed to mark JAR as deleted",
			"error", err,
			"plugin", plugin.PluginRef.Name)

		return errors.Wrapf(err, "failed to mark JAR as deleted for plugin %s", plugin.PluginRef.Name)
	}

	slog.InfoContext(ctx, "Plugin JAR deleted successfully",
		"server", server.Name,
		"plugin", plugin.PluginRef.Name)

	return nil
}

// applyNowMaxAge is the maximum age for apply-now annotation (5 minutes).
const applyNowMaxAge = 5 * time.Minute

// shouldApplyNow checks if the apply-now annotation is present and valid.
func (r *UpdateReconciler) shouldApplyNow(ctx context.Context, server *mcv1alpha1.PaperMCServer) bool {
	if server.Annotations == nil {
		return false
	}

	tsStr, exists := server.Annotations[AnnotationApplyNow]
	if !exists {
		return false
	}

	// Parse Unix timestamp
	var ts int64
	if _, err := fmt.Sscanf(tsStr, "%d", &ts); err != nil {
		slog.WarnContext(ctx, "Invalid apply-now annotation format",
			"value", tsStr,
			"server", server.Name)

		return false
	}

	annotationTime := time.Unix(ts, 0)
	age := time.Since(annotationTime)

	// Reject stale annotations
	if age > applyNowMaxAge {
		slog.InfoContext(ctx, "Ignoring stale apply-now annotation",
			"age", age,
			"maxAge", applyNowMaxAge,
			"server", server.Name)

		return false
	}

	return true
}

// removeApplyNowAnnotation removes the apply-now annotation from the server.
func (r *UpdateReconciler) removeApplyNowAnnotation(
	ctx context.Context,
	server *mcv1alpha1.PaperMCServer,
) error {
	if server.Annotations == nil {
		return nil
	}

	if _, exists := server.Annotations[AnnotationApplyNow]; !exists {
		return nil
	}

	// Re-fetch to avoid conflicts
	var currentServer mcv1alpha1.PaperMCServer
	if err := r.Get(ctx, client.ObjectKey{
		Name:      server.Name,
		Namespace: server.Namespace,
	}, &currentServer); err != nil {
		return errors.Wrap(err, "failed to get server for annotation removal")
	}

	delete(currentServer.Annotations, AnnotationApplyNow)

	if err := r.Update(ctx, &currentServer); err != nil {
		return errors.Wrap(err, "failed to remove apply-now annotation")
	}

	slog.InfoContext(ctx, "Removed apply-now annotation", "server", server.Name)

	return nil
}

// deletePod deletes the pod with the given name in the specified namespace.
// This is used to trigger StatefulSet recreation after graceful shutdown.
func (r *UpdateReconciler) deletePod(ctx context.Context, podName, namespace string) error {
	slog.InfoContext(ctx, "Deleting pod to trigger StatefulSet recreation",
		"pod", podName,
		"namespace", namespace)

	pod := &corev1.Pod{}
	if err := r.Get(ctx, client.ObjectKey{
		Name:      podName,
		Namespace: namespace,
	}, pod); err != nil {
		return errors.Wrap(err, "failed to get pod for deletion")
	}

	if err := r.Delete(ctx, pod); err != nil {
		return errors.Wrap(err, "failed to delete pod")
	}

	slog.InfoContext(ctx, "Pod deleted successfully", "pod", podName)
	return nil
}

// SetCron sets the cron scheduler for the reconciler.
func (r *UpdateReconciler) SetCron(scheduler mccron.Scheduler) {
	r.cron = scheduler
}

// SetupWithManager sets up the controller with the Manager.
func (r *UpdateReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Initialize cron entries map
	if r.cronEntries == nil {
		r.cronEntries = make(map[string]cronEntryInfo)
	}

	// Start cron scheduler if not mock
	if r.cron != nil {
		r.cron.Start()
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&mcv1alpha1.PaperMCServer{}).
		Named("update").
		Complete(r)
}
