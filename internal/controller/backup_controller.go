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
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	conditionTypeBackupCronValid = "BackupCronValid"
	reasonBackupCronInvalid      = "InvalidBackupCronExpression"
	reasonBackupCronValid        = "BackupCronScheduleConfigured"

	// backupNowMaxAge is the maximum age for backup-now annotation.
	backupNowMaxAge = 5 * time.Minute

	// defaultMaxBackupCount is the default retention count when not specified.
	defaultMaxBackupCount = 10
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
}

//+kubebuilder:rbac:groups=snapshot.storage.k8s.io,resources=volumesnapshots,verbs=get;list;watch;create;delete
//+kubebuilder:rbac:groups=snapshot.storage.k8s.io,resources=volumesnapshotclasses,verbs=get;list;watch
//nolint:revive // kubebuilder markers require no space after //

// Reconcile handles backup scheduling and execution for PaperMCServer resources.
//
//nolint:funlen,cyclop // Complex backup orchestration logic
func (r *BackupReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	start := time.Now()

	var skipMetrics bool

	defer func() {
		if r.Metrics != nil && !skipMetrics {
			r.Metrics.RecordReconcile("backup", nil, time.Since(start))
		}
	}()

	// Initialize maps if nil
	if r.cronEntries == nil {
		r.cronEntriesMu.Lock()
		if r.cronEntries == nil {
			r.cronEntries = make(map[string]cronEntryInfo)
		}
		r.cronEntriesMu.Unlock()
	}

	if r.cronTriggerTimes == nil {
		r.cronTriggerMu.Lock()
		if r.cronTriggerTimes == nil {
			r.cronTriggerTimes = make(map[string]time.Time)
		}
		r.cronTriggerMu.Unlock()
	}

	// Fetch the PaperMCServer resource
	var server mcv1beta1.PaperMCServer
	if err := r.Get(ctx, req.NamespacedName, &server); err != nil {
		if apierrors.IsNotFound(err) {
			skipMetrics = true
			r.removeBackupCronJob(req.String())
			slog.InfoContext(ctx, "PaperMCServer not found, removed backup cron if existed")

			return ctrl.Result{}, nil
		}

		return ctrl.Result{}, errors.Wrap(err, "failed to get server")
	}

	// Check if backup is enabled
	if server.Spec.Backup == nil || !server.Spec.Backup.Enabled {
		r.removeBackupCronJob(req.String())

		return ctrl.Result{}, nil
	}

	// Manage backup cron schedule
	if server.Spec.Backup.Schedule != "" {
		if !r.manageBackupCronSchedule(ctx, &server) {
			// Invalid cron — condition set, persist and return
			if updateErr := r.Status().Update(ctx, &server); updateErr != nil {
				slog.ErrorContext(ctx, "Failed to update status with cron condition", "error", updateErr)

				return ctrl.Result{}, errors.Wrap(updateErr, "failed to update status")
			}

			return ctrl.Result{}, nil
		}
	}

	// Check for manual backup trigger
	backupNow := r.shouldBackupNow(ctx, &server)
	if backupNow {
		slog.InfoContext(ctx, "Manual backup triggered", "server", server.Name)

		if err := r.removeBackupNowAnnotation(ctx, &server); err != nil {
			return ctrl.Result{}, errors.Wrap(err, "failed to remove backup-now annotation")
		}

		if err := r.performBackup(ctx, &server, "manual"); err != nil {
			slog.ErrorContext(ctx, "Manual backup failed", "error", err, "server", server.Name)

			return ctrl.Result{}, err
		}
	}

	// Check for scheduled backup trigger (cron fired since last backup)
	if r.shouldRunScheduledBackup(req.String(), &server) {
		slog.InfoContext(ctx, "Scheduled backup triggered", "server", server.Name)

		if err := r.performBackup(ctx, &server, "scheduled"); err != nil {
			slog.ErrorContext(ctx, "Scheduled backup failed", "error", err, "server", server.Name)

			return ctrl.Result{}, err
		}
	}

	// Requeue periodically to check for cron triggers
	if server.Spec.Backup.Schedule != "" {
		return ctrl.Result{RequeueAfter: 1 * time.Minute}, nil
	}

	return ctrl.Result{}, nil
}

// PerformBackup creates a VolumeSnapshot with RCON hooks for the given server.
// This is exported so UpdateReconciler can call it for pre-update backups.
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
	now := r.now()
	startedAt := metav1.NewTime(now)

	slog.InfoContext(ctx, "Starting backup", "server", server.Name, "trigger", trigger)

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
			}, 0)

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
			}, 0)

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
			}, 0)

			return errors.Wrap(err, "pre-snapshot hook failed")
		}
	}

	// Create VolumeSnapshot
	pvcName := fmt.Sprintf("data-%s-0", server.Name)
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
		OwnerReferences: []metav1.OwnerReference{
			*metav1.NewControllerRef(server, mcv1beta1.GroupVersion.WithKind("PaperMCServer")),
		},
	})

	// Post-snapshot hook: save-on (always run, even if snapshot creation failed)
	if rconClient != nil {
		if postErr := backup.PostSnapshotHook(ctx, rconClient); postErr != nil {
			slog.ErrorContext(ctx, "Post-snapshot hook failed", "error", postErr)
		}
	}

	if err != nil {
		r.persistBackupStatus(ctx, server, &mcv1beta1.BackupRecord{
			StartedAt:  startedAt,
			Successful: false,
			Trigger:    trigger,
		}, 0)

		return errors.Wrap(err, "failed to create VolumeSnapshot")
	}

	// Apply retention policy
	maxCount := defaultMaxBackupCount
	if server.Spec.Backup != nil && server.Spec.Backup.Retention.MaxCount > 0 {
		maxCount = server.Spec.Backup.Retention.MaxCount
	}

	deleted, retErr := r.Snapshotter.DeleteOldSnapshots(ctx, server.Namespace, server.Name, maxCount)
	if retErr != nil {
		slog.ErrorContext(ctx, "Failed to apply retention policy", "error", retErr)
	} else if deleted > 0 {
		slog.InfoContext(ctx, "Deleted old snapshots", "count", deleted, "server", server.Name)
	}

	// Count remaining snapshots
	snapshots, _ := r.Snapshotter.ListSnapshots(ctx, server.Namespace, server.Name)
	completedAt := metav1.NewTime(r.now())

	// Persist status with a single re-fetch + update to avoid resource version conflicts
	r.persistBackupStatus(ctx, server, &mcv1beta1.BackupRecord{
		SnapshotName: snapshotName,
		StartedAt:    startedAt,
		CompletedAt:  &completedAt,
		Successful:   true,
		Trigger:      trigger,
	}, len(snapshots))

	slog.InfoContext(ctx, "Backup completed successfully",
		"server", server.Name,
		"snapshot", snapshotName,
		"trigger", trigger)

	return nil
}

// persistBackupStatus re-fetches the server to get the latest ResourceVersion,
// sets the backup status, and performs a single Status().Update().
func (r *BackupReconciler) persistBackupStatus(
	ctx context.Context,
	server *mcv1beta1.PaperMCServer,
	record *mcv1beta1.BackupRecord,
	backupCount int,
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
	latestServer.Status.Backup.BackupCount = backupCount

	if updateErr := r.Status().Update(ctx, &latestServer); updateErr != nil {
		slog.ErrorContext(ctx, "Failed to update backup status", "error", updateErr)
	}

	// Copy updated status back to caller's server object
	server.Status = latestServer.Status
	server.ResourceVersion = latestServer.ResourceVersion
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
		r.cronTriggerTimes[serverKey] = time.Now()
		r.cronTriggerMu.Unlock()
		slog.InfoContext(ctx, "Backup cron triggered", "server", serverKey)
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
	meta.SetStatusCondition(&server.Status.Conditions, metav1.Condition{
		Type:               conditionTypeBackupCronValid,
		Status:             status,
		ObservedGeneration: server.Generation,
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	})
}

// removeBackupCronJob removes the backup cron job for a server.
func (r *BackupReconciler) removeBackupCronJob(serverKey string) {
	r.cronEntriesMu.Lock()
	defer r.cronEntriesMu.Unlock()

	if entry, exists := r.cronEntries[serverKey]; exists {
		r.cron.Remove(entry.ID)
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

	// Check if we already have a backup after this trigger
	if server.Status.Backup != nil && server.Status.Backup.LastBackup != nil {
		if !server.Status.Backup.LastBackup.StartedAt.Time.Before(triggerTime) {
			return false
		}
	}

	// Clear the trigger time after consuming it
	r.cronTriggerMu.Lock()
	delete(r.cronTriggerTimes, serverKey)
	r.cronTriggerMu.Unlock()

	return true
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

	if age > backupNowMaxAge {
		slog.InfoContext(ctx, "Ignoring stale backup-now annotation",
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

	delete(currentServer.Annotations, AnnotationBackupNow)

	if err := r.Update(ctx, &currentServer); err != nil {
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

// SetupWithManager sets up the controller with the Manager.
func (r *BackupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.cronEntries == nil {
		r.cronEntries = make(map[string]cronEntryInfo)
	}

	if r.cron != nil {
		r.cron.Start()
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&mcv1beta1.PaperMCServer{}).
		Named("backup").
		Complete(r)
}
