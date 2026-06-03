/*
Copyright 2026, Aleksei Sviridkin.

SPDX-License-Identifier: BSD-3-Clause
*/

package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
)

func TestVolumesChanged_IgnoresInjectedVolumes(t *testing.T) {
	dataVolume := corev1.Volume{
		Name: gcVolumeData,
		VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: "data-test-server-0",
			},
		},
	}
	projectedVolume := corev1.Volume{
		Name: "kube-api-access-abc12",
		VolumeSource: corev1.VolumeSource{
			Projected: &corev1.ProjectedVolumeSource{
				Sources: []corev1.VolumeProjection{
					{
						ServiceAccountToken: &corev1.ServiceAccountTokenProjection{
							Path: "token",
						},
					},
				},
			},
		},
	}

	existing := []corev1.Volume{dataVolume, projectedVolume}
	desired := []corev1.Volume{dataVolume}

	assert.False(t, volumesChanged(existing, desired),
		"extra projected volume in existing should not trigger change")
}

func TestVolumesChanged_DetectsModifiedVolume(t *testing.T) {
	existing := []corev1.Volume{
		{
			Name: gcConfigScript,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: "old-cm"},
				},
			},
		},
	}
	desired := []corev1.Volume{
		{
			Name: gcConfigScript,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: "new-cm"},
				},
			},
		},
	}

	assert.True(t, volumesChanged(existing, desired),
		"should detect when managed volume changes")
}

func TestVolumesChanged_DetectsMissingDesiredVolume(t *testing.T) {
	existing := []corev1.Volume{}
	desired := []corev1.Volume{
		{
			Name: gcVolumeData,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: gcDataTest0,
				},
			},
		},
	}

	assert.True(t, volumesChanged(existing, desired),
		"should detect when desired volume is missing from existing")
}

func TestVolumesChanged_NoChange(t *testing.T) {
	volumes := []corev1.Volume{
		{
			Name: gcVolumeData,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: gcDataTest0,
				},
			},
		},
	}

	assert.False(t, volumesChanged(volumes, volumes),
		"identical volumes should not trigger change")
}

// volumesChanged must detect removal of operator-managed volumes that were in existing
// but are no longer in desired (e.g., a ConfigMap volume that was removed from config).
func TestVolumesChanged_DetectsRemovedOperatorManagedVolume(t *testing.T) {
	existing := []corev1.Volume{
		{
			Name: gcVolumeData,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: gcDataTest0,
				},
			},
		},
		{
			Name: "cm-old-config-abcdef12",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: "old-config"},
				},
			},
		},
	}
	desired := []corev1.Volume{
		{
			Name: gcVolumeData,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: gcDataTest0,
				},
			},
		},
	}

	assert.True(t, volumesChanged(existing, desired),
		"should detect removal of operator-managed volume (cm-*) from desired")
}

// volumesChanged must detect removal of config-script volume.
func TestVolumesChanged_DetectsRemovedConfigScriptVolume(t *testing.T) {
	existing := []corev1.Volume{
		{
			Name: gcVolumeData,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: gcDataTest0,
				},
			},
		},
		{
			Name: gcConfigScript,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: "test-config-script"},
				},
			},
		},
	}
	desired := []corev1.Volume{
		{
			Name: gcVolumeData,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: gcDataTest0,
				},
			},
		},
	}

	assert.True(t, volumesChanged(existing, desired),
		"should detect removal of config-script volume from desired")
}

func TestIsOperatorManagedVolume(t *testing.T) {
	assert.True(t, isOperatorManagedVolume(gcVolumeData))
	assert.True(t, isOperatorManagedVolume(gcConfigScript))
	assert.True(t, isOperatorManagedVolume("cm-bluemap-abc12345"))
	assert.True(t, isOperatorManagedVolume("cm-"))
	assert.False(t, isOperatorManagedVolume("kube-api-access-abc12"))
	assert.False(t, isOperatorManagedVolume("custom-volume"))
	assert.False(t, isOperatorManagedVolume(""))
}
