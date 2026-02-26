/*
Copyright 2026, Aleksei Sviridkin.

SPDX-License-Identifier: BSD-3-Clause
*/

package v1beta1

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const testServerVersion = "1.21.1"

func validServer() *PaperMCServer {
	return &PaperMCServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-server",
			Namespace: "default",
		},
		Spec: PaperMCServerSpec{
			UpdateStrategy: "latest",
			UpdateSchedule: UpdateSchedule{
				CheckCron: "0 3 * * *",
				MaintenanceWindow: MaintenanceWindow{
					Cron:    "0 4 * * 0",
					Enabled: true,
				},
			},
			GracefulShutdown: GracefulShutdown{
				Timeout: metav1.Duration{},
			},
			RCON: RCONConfig{
				Enabled: true,
				PasswordSecret: SecretKeyRef{
					Name: "rcon-secret",
					Key:  "password",
				},
				Port: 25575,
			},
			PodTemplate: corev1.PodTemplateSpec{},
		},
	}
}

func TestServerValidateCreate_Valid(t *testing.T) {
	v := &PaperMCServerValidator{}
	s := validServer()

	warnings, err := v.ValidateCreate(context.Background(), s)
	require.NoError(t, err)
	assert.Empty(t, warnings)
}

func TestServerValidateCreate_PinMissingVersion(t *testing.T) {
	v := &PaperMCServerValidator{}
	s := validServer()
	s.Spec.UpdateStrategy = strategyPin

	_, err := v.ValidateCreate(context.Background(), s)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "version")
}

func TestServerValidateCreate_PinWithVersion(t *testing.T) {
	v := &PaperMCServerValidator{}
	s := validServer()
	s.Spec.UpdateStrategy = strategyPin
	s.Spec.Version = testServerVersion

	warnings, err := v.ValidateCreate(context.Background(), s)
	require.NoError(t, err)
	assert.Empty(t, warnings)
}

func TestServerValidateCreate_BuildPinMissingVersion(t *testing.T) {
	v := &PaperMCServerValidator{}
	s := validServer()
	s.Spec.UpdateStrategy = strategyBuildPin

	_, err := v.ValidateCreate(context.Background(), s)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "version")
}

func TestServerValidateCreate_BuildPinMissingBuild(t *testing.T) {
	v := &PaperMCServerValidator{}
	s := validServer()
	s.Spec.UpdateStrategy = strategyBuildPin
	s.Spec.Version = testServerVersion

	_, err := v.ValidateCreate(context.Background(), s)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "build")
}

func TestServerValidateCreate_BuildPinValid(t *testing.T) {
	v := &PaperMCServerValidator{}
	s := validServer()
	s.Spec.UpdateStrategy = strategyBuildPin
	s.Spec.Version = testServerVersion
	build := 91
	s.Spec.Build = &build

	warnings, err := v.ValidateCreate(context.Background(), s)
	require.NoError(t, err)
	assert.Empty(t, warnings)
}

func TestServerValidateCreate_InvalidCheckCron(t *testing.T) {
	v := &PaperMCServerValidator{}
	s := validServer()
	s.Spec.UpdateSchedule.CheckCron = "not-a-cron"

	_, err := v.ValidateCreate(context.Background(), s)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "checkCron")
}

func TestServerValidateCreate_InvalidMaintenanceCron(t *testing.T) {
	v := &PaperMCServerValidator{}
	s := validServer()
	s.Spec.UpdateSchedule.MaintenanceWindow.Cron = "invalid cron"

	_, err := v.ValidateCreate(context.Background(), s)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "maintenanceWindow")
}

func TestServerValidateCreate_RCONEnabledMissingSecretName(t *testing.T) {
	v := &PaperMCServerValidator{}
	s := validServer()
	s.Spec.RCON.PasswordSecret.Name = ""

	_, err := v.ValidateCreate(context.Background(), s)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "passwordSecret")
}

func TestServerValidateCreate_RCONEnabledMissingSecretKey(t *testing.T) {
	v := &PaperMCServerValidator{}
	s := validServer()
	s.Spec.RCON.PasswordSecret.Key = ""

	_, err := v.ValidateCreate(context.Background(), s)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "passwordSecret")
}

func TestServerValidateCreate_RCONDisabledNoSecret(t *testing.T) {
	v := &PaperMCServerValidator{}
	s := validServer()
	s.Spec.RCON.Enabled = false
	s.Spec.RCON.PasswordSecret = SecretKeyRef{}

	warnings, err := v.ValidateCreate(context.Background(), s)
	require.NoError(t, err)
	assert.Empty(t, warnings)
}

func TestServerValidateCreate_BackupInvalidCron(t *testing.T) {
	v := &PaperMCServerValidator{}
	s := validServer()
	s.Spec.Backup = &BackupSpec{
		Enabled:  true,
		Schedule: "bad cron",
	}

	_, err := v.ValidateCreate(context.Background(), s)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "backup")
}

func TestServerValidateCreate_BackupValidCron(t *testing.T) {
	v := &PaperMCServerValidator{}
	s := validServer()
	s.Spec.Backup = &BackupSpec{
		Enabled:  true,
		Schedule: "0 */6 * * *",
	}

	warnings, err := v.ValidateCreate(context.Background(), s)
	require.NoError(t, err)
	assert.Empty(t, warnings)
}

func TestServerValidateCreate_BackupDisabledInvalidCronIgnored(t *testing.T) {
	v := &PaperMCServerValidator{}
	s := validServer()
	s.Spec.Backup = &BackupSpec{
		Enabled:  false,
		Schedule: "bad cron",
	}

	warnings, err := v.ValidateCreate(context.Background(), s)
	require.NoError(t, err)
	assert.Empty(t, warnings)
}

func TestServerValidateCreate_GatewayEnabledNoParentRefs(t *testing.T) {
	v := &PaperMCServerValidator{}
	s := validServer()
	s.Spec.Gateway = &GatewayConfig{
		Enabled:    true,
		ParentRefs: nil,
	}

	_, err := v.ValidateCreate(context.Background(), s)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parentRefs")
}

func TestServerValidateCreate_GatewayEnabledWithParentRefs(t *testing.T) {
	v := &PaperMCServerValidator{}
	s := validServer()
	s.Spec.Gateway = &GatewayConfig{
		Enabled: true,
		ParentRefs: []GatewayParentRef{
			{Name: "my-gateway"},
		},
	}

	warnings, err := v.ValidateCreate(context.Background(), s)
	require.NoError(t, err)
	assert.Empty(t, warnings)
}

// Issue 4 (round 2): disabled maintenance window with invalid cron should be ignored.
func TestServerValidateCreate_MaintenanceWindowDisabledInvalidCronIgnored(t *testing.T) {
	v := &PaperMCServerValidator{}
	s := validServer()
	s.Spec.UpdateSchedule.MaintenanceWindow.Enabled = false
	s.Spec.UpdateSchedule.MaintenanceWindow.Cron = "bad cron"

	warnings, err := v.ValidateCreate(context.Background(), s)
	require.NoError(t, err)
	assert.Empty(t, warnings)
}

// Issue 6: auto strategy with version should be valid (no error).
func TestServerValidateCreate_AutoStrategyWithVersion(t *testing.T) {
	v := &PaperMCServerValidator{}
	s := validServer()
	s.Spec.UpdateStrategy = "auto"
	s.Spec.Version = testServerVersion

	warnings, err := v.ValidateCreate(context.Background(), s)
	require.NoError(t, err)
	assert.Empty(t, warnings)
}

// Issue 6: latest strategy should be valid (no error).
func TestServerValidateCreate_LatestStrategyValid(t *testing.T) {
	v := &PaperMCServerValidator{}
	s := validServer()
	s.Spec.UpdateStrategy = "latest"

	warnings, err := v.ValidateCreate(context.Background(), s)
	require.NoError(t, err)
	assert.Empty(t, warnings)
}

func TestServerValidateUpdate_Valid(t *testing.T) {
	v := &PaperMCServerValidator{}
	oldS := validServer()
	newS := validServer()
	newS.Spec.Version = "1.21.2"

	warnings, err := v.ValidateUpdate(context.Background(), oldS, newS)
	require.NoError(t, err)
	assert.Empty(t, warnings)
}

func TestServerValidateUpdate_InvalidNewSpec(t *testing.T) {
	v := &PaperMCServerValidator{}
	oldS := validServer()
	newS := validServer()
	newS.Spec.UpdateSchedule.CheckCron = "bad"

	_, err := v.ValidateUpdate(context.Background(), oldS, newS)
	require.Error(t, err)
}

func TestServerValidateDelete_AlwaysAllowed(t *testing.T) {
	v := &PaperMCServerValidator{}
	s := validServer()

	warnings, err := v.ValidateDelete(context.Background(), s)
	require.NoError(t, err)
	assert.Empty(t, warnings)
}
