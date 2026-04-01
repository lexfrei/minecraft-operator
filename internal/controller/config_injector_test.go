/*
Copyright 2026, Aleksei Sviridkin.

SPDX-License-Identifier: BSD-3-Clause
*/

package controller

import (
	"testing"

	mcv1beta1 "github.com/lexfrei/minecraft-operator/api/v1beta1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func newTestPlugin(name, dirName string, configs []mcv1beta1.PluginConfigFile) mcv1beta1.Plugin {
	return mcv1beta1.Plugin{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: mcv1beta1.PluginSpec{
			Source: mcv1beta1.PluginSource{
				Type:    "hangar",
				Project: name,
			},
			PluginDirName:    dirName,
			Configs:          configs,
			InstanceSelector: metav1.LabelSelector{},
		},
	}
}

func newConfigTestServer(name string) *mcv1beta1.PaperMCServer {
	return &mcv1beta1.PaperMCServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: mcv1beta1.PaperMCServerSpec{
			UpdateStrategy: "latest",
		},
	}
}

func TestBuildConfigScript_NoConfigs(t *testing.T) {
	server := newConfigTestServer("test")
	plugins := []mcv1beta1.Plugin{}

	script, refs, warnings := buildConfigScript(server, plugins)

	assert.Empty(t, script)
	assert.Empty(t, refs)
	assert.Empty(t, warnings)
}

func TestBuildConfigScript_PluginDefaultsOnly(t *testing.T) {
	server := newConfigTestServer("test")
	plugins := []mcv1beta1.Plugin{
		newTestPlugin("BlueMap", "BlueMap", []mcv1beta1.PluginConfigFile{
			{
				ConfigMapRef: mcv1beta1.ConfigMapKeyRef{Name: "bluemap-defaults", Key: "core.conf"},
				Path:         "core.conf",
				Overwrite:    "always",
			},
		}),
	}

	script, refs, warnings := buildConfigScript(server, plugins)

	assert.Contains(t, script, "mkdir -p /data/plugins/BlueMap")
	assert.Contains(t, script, "cp /configs/cm-bluemap-defaults/core.conf /data/plugins/BlueMap/core.conf")
	assert.NotContains(t, script, "if [ ! -f")
	require.Len(t, refs, 1)
	assert.Equal(t, "bluemap-defaults", refs[0].ConfigMapName)
	assert.Equal(t, "cm-bluemap-defaults", refs[0].VolumeName)
	assert.Empty(t, warnings)
}

func TestBuildConfigScript_PluginDefaultIfNotExists(t *testing.T) {
	server := newConfigTestServer("test")
	plugins := []mcv1beta1.Plugin{
		newTestPlugin("BlueMap", "BlueMap", []mcv1beta1.PluginConfigFile{
			{
				ConfigMapRef: mcv1beta1.ConfigMapKeyRef{Name: "bluemap-defaults", Key: "core.conf"},
				Path:         "core.conf",
				Overwrite:    "ifNotExists",
			},
		}),
	}

	script, refs, warnings := buildConfigScript(server, plugins)

	assert.Contains(t, script, "if [ ! -f /data/plugins/BlueMap/core.conf ]")
	assert.Contains(t, script, "cp /configs/cm-bluemap-defaults/core.conf /data/plugins/BlueMap/core.conf")
	require.Len(t, refs, 1)
	assert.Empty(t, warnings)
}

func TestBuildConfigScript_ServerOverridesPluginDefault(t *testing.T) {
	server := newConfigTestServer("test")
	server.Spec.PluginConfigs = []mcv1beta1.ServerPluginConfig{
		{
			PluginName: "BlueMap",
			Configs: []mcv1beta1.PluginConfigFile{
				{
					ConfigMapRef: mcv1beta1.ConfigMapKeyRef{Name: "prod-bluemap", Key: "core.conf"},
					Path:         "core.conf",
					Overwrite:    "always",
				},
			},
		},
	}
	plugins := []mcv1beta1.Plugin{
		newTestPlugin("BlueMap", "BlueMap", []mcv1beta1.PluginConfigFile{
			{
				ConfigMapRef: mcv1beta1.ConfigMapKeyRef{Name: "bluemap-defaults", Key: "core.conf"},
				Path:         "core.conf",
				Overwrite:    "always",
			},
		}),
	}

	script, refs, warnings := buildConfigScript(server, plugins)

	// Server override should be used, not plugin default
	assert.Contains(t, script, "cp /configs/cm-prod-bluemap/core.conf /data/plugins/BlueMap/core.conf")
	assert.NotContains(t, script, "cm-bluemap-defaults")
	require.Len(t, refs, 1)
	assert.Equal(t, "prod-bluemap", refs[0].ConfigMapName)
	assert.Empty(t, warnings)
}

func TestBuildConfigScript_ServerConfigs(t *testing.T) {
	server := newConfigTestServer("test")
	server.Spec.ServerConfigs = []mcv1beta1.ServerConfigFile{
		{
			ConfigMapRef: mcv1beta1.ConfigMapKeyRef{Name: "mc-server-config", Key: "server.properties"},
			Path:         "server.properties",
			Overwrite:    "always",
		},
	}
	plugins := []mcv1beta1.Plugin{}

	script, refs, warnings := buildConfigScript(server, plugins)

	assert.Contains(t, script, "cp /configs/cm-mc-server-config/server.properties /data/server.properties")
	assert.NotContains(t, script, "if [ ! -f")
	require.Len(t, refs, 1)
	assert.Equal(t, "mc-server-config", refs[0].ConfigMapName)
	assert.Empty(t, warnings)
}

func TestBuildConfigScript_ServerConfigIfNotExists(t *testing.T) {
	server := newConfigTestServer("test")
	server.Spec.ServerConfigs = []mcv1beta1.ServerConfigFile{
		{
			ConfigMapRef: mcv1beta1.ConfigMapKeyRef{Name: "mc-server-config", Key: "server.properties"},
			Path:         "server.properties",
			Overwrite:    "ifNotExists",
		},
	}
	plugins := []mcv1beta1.Plugin{}

	script, refs, warnings := buildConfigScript(server, plugins)

	assert.Contains(t, script, "if [ ! -f /data/server.properties ]")
	assert.Contains(t, script, "cp /configs/cm-mc-server-config/server.properties /data/server.properties")
	require.Len(t, refs, 1)
	assert.Empty(t, warnings)
}

func TestBuildConfigScript_MixedPluginAndServerConfigs(t *testing.T) {
	server := newConfigTestServer("test")
	server.Spec.ServerConfigs = []mcv1beta1.ServerConfigFile{
		{
			ConfigMapRef: mcv1beta1.ConfigMapKeyRef{Name: "mc-config", Key: "server.properties"},
			Path:         "server.properties",
			Overwrite:    "always",
		},
	}
	plugins := []mcv1beta1.Plugin{
		newTestPlugin("BlueMap", "BlueMap", []mcv1beta1.PluginConfigFile{
			{
				ConfigMapRef: mcv1beta1.ConfigMapKeyRef{Name: "bluemap-defaults", Key: "core.conf"},
				Path:         "core.conf",
				Overwrite:    "always",
			},
		}),
	}

	script, refs, warnings := buildConfigScript(server, plugins)

	assert.Contains(t, script, "mkdir -p /data/plugins/BlueMap")
	assert.Contains(t, script, "cp /configs/cm-bluemap-defaults/core.conf /data/plugins/BlueMap/core.conf")
	assert.Contains(t, script, "cp /configs/cm-mc-config/server.properties /data/server.properties")
	require.Len(t, refs, 2)
	assert.Empty(t, warnings)
}

func TestBuildConfigScript_MultiplePlugins(t *testing.T) {
	server := newConfigTestServer("test")
	plugins := []mcv1beta1.Plugin{
		newTestPlugin("BlueMap", "BlueMap", []mcv1beta1.PluginConfigFile{
			{
				ConfigMapRef: mcv1beta1.ConfigMapKeyRef{Name: "bluemap-defaults", Key: "core.conf"},
				Path:         "core.conf",
				Overwrite:    "always",
			},
		}),
		newTestPlugin("EssentialsX", "Essentials", []mcv1beta1.PluginConfigFile{
			{
				ConfigMapRef: mcv1beta1.ConfigMapKeyRef{Name: "essentials-defaults", Key: "config.yml"},
				Path:         "config.yml",
				Overwrite:    "ifNotExists",
			},
		}),
	}

	script, refs, warnings := buildConfigScript(server, plugins)

	assert.Contains(t, script, "mkdir -p /data/plugins/BlueMap")
	assert.Contains(t, script, "mkdir -p /data/plugins/Essentials")
	assert.Contains(t, script, "cp /configs/cm-bluemap-defaults/core.conf /data/plugins/BlueMap/core.conf")
	assert.Contains(t, script, "if [ ! -f /data/plugins/Essentials/config.yml ]")
	require.Len(t, refs, 2)
	assert.Empty(t, warnings)
}

func TestBuildConfigScript_PluginWithoutDirNameUsesProjectName(t *testing.T) {
	server := newConfigTestServer("test")
	plugin := newTestPlugin("BlueMap", "", []mcv1beta1.PluginConfigFile{
		{
			ConfigMapRef: mcv1beta1.ConfigMapKeyRef{Name: "bluemap-defaults", Key: "core.conf"},
			Path:         "core.conf",
			Overwrite:    "always",
		},
	})
	// pluginDirName is empty, should fall back to source.project
	plugins := []mcv1beta1.Plugin{plugin}

	script, _, _ := buildConfigScript(server, plugins)

	assert.Contains(t, script, "mkdir -p /data/plugins/BlueMap")
	assert.Contains(t, script, "/data/plugins/BlueMap/core.conf")
}

func TestBuildConfigScript_PluginWithNoConfigsSkipped(t *testing.T) {
	server := newConfigTestServer("test")
	plugins := []mcv1beta1.Plugin{
		newTestPlugin("BlueMap", "BlueMap", nil),
	}

	script, refs, warnings := buildConfigScript(server, plugins)

	assert.Empty(t, script)
	assert.Empty(t, refs)
	assert.Empty(t, warnings)
}

func TestBuildConfigScript_DeduplicatesConfigMapVolumes(t *testing.T) {
	server := newConfigTestServer("test")
	plugins := []mcv1beta1.Plugin{
		newTestPlugin("BlueMap", "BlueMap", []mcv1beta1.PluginConfigFile{
			{
				ConfigMapRef: mcv1beta1.ConfigMapKeyRef{Name: "shared-config", Key: "core.conf"},
				Path:         "core.conf",
				Overwrite:    "always",
			},
			{
				ConfigMapRef: mcv1beta1.ConfigMapKeyRef{Name: "shared-config", Key: "render.conf"},
				Path:         "render.conf",
				Overwrite:    "always",
			},
		}),
	}

	_, refs, _ := buildConfigScript(server, plugins)

	// Same ConfigMap referenced twice should produce only one volume ref
	require.Len(t, refs, 1)
	assert.Equal(t, "shared-config", refs[0].ConfigMapName)
}

func TestBuildConfigScript_SubdirectoryPath(t *testing.T) {
	server := newConfigTestServer("test")
	plugins := []mcv1beta1.Plugin{
		newTestPlugin("BlueMap", "BlueMap", []mcv1beta1.PluginConfigFile{
			{
				ConfigMapRef: mcv1beta1.ConfigMapKeyRef{Name: "bluemap-defaults", Key: "overworld.conf"},
				Path:         "maps/overworld.conf",
				Overwrite:    "always",
			},
		}),
	}

	script, _, _ := buildConfigScript(server, plugins)

	assert.Contains(t, script, "mkdir -p /data/plugins/BlueMap/maps")
	assert.Contains(t, script, "cp /configs/cm-bluemap-defaults/overworld.conf /data/plugins/BlueMap/maps/overworld.conf")
}

func TestBuildConfigScript_ServerConfigSubdirectory(t *testing.T) {
	server := newConfigTestServer("test")
	server.Spec.ServerConfigs = []mcv1beta1.ServerConfigFile{
		{
			ConfigMapRef: mcv1beta1.ConfigMapKeyRef{Name: "mc-config", Key: "paper-global.yml"},
			Path:         "config/paper-global.yml",
			Overwrite:    "always",
		},
	}
	plugins := []mcv1beta1.Plugin{}

	script, _, _ := buildConfigScript(server, plugins)

	assert.Contains(t, script, "mkdir -p /data/config")
	assert.Contains(t, script, "cp /configs/cm-mc-config/paper-global.yml /data/config/paper-global.yml")
}

func TestBuildConfigScript_ServerOverrideForUnmatchedPlugin(t *testing.T) {
	server := newConfigTestServer("test")
	server.Spec.PluginConfigs = []mcv1beta1.ServerPluginConfig{
		{
			PluginName: "UnknownPlugin",
			Configs: []mcv1beta1.PluginConfigFile{
				{
					ConfigMapRef: mcv1beta1.ConfigMapKeyRef{Name: "unknown-config", Key: "config.yml"},
					Path:         "config.yml",
					Overwrite:    "always",
				},
			},
		},
	}
	plugins := []mcv1beta1.Plugin{}

	script, refs, warnings := buildConfigScript(server, plugins)

	// Unmatched plugin override should still produce config entries with a warning
	assert.Contains(t, script, "cp /configs/cm-unknown-config/config.yml")
	require.Len(t, refs, 1)
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], "UnknownPlugin")
}

func TestBuildConfigScript_ScriptStartsWithShebang(t *testing.T) {
	server := newConfigTestServer("test")
	plugins := []mcv1beta1.Plugin{
		newTestPlugin("BlueMap", "BlueMap", []mcv1beta1.PluginConfigFile{
			{
				ConfigMapRef: mcv1beta1.ConfigMapKeyRef{Name: "bluemap-defaults", Key: "core.conf"},
				Path:         "core.conf",
				Overwrite:    "always",
			},
		}),
	}

	script, _, _ := buildConfigScript(server, plugins)

	assert.True(t, len(script) > 0)
	assert.Contains(t, script, "#!/bin/sh")
	assert.Contains(t, script, "set -e")
}

func TestConfigMapVolumeName_Short(t *testing.T) {
	name := configMapVolumeName("my-config")
	assert.Equal(t, "cm-my-config", name)
}

func TestConfigMapVolumeName_Long(t *testing.T) {
	longName := "very-long-configmap-name-that-exceeds-the-maximum-kubernetes-volume-name-length"
	name := configMapVolumeName(longName)
	assert.LessOrEqual(t, len(name), maxVolumeNameLength)
	assert.True(t, len(name) > 0)
}

// --- buildConfigInjection tests ---

func TestBuildConfigInjection_NoConfigs(t *testing.T) {
	server := newConfigTestServer("test")
	plugins := []mcv1beta1.Plugin{}

	initContainer, volumes, scriptCM := buildConfigInjection(server, plugins)

	assert.Nil(t, initContainer)
	assert.Nil(t, volumes)
	assert.Nil(t, scriptCM)
}

func TestBuildConfigInjection_WithPluginConfig(t *testing.T) {
	server := newConfigTestServer("test")
	plugins := []mcv1beta1.Plugin{
		newTestPlugin("BlueMap", "BlueMap", []mcv1beta1.PluginConfigFile{
			{
				ConfigMapRef: mcv1beta1.ConfigMapKeyRef{Name: "bluemap-defaults", Key: "core.conf"},
				Path:         "core.conf",
				Overwrite:    "always",
			},
		}),
	}

	initContainer, volumes, scriptCM := buildConfigInjection(server, plugins)

	require.NotNil(t, initContainer)
	assert.Equal(t, "config-injector", initContainer.Name)
	assert.Equal(t, ConfigInjectorImage, initContainer.Image)
	assert.Equal(t, []string{"sh", "/scripts/inject-configs.sh"}, initContainer.Command)

	// Should have: data volume + config-script volume + 1 ConfigMap volume
	require.Len(t, volumes, 2) // config-script + 1 configmap

	// Init container should mount data, config-script, and the ConfigMap
	require.Len(t, initContainer.VolumeMounts, 3) // data + script + configmap

	// Verify data mount
	foundData := false
	for _, vm := range initContainer.VolumeMounts {
		if vm.Name == "data" && vm.MountPath == "/data" {
			foundData = true
		}
	}
	assert.True(t, foundData, "init container must mount data volume at /data")

	// Verify script ConfigMap
	require.NotNil(t, scriptCM)
	assert.Equal(t, "test-config-script", scriptCM.Name)
	assert.Contains(t, scriptCM.Data, configScriptKey)
	assert.Contains(t, scriptCM.Data[configScriptKey], "cp /configs/cm-bluemap-defaults/core.conf")
}

func TestBuildConfigInjection_WithServerConfig(t *testing.T) {
	server := newConfigTestServer("test")
	server.Spec.ServerConfigs = []mcv1beta1.ServerConfigFile{
		{
			ConfigMapRef: mcv1beta1.ConfigMapKeyRef{Name: "mc-config", Key: "server.properties"},
			Path:         "server.properties",
			Overwrite:    "always",
		},
	}
	plugins := []mcv1beta1.Plugin{}

	initContainer, volumes, scriptCM := buildConfigInjection(server, plugins)

	require.NotNil(t, initContainer)
	require.Len(t, volumes, 2)                    // config-script + 1 configmap
	require.Len(t, initContainer.VolumeMounts, 3) // data + script + configmap
	require.NotNil(t, scriptCM)
}

func TestBuildConfigInjection_ScriptConfigMapOwnership(t *testing.T) {
	server := newConfigTestServer("my-server")
	server.Spec.ServerConfigs = []mcv1beta1.ServerConfigFile{
		{
			ConfigMapRef: mcv1beta1.ConfigMapKeyRef{Name: "mc-config", Key: "server.properties"},
			Path:         "server.properties",
		},
	}

	_, _, scriptCM := buildConfigInjection(server, nil)

	require.NotNil(t, scriptCM)
	assert.Equal(t, "my-server-config-script", scriptCM.Name)
	assert.Equal(t, "default", scriptCM.Namespace)
}

func TestBuildConfigInjection_InitContainerSecurityDefaults(t *testing.T) {
	server := newConfigTestServer("test")
	server.Spec.ServerConfigs = []mcv1beta1.ServerConfigFile{
		{
			ConfigMapRef: mcv1beta1.ConfigMapKeyRef{Name: "mc-config", Key: "server.properties"},
			Path:         "server.properties",
		},
	}

	initContainer, _, _ := buildConfigInjection(server, nil)

	require.NotNil(t, initContainer)
	assert.Equal(t, "config-injector", initContainer.Name)
	assert.Equal(t, ConfigInjectorImage, initContainer.Image)

	// SecurityContext hardening: privilege escalation blocked, read-only root, all caps dropped.
	require.NotNil(t, initContainer.SecurityContext, "init container must have SecurityContext")
	assert.Equal(t, boolPtr(false), initContainer.SecurityContext.AllowPrivilegeEscalation)
	assert.Equal(t, boolPtr(true), initContainer.SecurityContext.ReadOnlyRootFilesystem)
	require.NotNil(t, initContainer.SecurityContext.Capabilities)
	assert.Contains(t, initContainer.SecurityContext.Capabilities.Drop, corev1.Capability("ALL"))
}

func TestBuildConfigInjection_MultipleConfigMaps(t *testing.T) {
	server := newConfigTestServer("test")
	server.Spec.ServerConfigs = []mcv1beta1.ServerConfigFile{
		{
			ConfigMapRef: mcv1beta1.ConfigMapKeyRef{Name: "config-a", Key: "file-a"},
			Path:         "file-a",
		},
		{
			ConfigMapRef: mcv1beta1.ConfigMapKeyRef{Name: "config-b", Key: "file-b"},
			Path:         "file-b",
		},
	}

	_, volumes, _ := buildConfigInjection(server, nil)

	// config-script + 2 configmap volumes
	require.Len(t, volumes, 3)
}

// --- collectReferencedConfigMaps tests ---

func TestCollectReferencedConfigMaps_NoConfigs(t *testing.T) {
	server := newConfigTestServer("test")
	refs := collectReferencedConfigMaps(server, nil)
	assert.Empty(t, refs)
}

func TestCollectReferencedConfigMaps_PluginConfigs(t *testing.T) {
	server := newConfigTestServer("test")
	plugins := []mcv1beta1.Plugin{
		newTestPlugin("BlueMap", "BlueMap", []mcv1beta1.PluginConfigFile{
			{
				ConfigMapRef: mcv1beta1.ConfigMapKeyRef{Name: "bluemap-defaults", Key: "core.conf"},
				Path:         "core.conf",
			},
		}),
	}

	refs := collectReferencedConfigMaps(server, plugins)
	require.Len(t, refs, 1)
	assert.Equal(t, "bluemap-defaults", refs[0].Name)
	assert.Equal(t, "core.conf", refs[0].Key)
}

func TestCollectReferencedConfigMaps_ServerConfigs(t *testing.T) {
	server := newConfigTestServer("test")
	server.Spec.ServerConfigs = []mcv1beta1.ServerConfigFile{
		{
			ConfigMapRef: mcv1beta1.ConfigMapKeyRef{Name: "mc-config", Key: "server.properties"},
			Path:         "server.properties",
		},
	}

	refs := collectReferencedConfigMaps(server, nil)
	require.Len(t, refs, 1)
	assert.Equal(t, "mc-config", refs[0].Name)
}

func TestCollectReferencedConfigMaps_Deduplicates(t *testing.T) {
	server := newConfigTestServer("test")
	plugins := []mcv1beta1.Plugin{
		newTestPlugin("BlueMap", "BlueMap", []mcv1beta1.PluginConfigFile{
			{
				ConfigMapRef: mcv1beta1.ConfigMapKeyRef{Name: "shared-config", Key: "core.conf"},
				Path:         "core.conf",
			},
			{
				ConfigMapRef: mcv1beta1.ConfigMapKeyRef{Name: "shared-config", Key: "render.conf"},
				Path:         "render.conf",
			},
		}),
	}

	refs := collectReferencedConfigMaps(server, plugins)
	// Two files from same ConfigMap but different keys => 2 refs (since they're different key refs)
	require.Len(t, refs, 2)
}

func TestBuildRCONPropertiesScript_Enabled(t *testing.T) {
	server := &mcv1beta1.PaperMCServer{
		Spec: mcv1beta1.PaperMCServerSpec{
			RCON: mcv1beta1.RCONConfig{
				Enabled: true,
				Port:    25575,
			},
		},
	}

	script := buildRCONPropertiesScript(server)
	assert.Contains(t, script, "enable-rcon=true",
		"script should set enable-rcon to true")
	assert.Contains(t, script, "rcon.port=25575",
		"script should set rcon.port")
	assert.Contains(t, script, "server.properties",
		"script should target server.properties")
}

func TestBuildRCONPropertiesScript_Disabled(t *testing.T) {
	server := &mcv1beta1.PaperMCServer{
		Spec: mcv1beta1.PaperMCServerSpec{
			RCON: mcv1beta1.RCONConfig{
				Enabled: false,
			},
		},
	}

	script := buildRCONPropertiesScript(server)
	assert.Empty(t, script, "no RCON script when RCON disabled")
}

func TestBuildRCONPropertiesScript_DefaultPort(t *testing.T) {
	server := &mcv1beta1.PaperMCServer{
		Spec: mcv1beta1.PaperMCServerSpec{
			RCON: mcv1beta1.RCONConfig{
				Enabled: true,
				Port:    0, // should default to 25575
			},
		},
	}

	script := buildRCONPropertiesScript(server)
	assert.Contains(t, script, "rcon.port=25575")
}

func TestBuildConfigScript_IncludesRCON(t *testing.T) {
	server := &mcv1beta1.PaperMCServer{
		Spec: mcv1beta1.PaperMCServerSpec{
			RCON: mcv1beta1.RCONConfig{
				Enabled: true,
				Port:    25575,
			},
		},
	}

	script, _, _ := buildConfigScript(server, nil)
	assert.Contains(t, script, "enable-rcon=true",
		"buildConfigScript should include RCON injection even without config entries")
}
