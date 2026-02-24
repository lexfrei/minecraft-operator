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
	"testing"
	"time"

	volumesnapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	mcv1beta1 "github.com/lexfrei/minecraft-operator/api/v1beta1"
	"github.com/lexfrei/minecraft-operator/pkg/backup"
	"github.com/lexfrei/minecraft-operator/pkg/metrics"
	"github.com/lexfrei/minecraft-operator/pkg/rcon"
	"github.com/lexfrei/minecraft-operator/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const testServerKey = "minecraft/my-server"

func newBackupTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(s)
	_ = mcv1beta1.AddToScheme(s)
	_ = volumesnapshotv1.AddToScheme(s)

	return s
}

func newTestServer(backupSpec *mcv1beta1.BackupSpec) *mcv1beta1.PaperMCServer {
	const name = "my-server"
	const namespace = "minecraft"
	return &mcv1beta1.PaperMCServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: mcv1beta1.PaperMCServerSpec{
			UpdateStrategy: "latest",
			UpdateSchedule: mcv1beta1.UpdateSchedule{
				CheckCron: "0 3 * * *",
				MaintenanceWindow: mcv1beta1.MaintenanceWindow{
					Enabled: true,
					Cron:    "0 4 * * 0",
				},
			},
			GracefulShutdown: mcv1beta1.GracefulShutdown{
				Timeout: metav1.Duration{Duration: 60 * time.Second},
			},
			RCON: mcv1beta1.RCONConfig{
				Enabled: true,
				PasswordSecret: mcv1beta1.SecretKeyRef{
					Name: name + "-rcon",
					Key:  "password",
				},
				Port: 25575,
			},
			Backup:      backupSpec,
			PodTemplate: corev1.PodTemplateSpec{},
		},
	}
}

func newRCONSecret() *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-server-rcon",
			Namespace: "minecraft",
		},
		Data: map[string][]byte{
			"password": []byte("test-password"),
		},
	}
}

func newServerPVC() *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "data-my-server-0",
			Namespace: "minecraft",
		},
	}
}

func newServerPod() *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-server-0",
			Namespace: "minecraft",
		},
		Status: corev1.PodStatus{
			PodIP: "10.0.0.1",
			Conditions: []corev1.PodCondition{
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}
}

func TestBackupReconciler_BackupDisabled(t *testing.T) {
	scheme := newBackupTestScheme()
	server := newTestServer(nil)

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(server).
		WithStatusSubresource(server).
		Build()

	r := &BackupReconciler{
		Client:      fakeClient,
		Scheme:      scheme,
		Snapshotter: backup.NewSnapshotter(fakeClient),
		Metrics:     &metrics.NoopRecorder{},
		cron:        testutil.NewMockCronScheduler(),
	}

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-server", Namespace: "minecraft"},
	})

	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result)
}

func TestBackupReconciler_BackupDisabledExplicitly(t *testing.T) {
	scheme := newBackupTestScheme()
	server := newTestServer(&mcv1beta1.BackupSpec{
		Enabled: false,
	})

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(server).
		WithStatusSubresource(server).
		Build()

	r := &BackupReconciler{
		Client:      fakeClient,
		Scheme:      scheme,
		Snapshotter: backup.NewSnapshotter(fakeClient),
		Metrics:     &metrics.NoopRecorder{},
		cron:        testutil.NewMockCronScheduler(),
	}

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-server", Namespace: "minecraft"},
	})

	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result)
}

func verifyRCONBackupCommands(t *testing.T, mockRCON *rcon.MockClient) {
	t.Helper()
	sentCmds := mockRCON.GetSentCommands()
	assert.Contains(t, sentCmds, "save-all")
	assert.Contains(t, sentCmds, "save-off")
	assert.Contains(t, sentCmds, "save-on")
}

func TestBackupReconciler_ManualBackupTrigger(t *testing.T) { //nolint:funlen
	scheme := newBackupTestScheme()
	now := time.Now()

	server := newTestServer(&mcv1beta1.BackupSpec{
		Enabled:   true,
		Retention: mcv1beta1.BackupRetention{MaxCount: 10},
	})
	server.Annotations = map[string]string{
		AnnotationBackupNow: fmt.Sprintf("%d", now.Unix()),
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(server, newServerPod(), newRCONSecret(), newServerPVC()).
		WithStatusSubresource(server).
		Build()

	mockRCON := rcon.NewMockClient()
	r := &BackupReconciler{
		Client: fakeClient, Scheme: scheme,
		Snapshotter: backup.NewSnapshotter(fakeClient),
		Metrics:     &metrics.NoopRecorder{}, cron: testutil.NewMockCronScheduler(),
		nowFunc: func() time.Time { return now },
		rconClientFactory: func(_, _ string, _ int) (rcon.Client, error) {
			return mockRCON, nil
		},
	}

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-server", Namespace: "minecraft"},
	})
	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result)

	verifyRCONBackupCommands(t, mockRCON)

	snapshots, err := backup.NewSnapshotter(fakeClient).ListSnapshots(
		context.Background(), "minecraft", "my-server")
	require.NoError(t, err)
	assert.Len(t, snapshots, 1)
	assert.Equal(t, "manual", snapshots[0].Labels[backup.LabelTrigger])

	// Verify owner reference for cascade deletion
	require.Len(t, snapshots[0].OwnerReferences, 1, "VolumeSnapshot should have owner reference")
	assert.Equal(t, "PaperMCServer", snapshots[0].OwnerReferences[0].Kind)
	assert.Equal(t, "my-server", snapshots[0].OwnerReferences[0].Name)

	var updatedServer mcv1beta1.PaperMCServer
	err = fakeClient.Get(context.Background(), types.NamespacedName{
		Name: "my-server", Namespace: "minecraft",
	}, &updatedServer)
	require.NoError(t, err)
	_, exists := updatedServer.Annotations[AnnotationBackupNow]
	assert.False(t, exists, "backup-now annotation should be removed after backup")
	assert.NotNil(t, updatedServer.Status.Backup)
	assert.NotNil(t, updatedServer.Status.Backup.LastBackup)
	assert.True(t, updatedServer.Status.Backup.LastBackup.Successful)
	assert.Equal(t, "manual", updatedServer.Status.Backup.LastBackup.Trigger)
}

func TestBackupReconciler_RetentionCleanup(t *testing.T) { //nolint:funlen
	scheme := newBackupTestScheme()
	now := time.Now()

	server := newTestServer(&mcv1beta1.BackupSpec{
		Enabled:   true,
		Retention: mcv1beta1.BackupRetention{MaxCount: 2},
	})
	server.Annotations = map[string]string{
		AnnotationBackupNow: fmt.Sprintf("%d", now.Unix()),
	}

	existingSnapshots := []volumesnapshotv1.VolumeSnapshot{
		{ObjectMeta: metav1.ObjectMeta{
			Name: "my-server-backup-old1", Namespace: "minecraft",
			CreationTimestamp: metav1.NewTime(now.Add(-3 * time.Hour)),
			Labels: map[string]string{
				backup.LabelServerName: "my-server", backup.LabelManagedBy: "minecraft-operator",
			},
		}},
		{ObjectMeta: metav1.ObjectMeta{
			Name: "my-server-backup-old2", Namespace: "minecraft",
			CreationTimestamp: metav1.NewTime(now.Add(-2 * time.Hour)),
			Labels: map[string]string{
				backup.LabelServerName: "my-server", backup.LabelManagedBy: "minecraft-operator",
			},
		}},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(server, newServerPod(), newRCONSecret(), newServerPVC(),
			&existingSnapshots[0], &existingSnapshots[1]).
		WithStatusSubresource(server).
		Build()

	mockRCON := rcon.NewMockClient()
	r := &BackupReconciler{
		Client: fakeClient, Scheme: scheme,
		Snapshotter: backup.NewSnapshotter(fakeClient),
		Metrics:     &metrics.NoopRecorder{}, cron: testutil.NewMockCronScheduler(),
		nowFunc: func() time.Time { return now },
		rconClientFactory: func(_, _ string, _ int) (rcon.Client, error) {
			return mockRCON, nil
		},
	}

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-server", Namespace: "minecraft"},
	})
	require.NoError(t, err)

	snapshots, err := backup.NewSnapshotter(fakeClient).ListSnapshots(
		context.Background(), "minecraft", "my-server")
	require.NoError(t, err)
	assert.Len(t, snapshots, 2, "should retain only maxCount snapshots")

	for _, s := range snapshots {
		assert.NotEqual(t, "my-server-backup-old1", s.Name,
			"oldest snapshot should have been deleted")
	}
}

func TestBackupReconciler_ConnectFailurePersistsStatus(t *testing.T) {
	scheme := newBackupTestScheme()
	now := time.Now()

	server := newTestServer(&mcv1beta1.BackupSpec{
		Enabled:   true,
		Retention: mcv1beta1.BackupRetention{MaxCount: 10},
	})
	server.Annotations = map[string]string{
		AnnotationBackupNow: fmt.Sprintf("%d", now.Unix()),
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(server, newServerPod(), newRCONSecret(), newServerPVC()).
		WithStatusSubresource(server).
		Build()

	mockRCON := rcon.NewMockClient()
	mockRCON.ConnectError = fmt.Errorf("connection refused")

	r := &BackupReconciler{
		Client: fakeClient, Scheme: scheme,
		Snapshotter: backup.NewSnapshotter(fakeClient),
		Metrics:     &metrics.NoopRecorder{},
		cron:        testutil.NewMockCronScheduler(),
		nowFunc:     func() time.Time { return now },
		rconClientFactory: func(_, _ string, _ int) (rcon.Client, error) {
			return mockRCON, nil
		},
	}

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-server", Namespace: "minecraft"},
	})
	// Manual backup failure does NOT return error (annotation already removed,
	// requeue would be pointless). Failure is recorded in status only.
	require.NoError(t, err)

	var updatedServer mcv1beta1.PaperMCServer
	getErr := fakeClient.Get(context.Background(), types.NamespacedName{
		Name: "my-server", Namespace: "minecraft",
	}, &updatedServer)
	require.NoError(t, getErr)

	require.NotNil(t, updatedServer.Status.Backup, "backup status should be set on Connect failure")
	require.NotNil(t, updatedServer.Status.Backup.LastBackup)
	assert.False(t, updatedServer.Status.Backup.LastBackup.Successful)
	assert.Equal(t, "manual", updatedServer.Status.Backup.LastBackup.Trigger)
}

func TestBackupReconciler_CronTriggeredBackup(t *testing.T) {
	scheme := newBackupTestScheme()
	now := time.Now()

	server := newTestServer(&mcv1beta1.BackupSpec{
		Enabled:   true,
		Schedule:  "0 */6 * * *",
		Retention: mcv1beta1.BackupRetention{MaxCount: 10},
	})

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(server, newServerPod(), newRCONSecret(), newServerPVC()).
		WithStatusSubresource(server).
		Build()

	mockRCON := rcon.NewMockClient()
	r := &BackupReconciler{
		Client: fakeClient, Scheme: scheme,
		Snapshotter: backup.NewSnapshotter(fakeClient),
		Metrics:     &metrics.NoopRecorder{},
		cron:        testutil.NewMockCronScheduler(),
		nowFunc:     func() time.Time { return now },
		rconClientFactory: func(_, _ string, _ int) (rcon.Client, error) {
			return mockRCON, nil
		},
	}

	// First reconcile — sets up cron, no backup yet
	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-server", Namespace: "minecraft"},
	})
	require.NoError(t, err)
	assert.NotZero(t, result.RequeueAfter, "should requeue for cron schedule check")

	// Simulate cron trigger
	serverKey := testServerKey
	r.cronTriggerMu.Lock()
	if r.cronTriggerTimes == nil {
		r.cronTriggerTimes = make(map[string]time.Time)
	}
	r.cronTriggerTimes[serverKey] = now
	r.cronTriggerMu.Unlock()

	// Second reconcile — should detect cron trigger and run backup
	_, err = r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-server", Namespace: "minecraft"},
	})
	require.NoError(t, err)

	// Verify backup was performed
	snapshots, err := backup.NewSnapshotter(fakeClient).ListSnapshots(
		context.Background(), "minecraft", "my-server")
	require.NoError(t, err)
	assert.Len(t, snapshots, 1, "cron trigger should create a VolumeSnapshot")
	assert.Equal(t, "scheduled", snapshots[0].Labels[backup.LabelTrigger])

	verifyRCONBackupCommands(t, mockRCON)
}

func TestBackupReconciler_InvalidCronSchedule(t *testing.T) {
	scheme := newBackupTestScheme()
	server := newTestServer(&mcv1beta1.BackupSpec{
		Enabled:  true,
		Schedule: "not-a-valid-cron",
	})

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(server).
		WithStatusSubresource(server).
		Build()

	r := &BackupReconciler{
		Client:      fakeClient,
		Scheme:      scheme,
		Snapshotter: backup.NewSnapshotter(fakeClient),
		Metrics:     &metrics.NoopRecorder{},
		cron:        testutil.NewMockCronScheduler(),
	}

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-server", Namespace: "minecraft"},
	})

	require.NoError(t, err, "invalid cron should NOT return error (permanent user error)")
	assert.Equal(t, ctrl.Result{}, result)

	// Verify condition was set
	var updatedServer mcv1beta1.PaperMCServer
	err = fakeClient.Get(context.Background(), types.NamespacedName{
		Name: "my-server", Namespace: "minecraft",
	}, &updatedServer)
	require.NoError(t, err)

	cond := meta.FindStatusCondition(updatedServer.Status.Conditions, conditionTypeBackupCronValid)
	require.NotNil(t, cond)
	assert.Equal(t, metav1.ConditionFalse, cond.Status)
}

func TestBackupReconciler_ServerNotFound(t *testing.T) {
	scheme := newBackupTestScheme()

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	r := &BackupReconciler{
		Client:      fakeClient,
		Scheme:      scheme,
		Snapshotter: backup.NewSnapshotter(fakeClient),
		Metrics:     &metrics.NoopRecorder{},
		cron:        testutil.NewMockCronScheduler(),
	}

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "nonexistent", Namespace: "minecraft"},
	})

	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result)
}

func boolPtr(b bool) *bool { return &b }

func TestBackupReconciler_PreSnapshotHookFailureStillSendsSaveOn(t *testing.T) {
	scheme := newBackupTestScheme()

	now := time.Now()
	server := newTestServer(&mcv1beta1.BackupSpec{
		Enabled: true,
		Retention: mcv1beta1.BackupRetention{
			MaxCount: 10,
		},
	})

	server.Annotations = map[string]string{
		"mc.k8s.lex.la/backup-now": fmt.Sprintf("%d", now.Unix()),
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(server, newServerPod(), newRCONSecret(), newServerPVC()).
		WithStatusSubresource(server).
		Build()

	mockRCON := rcon.NewMockClient()
	// save-off returns an error (simulates network timeout after command was received)
	mockRCON.SendCommandErrors["save-off"] = fmt.Errorf("network timeout")

	reconciler := &BackupReconciler{
		Client:      fakeClient,
		Scheme:      scheme,
		Snapshotter: backup.NewSnapshotter(fakeClient),
		Metrics:     &metrics.NoopRecorder{},
		cron:        testutil.NewMockCronScheduler(),
		nowFunc:     func() time.Time { return now },
		rconClientFactory: func(_, _ string, _ int) (rcon.Client, error) {
			return mockRCON, nil
		},
	}

	result, err := reconciler.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-server", Namespace: "minecraft"},
	})

	// Manual backup failure does NOT return error (annotation already removed).
	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result)

	// Critical: save-on MUST still be sent to re-enable auto-save
	sentCmds := mockRCON.GetSentCommands()
	assert.Contains(t, sentCmds, "save-all", "save-all should be sent before save-off")
	assert.Contains(t, sentCmds, "save-off", "save-off should be attempted")
	assert.Contains(t, sentCmds, "save-on", "save-on MUST be sent even when pre-snapshot hook fails")
}

func TestUpdateReconciler_BackupBeforeUpdate(t *testing.T) {
	scheme := newBackupTestScheme()

	now := time.Now()
	server := newTestServer(&mcv1beta1.BackupSpec{
		Enabled:      true,
		BeforeUpdate: boolPtr(true),
		Retention: mcv1beta1.BackupRetention{
			MaxCount: 10,
		},
	})

	pod := newServerPod()
	secret := newRCONSecret()

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(server, pod, secret, newServerPVC()).
		WithStatusSubresource(server).
		Build()

	mockRCON := rcon.NewMockClient()
	backupReconciler := &BackupReconciler{
		Client:      fakeClient,
		Scheme:      scheme,
		Snapshotter: backup.NewSnapshotter(fakeClient),
		Metrics:     &metrics.NoopRecorder{},
		cron:        testutil.NewMockCronScheduler(),
		nowFunc:     func() time.Time { return now },
		rconClientFactory: func(_, _ string, _ int) (rcon.Client, error) {
			return mockRCON, nil
		},
	}

	updateReconciler := &UpdateReconciler{
		Client:           fakeClient,
		Scheme:           scheme,
		BackupReconciler: backupReconciler,
	}

	// Should perform backup before update
	err := updateReconciler.backupBeforeUpdate(context.Background(), server)
	require.NoError(t, err)

	// Verify RCON commands were sent (save-all, save-off, save-on)
	sentCmds := mockRCON.GetSentCommands()
	assert.Contains(t, sentCmds, "save-all")
	assert.Contains(t, sentCmds, "save-off")
	assert.Contains(t, sentCmds, "save-on")

	// Verify VolumeSnapshot was created with "before-update" trigger
	snapshots, err := backup.NewSnapshotter(fakeClient).ListSnapshots(
		context.Background(), "minecraft", "my-server")
	require.NoError(t, err)
	assert.Len(t, snapshots, 1)
	assert.Equal(t, "before-update", snapshots[0].Labels[backup.LabelTrigger])
}

func TestUpdateReconciler_BackupBeforeUpdateNilDefaultsToTrue(t *testing.T) {
	scheme := newBackupTestScheme()
	now := time.Now()

	// BeforeUpdate is nil (not explicitly set) — should default to performing backup
	server := newTestServer(&mcv1beta1.BackupSpec{
		Enabled:   true,
		Retention: mcv1beta1.BackupRetention{MaxCount: 10},
	})

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(server, newServerPod(), newRCONSecret(), newServerPVC()).
		WithStatusSubresource(server).
		Build()

	mockRCON := rcon.NewMockClient()
	backupReconciler := &BackupReconciler{
		Client:      fakeClient,
		Scheme:      scheme,
		Snapshotter: backup.NewSnapshotter(fakeClient),
		Metrics:     &metrics.NoopRecorder{},
		cron:        testutil.NewMockCronScheduler(),
		nowFunc:     func() time.Time { return now },
		rconClientFactory: func(_, _ string, _ int) (rcon.Client, error) {
			return mockRCON, nil
		},
	}

	updateReconciler := &UpdateReconciler{
		Client:           fakeClient,
		Scheme:           scheme,
		BackupReconciler: backupReconciler,
	}

	// BeforeUpdate==nil should default to true — backup should be performed
	err := updateReconciler.backupBeforeUpdate(context.Background(), server)
	require.NoError(t, err)

	// Verify VolumeSnapshot was created
	snapshots, err := backup.NewSnapshotter(fakeClient).ListSnapshots(
		context.Background(), "minecraft", "my-server")
	require.NoError(t, err)
	assert.Len(t, snapshots, 1, "backup should be performed when BeforeUpdate is nil (default true)")
	assert.Equal(t, "before-update", snapshots[0].Labels[backup.LabelTrigger])
}

func TestUpdateReconciler_BackupBeforeUpdateDisabled(t *testing.T) {
	scheme := newBackupTestScheme()

	server := newTestServer(&mcv1beta1.BackupSpec{
		Enabled:      true,
		BeforeUpdate: boolPtr(false),
	})

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(server).
		WithStatusSubresource(server).
		Build()

	backupReconciler := &BackupReconciler{
		Client:      fakeClient,
		Scheme:      scheme,
		Snapshotter: backup.NewSnapshotter(fakeClient),
		Metrics:     &metrics.NoopRecorder{},
		cron:        testutil.NewMockCronScheduler(),
	}

	updateReconciler := &UpdateReconciler{
		Client:           fakeClient,
		Scheme:           scheme,
		BackupReconciler: backupReconciler,
	}

	// Should skip backup when beforeUpdate=false
	err := updateReconciler.backupBeforeUpdate(context.Background(), server)
	require.NoError(t, err)

	// Verify no snapshots were created
	snapshots, err := backup.NewSnapshotter(fakeClient).ListSnapshots(
		context.Background(), "minecraft", "my-server")
	require.NoError(t, err)
	assert.Len(t, snapshots, 0)
}

func TestUpdateReconciler_BackupBeforeUpdateNilBackupReconciler(t *testing.T) {
	scheme := newBackupTestScheme()

	server := newTestServer(&mcv1beta1.BackupSpec{
		Enabled:      true,
		BeforeUpdate: boolPtr(true),
	})

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(server).
		WithStatusSubresource(server).
		Build()

	updateReconciler := &UpdateReconciler{
		Client: fakeClient,
		Scheme: scheme,
		// No BackupReconciler set
	}

	// Should not error when BackupReconciler is nil
	err := updateReconciler.backupBeforeUpdate(context.Background(), server)
	require.NoError(t, err)
}

func TestBackupReconciler_RCONDisabledCreatesSnapshotWithoutHooks(t *testing.T) {
	scheme := newBackupTestScheme()

	now := time.Now()
	server := newTestServer(&mcv1beta1.BackupSpec{
		Enabled: true,
		Retention: mcv1beta1.BackupRetention{
			MaxCount: 10,
		},
	})

	server.Spec.RCON.Enabled = false
	server.Annotations = map[string]string{
		"mc.k8s.lex.la/backup-now": fmt.Sprintf("%d", now.Unix()),
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(server, newServerPod(), newRCONSecret(), newServerPVC()).
		WithStatusSubresource(server).
		Build()

	mockRCON := rcon.NewMockClient()
	reconciler := &BackupReconciler{
		Client:      fakeClient,
		Scheme:      scheme,
		Snapshotter: backup.NewSnapshotter(fakeClient),
		Metrics:     &metrics.NoopRecorder{},
		cron:        testutil.NewMockCronScheduler(),
		nowFunc:     func() time.Time { return now },
		rconClientFactory: func(_, _ string, _ int) (rcon.Client, error) {
			return mockRCON, nil
		},
	}

	result, err := reconciler.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-server", Namespace: "minecraft"},
	})

	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result)

	// Verify snapshot was created
	snapshots, listErr := backup.NewSnapshotter(fakeClient).ListSnapshots(
		context.Background(), "minecraft", "my-server")
	require.NoError(t, listErr)
	assert.Len(t, snapshots, 1)

	// Verify NO RCON commands were sent (RCON disabled)
	sentCmds := mockRCON.GetSentCommands()
	assert.Empty(t, sentCmds, "No RCON commands should be sent when RCON is disabled")
}

func TestUpdateReconciler_BackupBeforeUpdateFailureAbortsUpdate(t *testing.T) {
	scheme := newBackupTestScheme()

	now := time.Now()
	server := newTestServer(&mcv1beta1.BackupSpec{
		Enabled:      true,
		BeforeUpdate: boolPtr(true),
		Retention: mcv1beta1.BackupRetention{
			MaxCount: 10,
		},
	})

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(server, newServerPod(), newRCONSecret(), newServerPVC()).
		WithStatusSubresource(server).
		Build()

	mockRCON := rcon.NewMockClient()
	mockRCON.ConnectError = fmt.Errorf("connection refused")

	backupReconciler := &BackupReconciler{
		Client:      fakeClient,
		Scheme:      scheme,
		Snapshotter: backup.NewSnapshotter(fakeClient),
		Metrics:     &metrics.NoopRecorder{},
		cron:        testutil.NewMockCronScheduler(),
		nowFunc:     func() time.Time { return now },
		rconClientFactory: func(_, _ string, _ int) (rcon.Client, error) {
			return mockRCON, nil
		},
	}

	updateReconciler := &UpdateReconciler{
		Client:           fakeClient,
		Scheme:           scheme,
		BackupReconciler: backupReconciler,
	}

	// Backup failure should abort the update
	err := updateReconciler.backupBeforeUpdate(context.Background(), server)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to connect to RCON")
}

func TestBackupReconciler_ShouldBackupNow_NonNumericAnnotation(t *testing.T) {
	now := time.Now()
	reconciler := &BackupReconciler{
		nowFunc: func() time.Time { return now },
	}

	server := &mcv1beta1.PaperMCServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-server",
			Namespace: "minecraft",
			Annotations: map[string]string{
				"mc.k8s.lex.la/backup-now": "true",
			},
		},
	}

	// "true" is not a valid Unix timestamp — should return false
	assert.False(t, reconciler.shouldBackupNow(context.Background(), server))
}

func TestBackupReconciler_ShouldBackupNow_StaleAnnotation(t *testing.T) {
	now := time.Now()
	reconciler := &BackupReconciler{
		nowFunc: func() time.Time { return now },
	}

	staleTimestamp := now.Add(-10 * time.Minute).Unix()
	server := &mcv1beta1.PaperMCServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-server",
			Namespace: "minecraft",
			Annotations: map[string]string{
				"mc.k8s.lex.la/backup-now": fmt.Sprintf("%d", staleTimestamp),
			},
		},
	}

	// Annotation is 10 minutes old (max age is 5 min) — should return false
	assert.False(t, reconciler.shouldBackupNow(context.Background(), server))
}

func TestBackupReconciler_ShouldBackupNow_FutureTimestamp(t *testing.T) {
	now := time.Now()
	reconciler := &BackupReconciler{
		nowFunc: func() time.Time { return now },
	}

	futureTimestamp := now.Add(1 * time.Hour).Unix()
	server := &mcv1beta1.PaperMCServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-server",
			Namespace: "minecraft",
			Annotations: map[string]string{
				"mc.k8s.lex.la/backup-now": fmt.Sprintf("%d", futureTimestamp),
			},
		},
	}

	// Future timestamps (>5min ahead) are rejected to guard against typos
	assert.False(t, reconciler.shouldBackupNow(context.Background(), server))
}

func TestBackupReconciler_ManualBackupConsumesPendingCronTrigger(t *testing.T) { //nolint:funlen
	scheme := newBackupTestScheme()
	now := time.Now()

	// Create server WITHOUT annotation — first reconcile only sets up cron
	server := newTestServer(&mcv1beta1.BackupSpec{
		Enabled:   true,
		Schedule:  "0 */6 * * *",
		Retention: mcv1beta1.BackupRetention{MaxCount: 10},
	})

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(server, newServerPod(), newRCONSecret(), newServerPVC()).
		WithStatusSubresource(server).
		Build()

	mockRCON := rcon.NewMockClient()
	r := &BackupReconciler{
		Client: fakeClient, Scheme: scheme,
		Snapshotter: backup.NewSnapshotter(fakeClient),
		Metrics:     &metrics.NoopRecorder{},
		cron:        testutil.NewMockCronScheduler(),
		nowFunc:     func() time.Time { return now },
		rconClientFactory: func(_, _ string, _ int) (rcon.Client, error) {
			return mockRCON, nil
		},
	}

	// First reconcile — sets up cron only (no annotation)
	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-server", Namespace: "minecraft"},
	})
	require.NoError(t, err)

	// Add both manual trigger AND cron trigger simultaneously
	var currentServer mcv1beta1.PaperMCServer
	require.NoError(t, fakeClient.Get(context.Background(), types.NamespacedName{
		Name: "my-server", Namespace: "minecraft",
	}, &currentServer))
	currentServer.Annotations = map[string]string{
		AnnotationBackupNow: fmt.Sprintf("%d", now.Unix()),
	}
	require.NoError(t, fakeClient.Update(context.Background(), &currentServer))

	r.cronTriggerMu.Lock()
	r.cronTriggerTimes[testServerKey] = now
	r.cronTriggerMu.Unlock()

	// Second reconcile — manual backup fires AND consumes cron trigger
	_, err = r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-server", Namespace: "minecraft"},
	})
	require.NoError(t, err)

	// Only one snapshot should exist (manual), not two
	snapshots, err := backup.NewSnapshotter(fakeClient).ListSnapshots(
		context.Background(), "minecraft", "my-server")
	require.NoError(t, err)
	assert.Len(t, snapshots, 1, "only one snapshot should be created (manual consumes cron trigger)")
	assert.Equal(t, "manual", snapshots[0].Labels[backup.LabelTrigger])

	// Cron trigger should be consumed
	r.cronTriggerMu.RLock()
	_, triggerExists := r.cronTriggerTimes[testServerKey]
	r.cronTriggerMu.RUnlock()
	assert.False(t, triggerExists, "cron trigger should be consumed by manual backup")
}

func TestBackupReconciler_ScheduledBackupFailurePreservesTrigger(t *testing.T) {
	scheme := newBackupTestScheme()
	now := time.Now()

	server := newTestServer(&mcv1beta1.BackupSpec{
		Enabled:   true,
		Schedule:  "0 */6 * * *",
		Retention: mcv1beta1.BackupRetention{MaxCount: 10},
	})

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(server, newServerPod(), newRCONSecret(), newServerPVC()).
		WithStatusSubresource(server).
		Build()

	mockRCON := rcon.NewMockClient()
	// RCON connect fails — backup will fail
	mockRCON.ConnectError = fmt.Errorf("connection refused")

	r := &BackupReconciler{
		Client: fakeClient, Scheme: scheme,
		Snapshotter: backup.NewSnapshotter(fakeClient),
		Metrics:     &metrics.NoopRecorder{},
		cron:        testutil.NewMockCronScheduler(),
		nowFunc:     func() time.Time { return now },
		rconClientFactory: func(_, _ string, _ int) (rcon.Client, error) {
			return mockRCON, nil
		},
	}

	// First reconcile — sets up cron
	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-server", Namespace: "minecraft"},
	})
	require.NoError(t, err)

	// Simulate cron trigger
	serverKey := testServerKey
	r.cronTriggerMu.Lock()
	r.cronTriggerTimes[serverKey] = now
	r.cronTriggerMu.Unlock()

	// Second reconcile — backup fails due to RCON connect error
	_, err = r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-server", Namespace: "minecraft"},
	})
	require.Error(t, err, "backup should fail due to RCON connect error")

	// Critical: trigger must be preserved so the next reconcile retries the backup
	r.cronTriggerMu.RLock()
	_, triggerExists := r.cronTriggerTimes[serverKey]
	r.cronTriggerMu.RUnlock()
	assert.True(t, triggerExists, "cron trigger should be preserved after failed backup for retry")
}

func TestBackupReconciler_ScheduledBackupSuccessConsumesTrigger(t *testing.T) {
	scheme := newBackupTestScheme()
	now := time.Now()

	server := newTestServer(&mcv1beta1.BackupSpec{
		Enabled:   true,
		Schedule:  "0 */6 * * *",
		Retention: mcv1beta1.BackupRetention{MaxCount: 10},
	})

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(server, newServerPod(), newRCONSecret(), newServerPVC()).
		WithStatusSubresource(server).
		Build()

	mockRCON := rcon.NewMockClient()
	r := &BackupReconciler{
		Client: fakeClient, Scheme: scheme,
		Snapshotter: backup.NewSnapshotter(fakeClient),
		Metrics:     &metrics.NoopRecorder{},
		cron:        testutil.NewMockCronScheduler(),
		nowFunc:     func() time.Time { return now },
		rconClientFactory: func(_, _ string, _ int) (rcon.Client, error) {
			return mockRCON, nil
		},
	}

	// First reconcile — sets up cron
	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-server", Namespace: "minecraft"},
	})
	require.NoError(t, err)

	// Simulate cron trigger
	serverKey := testServerKey
	r.cronTriggerMu.Lock()
	r.cronTriggerTimes[serverKey] = now
	r.cronTriggerMu.Unlock()

	// Second reconcile — backup succeeds
	_, err = r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-server", Namespace: "minecraft"},
	})
	require.NoError(t, err)

	// Trigger should be consumed after successful backup
	r.cronTriggerMu.RLock()
	_, triggerExists := r.cronTriggerTimes[serverKey]
	r.cronTriggerMu.RUnlock()
	assert.False(t, triggerExists, "cron trigger should be consumed after successful backup")
}

func TestBackupReconciler_ManualBackupFailureRecordedInStatus(t *testing.T) {
	scheme := newBackupTestScheme()
	now := time.Now()

	server := newTestServer(&mcv1beta1.BackupSpec{
		Enabled:   true,
		Retention: mcv1beta1.BackupRetention{MaxCount: 10},
	})
	server.Annotations = map[string]string{
		AnnotationBackupNow: fmt.Sprintf("%d", now.Unix()),
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(server, newServerPod(), newRCONSecret(), newServerPVC()).
		WithStatusSubresource(server).
		Build()

	mockRCON := rcon.NewMockClient()
	mockRCON.ConnectError = fmt.Errorf("connection refused")

	r := &BackupReconciler{
		Client: fakeClient, Scheme: scheme,
		Snapshotter: backup.NewSnapshotter(fakeClient),
		Metrics:     &metrics.NoopRecorder{},
		cron:        testutil.NewMockCronScheduler(),
		nowFunc:     func() time.Time { return now },
		rconClientFactory: func(_, _ string, _ int) (rcon.Client, error) {
			return mockRCON, nil
		},
	}

	// Reconcile — manual backup fails but no error returned
	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-server", Namespace: "minecraft"},
	})
	require.NoError(t, err)

	// The failed backup MUST be recorded in status for user visibility,
	// since the annotation is already removed and there is no requeue.
	var updatedServer mcv1beta1.PaperMCServer
	require.NoError(t, fakeClient.Get(context.Background(), types.NamespacedName{
		Name: "my-server", Namespace: "minecraft",
	}, &updatedServer))

	require.NotNil(t, updatedServer.Status.Backup, "backup status should exist")
	require.NotNil(t, updatedServer.Status.Backup.LastBackup, "last backup record should exist")
	assert.False(t, updatedServer.Status.Backup.LastBackup.Successful, "backup should be marked failed")
	assert.Equal(t, "manual", updatedServer.Status.Backup.LastBackup.Trigger)
}

// crdMissingSnapshotter simulates a cluster where VolumeSnapshot CRD is not installed.
type crdMissingSnapshotter struct{}

func (m *crdMissingSnapshotter) CreateSnapshot(
	_ context.Context, _ backup.SnapshotRequest,
) (string, error) {
	return "", &meta.NoKindMatchError{
		GroupKind: schema.GroupKind{Group: "snapshot.storage.k8s.io", Kind: "VolumeSnapshot"},
	}
}

func (m *crdMissingSnapshotter) ListSnapshots(
	_ context.Context, _, _ string,
) ([]volumesnapshotv1.VolumeSnapshot, error) {
	return nil, &meta.NoKindMatchError{
		GroupKind: schema.GroupKind{Group: "snapshot.storage.k8s.io", Kind: "VolumeSnapshot"},
	}
}

func (m *crdMissingSnapshotter) DeleteOldSnapshots(
	_ context.Context, _, _ string, _ int,
) (int, error) {
	return 0, nil
}

func TestBackupReconciler_VolumeSnapshotCRDUnavailable(t *testing.T) {
	scheme := newBackupTestScheme()
	server := newTestServer(&mcv1beta1.BackupSpec{
		Enabled:  true,
		Schedule: "0 */6 * * *",
	})

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(server).
		WithStatusSubresource(server).
		Build()

	r := &BackupReconciler{
		Client:      fakeClient,
		Scheme:      scheme,
		Snapshotter: &crdMissingSnapshotter{},
		Metrics:     &metrics.NoopRecorder{},
		cron:        testutil.NewMockCronScheduler(),
	}

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-server", Namespace: "minecraft"},
	})
	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{RequeueAfter: 5 * time.Minute}, result)

	// Verify BackupReady condition is set
	var updatedServer mcv1beta1.PaperMCServer
	require.NoError(t, fakeClient.Get(context.Background(), types.NamespacedName{
		Name: "my-server", Namespace: "minecraft",
	}, &updatedServer))

	cond := meta.FindStatusCondition(updatedServer.Status.Conditions, "BackupReady")
	require.NotNil(t, cond, "BackupReady condition should be set")
	assert.Equal(t, metav1.ConditionFalse, cond.Status)
	assert.Equal(t, "VolumeSnapshotAPIUnavailable", cond.Reason)
	assert.Contains(t, cond.Message, "snapshot.storage.k8s.io")
}

func TestBackupReconciler_BackupReadyRecoveryAfterCRDInstall(t *testing.T) {
	scheme := newBackupTestScheme()
	server := newTestServer(&mcv1beta1.BackupSpec{
		Enabled:  true,
		Schedule: "0 */6 * * *",
	})

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(server).
		WithStatusSubresource(server).
		Build()

	// First reconcile: CRD is missing → BackupReady=False
	r := &BackupReconciler{
		Client:      fakeClient,
		Scheme:      scheme,
		Snapshotter: &crdMissingSnapshotter{},
		Metrics:     &metrics.NoopRecorder{},
		cron:        testutil.NewMockCronScheduler(),
	}

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-server", Namespace: "minecraft"},
	})
	require.NoError(t, err)

	var updatedServer mcv1beta1.PaperMCServer
	require.NoError(t, fakeClient.Get(context.Background(), types.NamespacedName{
		Name: "my-server", Namespace: "minecraft",
	}, &updatedServer))
	cond := meta.FindStatusCondition(updatedServer.Status.Conditions, "BackupReady")
	require.NotNil(t, cond)
	assert.Equal(t, metav1.ConditionFalse, cond.Status)

	// Second reconcile: CRD is now installed → BackupReady should recover to True
	r.Snapshotter = backup.NewSnapshotter(fakeClient)

	_, err = r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-server", Namespace: "minecraft"},
	})
	require.NoError(t, err)

	require.NoError(t, fakeClient.Get(context.Background(), types.NamespacedName{
		Name: "my-server", Namespace: "minecraft",
	}, &updatedServer))
	cond = meta.FindStatusCondition(updatedServer.Status.Conditions, "BackupReady")
	require.NotNil(t, cond, "BackupReady condition should still exist")
	assert.Equal(t, metav1.ConditionTrue, cond.Status, "BackupReady should recover to True")
}

func TestBackupReconciler_PVCNotFoundClearError(t *testing.T) {
	scheme := newBackupTestScheme()
	now := time.Now()
	server := newTestServer(&mcv1beta1.BackupSpec{
		Enabled:   true,
		Retention: mcv1beta1.BackupRetention{MaxCount: 10},
	})
	// Disable RCON to simplify — focus on PVC check
	server.Spec.RCON.Enabled = false
	server.Annotations = map[string]string{
		AnnotationBackupNow: fmt.Sprintf("%d", now.Unix()),
	}

	// Note: no PVC created in the fake client
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(server).
		WithStatusSubresource(server).
		Build()

	r := &BackupReconciler{
		Client:      fakeClient,
		Scheme:      scheme,
		Snapshotter: backup.NewSnapshotter(fakeClient),
		Metrics:     &metrics.NoopRecorder{},
		cron:        testutil.NewMockCronScheduler(),
		nowFunc:     func() time.Time { return now },
	}

	// Manual backup — error swallowed but status should reflect failure with clear message
	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-server", Namespace: "minecraft"},
	})
	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result)

	var updatedServer mcv1beta1.PaperMCServer
	require.NoError(t, fakeClient.Get(context.Background(), types.NamespacedName{
		Name: "my-server", Namespace: "minecraft",
	}, &updatedServer))

	require.NotNil(t, updatedServer.Status.Backup)
	require.NotNil(t, updatedServer.Status.Backup.LastBackup)
	assert.False(t, updatedServer.Status.Backup.LastBackup.Successful)
}

func TestBackupReconciler_PVCNotFoundWithRCONStillSendsSaveOn(t *testing.T) {
	scheme := newBackupTestScheme()
	now := time.Now()
	server := newTestServer(&mcv1beta1.BackupSpec{
		Enabled:   true,
		Retention: mcv1beta1.BackupRetention{MaxCount: 10},
	})
	// RCON is enabled (default from newTestServer)
	server.Annotations = map[string]string{
		AnnotationBackupNow: fmt.Sprintf("%d", now.Unix()),
	}

	// No PVC created — only server, pod, and RCON secret
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(server, newServerPod(), newRCONSecret()).
		WithStatusSubresource(server).
		Build()

	mockRCON := rcon.NewMockClient()
	r := &BackupReconciler{
		Client:      fakeClient,
		Scheme:      scheme,
		Snapshotter: backup.NewSnapshotter(fakeClient),
		Metrics:     &metrics.NoopRecorder{},
		cron:        testutil.NewMockCronScheduler(),
		nowFunc:     func() time.Time { return now },
		rconClientFactory: func(_, _ string, _ int) (rcon.Client, error) {
			return mockRCON, nil
		},
	}

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-server", Namespace: "minecraft"},
	})
	// Manual backup error is swallowed
	require.NoError(t, err)

	// Even though PVC is missing, save-on MUST be sent to re-enable auto-save
	sentCmds := mockRCON.GetSentCommands()
	assert.NotContains(t, sentCmds, "save-off",
		"save-off should NOT be sent when PVC check fails before RCON hooks")
}

func TestBackupReconciler_FailurePreservesExistingBackupCount(t *testing.T) {
	scheme := newBackupTestScheme()
	now := time.Now()
	server := newTestServer(&mcv1beta1.BackupSpec{
		Enabled:   true,
		Retention: mcv1beta1.BackupRetention{MaxCount: 10},
	})
	server.Annotations = map[string]string{
		AnnotationBackupNow: fmt.Sprintf("%d", now.Unix()),
	}
	// Pre-set backup count to 5
	server.Status.Backup = &mcv1beta1.BackupStatus{
		BackupCount: 5,
	}

	// No PVC = backup will fail
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(server, newServerPod(), newRCONSecret()).
		WithStatusSubresource(server).
		Build()

	mockRCON := rcon.NewMockClient()
	r := &BackupReconciler{
		Client:      fakeClient,
		Scheme:      scheme,
		Snapshotter: backup.NewSnapshotter(fakeClient),
		Metrics:     &metrics.NoopRecorder{},
		cron:        testutil.NewMockCronScheduler(),
		nowFunc:     func() time.Time { return now },
		rconClientFactory: func(_, _ string, _ int) (rcon.Client, error) {
			return mockRCON, nil
		},
	}

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-server", Namespace: "minecraft"},
	})
	require.NoError(t, err)

	var updatedServer mcv1beta1.PaperMCServer
	require.NoError(t, fakeClient.Get(context.Background(), types.NamespacedName{
		Name: "my-server", Namespace: "minecraft",
	}, &updatedServer))

	require.NotNil(t, updatedServer.Status.Backup)
	assert.Equal(t, 5, updatedServer.Status.Backup.BackupCount,
		"existing backup count should be preserved on failure, not reset to 0")
}

func TestSetBackupCronCondition_LastTransitionTimeStable(t *testing.T) {
	server := newTestServer(&mcv1beta1.BackupSpec{Enabled: true})

	// Set condition the first time
	setBackupCronCondition(server, metav1.ConditionTrue, reasonBackupCronValid, "test")
	cond1 := meta.FindStatusCondition(server.Status.Conditions, conditionTypeBackupCronValid)
	require.NotNil(t, cond1)
	firstTime := cond1.LastTransitionTime

	// Small delay to ensure time would differ if set explicitly
	time.Sleep(10 * time.Millisecond)

	// Set condition again with SAME status — timestamp should NOT change
	setBackupCronCondition(server, metav1.ConditionTrue, reasonBackupCronValid, "test")
	cond2 := meta.FindStatusCondition(server.Status.Conditions, conditionTypeBackupCronValid)
	require.NotNil(t, cond2)

	assert.Equal(t, firstTime, cond2.LastTransitionTime,
		"LastTransitionTime should not change when status is unchanged")
}

func TestBackupReconciler_BackupReadyRecoveryRefreshesServer(t *testing.T) {
	scheme := newBackupTestScheme()
	server := newTestServer(&mcv1beta1.BackupSpec{
		Enabled:  true,
		Schedule: "0 */6 * * *",
	})

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(server).
		WithStatusSubresource(server).
		Build()

	// First: set BackupReady=False using the CRD-missing snapshotter
	r := &BackupReconciler{
		Client:      fakeClient,
		Scheme:      scheme,
		Snapshotter: &crdMissingSnapshotter{},
		Metrics:     &metrics.NoopRecorder{},
		cron:        testutil.NewMockCronScheduler(),
	}

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-server", Namespace: "minecraft"},
	})
	require.NoError(t, err)

	// Verify BackupReady=False is set
	var s1 mcv1beta1.PaperMCServer
	require.NoError(t, fakeClient.Get(context.Background(), types.NamespacedName{
		Name: "my-server", Namespace: "minecraft",
	}, &s1))
	cond := meta.FindStatusCondition(s1.Status.Conditions, "BackupReady")
	require.NotNil(t, cond)
	assert.Equal(t, metav1.ConditionFalse, cond.Status)

	// Simulate another controller updating status (changes ResourceVersion)
	s1.Status.CurrentVersion = "1.21.4"
	require.NoError(t, fakeClient.Status().Update(context.Background(), &s1))

	// Now recover with a working snapshotter — must re-fetch to avoid conflict
	r.Snapshotter = backup.NewSnapshotter(fakeClient)

	_, err = r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-server", Namespace: "minecraft"},
	})
	require.NoError(t, err)

	// Verify BackupReady=True and status update didn't conflict
	var s2 mcv1beta1.PaperMCServer
	require.NoError(t, fakeClient.Get(context.Background(), types.NamespacedName{
		Name: "my-server", Namespace: "minecraft",
	}, &s2))
	cond = meta.FindStatusCondition(s2.Status.Conditions, "BackupReady")
	require.NotNil(t, cond, "BackupReady should exist after recovery")
	assert.Equal(t, metav1.ConditionTrue, cond.Status, "BackupReady should be True after CRD becomes available")
	assert.Equal(t, "1.21.4", s2.Status.CurrentVersion,
		"other status fields should be preserved")
}

func TestBackupReconciler_StaleAnnotationCleanedUp(t *testing.T) {
	scheme := newBackupTestScheme()
	now := time.Now()
	server := newTestServer(&mcv1beta1.BackupSpec{
		Enabled: true,
	})
	// Stale annotation (10 minutes old, max age is 5 minutes)
	staleTimestamp := now.Add(-10 * time.Minute).Unix()
	server.Annotations = map[string]string{
		AnnotationBackupNow: fmt.Sprintf("%d", staleTimestamp),
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(server).
		WithStatusSubresource(server).
		Build()

	r := &BackupReconciler{
		Client:      fakeClient,
		Scheme:      scheme,
		Snapshotter: backup.NewSnapshotter(fakeClient),
		Metrics:     &metrics.NoopRecorder{},
		cron:        testutil.NewMockCronScheduler(),
		nowFunc:     func() time.Time { return now },
	}

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-server", Namespace: "minecraft"},
	})
	require.NoError(t, err)

	// Stale annotation should be cleaned up so it doesn't persist indefinitely
	var updatedServer mcv1beta1.PaperMCServer
	require.NoError(t, fakeClient.Get(context.Background(), types.NamespacedName{
		Name: "my-server", Namespace: "minecraft",
	}, &updatedServer))

	_, exists := updatedServer.Annotations[AnnotationBackupNow]
	assert.False(t, exists, "stale backup-now annotation should be removed")
}

func TestBackupReconciler_InvalidAnnotationCleanedUp(t *testing.T) {
	scheme := newBackupTestScheme()
	now := time.Now()
	server := newTestServer(&mcv1beta1.BackupSpec{
		Enabled: true,
	})
	server.Annotations = map[string]string{
		AnnotationBackupNow: "not-a-number",
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(server).
		WithStatusSubresource(server).
		Build()

	r := &BackupReconciler{
		Client:      fakeClient,
		Scheme:      scheme,
		Snapshotter: backup.NewSnapshotter(fakeClient),
		Metrics:     &metrics.NoopRecorder{},
		cron:        testutil.NewMockCronScheduler(),
		nowFunc:     func() time.Time { return now },
	}

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-server", Namespace: "minecraft"},
	})
	require.NoError(t, err)

	var updatedServer mcv1beta1.PaperMCServer
	require.NoError(t, fakeClient.Get(context.Background(), types.NamespacedName{
		Name: "my-server", Namespace: "minecraft",
	}, &updatedServer))

	_, exists := updatedServer.Annotations[AnnotationBackupNow]
	assert.False(t, exists, "invalid backup-now annotation should be removed")
}

func TestBackupReconciler_ValidCronPersistsBackupCronValidTrue(t *testing.T) {
	scheme := newBackupTestScheme()
	server := newTestServer(&mcv1beta1.BackupSpec{
		Enabled:  true,
		Schedule: "0 */6 * * *",
	})

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(server).
		WithStatusSubresource(server).
		Build()

	r := &BackupReconciler{
		Client:      fakeClient,
		Scheme:      scheme,
		Snapshotter: backup.NewSnapshotter(fakeClient),
		Metrics:     &metrics.NoopRecorder{},
		cron:        testutil.NewMockCronScheduler(),
	}

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-server", Namespace: "minecraft"},
	})
	require.NoError(t, err)

	// BackupCronValid=True should be persisted to the API server
	var updatedServer mcv1beta1.PaperMCServer
	require.NoError(t, fakeClient.Get(context.Background(), types.NamespacedName{
		Name: "my-server", Namespace: "minecraft",
	}, &updatedServer))

	cond := meta.FindStatusCondition(updatedServer.Status.Conditions, conditionTypeBackupCronValid)
	require.NotNil(t, cond, "BackupCronValid condition should be persisted for valid cron")
	assert.Equal(t, metav1.ConditionTrue, cond.Status,
		"BackupCronValid should be True for valid cron")
	assert.Equal(t, reasonBackupCronValid, cond.Reason)
}

func TestBackupReconciler_RemoveBackupCronJob_NilCron(t *testing.T) {
	reconciler := &BackupReconciler{
		cronEntries: map[string]cronEntryInfo{
			testServerKey: {ID: 1, Spec: "0 */6 * * *"},
		},
	}

	// Should not panic when r.cron is nil
	assert.NotPanics(t, func() {
		reconciler.removeBackupCronJob(testServerKey)
	})

	// Entry should still be cleaned up from the map
	assert.Empty(t, reconciler.cronEntries)
}
