/*
Copyright 2026.

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
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/cockroachdb/errors"
	mcv1beta1 "github.com/lexfrei/minecraft-operator/api/v1beta1"
	"github.com/lexfrei/minecraft-operator/pkg/backup"
	mccron "github.com/lexfrei/minecraft-operator/pkg/cron"
	"github.com/lexfrei/minecraft-operator/pkg/metrics"
	"github.com/lexfrei/minecraft-operator/pkg/rcon"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

const (
	conditionTypeBackupCronValid = "BackupCronValid"
	conditionTypeBackupReady     = "BackupReady"
	reasonBackupCronInvalid      = "InvalidBackupCronExpression"
	reasonBackupCronValid        = "BackupCronScheduleConfigured"
	reasonSnapshotAPIUnavailable = "VolumeSnapshotAPIUnavailable"

	// snapshotAPIRetryInterval is the requeue interval when VolumeSnapshot CRD is missing.
	snapshotAPIRetryInterval = 5 * time.Minute

	// backupNowMaxAge is the maximum age for backup-now annotation.
	backupNowMaxAge = 5 * time.Minute

	// defaultMaxBackupCount is the default retention count when not specified.
	defaultMaxBackupCount int32 = 10
)

// RCONClientFactory creates RCON clients. Allows injection for testing.
type RCONClientFactory func(host, password string, port int) (rcon.Client, error)

// BackupReconciler reconciles PaperMCServer resources for VolumeSnapshot backups.
type BackupReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	Snapshotter backup.Snapshotter
	Metrics     metrics.Recorder
	cron        mccron.Scheduler

	// nowFunc returns current time; override in tests for deterministic behavior.
	nowFunc func() time.Time

	// rconClientFactory creates RCON clients; override in tests.
	rconClientFactory RCONClientFactory

	// Track cron entries per server
	cronEntriesMu sync.RWMutex
	cronEntries   map[string]cronEntryInfo

	// Track cron trigger times per server for scheduled backups
	cronTriggerMu    sync.RWMutex
	cronTriggerTimes map[string]time.Time

	// backupMu serializes backup execution per server to prevent concurrent
	// VolumeSnapshot creation when BackupReconciler and UpdateReconciler
	// both trigger backups for the same server simultaneously.
	backupMu sync.Map // map[string]*sync.Mutex — key is "namespace/name"

	// initOnce ensures maps are initialized exactly once.
	initOnce sync.Once
}

//+kubebuilder:rbac:groups=snapshot.storage.k8s.io,resources=volumesnapshots,verbs=get;list;watch;create;delete
//+kubebuilder:rbac:groups=snapshot.storage.k8s.io,resources=volumesnapshotclasses,verbs=get;list;watch
//nolint:revive // kubebuilder markers require no space after //

// Reconcile handles backup scheduling and execution for PaperMCServer resources.
//
//nolint:funlen,cyclop // Complex backup orchestration logic
func (r *BackupReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, retErr error) {
	r.initOnce.Do(r.initMaps)
	start := time.Now()

	var skipMetrics bool

	defer func() {
		if r.Metrics != nil && !skipMetrics {
			r.Metrics.RecordReconcile("backup", retErr, time.Since(start))
		}
	}()

	// Fetch the PaperMCServer resource
	var server mcv1beta1.PaperMCServer
	if err := r.Get(ctx, req.NamespacedName, &server); err != nil {
		if apierrors.IsNotFound(err) {
			skipMetrics = true
			r.removeBackupCronJob(req.String())
			r.consumeCronTrigger(req.String())
			r.backupMu.Delete(req.String())
			slog.InfoContext(ctx, "PaperMCServer not found, removed backup cron if existed")

			return ctrl.Result{}, nil
		}

		return ctrl.Result{}, errors.Wrap(err, "failed to get server")
	}

	// Check if backup is enabled
	if server.Spec.Backup == nil || !server.Spec.Backup.Enabled {
		r.removeBackupCronJob(req.String())
		r.consumeCronTrigger(req.String())
		r.backupMu.Delete(req.String())

		return ctrl.Result{}, nil
	}

	// Manage backup cron schedule
	if server.Spec.Backup.Schedule != "" {
		cronValid := r.manageBackupCronSchedule(ctx, &server)

		// Persist the BackupCronValid condition (True or False).
		// Re-fetch to avoid resource version conflicts with concurrent controllers.
		if err := r.persistCronCondition(ctx, req.NamespacedName, &server); err != nil {
			return ctrl.Result{}, errors.Wrap(err, "failed to persist cron condition")
		}

		if !cronValid {
			return ctrl.Result{}, nil
		}
	} else {
		// Schedule was removed from spec — clean up any existing cron job
		// to prevent orphaned goroutines and stale trigger times.
		r.removeBackupCronJob(req.String())
		r.consumeCronTrigger(req.String())
	}

	// Pre-flight: verify VolumeSnapshot API is available in the cluster
	if r.isSnapshotAPIUnavailable(ctx, &server) {
		return ctrl.Result{RequeueAfter: snapshotAPIRetryInterval}, nil
	}

	// Clear stale BackupReady=False condition after CRD becomes available.
	// Re-fetch the server to avoid resource version conflicts with concurrent controllers.
	if cond := meta.FindStatusCondition(server.Status.Conditions, conditionTypeBackupReady); cond != nil &&
		cond.Status == metav1.ConditionFalse {
		var latestServer mcv1beta1.PaperMCServer
		if getErr := r.Get(ctx, req.NamespacedName, &latestServer); getErr != nil {
			slog.ErrorContext(ctx, "Failed to re-fetch server for BackupReady recovery", "error", getErr)
		} else {
			meta.SetStatusCondition(&latestServer.Status.Conditions, metav1.Condition{
				Type:               conditionTypeBackupReady,
				Status:             metav1.ConditionTrue,
				Reason:             "VolumeSnapshotAPIAvailable",
				ObservedGeneration: latestServer.Generation,
				Message:            "VolumeSnapshot API is available in the cluster.",
			})

			if updateErr := r.Status().Update(ctx, &latestServer); updateErr != nil {
				slog.ErrorContext(ctx, "Failed to update BackupReady condition", "error", updateErr)
			}

			// Copy updated status back to in-memory server
			server.Status = latestServer.Status
			server.ResourceVersion = latestServer.ResourceVersion
		}
	}

	// Check for manual backup trigger
	backupNow := r.shouldBackupNow(ctx, &server)
	if !backupNow && server.Annotations != nil {
		// Clean up stale or invalid backup-now annotation so it doesn't persist indefinitely.
		// shouldBackupNow already logged the reason for rejection.
		// Return error to trigger requeue with backoff if cleanup fails.
		if _, hasAnnotation := server.Annotations[AnnotationBackupNow]; hasAnnotation {
			if err := r.removeBackupNowAnnotation(ctx, &server); err != nil {
				return ctrl.Result{}, errors.Wrap(err, "failed to remove stale backup-now annotation")
			}
		}
	}

	if backupNow {
		slog.InfoContext(ctx, "Manual backup triggered", "server", server.Name)

		// Remove annotation BEFORE performing backup to prevent duplicate backups
		// from concurrent reconciliations. Trade-off: if backup fails, user must
		// re-apply the annotation to retry. This is preferable to risking two
		// simultaneous VolumeSnapshots from the same trigger.
		if err := r.removeBackupNowAnnotation(ctx, &server); err != nil {
			return ctrl.Result{}, errors.Wrap(err, "failed to remove backup-now annotation")
		}

		if err := r.performBackup(ctx, &server, "manual"); err != nil {
			// Log but do not return error: the annotation is already removed, so
			// a requeue would be pointless (no trigger to retry). The failure is
			// recorded in status.backup.lastBackup for user visibility.
			slog.ErrorContext(ctx, "Manual backup failed", "error", err, "server", server.Name)
		} else {
			// A successful manual backup satisfies any pending cron trigger too,
			// since the data is already snapshotted. Consuming it avoids either
			// a redundant second backup or a stale trigger that never clears
			// (when manual backup's StartedAt >= triggerTime).
			r.consumeCronTrigger(req.String())
		}
	}

	// Check for scheduled backup trigger (cron fired since last backup)
	if r.shouldRunScheduledBackup(req.String(), &server) {
		slog.InfoContext(ctx, "Scheduled backup triggered", "server", server.Name)

		if err := r.performBackup(ctx, &server, "scheduled"); err != nil {
			slog.ErrorContext(ctx, "Scheduled backup failed", "error", err, "server", server.Name)
			// Trigger is intentionally NOT consumed on failure so the next reconcile retries.

			return ctrl.Result{}, err
		}

		// Consume the trigger only after a successful backup
		r.consumeCronTrigger(req.String())
	}

	// Requeue periodically to check for cron triggers
	if server.Spec.Backup.Schedule != "" {
		return ctrl.Result{RequeueAfter: 1 * time.Minute}, nil
	}

	return ctrl.Result{}, nil
}

// isSnapshotAPIUnavailable checks whether the VolumeSnapshot API is available.
// Returns true (and sets a condition) if the CRD is not installed.
func (r *BackupReconciler) isSnapshotAPIUnavailable(
	ctx context.Context,
	server *mcv1beta1.PaperMCServer,
) bool {
	_, err := r.Snapshotter.ListSnapshots(ctx, server.Namespace, server.Name)
	if err == nil {
		return false
	}

	var noKind *meta.NoKindMatchError
	var noResource *meta.NoResourceMatchError

	if errors.As(err, &noKind) || errors.As(err, &noResource) {
		slog.WarnContext(ctx, "VolumeSnapshot API not available in cluster",
			"error", err, "server", server.Name)

		// Re-fetch the server to get the latest ResourceVersion before Status().Update()
		// to avoid conflicts with concurrent controllers.
		var latestServer mcv1beta1.PaperMCServer
		if getErr := r.Get(ctx, client.ObjectKey{
			Name: server.Name, Namespace: server.Namespace,
		}, &latestServer); getErr != nil {
			slog.ErrorContext(ctx, "Failed to re-fetch server for snapshot API condition", "error", getErr)

			return true
		}

		meta.SetStatusCondition(&latestServer.Status.Conditions, metav1.Condition{
			Type:               conditionTypeBackupReady,
			Status:             metav1.ConditionFalse,
			Reason:             reasonSnapshotAPIUnavailable,
			ObservedGeneration: latestServer.Generation,
			Message:            "VolumeSnapshot CRD (snapshot.storage.k8s.io) is not installed. Install the CSI snapshot controller and CRDs to enable backups.",
		})

		if updateErr := r.Status().Update(ctx, &latestServer); updateErr != nil {
			slog.ErrorContext(ctx, "Failed to update status with snapshot API condition", "error", updateErr)
		}

		// Copy updated status back to in-memory server
		server.Status = latestServer.Status
		server.ResourceVersion = latestServer.ResourceVersion

		return true
	}

	// Non-API errors (network, etc.) — log but continue; backup attempt will produce a clearer error.
	slog.WarnContext(ctx, "Pre-flight snapshot API check failed, will attempt backup anyway",
		"error", err, "server", server.Name)

	return false
}

// isSnapshotCRDMissing checks whether the VolumeSnapshot CRD is installed in the cluster.
// Unlike isSnapshotAPIUnavailable, this method has no side effects — it does not modify
// server status or write conditions. Safe to call from any controller.
func (r *BackupReconciler) isSnapshotCRDMissing(ctx context.Context, namespace, serverName string) bool {
	_, err := r.Snapshotter.ListSnapshots(ctx, namespace, serverName)
	if err == nil {
		return false
	}

	var noKind *meta.NoKindMatchError
	var noResource *meta.NoResourceMatchError

	return errors.As(err, &noKind) || errors.As(err, &noResource)
}

// getServerBackupMu returns a per-server mutex for serializing backup execution.
func (r *BackupReconciler) getServerBackupMu(server *mcv1beta1.PaperMCServer) *sync.Mutex {
	key := server.Namespace + "/" + server.Name
	actual, _ := r.backupMu.LoadOrStore(key, &sync.Mutex{})

	//nolint:forcetypeassert // LoadOrStore always stores *sync.Mutex
	return actual.(*sync.Mutex)
}

// PerformBackup creates a VolumeSnapshot with RCON hooks for the given server.
// This is exported so UpdateReconciler can call it for pre-update backups.
// Concurrent calls for the same server are serialized via a per-server mutex.
func (r *BackupReconciler) PerformBackup(
	ctx context.Context,
	server *mcv1beta1.PaperMCServer,
	trigger string,
) error {
	return r.performBackup(ctx, server, trigger)
}

// performBackup creates a VolumeSnapshot with RCON hooks.
//
//nolint:funlen,cyclop // Backup orchestration with RCON hooks, snapshot creation, retention, and status update
func (r *BackupReconciler) performBackup(
	ctx context.Context,
	server *mcv1beta1.PaperMCServer,
	trigger string,
) error {
	// Serialize backup execution per server to prevent concurrent VolumeSnapshot creation
	// when BackupReconciler and UpdateReconciler both trigger backups simultaneously.
	mu := r.getServerBackupMu(server)
	mu.Lock()
	defer mu.Unlock()

	now := r.now()
	startedAt := metav1.NewTime(now)

	slog.InfoContext(ctx, "Starting backup", "server", server.Name, "trigger", trigger)

	// Verify PVC exists BEFORE running RCON hooks.
	// PVC name follows the StatefulSet convention: {claimTemplateName}-{statefulSetName}-{ordinal}.
	// The PaperMCServer controller always uses "data" as the volume claim template name,
	// and we only snapshot the first replica (ordinal 0).
	pvcName := fmt.Sprintf("data-%s-0", server.Name)

	var pvc corev1.PersistentVolumeClaim
	if pvcErr := r.Get(ctx, client.ObjectKey{Name: pvcName, Namespace: server.Namespace}, &pvc); pvcErr != nil {
		r.persistBackupStatus(ctx, server, &mcv1beta1.BackupRecord{
			StartedAt:  startedAt,
			Successful: false,
			Trigger:    trigger,
		}, -1)

		if apierrors.IsNotFound(pvcErr) {
			return errors.Newf(
				"PVC %s not found in namespace %s; ensure the StatefulSet has created its volume",
				pvcName, server.Namespace)
		}

		return errors.Wrap(pvcErr, "failed to check PVC existence before backup")
	}

	// Create RCON client for hooks (only if RCON is enabled)
	var rconClient rcon.Client

	if server.Spec.RCON.Enabled {
		var err error

		rconClient, err = r.createBackupRCONClient(ctx, server)
		if err != nil {
			r.persistBackupStatus(ctx, server, &mcv1beta1.BackupRecord{
				StartedAt:  startedAt,
				Successful: false,
				Trigger:    trigger,
			}, -1)

			return errors.Wrap(err, "failed to create RCON client for backup")
		}

		defer func() {
			if closeErr := rconClient.Close(); closeErr != nil {
				slog.ErrorContext(ctx, "Failed to close RCON connection", "error", closeErr)
			}
		}()

		// Connect to RCON
		if err := rconClient.Connect(ctx); err != nil {
			r.persistBackupStatus(ctx, server, &mcv1beta1.BackupRecord{
				StartedAt:  startedAt,
				Successful: false,
				Trigger:    trigger,
			}, -1)

			return errors.Wrap(err, "failed to connect to RCON for backup")
		}

		// Pre-snapshot hook: save-all + save-off
		if err := backup.PreSnapshotHook(ctx, rconClient); err != nil {
			// Always attempt save-on to re-enable auto-save, even if pre-hook failed.
			// save-off may have executed on the server despite the RCON error.
			if postErr := backup.PostSnapshotHook(ctx, rconClient); postErr != nil {
				slog.ErrorContext(ctx, "Post-snapshot hook failed after pre-hook error", "error", postErr)
			}

			r.persistBackupStatus(ctx, server, &mcv1beta1.BackupRecord{
				StartedAt:  startedAt,
				Successful: false,
				Trigger:    trigger,
			}, -1)

			return errors.Wrap(err, "pre-snapshot hook failed")
		}

		// Guarantee save-on runs even on panic (e.g., nil pointer in Snapshotter).
		// Leaving auto-save disabled permanently would cause data loss.
		defer func() {
			if postErr := backup.PostSnapshotHook(ctx, rconClient); postErr != nil {
				slog.ErrorContext(ctx, "Post-snapshot hook failed", "error", postErr)
			}
		}()
	}

	snapshotClass := ""

	if server.Spec.Backup != nil {
		snapshotClass = server.Spec.Backup.VolumeSnapshotClassName
	}

	snapshotName, err := r.Snapshotter.CreateSnapshot(ctx, backup.SnapshotRequest{
		Namespace:               server.Namespace,
		PVCName:                 pvcName,
		ServerName:              server.Name,
		VolumeSnapshotClassName: snapshotClass,
		Trigger:                 trigger,
		Timestamp:               now,
		OwnerReferences: []metav1.OwnerReference{
			*metav1.NewControllerRef(server, mcv1beta1.GroupVersion.WithKind("PaperMCServer")),
		},
	})

	if err != nil {
		r.persistBackupStatus(ctx, server, &mcv1beta1.BackupRecord{
			StartedAt:  startedAt,
			Successful: false,
			Trigger:    trigger,
		}, -1)

		return errors.Wrap(err, "failed to create VolumeSnapshot")
	}

	// Apply retention policy
	maxCount := defaultMaxBackupCount
	if server.Spec.Backup != nil && server.Spec.Backup.Retention.MaxCount > 0 {
		maxCount = server.Spec.Backup.Retention.MaxCount
	}

	deleted, retErr := r.Snapshotter.DeleteOldSnapshots(ctx, server.Namespace, server.Name, int(maxCount))
	if retErr != nil {
		slog.ErrorContext(ctx, "Failed to apply retention policy", "error", retErr)
	} else if deleted > 0 {
		slog.InfoContext(ctx, "Deleted old snapshots", "count", deleted, "server", server.Name)
	}

	// Count remaining snapshots. On error, preserve existing count to avoid
	// recording 0 when snapshots actually exist.
	var backupCount int32 = -1

	snapshots, listErr := r.Snapshotter.ListSnapshots(ctx, server.Namespace, server.Name)
	if listErr != nil {
		slog.ErrorContext(ctx, "Failed to count snapshots after backup", "error", listErr)
	} else {
		backupCount = int32(len(snapshots))
	}

	completedAt := metav1.NewTime(r.now())

	// Persist status with a single re-fetch + update to avoid resource version conflicts
	r.persistBackupStatus(ctx, server, &mcv1beta1.BackupRecord{
		SnapshotName: snapshotName,
		StartedAt:    startedAt,
		CompletedAt:  &completedAt,
		Successful:   true,
		Trigger:      trigger,
	}, backupCount)

	slog.InfoContext(ctx, "Backup completed successfully",
		"server", server.Name,
		"snapshot", snapshotName,
		"trigger", trigger)

	return nil
}

// persistBackupStatus re-fetches the server to get the latest ResourceVersion,
// sets the backup status, and performs a single Status().Update().
// Errors are intentionally logged and swallowed: status persistence is best-effort.
// The backup itself may have succeeded, and a status update failure should not
// cause the controller to report a failed backup when the snapshot exists.
func (r *BackupReconciler) persistBackupStatus(
	ctx context.Context,
	server *mcv1beta1.PaperMCServer,
	record *mcv1beta1.BackupRecord,
	backupCount int32,
) {
	// Re-fetch the server to get the latest ResourceVersion
	var latestServer mcv1beta1.PaperMCServer
	if err := r.Get(ctx, client.ObjectKey{
		Name:      server.Name,
		Namespace: server.Namespace,
	}, &latestServer); err != nil {
		slog.ErrorContext(ctx, "Failed to re-fetch server for backup status update", "error", err)

		return
	}

	if latestServer.Status.Backup == nil {
		latestServer.Status.Backup = &mcv1beta1.BackupStatus{}
	}

	latestServer.Status.Backup.LastBackup = record

	// Only update BackupCount if we successfully counted snapshots (>= 0).
	// A negative value means ListSnapshots failed and we should preserve the existing count.
	if backupCount >= 0 {
		latestServer.Status.Backup.BackupCount = backupCount
	}

	if updateErr := r.Status().Update(ctx, &latestServer); updateErr != nil {
		slog.ErrorContext(ctx, "Failed to update backup status", "error", updateErr)
	}

	// Copy updated status back to caller's server object
	server.Status = latestServer.Status
	server.ResourceVersion = latestServer.ResourceVersion
}

// persistCronCondition re-fetches the server, copies the BackupCronValid condition, and persists it.
func (r *BackupReconciler) persistCronCondition(
	ctx context.Context,
	key types.NamespacedName,
	server *mcv1beta1.PaperMCServer,
) error {
	var latestServer mcv1beta1.PaperMCServer
	if err := r.Get(ctx, key, &latestServer); err != nil {
		return errors.Wrap(err, "failed to re-fetch server for cron condition")
	}

	// Copy the in-memory condition to the latest server
	if cond := meta.FindStatusCondition(server.Status.Conditions, conditionTypeBackupCronValid); cond != nil {
		meta.SetStatusCondition(&latestServer.Status.Conditions, *cond)
	}

	if updateErr := r.Status().Update(ctx, &latestServer); updateErr != nil {
		slog.ErrorContext(ctx, "Failed to persist BackupCronValid condition", "error", updateErr)

		return errors.Wrap(updateErr, "failed to update cron condition")
	}

	// Copy updated status back
	server.Status = latestServer.Status
	server.ResourceVersion = latestServer.ResourceVersion

	return nil
}

// rconConnInfo holds the connection details for an RCON client.
type rconConnInfo struct {
	host     string
	password string
	port     int
}

// getRCONConnInfo retrieves RCON connection details from the pod and secret.
func (r *BackupReconciler) getRCONConnInfo(
	ctx context.Context,
	server *mcv1beta1.PaperMCServer,
) (*rconConnInfo, error) {
	var pod corev1.Pod
	if err := r.Get(ctx, client.ObjectKey{
		Name: server.Name + "-0", Namespace: server.Namespace,
	}, &pod); err != nil {
		return nil, errors.Wrap(err, "failed to get pod for RCON connection")
	}

	if pod.Status.PodIP == "" {
		return nil, errors.New("pod IP not available")
	}

	var secret corev1.Secret
	if err := r.Get(ctx, client.ObjectKey{
		Name: server.Spec.RCON.PasswordSecret.Name, Namespace: server.Namespace,
	}, &secret); err != nil {
		return nil, errors.Wrap(err, "failed to get RCON password secret")
	}

	passwordBytes, exists := secret.Data[server.Spec.RCON.PasswordSecret.Key]
	if !exists {
		return nil, errors.Newf("key %s not found in secret %s",
			server.Spec.RCON.PasswordSecret.Key, server.Spec.RCON.PasswordSecret.Name)
	}

	port := int(server.Spec.RCON.Port)
	if port == 0 {
		port = 25575
	}

	return &rconConnInfo{host: pod.Status.PodIP, password: string(passwordBytes), port: port}, nil
}

// createBackupRCONClient creates an RCON client for backup hooks.
func (r *BackupReconciler) createBackupRCONClient(
	ctx context.Context,
	server *mcv1beta1.PaperMCServer,
) (rcon.Client, error) {
	info, err := r.getRCONConnInfo(ctx, server)
	if err != nil {
		return nil, err
	}

	if r.rconClientFactory != nil {
		return r.rconClientFactory(info.host, info.password, info.port)
	}

	rconClient, err := rcon.NewRCONClient(info.host, info.port, info.password)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create RCON client")
	}

	return rconClient, nil
}

// manageBackupCronSchedule manages the cron schedule for backup.
// Returns true if the schedule is valid, false if invalid.
func (r *BackupReconciler) manageBackupCronSchedule(
	ctx context.Context,
	server *mcv1beta1.PaperMCServer,
) bool {
	serverKey := types.NamespacedName{
		Name:      server.Name,
		Namespace: server.Namespace,
	}.String()

	cronSpec := server.Spec.Backup.Schedule

	// Check if cron job already exists with same spec
	r.cronEntriesMu.RLock()
	existing, exists := r.cronEntries[serverKey]
	r.cronEntriesMu.RUnlock()

	if exists {
		if existing.Spec == cronSpec {
			return true
		}

		r.removeBackupCronJob(serverKey)
	}

	// Add new cron job — callback records trigger time for Reconcile to pick up
	entryID, err := r.cron.AddFunc(cronSpec, func() {
		r.cronTriggerMu.Lock()
		r.cronTriggerTimes[serverKey] = r.now()
		r.cronTriggerMu.Unlock()
		slog.InfoContext(context.Background(), "Backup cron triggered", "server", serverKey)
	})

	if err != nil {
		slog.WarnContext(ctx, "Invalid backup cron expression",
			"error", err, "cronSpec", cronSpec)
		setBackupCronCondition(server, metav1.ConditionFalse, reasonBackupCronInvalid, err.Error())

		return false
	}

	setBackupCronCondition(server, metav1.ConditionTrue, reasonBackupCronValid,
		"Backup cron schedule configured: "+cronSpec)

	r.cronEntriesMu.Lock()
	r.cronEntries[serverKey] = cronEntryInfo{ID: entryID, Spec: cronSpec}
	r.cronEntriesMu.Unlock()

	slog.InfoContext(ctx, "Added backup cron job", "server", serverKey, "spec", cronSpec)

	return true
}

// setBackupCronCondition sets the BackupCronValid condition on the server.
func setBackupCronCondition(server *mcv1beta1.PaperMCServer, status metav1.ConditionStatus, reason, message string) {
	// Do NOT set LastTransitionTime explicitly — meta.SetStatusCondition manages it
	// and only updates the timestamp when the status actually changes.
	meta.SetStatusCondition(&server.Status.Conditions, metav1.Condition{
		Type:               conditionTypeBackupCronValid,
		Status:             status,
		ObservedGeneration: server.Generation,
		Reason:             reason,
		Message:            message,
	})
}

// removeBackupCronJob removes the backup cron job for a server.
func (r *BackupReconciler) removeBackupCronJob(serverKey string) {
	r.cronEntriesMu.Lock()
	defer r.cronEntriesMu.Unlock()

	if entry, exists := r.cronEntries[serverKey]; exists {
		if r.cron != nil {
			r.cron.Remove(entry.ID)
		}

		delete(r.cronEntries, serverKey)
	}
}

// shouldRunScheduledBackup checks if the cron has fired since the last backup.
func (r *BackupReconciler) shouldRunScheduledBackup(
	serverKey string,
	server *mcv1beta1.PaperMCServer,
) bool {
	r.cronTriggerMu.RLock()
	triggerTime, triggered := r.cronTriggerTimes[serverKey]
	r.cronTriggerMu.RUnlock()

	if !triggered {
		return false
	}

	// Check if we already have a SUCCESSFUL backup after this trigger.
	// Failed backups do not satisfy a cron trigger — the scheduled backup must still run.
	if server.Status.Backup != nil && server.Status.Backup.LastBackup != nil {
		lastBackup := server.Status.Backup.LastBackup
		if lastBackup.Successful && !lastBackup.StartedAt.Time.Before(triggerTime) {
			return false
		}
	}

	return true
}

// consumeCronTrigger removes the cron trigger for a server after a successful backup.
func (r *BackupReconciler) consumeCronTrigger(serverKey string) {
	r.cronTriggerMu.Lock()
	delete(r.cronTriggerTimes, serverKey)
	r.cronTriggerMu.Unlock()
}

// shouldBackupNow checks if the backup-now annotation is present and valid.
func (r *BackupReconciler) shouldBackupNow(ctx context.Context, server *mcv1beta1.PaperMCServer) bool {
	if server.Annotations == nil {
		return false
	}

	tsStr, exists := server.Annotations[AnnotationBackupNow]
	if !exists {
		return false
	}

	var ts int64
	if _, err := fmt.Sscanf(tsStr, "%d", &ts); err != nil {
		slog.WarnContext(ctx, "Invalid backup-now annotation format",
			"value", tsStr, "server", server.Name)

		return false
	}

	annotationTime := time.Unix(ts, 0)
	age := r.now().Sub(annotationTime)

	if age > backupNowMaxAge || age < -backupNowMaxAge {
		slog.InfoContext(ctx, "Ignoring stale or future backup-now annotation",
			"age", age, "maxAge", backupNowMaxAge, "server", server.Name)

		return false
	}

	return true
}

// removeBackupNowAnnotation removes the backup-now annotation from the server.
func (r *BackupReconciler) removeBackupNowAnnotation(
	ctx context.Context,
	server *mcv1beta1.PaperMCServer,
) error {
	var currentServer mcv1beta1.PaperMCServer
	if err := r.Get(ctx, client.ObjectKey{
		Name:      server.Name,
		Namespace: server.Namespace,
	}, &currentServer); err != nil {
		return errors.Wrap(err, "failed to get server for annotation removal")
	}

	patch := client.MergeFrom(currentServer.DeepCopy())
	delete(currentServer.Annotations, AnnotationBackupNow)

	if err := r.Patch(ctx, &currentServer, patch); err != nil {
		return errors.Wrap(err, "failed to remove backup-now annotation")
	}

	// Update the server in-memory to reflect removed annotation and new ResourceVersion
	server.Annotations = currentServer.Annotations
	server.ResourceVersion = currentServer.ResourceVersion

	return nil
}

// now returns the current time, using nowFunc if set (for testing).
func (r *BackupReconciler) now() time.Time {
	if r.nowFunc != nil {
		return r.nowFunc()
	}

	return time.Now()
}

// SetCron sets the cron scheduler for the reconciler.
func (r *BackupReconciler) SetCron(scheduler mccron.Scheduler) {
	r.cron = scheduler
}

// initMaps initializes internal maps. Called via sync.Once.
func (r *BackupReconciler) initMaps() {
	if r.cronEntries == nil {
		r.cronEntries = make(map[string]cronEntryInfo)
	}

	if r.cronTriggerTimes == nil {
		r.cronTriggerTimes = make(map[string]time.Time)
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *BackupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.initMaps()

	if r.cron != nil {
		r.cron.Start()
	}

	return ctrl.NewControllerManagedBy(mgr).
		// Only reconcile on spec changes (generation) or annotation changes (backup-now).
		// Status-only updates from other controllers are filtered out to avoid churn.
		For(&mcv1beta1.PaperMCServer{}, builder.WithPredicates(
			predicate.Or(
				predicate.GenerationChangedPredicate{},
				predicate.AnnotationChangedPredicate{},
			),
		)).
		Named("backup").
		Complete(r)
}
