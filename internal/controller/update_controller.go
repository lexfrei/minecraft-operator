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
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/cockroachdb/errors"
	mcv1alpha1 "github.com/lexfrei/minecraft-operator/api/v1alpha1"
	"github.com/lexfrei/minecraft-operator/pkg/paper"
	"github.com/lexfrei/minecraft-operator/pkg/plugins"
	"github.com/lexfrei/minecraft-operator/pkg/testutil"
	"github.com/robfig/cron/v3"
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
	reasonUpdateFailed     = "UpdateFailed"
)

// UpdateReconciler reconciles PaperMCServer resources for scheduled updates.
type UpdateReconciler struct {
	client.Client
	Scheme       *runtime.Scheme
	PaperClient  *paper.Client
	PluginClient plugins.PluginClient
	cron         testutil.CronScheduler

	// Track cron entries per server
	cronEntriesMu sync.RWMutex
	cronEntries   map[string]cron.EntryID
}

//+kubebuilder:rbac:groups=mc.k8s.lex.la,resources=papermcservers,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=mc.k8s.lex.la,resources=papermcservers/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=mc.k8s.lex.la,resources=plugins,verbs=get;list;watch
//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;delete
//+kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch
//nolint:revive // kubebuilder markers require no space after //

// Reconcile reconciles PaperMCServer resources for update management.
func (r *UpdateReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)

	// Initialize cronEntries if nil
	if r.cronEntries == nil {
		r.cronEntriesMu.Lock()
		if r.cronEntries == nil {
			r.cronEntries = make(map[string]cron.EntryID)
		}
		r.cronEntriesMu.Unlock()
	}

	// Fetch the PaperMCServer resource
	var server mcv1alpha1.PaperMCServer
	if err := r.Get(ctx, req.NamespacedName, &server); err != nil {
		if apierrors.IsNotFound(err) {
			// Resource deleted - remove cron job if exists
			r.removeCronJob(req.String())
			log.Info("PaperMCServer resource not found, removed cron job if existed")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get PaperMCServer resource")
		return ctrl.Result{}, errors.Wrap(err, "failed to get server")
	}

	// Manage cron schedule for this server
	if err := r.manageCronSchedule(ctx, &server); err != nil {
		log.Error(err, "Failed to manage cron schedule")
		return ctrl.Result{}, err
	}

	// Note: Actual update logic will be implemented in later iterations
	// For now, we just manage cron schedules

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

// manageCronSchedule adds, updates, or removes cron jobs based on server spec.
func (r *UpdateReconciler) manageCronSchedule(ctx context.Context, server *mcv1alpha1.PaperMCServer) error {
	log := ctrl.LoggerFrom(ctx)
	serverKey := types.NamespacedName{
		Name:      server.Name,
		Namespace: server.Namespace,
	}.String()

	// Check if maintenance window is enabled
	if !server.Spec.UpdateSchedule.MaintenanceWindow.Enabled {
		// Remove cron job if exists
		r.removeCronJob(serverKey)
		log.Info("Maintenance window disabled, removed cron job")
		return nil
	}

	cronSpec := server.Spec.UpdateSchedule.MaintenanceWindow.Cron

	// Check if cron job already exists
	r.cronEntriesMu.RLock()
	existingID, exists := r.cronEntries[serverKey]
	r.cronEntriesMu.RUnlock()

	if exists {
		// Check if spec changed
		existingJob := r.getExistingJobSpec(existingID)
		if existingJob != cronSpec {
			// Spec changed - remove old and add new
			r.removeCronJob(serverKey)
			log.Info("Cron spec changed, updating job", "old", existingJob, "new", cronSpec)
		} else {
			// Spec unchanged - nothing to do
			return nil
		}
	}

	// Add new cron job
	entryID, err := r.cron.AddFunc(cronSpec, func() {
		// Trigger reconciliation on cron schedule
		log.Info("Maintenance window triggered by cron", "server", serverKey)
		// Note: actual update logic will be implemented in later iterations
	})

	if err != nil {
		return errors.Wrap(err, "failed to add cron job")
	}

	// Store entry ID
	r.cronEntriesMu.Lock()
	r.cronEntries[serverKey] = entryID
	r.cronEntriesMu.Unlock()

	log.Info("Added cron job", "server", serverKey, "spec", cronSpec, "entryID", entryID)
	return nil
}

// removeCronJob removes the cron job for a server.
func (r *UpdateReconciler) removeCronJob(serverKey string) {
	r.cronEntriesMu.Lock()
	defer r.cronEntriesMu.Unlock()

	if entryID, exists := r.cronEntries[serverKey]; exists {
		r.cron.Remove(entryID)
		delete(r.cronEntries, serverKey)
	}
}

// getExistingJobSpec gets the cron spec for an existing job.
func (r *UpdateReconciler) getExistingJobSpec(entryID cron.EntryID) string {
	// For mock scheduler, we can retrieve the spec
	if mock, ok := r.cron.(*testutil.MockCronScheduler); ok {
		job := mock.GetJob(entryID)
		if job != nil {
			return job.Spec
		}
	}

	// For real scheduler, we cannot retrieve the spec easily
	// In production, we would track specs separately or always recreate
	return ""
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
	log := ctrl.LoggerFrom(ctx)

	// Connect to RCON
	if err := rconClient.Connect(ctx); err != nil {
		return errors.Wrap(err, "failed to connect to RCON")
	}
	defer func() {
		if err := rconClient.Close(); err != nil {
			log.Error(err, "Failed to close RCON connection")
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

	log.Info("Graceful shutdown completed successfully")
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
		AppliedAt:            now,
		PreviousPaperVersion: server.Status.CurrentPaperVersion,
		Successful:           successful,
	}

	// Clear availableUpdate if successful
	if successful {
		server.Status.AvailableUpdate = nil
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

// SetCron sets the cron scheduler for the reconciler.
func (r *UpdateReconciler) SetCron(scheduler testutil.CronScheduler) {
	r.cron = scheduler
}

// SetupWithManager sets up the controller with the Manager.
func (r *UpdateReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Initialize cron entries map
	if r.cronEntries == nil {
		r.cronEntries = make(map[string]cron.EntryID)
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
