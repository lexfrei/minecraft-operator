package backup_test

import (
	"context"
	"testing"
	"time"

	volumesnapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	"github.com/lexfrei/minecraft-operator/pkg/backup"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = volumesnapshotv1.AddToScheme(s)

	return s
}

func TestCreateSnapshot(t *testing.T) { //nolint:funlen
	t.Run("creates VolumeSnapshot with correct fields", func(t *testing.T) {
		scheme := newTestScheme()
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
		snapshotter := backup.NewSnapshotter(fakeClient)

		ctx := context.Background()
		name, err := snapshotter.CreateSnapshot(ctx, backup.SnapshotRequest{
			Namespace:               "minecraft",
			PVCName:                 "data-my-server-0",
			ServerName:              "my-server",
			VolumeSnapshotClassName: "csi-snapclass",
			Trigger:                 "scheduled",
		})

		require.NoError(t, err)
		assert.NotEmpty(t, name)
		assert.Contains(t, name, "my-server-backup-")

		// Verify the snapshot was created
		snapshots, err := snapshotter.ListSnapshots(ctx, "minecraft", "my-server")
		require.NoError(t, err)
		require.Len(t, snapshots, 1)
		assert.Equal(t, name, snapshots[0].Name)
		assert.Equal(t, "data-my-server-0", *snapshots[0].Spec.Source.PersistentVolumeClaimName)
		assert.Equal(t, "csi-snapclass", *snapshots[0].Spec.VolumeSnapshotClassName)
	})

	t.Run("creates snapshot without VolumeSnapshotClassName when empty", func(t *testing.T) {
		scheme := newTestScheme()
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
		snapshotter := backup.NewSnapshotter(fakeClient)

		ctx := context.Background()
		name, err := snapshotter.CreateSnapshot(ctx, backup.SnapshotRequest{
			Namespace:  "minecraft",
			PVCName:    "data-my-server-0",
			ServerName: "my-server",
			Trigger:    "manual",
		})

		require.NoError(t, err)
		assert.NotEmpty(t, name)

		snapshots, err := snapshotter.ListSnapshots(ctx, "minecraft", "my-server")
		require.NoError(t, err)
		require.Len(t, snapshots, 1)
		assert.Nil(t, snapshots[0].Spec.VolumeSnapshotClassName)
	})

	t.Run("sets labels for server name and trigger", func(t *testing.T) {
		scheme := newTestScheme()
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
		snapshotter := backup.NewSnapshotter(fakeClient)

		ctx := context.Background()
		name, err := snapshotter.CreateSnapshot(ctx, backup.SnapshotRequest{
			Namespace:  "minecraft",
			PVCName:    "data-my-server-0",
			ServerName: "my-server",
			Trigger:    "pre-update",
		})

		require.NoError(t, err)

		snapshots, err := snapshotter.ListSnapshots(ctx, "minecraft", "my-server")
		require.NoError(t, err)
		require.Len(t, snapshots, 1)

		snap := snapshots[0]
		assert.Equal(t, name, snap.Name)
		assert.Equal(t, "my-server", snap.Labels[backup.LabelServerName])
		assert.Equal(t, "pre-update", snap.Labels[backup.LabelTrigger])
		assert.Equal(t, "minecraft-operator", snap.Labels[backup.LabelManagedBy])
	})
}

func TestListSnapshots(t *testing.T) {
	t.Run("returns only snapshots for the specified server", func(t *testing.T) {
		scheme := newTestScheme()
		existingSnapshots := []volumesnapshotv1.VolumeSnapshot{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "server-a-backup-1",
					Namespace: "minecraft",
					Labels: map[string]string{
						backup.LabelServerName: "server-a",
						backup.LabelManagedBy:  "minecraft-operator",
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "server-b-backup-1",
					Namespace: "minecraft",
					Labels: map[string]string{
						backup.LabelServerName: "server-b",
						backup.LabelManagedBy:  "minecraft-operator",
					},
				},
			},
		}
		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithLists(&volumesnapshotv1.VolumeSnapshotList{Items: existingSnapshots}).
			Build()
		snapshotter := backup.NewSnapshotter(fakeClient)

		ctx := context.Background()
		snapshots, err := snapshotter.ListSnapshots(ctx, "minecraft", "server-a")

		require.NoError(t, err)
		require.Len(t, snapshots, 1)
		assert.Equal(t, "server-a-backup-1", snapshots[0].Name)
	})

	t.Run("returns empty list when no snapshots exist", func(t *testing.T) {
		scheme := newTestScheme()
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
		snapshotter := backup.NewSnapshotter(fakeClient)

		ctx := context.Background()
		snapshots, err := snapshotter.ListSnapshots(ctx, "minecraft", "my-server")

		require.NoError(t, err)
		assert.Empty(t, snapshots)
	})
}

func TestDeleteOldSnapshots(t *testing.T) { //nolint:funlen
	t.Run("deletes oldest snapshots when count exceeds maxCount", func(t *testing.T) {
		scheme := newTestScheme()

		now := time.Now()
		existingSnapshots := []volumesnapshotv1.VolumeSnapshot{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "server-backup-oldest",
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
					Name:              "server-backup-middle",
					Namespace:         "minecraft",
					CreationTimestamp: metav1.NewTime(now.Add(-2 * time.Hour)),
					Labels: map[string]string{
						backup.LabelServerName: "my-server",
						backup.LabelManagedBy:  "minecraft-operator",
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "server-backup-newest",
					Namespace:         "minecraft",
					CreationTimestamp: metav1.NewTime(now.Add(-1 * time.Hour)),
					Labels: map[string]string{
						backup.LabelServerName: "my-server",
						backup.LabelManagedBy:  "minecraft-operator",
					},
				},
			},
		}
		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithLists(&volumesnapshotv1.VolumeSnapshotList{Items: existingSnapshots}).
			Build()
		snapshotter := backup.NewSnapshotter(fakeClient)

		ctx := context.Background()
		deleted, err := snapshotter.DeleteOldSnapshots(ctx, "minecraft", "my-server", 2)

		require.NoError(t, err)
		assert.Equal(t, 1, deleted)

		// Verify only 2 snapshots remain
		remaining, err := snapshotter.ListSnapshots(ctx, "minecraft", "my-server")
		require.NoError(t, err)
		assert.Len(t, remaining, 2)

		// The oldest should be deleted
		for _, s := range remaining {
			assert.NotEqual(t, "server-backup-oldest", s.Name)
		}
	})

	t.Run("does nothing when count is within limit", func(t *testing.T) {
		scheme := newTestScheme()

		existingSnapshots := []volumesnapshotv1.VolumeSnapshot{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "server-backup-1",
					Namespace: "minecraft",
					Labels: map[string]string{
						backup.LabelServerName: "my-server",
						backup.LabelManagedBy:  "minecraft-operator",
					},
				},
			},
		}
		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithLists(&volumesnapshotv1.VolumeSnapshotList{Items: existingSnapshots}).
			Build()
		snapshotter := backup.NewSnapshotter(fakeClient)

		ctx := context.Background()
		deleted, err := snapshotter.DeleteOldSnapshots(ctx, "minecraft", "my-server", 10)

		require.NoError(t, err)
		assert.Equal(t, 0, deleted)
	})
}
