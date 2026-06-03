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
			UpdateStrategy: strategyLatest,
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
		Schedule: testCronBad,
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
		Schedule: testCronBad,
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
			{Name: testGatewayName},
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
	s.Spec.UpdateSchedule.MaintenanceWindow.Cron = testCronBad

	warnings, err := v.ValidateCreate(context.Background(), s)
	require.NoError(t, err)
	assert.Empty(t, warnings)
}

// Issue 6: auto strategy with version should be valid (no error).
func TestServerValidateCreate_AutoStrategyWithVersion(t *testing.T) {
	v := &PaperMCServerValidator{}
	s := validServer()
	s.Spec.UpdateStrategy = strategyAuto
	s.Spec.Version = testServerVersion

	warnings, err := v.ValidateCreate(context.Background(), s)
	require.NoError(t, err)
	assert.Empty(t, warnings)
}

// Issue 6: latest strategy should be valid (no error).
func TestServerValidateCreate_LatestStrategyValid(t *testing.T) {
	v := &PaperMCServerValidator{}
	s := validServer()
	s.Spec.UpdateStrategy = strategyLatest

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
	newS.Spec.UpdateSchedule.CheckCron = testNameBad

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

// --- HTTPRoutes validation tests ---

func TestServerValidateCreate_HTTPRoutesWithGatewayEnabled(t *testing.T) {
	v := &PaperMCServerValidator{}
	s := validServer()
	s.Spec.Gateway = &GatewayConfig{
		Enabled:    true,
		ParentRefs: []GatewayParentRef{{Name: testGatewayName}},
		HTTPRoutes: []PluginHTTPRoute{
			{PluginName: testPluginBlueMap, EndpointName: testEndpointWebUI, Hostname: testHostMapExample},
		},
	}

	warnings, err := v.ValidateCreate(context.Background(), s)
	require.NoError(t, err)
	assert.Empty(t, warnings)
}

func TestServerValidateCreate_HTTPRoutesWithGatewayDisabled(t *testing.T) {
	v := &PaperMCServerValidator{}
	s := validServer()
	s.Spec.Gateway = &GatewayConfig{
		Enabled: false,
		HTTPRoutes: []PluginHTTPRoute{
			{PluginName: testPluginBlueMap, EndpointName: testEndpointWebUI, Hostname: testHostMapExample},
		},
	}

	_, err := v.ValidateCreate(context.Background(), s)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "httpRoutes")
}

func TestServerValidateCreate_HTTPRoutesWithoutParentRefs(t *testing.T) {
	v := &PaperMCServerValidator{}
	s := validServer()
	s.Spec.Gateway = &GatewayConfig{
		Enabled: true,
		HTTPRoutes: []PluginHTTPRoute{
			{PluginName: testPluginBlueMap, EndpointName: testEndpointWebUI, Hostname: testHostMapExample},
		},
	}

	_, err := v.ValidateCreate(context.Background(), s)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parentRefs")
}

func TestServerValidateCreate_HTTPRoutesDuplicatePluginEndpoint(t *testing.T) {
	v := &PaperMCServerValidator{}
	s := validServer()
	s.Spec.Gateway = &GatewayConfig{
		Enabled:    true,
		ParentRefs: []GatewayParentRef{{Name: testGatewayName}},
		HTTPRoutes: []PluginHTTPRoute{
			{PluginName: testPluginBlueMap, EndpointName: testEndpointWebUI, Hostname: "map1.example.com"},
			{PluginName: testPluginBlueMap, EndpointName: testEndpointWebUI, Hostname: "map2.example.com"},
		},
	}

	_, err := v.ValidateCreate(context.Background(), s)
	require.Error(t, err)
	assert.Contains(t, err.Error(), testPluginBlueMap)
}

func TestServerValidateCreate_HTTPRoutesEmptyPluginName(t *testing.T) {
	v := &PaperMCServerValidator{}
	s := validServer()
	s.Spec.Gateway = &GatewayConfig{
		Enabled:    true,
		ParentRefs: []GatewayParentRef{{Name: testGatewayName}},
		HTTPRoutes: []PluginHTTPRoute{
			{PluginName: "", EndpointName: testEndpointWebUI, Hostname: testHostMapExample},
		},
	}

	_, err := v.ValidateCreate(context.Background(), s)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pluginName")
}

func TestServerValidateCreate_HTTPRoutesEmptyEndpointName(t *testing.T) {
	v := &PaperMCServerValidator{}
	s := validServer()
	s.Spec.Gateway = &GatewayConfig{
		Enabled:    true,
		ParentRefs: []GatewayParentRef{{Name: testGatewayName}},
		HTTPRoutes: []PluginHTTPRoute{
			{PluginName: testPluginBlueMap, EndpointName: "", Hostname: testHostMapExample},
		},
	}

	_, err := v.ValidateCreate(context.Background(), s)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "endpointName")
}

func TestServerValidateCreate_HTTPRoutesEmptyHostname(t *testing.T) {
	v := &PaperMCServerValidator{}
	s := validServer()
	s.Spec.Gateway = &GatewayConfig{
		Enabled:    true,
		ParentRefs: []GatewayParentRef{{Name: testGatewayName}},
		HTTPRoutes: []PluginHTTPRoute{
			{PluginName: testPluginBlueMap, EndpointName: testEndpointWebUI, Hostname: ""},
		},
	}

	_, err := v.ValidateCreate(context.Background(), s)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "hostname")
}

func TestServerValidateCreate_HTTPRoutesDuplicateHostnamePathPrefix(t *testing.T) {
	v := &PaperMCServerValidator{}
	s := validServer()
	s.Spec.Gateway = &GatewayConfig{
		Enabled:    true,
		ParentRefs: []GatewayParentRef{{Name: testGatewayName}},
		HTTPRoutes: []PluginHTTPRoute{
			{PluginName: testPluginBlueMap, EndpointName: testEndpointWebUI, Hostname: testHostMapExample, PathPrefix: testPathMap},
			{PluginName: "dynmap", EndpointName: "dynmap-ui", Hostname: testHostMapExample, PathPrefix: testPathMap},
		},
	}

	_, err := v.ValidateCreate(context.Background(), s)
	require.Error(t, err)
	assert.Contains(t, err.Error(), testHostMapExample)
}

func TestServerValidateCreate_HTTPRoutesSameHostnameDifferentPathValid(t *testing.T) {
	v := &PaperMCServerValidator{}
	s := validServer()
	s.Spec.Gateway = &GatewayConfig{
		Enabled:    true,
		ParentRefs: []GatewayParentRef{{Name: testGatewayName}},
		HTTPRoutes: []PluginHTTPRoute{
			{PluginName: testPluginBlueMap, EndpointName: testEndpointWebUI, Hostname: testHostMCExample, PathPrefix: testPathMap},
			{PluginName: "dynmap", EndpointName: "dynmap-ui", Hostname: testHostMCExample, PathPrefix: "/dynmap"},
		},
	}

	warnings, err := v.ValidateCreate(context.Background(), s)
	require.NoError(t, err)
	assert.Empty(t, warnings)
}

func TestServerValidateCreate_HTTPRoutesEmptyListValid(t *testing.T) {
	v := &PaperMCServerValidator{}
	s := validServer()
	s.Spec.Gateway = &GatewayConfig{
		Enabled:    true,
		ParentRefs: []GatewayParentRef{{Name: testGatewayName}},
		HTTPRoutes: nil,
	}

	warnings, err := v.ValidateCreate(context.Background(), s)
	require.NoError(t, err)
	assert.Empty(t, warnings)
}

func TestServerValidateCreate_HTTPRoutesInvalidHostname(t *testing.T) {
	v := &PaperMCServerValidator{}
	s := validServer()
	s.Spec.Gateway = &GatewayConfig{
		Enabled:    true,
		ParentRefs: []GatewayParentRef{{Name: testGatewayName}},
		HTTPRoutes: []PluginHTTPRoute{
			{PluginName: testPluginBlueMap, EndpointName: testEndpointWebUI, Hostname: "not a valid hostname!"},
		},
	}

	_, err := v.ValidateCreate(context.Background(), s)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "hostname")
}

func TestServerValidateCreate_HTTPRoutesPathPrefixNoSlash(t *testing.T) {
	v := &PaperMCServerValidator{}
	s := validServer()
	s.Spec.Gateway = &GatewayConfig{
		Enabled:    true,
		ParentRefs: []GatewayParentRef{{Name: testGatewayName}},
		HTTPRoutes: []PluginHTTPRoute{
			{PluginName: testPluginBlueMap, EndpointName: testEndpointWebUI, Hostname: testHostMapExample, PathPrefix: "map"},
		},
	}

	_, err := v.ValidateCreate(context.Background(), s)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pathPrefix")
}

func TestServerValidateCreate_HTTPRoutesWithPathPrefix(t *testing.T) {
	v := &PaperMCServerValidator{}
	s := validServer()
	s.Spec.Gateway = &GatewayConfig{
		Enabled:    true,
		ParentRefs: []GatewayParentRef{{Name: testGatewayName}},
		HTTPRoutes: []PluginHTTPRoute{
			{PluginName: testPluginBlueMap, EndpointName: testEndpointWebUI, Hostname: testHostMCExample, PathPrefix: testPathMap},
		},
	}

	warnings, err := v.ValidateCreate(context.Background(), s)
	require.NoError(t, err)
	assert.Empty(t, warnings)
}

// --- PluginConfigs validation tests ---

func TestServerValidateCreate_PluginConfigsValid(t *testing.T) {
	v := &PaperMCServerValidator{}
	s := validServer()
	s.Spec.PluginConfigs = []ServerPluginConfig{
		{
			PluginName: testPluginBlueMap,
			Configs: []PluginConfigFile{
				{
					ConfigMapRef: ConfigMapKeyRef{Name: testCMBlueMapConfig, Key: testConfigCoreConf},
					Path:         testConfigCoreConf,
					Overwrite:    testOverwriteAlways,
				},
			},
		},
	}

	warnings, err := v.ValidateCreate(context.Background(), s)
	require.NoError(t, err)
	assert.Empty(t, warnings)
}

func TestServerValidateCreate_PluginConfigsMissingPluginName(t *testing.T) {
	v := &PaperMCServerValidator{}
	s := validServer()
	s.Spec.PluginConfigs = []ServerPluginConfig{
		{
			PluginName: "",
			Configs: []PluginConfigFile{
				{
					ConfigMapRef: ConfigMapKeyRef{Name: testCMBlueMapConfig, Key: testConfigCoreConf},
					Path:         testConfigCoreConf,
				},
			},
		},
	}

	_, err := v.ValidateCreate(context.Background(), s)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pluginName")
}

func TestServerValidateCreate_PluginConfigsPathTraversal(t *testing.T) {
	v := &PaperMCServerValidator{}
	s := validServer()
	s.Spec.PluginConfigs = []ServerPluginConfig{
		{
			PluginName: testPluginBlueMap,
			Configs: []PluginConfigFile{
				{
					ConfigMapRef: ConfigMapKeyRef{Name: testCMEvil, Key: testCMKeyPayload},
					Path:         testPathTraversalPasswd,
				},
			},
		},
	}

	_, err := v.ValidateCreate(context.Background(), s)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "..")
}

func TestServerValidateCreate_PluginConfigsAbsolutePath(t *testing.T) {
	v := &PaperMCServerValidator{}
	s := validServer()
	s.Spec.PluginConfigs = []ServerPluginConfig{
		{
			PluginName: testPluginBlueMap,
			Configs: []PluginConfigFile{
				{
					ConfigMapRef: ConfigMapKeyRef{Name: testCMEvil, Key: testCMKeyPayload},
					Path:         testPathEtcPasswd,
				},
			},
		},
	}

	_, err := v.ValidateCreate(context.Background(), s)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path")
}

func TestServerValidateCreate_PluginConfigsMissingConfigMapName(t *testing.T) {
	v := &PaperMCServerValidator{}
	s := validServer()
	s.Spec.PluginConfigs = []ServerPluginConfig{
		{
			PluginName: testPluginBlueMap,
			Configs: []PluginConfigFile{
				{
					ConfigMapRef: ConfigMapKeyRef{Name: "", Key: testConfigCoreConf},
					Path:         testConfigCoreConf,
				},
			},
		},
	}

	_, err := v.ValidateCreate(context.Background(), s)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "configMapRef")
}

func TestServerValidateCreate_PluginConfigsMissingConfigMapKey(t *testing.T) {
	v := &PaperMCServerValidator{}
	s := validServer()
	s.Spec.PluginConfigs = []ServerPluginConfig{
		{
			PluginName: testPluginBlueMap,
			Configs: []PluginConfigFile{
				{
					ConfigMapRef: ConfigMapKeyRef{Name: testCMBlueMapConfig, Key: ""},
					Path:         testConfigCoreConf,
				},
			},
		},
	}

	_, err := v.ValidateCreate(context.Background(), s)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "configMapRef")
}

func TestServerValidateCreate_PluginConfigsDuplicatePath(t *testing.T) {
	v := &PaperMCServerValidator{}
	s := validServer()
	s.Spec.PluginConfigs = []ServerPluginConfig{
		{
			PluginName: testPluginBlueMap,
			Configs: []PluginConfigFile{
				{
					ConfigMapRef: ConfigMapKeyRef{Name: "cm1", Key: testConfigCoreConf},
					Path:         testConfigCoreConf,
				},
				{
					ConfigMapRef: ConfigMapKeyRef{Name: "cm2", Key: testConfigCoreConf},
					Path:         testConfigCoreConf,
				},
			},
		},
	}

	_, err := v.ValidateCreate(context.Background(), s)
	require.Error(t, err)
	assert.Contains(t, err.Error(), testConfigCoreConf)
}

func TestServerValidateCreate_PluginConfigsEmptyList(t *testing.T) {
	v := &PaperMCServerValidator{}
	s := validServer()
	s.Spec.PluginConfigs = nil

	warnings, err := v.ValidateCreate(context.Background(), s)
	require.NoError(t, err)
	assert.Empty(t, warnings)
}

// --- ServerConfigs validation tests ---

func TestServerValidateCreate_ServerConfigsValid(t *testing.T) {
	v := &PaperMCServerValidator{}
	s := validServer()
	s.Spec.ServerConfigs = []ServerConfigFile{
		{
			ConfigMapRef: ConfigMapKeyRef{Name: testCMMCConfig, Key: testConfigServerProps},
			Path:         testConfigServerProps,
			Overwrite:    testOverwriteAlways,
		},
	}

	warnings, err := v.ValidateCreate(context.Background(), s)
	require.NoError(t, err)
	assert.Empty(t, warnings)
}

func TestServerValidateCreate_ServerConfigsPathTraversal(t *testing.T) {
	v := &PaperMCServerValidator{}
	s := validServer()
	s.Spec.ServerConfigs = []ServerConfigFile{
		{
			ConfigMapRef: ConfigMapKeyRef{Name: testCMEvil, Key: testCMKeyPayload},
			Path:         testPathTraversalPasswd,
		},
	}

	_, err := v.ValidateCreate(context.Background(), s)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "..")
}

func TestServerValidateCreate_ServerConfigsAbsolutePath(t *testing.T) {
	v := &PaperMCServerValidator{}
	s := validServer()
	s.Spec.ServerConfigs = []ServerConfigFile{
		{
			ConfigMapRef: ConfigMapKeyRef{Name: testCMEvil, Key: testCMKeyPayload},
			Path:         testPathEtcPasswd,
		},
	}

	_, err := v.ValidateCreate(context.Background(), s)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path")
}

func TestServerValidateCreate_ServerConfigsMissingConfigMapName(t *testing.T) {
	v := &PaperMCServerValidator{}
	s := validServer()
	s.Spec.ServerConfigs = []ServerConfigFile{
		{
			ConfigMapRef: ConfigMapKeyRef{Name: "", Key: testConfigServerProps},
			Path:         testConfigServerProps,
		},
	}

	_, err := v.ValidateCreate(context.Background(), s)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "configMapRef")
}

func TestServerValidateCreate_ServerConfigsMissingConfigMapKey(t *testing.T) {
	v := &PaperMCServerValidator{}
	s := validServer()
	s.Spec.ServerConfigs = []ServerConfigFile{
		{
			ConfigMapRef: ConfigMapKeyRef{Name: testCMMCConfig, Key: ""},
			Path:         testConfigServerProps,
		},
	}

	_, err := v.ValidateCreate(context.Background(), s)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "configMapRef")
}

func TestServerValidateCreate_ServerConfigsDuplicatePath(t *testing.T) {
	v := &PaperMCServerValidator{}
	s := validServer()
	s.Spec.ServerConfigs = []ServerConfigFile{
		{
			ConfigMapRef: ConfigMapKeyRef{Name: "cm1", Key: testConfigServerProps},
			Path:         testConfigServerProps,
		},
		{
			ConfigMapRef: ConfigMapKeyRef{Name: "cm2", Key: testConfigServerProps},
			Path:         testConfigServerProps,
		},
	}

	_, err := v.ValidateCreate(context.Background(), s)
	require.Error(t, err)
	assert.Contains(t, err.Error(), testConfigServerProps)
}

func TestServerValidateCreate_ServerConfigsEmptyPath(t *testing.T) {
	v := &PaperMCServerValidator{}
	s := validServer()
	s.Spec.ServerConfigs = []ServerConfigFile{
		{
			ConfigMapRef: ConfigMapKeyRef{Name: testCMMCConfig, Key: testConfigServerProps},
			Path:         "",
		},
	}

	_, err := v.ValidateCreate(context.Background(), s)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path")
}

func TestServerValidateCreate_ServerConfigsSubdirectoryPath(t *testing.T) {
	v := &PaperMCServerValidator{}
	s := validServer()
	s.Spec.ServerConfigs = []ServerConfigFile{
		{
			ConfigMapRef: ConfigMapKeyRef{Name: testCMMCConfig, Key: "paper-global.yml"},
			Path:         "config/paper-global.yml",
			Overwrite:    "ifNotExists",
		},
	}

	warnings, err := v.ValidateCreate(context.Background(), s)
	require.NoError(t, err)
	assert.Empty(t, warnings)
}

// --- ValidateUpdate tests for pluginConfigs and serverConfigs ---

func TestServerValidateUpdate_ValidPluginConfigs(t *testing.T) {
	v := &PaperMCServerValidator{}
	oldS := validServer()
	newS := validServer()
	newS.Spec.PluginConfigs = []ServerPluginConfig{
		{
			PluginName: testPluginBlueMap,
			Configs: []PluginConfigFile{
				{
					ConfigMapRef: ConfigMapKeyRef{Name: testCMBlueMapConfig, Key: testConfigCoreConf},
					Path:         testConfigCoreConf,
					Overwrite:    testOverwriteAlways,
				},
			},
		},
	}

	warnings, err := v.ValidateUpdate(context.Background(), oldS, newS)
	require.NoError(t, err)
	assert.Empty(t, warnings)
}

func TestServerValidateUpdate_InvalidPluginConfigsPathTraversal(t *testing.T) {
	v := &PaperMCServerValidator{}
	oldS := validServer()
	newS := validServer()
	newS.Spec.PluginConfigs = []ServerPluginConfig{
		{
			PluginName: testPluginBlueMap,
			Configs: []PluginConfigFile{
				{
					ConfigMapRef: ConfigMapKeyRef{Name: testCMEvil, Key: testCMKeyPayload},
					Path:         testPathTraversalPasswd,
				},
			},
		},
	}

	_, err := v.ValidateUpdate(context.Background(), oldS, newS)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "..")
}

func TestServerValidateUpdate_ValidServerConfigs(t *testing.T) {
	v := &PaperMCServerValidator{}
	oldS := validServer()
	newS := validServer()
	newS.Spec.ServerConfigs = []ServerConfigFile{
		{
			ConfigMapRef: ConfigMapKeyRef{Name: testCMMCConfig, Key: testConfigServerProps},
			Path:         testConfigServerProps,
			Overwrite:    testOverwriteAlways,
		},
	}

	warnings, err := v.ValidateUpdate(context.Background(), oldS, newS)
	require.NoError(t, err)
	assert.Empty(t, warnings)
}

func TestServerValidateUpdate_InvalidServerConfigs(t *testing.T) {
	v := &PaperMCServerValidator{}
	oldS := validServer()
	newS := validServer()
	newS.Spec.ServerConfigs = []ServerConfigFile{
		{
			ConfigMapRef: ConfigMapKeyRef{Name: testCMEvil, Key: testCMKeyPayload},
			Path:         testPathEtcPasswd,
		},
	}

	_, err := v.ValidateUpdate(context.Background(), oldS, newS)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path")
}

func TestServerValidateCreate_MixedPluginAndServerConfigs(t *testing.T) {
	v := &PaperMCServerValidator{}
	s := validServer()
	s.Spec.PluginConfigs = []ServerPluginConfig{
		{
			PluginName: testPluginBlueMap,
			Configs: []PluginConfigFile{
				{
					ConfigMapRef: ConfigMapKeyRef{Name: testCMBlueMapConfig, Key: testConfigCoreConf},
					Path:         testConfigCoreConf,
				},
			},
		},
	}
	s.Spec.ServerConfigs = []ServerConfigFile{
		{
			ConfigMapRef: ConfigMapKeyRef{Name: testCMMCConfig, Key: testConfigServerProps},
			Path:         testConfigServerProps,
		},
	}

	warnings, err := v.ValidateCreate(context.Background(), s)
	require.NoError(t, err)
	assert.Empty(t, warnings)
}
