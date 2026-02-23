package backup

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/cockroachdb/errors"
	volumesnapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// LabelServerName is the label key for the PaperMCServer name.
	LabelServerName = "mc.k8s.lex.la/server-name"

	// LabelTrigger is the label key for what triggered the backup.
	LabelTrigger = "mc.k8s.lex.la/backup-trigger"

	// LabelManagedBy is the label key indicating the managing controller.
	LabelManagedBy = "app.kubernetes.io/managed-by"
)

// SnapshotRequest contains the parameters for creating a VolumeSnapshot.
type SnapshotRequest struct {
	Namespace               string
	PVCName                 string
	ServerName              string
	VolumeSnapshotClassName string
	Trigger                 string
}

// Snapshotter creates and manages VolumeSnapshots.
type Snapshotter interface {
	CreateSnapshot(ctx context.Context, req SnapshotRequest) (string, error)
	ListSnapshots(ctx context.Context, namespace, serverName string) ([]volumesnapshotv1.VolumeSnapshot, error)
	DeleteOldSnapshots(ctx context.Context, namespace, serverName string, maxCount int) (int, error)
}

// VolumeSnapshotSnapshotter implements Snapshotter using the Kubernetes VolumeSnapshot API.
type VolumeSnapshotSnapshotter struct {
	client client.Client
}

// NewSnapshotter creates a new VolumeSnapshotSnapshotter.
func NewSnapshotter(c client.Client) *VolumeSnapshotSnapshotter {
	return &VolumeSnapshotSnapshotter{client: c}
}

// CreateSnapshot creates a VolumeSnapshot from the specified PVC.
func (s *VolumeSnapshotSnapshotter) CreateSnapshot(ctx context.Context, req SnapshotRequest) (string, error) {
	now := time.Now()
	name := fmt.Sprintf("%s-backup-%d", req.ServerName, now.Unix())

	snapshot := &volumesnapshotv1.VolumeSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Namespace:         req.Namespace,
			CreationTimestamp: metav1.NewTime(now),
			Labels: map[string]string{
				LabelServerName: req.ServerName,
				LabelTrigger:    req.Trigger,
				LabelManagedBy:  "minecraft-operator",
			},
		},
		Spec: volumesnapshotv1.VolumeSnapshotSpec{
			Source: volumesnapshotv1.VolumeSnapshotSource{
				PersistentVolumeClaimName: &req.PVCName,
			},
		},
	}

	if req.VolumeSnapshotClassName != "" {
		snapshot.Spec.VolumeSnapshotClassName = &req.VolumeSnapshotClassName
	}

	if err := s.client.Create(ctx, snapshot); err != nil {
		return "", errors.Wrap(err, "failed to create VolumeSnapshot")
	}

	return name, nil
}

// ListSnapshots returns all VolumeSnapshots managed by this operator for the given server.
func (s *VolumeSnapshotSnapshotter) ListSnapshots(
	ctx context.Context,
	namespace, serverName string,
) ([]volumesnapshotv1.VolumeSnapshot, error) {
	var snapList volumesnapshotv1.VolumeSnapshotList

	selector := labels.SelectorFromSet(labels.Set{
		LabelServerName: serverName,
		LabelManagedBy:  "minecraft-operator",
	})

	if err := s.client.List(ctx, &snapList,
		client.InNamespace(namespace),
		client.MatchingLabelsSelector{Selector: selector},
	); err != nil {
		return nil, errors.Wrap(err, "failed to list VolumeSnapshots")
	}

	return snapList.Items, nil
}

// DeleteOldSnapshots deletes the oldest snapshots when the count exceeds maxCount.
// Returns the number of snapshots deleted.
func (s *VolumeSnapshotSnapshotter) DeleteOldSnapshots(
	ctx context.Context,
	namespace, serverName string,
	maxCount int,
) (int, error) {
	snapshots, err := s.ListSnapshots(ctx, namespace, serverName)
	if err != nil {
		return 0, err
	}

	if len(snapshots) <= maxCount {
		return 0, nil
	}

	// Sort by creation timestamp (oldest first)
	sort.Slice(snapshots, func(i, j int) bool {
		return snapshots[i].CreationTimestamp.Before(&snapshots[j].CreationTimestamp)
	})

	toDelete := len(snapshots) - maxCount
	deleted := 0

	for i := range toDelete {
		snap := &snapshots[i]
		if err := s.client.Delete(ctx, snap); err != nil {
			return deleted, errors.Wrapf(err, "failed to delete snapshot %s", snap.Name)
		}

		deleted++
	}

	return deleted, nil
}
