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
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newBackupTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(s)
	_ = mcv1beta1.AddToScheme(s)
	_ = volumesnapshotv1.AddToScheme(s)

	return s
}

func newTestServer(name, namespace string, backupSpec *mcv1beta1.BackupSpec) *mcv1beta1.PaperMCServer {
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

func newRCONSecret(name, namespace string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name + "-rcon",
			Namespace: namespace,
		},
		Data: map[string][]byte{
			"password": []byte("test-password"),
		},
	}
}

func newServerPod(name, namespace string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name + "-0",
			Namespace: namespace,
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
	server := newTestServer("my-server", "minecraft", nil)

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
	server := newTestServer("my-server", "minecraft", &mcv1beta1.BackupSpec{
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

func TestBackupReconciler_ManualBackupTrigger(t *testing.T) {
	scheme := newBackupTestScheme()

	now := time.Now()
	server := newTestServer("my-server", "minecraft", &mcv1beta1.BackupSpec{
		Enabled: true,
		Retention: mcv1beta1.BackupRetention{
			MaxCount: 10,
		},
	})
	server.Annotations = map[string]string{
		AnnotationBackupNow: fmt.Sprintf("%d", now.Unix()),
	}

	pod := newServerPod("my-server", "minecraft")
	secret := newRCONSecret("my-server", "minecraft")

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(server, pod, secret).
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

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-server", Namespace: "minecraft"},
	})

	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result)

	// Verify RCON commands were sent
	sentCmds := mockRCON.GetSentCommands()
	assert.Contains(t, sentCmds, "save-all")
	assert.Contains(t, sentCmds, "save-off")
	assert.Contains(t, sentCmds, "save-on")

	// Verify VolumeSnapshot was created
	snapshots, err := backup.NewSnapshotter(fakeClient).ListSnapshots(
		context.Background(), "minecraft", "my-server")
	require.NoError(t, err)
	assert.Len(t, snapshots, 1)
	assert.Equal(t, "manual", snapshots[0].Labels[backup.LabelTrigger])

	// Verify annotation was removed
	var updatedServer mcv1beta1.PaperMCServer
	err = fakeClient.Get(context.Background(), types.NamespacedName{
		Name: "my-server", Namespace: "minecraft",
	}, &updatedServer)
	require.NoError(t, err)
	_, exists := updatedServer.Annotations[AnnotationBackupNow]
	assert.False(t, exists, "backup-now annotation should be removed after backup")

	// Verify status was updated
	assert.NotNil(t, updatedServer.Status.Backup)
	assert.NotNil(t, updatedServer.Status.Backup.LastBackup)
	assert.True(t, updatedServer.Status.Backup.LastBackup.Successful)
	assert.Equal(t, "manual", updatedServer.Status.Backup.LastBackup.Trigger)
}

func TestBackupReconciler_RetentionCleanup(t *testing.T) {
	scheme := newBackupTestScheme()

	now := time.Now()
	server := newTestServer("my-server", "minecraft", &mcv1beta1.BackupSpec{
		Enabled: true,
		Retention: mcv1beta1.BackupRetention{
			MaxCount: 2,
		},
	})
	server.Annotations = map[string]string{
		AnnotationBackupNow: fmt.Sprintf("%d", now.Unix()),
	}

	pod := newServerPod("my-server", "minecraft")
	secret := newRCONSecret("my-server", "minecraft")

	// Pre-existing snapshots
	existingSnapshots := []volumesnapshotv1.VolumeSnapshot{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "my-server-backup-old1",
				Namespace:         "minecraft",
				CreationTimestamp: metav1.NewTime(now.Add(-3 * time.Hour)),
				Labels: map[string]string{
					backup.LabelServerName: "my-server",
					backup.LabelManagedBy:  "minecraft-operator",
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "my-server-backup-old2",
				Namespace:         "minecraft",
				CreationTimestamp: metav1.NewTime(now.Add(-2 * time.Hour)),
				Labels: map[string]string{
					backup.LabelServerName: "my-server",
					backup.LabelManagedBy:  "minecraft-operator",
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(server, pod, secret, &existingSnapshots[0], &existingSnapshots[1]).
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

	// After creating 1 new snapshot (total 3), with maxCount=2, oldest should be deleted
	snapshots, err := backup.NewSnapshotter(fakeClient).ListSnapshots(
		context.Background(), "minecraft", "my-server")
	require.NoError(t, err)
	assert.Len(t, snapshots, 2, "should retain only maxCount snapshots")

	// The oldest snapshot should have been deleted
	for _, s := range snapshots {
		assert.NotEqual(t, "my-server-backup-old1", s.Name,
			"oldest snapshot should have been deleted")
	}
}

func TestBackupReconciler_InvalidCronSchedule(t *testing.T) {
	scheme := newBackupTestScheme()
	server := newTestServer("my-server", "minecraft", &mcv1beta1.BackupSpec{
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

func TestUpdateReconciler_BackupBeforeUpdate(t *testing.T) {
	scheme := newBackupTestScheme()

	now := time.Now()
	server := newTestServer("my-server", "minecraft", &mcv1beta1.BackupSpec{
		Enabled:      true,
		BeforeUpdate: true,
		Retention: mcv1beta1.BackupRetention{
			MaxCount: 10,
		},
	})

	pod := newServerPod("my-server", "minecraft")
	secret := newRCONSecret("my-server", "minecraft")

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(server, pod, secret).
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

func TestUpdateReconciler_BackupBeforeUpdateDisabled(t *testing.T) {
	scheme := newBackupTestScheme()

	server := newTestServer("my-server", "minecraft", &mcv1beta1.BackupSpec{
		Enabled:      true,
		BeforeUpdate: false,
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

	server := newTestServer("my-server", "minecraft", &mcv1beta1.BackupSpec{
		Enabled:      true,
		BeforeUpdate: true,
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
