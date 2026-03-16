/*
Copyright 2026, Aleksei Sviridkin.

SPDX-License-Identifier: BSD-3-Clause
*/

package controller

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	mcv1beta1 "github.com/lexfrei/minecraft-operator/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// maxVolumeNameLength is the maximum length for Kubernetes volume names (DNS label).
	maxVolumeNameLength = 63

	// volumeNameHashSuffixLength is the length of the hash suffix for truncated volume names.
	volumeNameHashSuffixLength = 9

	// configMountBase is the base path for ConfigMap mounts in the init container.
	configMountBase = "/configs"

	// dataMountPath is the mount path for the data PVC.
	dataMountPath = "/data"

	// configScriptPath is the mount path for the config injection script.
	configScriptPath = "/scripts"

	// configScriptKey is the key in the script ConfigMap.
	configScriptKey = "inject-configs.sh"
)

// configMapVolumeRef tracks a ConfigMap that needs to be mounted as a volume.
type configMapVolumeRef struct {
	// VolumeName is the sanitized volume name: cm-{configmap-name} (max 63 chars).
	VolumeName string
	// ConfigMapName is the original ConfigMap name.
	ConfigMapName string
	// MountPath is the mount path: /configs/{volumeName}.
	MountPath string
}

// configEntry represents a single file to be copied by the init container.
type configEntry struct {
	// sourcePath is the full source path in the ConfigMap mount (e.g., /configs/cm-bluemap-defaults/core.conf).
	sourcePath string
	// targetPath is the full target path (e.g., /data/plugins/BlueMap/core.conf).
	targetPath string
	// overwrite is the overwrite policy ("always" or "ifNotExists").
	overwrite string
	// comment describes the config entry for the generated script.
	comment string
}

// configMapVolumeName generates a sanitized volume name for a ConfigMap.
// Format: cm-{configmap-name}, truncated to maxVolumeNameLength with hash suffix if needed.
func configMapVolumeName(configMapName string) string {
	name := "cm-" + configMapName

	if len(name) <= maxVolumeNameLength {
		return name
	}

	hash := sha256.Sum256([]byte(name))
	suffix := hex.EncodeToString(hash[:4])

	return name[:maxVolumeNameLength-volumeNameHashSuffixLength] + "-" + suffix
}

// resolvePluginDirName returns the effective directory name for a plugin.
// Uses pluginDirName if set, otherwise falls back to source.project.
func resolvePluginDirName(plugin *mcv1beta1.Plugin) string {
	if plugin.Spec.PluginDirName != "" {
		return plugin.Spec.PluginDirName
	}

	return plugin.Spec.Source.Project
}

// buildConfigScript generates a shell script that copies config files from ConfigMap
// volumes to the appropriate locations in the data PVC.
//
// It collects configs from:
//  1. Matched plugins' spec.configs (defaults)
//  2. Server's spec.pluginConfigs (overrides, same path replaces plugin default)
//  3. Server's spec.serverConfigs (server-level files)
//
// Returns:
//   - script: the shell script content (empty if no configs)
//   - refs: list of unique ConfigMap volume references needed
//   - warnings: list of warning messages (e.g., unmatched plugin overrides)
func buildConfigScript(
	server *mcv1beta1.PaperMCServer,
	matchedPlugins []mcv1beta1.Plugin,
) (string, []configMapVolumeRef, []string) {
	refMap := make(map[string]configMapVolumeRef)
	serverOverrides := buildServerOverridesMap(server)

	entries, consumedOverrides := collectPluginConfigEntries(matchedPlugins, serverOverrides, refMap)

	// Process unmatched server plugin overrides.
	unmatchedEntries, warnings := collectUnmatchedOverrideEntries(serverOverrides, consumedOverrides, refMap)
	entries = append(entries, unmatchedEntries...)

	// Process server-level configs.
	entries = append(entries, collectServerConfigEntries(server, refMap)...)

	if len(entries) == 0 {
		return "", nil, nil
	}

	script := renderConfigScript(entries)
	refs := sortedConfigMapRefs(refMap)

	return script, refs, warnings
}

// buildServerOverridesMap builds a lookup of server plugin config overrides by plugin name.
func buildServerOverridesMap(
	server *mcv1beta1.PaperMCServer,
) map[string]map[string]mcv1beta1.PluginConfigFile {
	overrides := make(map[string]map[string]mcv1beta1.PluginConfigFile)

	for _, pc := range server.Spec.PluginConfigs {
		if _, exists := overrides[pc.PluginName]; !exists {
			overrides[pc.PluginName] = make(map[string]mcv1beta1.PluginConfigFile)
		}

		for _, cfg := range pc.Configs {
			overrides[pc.PluginName][cfg.Path] = cfg
		}
	}

	return overrides
}

// collectPluginConfigEntries processes matched plugins and their config files,
// applying server overrides where applicable.
func collectPluginConfigEntries(
	matchedPlugins []mcv1beta1.Plugin,
	serverOverrides map[string]map[string]mcv1beta1.PluginConfigFile,
	refMap map[string]configMapVolumeRef,
) ([]configEntry, map[string]bool) {
	var entries []configEntry

	consumedOverrides := make(map[string]bool)

	for i := range matchedPlugins {
		plugin := &matchedPlugins[i]
		dirName := resolvePluginDirName(plugin)

		if dirName == "" || len(plugin.Spec.Configs) == 0 {
			if overrides, exists := serverOverrides[plugin.Name]; exists {
				consumedOverrides[plugin.Name] = true

				for _, cfg := range overrides {
					entry, ref := buildPluginConfigEntry(dirName, cfg)
					entries = append(entries, entry)
					refMap[ref.ConfigMapName] = ref
				}
			}

			continue
		}

		consumedOverrides[plugin.Name] = true
		pluginEntries := resolvePluginConfigs(plugin, dirName, serverOverrides, refMap)
		entries = append(entries, pluginEntries...)
	}

	return entries, consumedOverrides
}

// resolvePluginConfigs resolves config entries for a single plugin with server overrides.
func resolvePluginConfigs(
	plugin *mcv1beta1.Plugin,
	dirName string,
	serverOverrides map[string]map[string]mcv1beta1.PluginConfigFile,
	refMap map[string]configMapVolumeRef,
) []configEntry {
	var entries []configEntry

	for _, pluginCfg := range plugin.Spec.Configs {
		effectiveCfg := pluginCfg
		if overrides, exists := serverOverrides[plugin.Name]; exists {
			if override, found := overrides[pluginCfg.Path]; found {
				effectiveCfg = override
			}
		}

		entry, ref := buildPluginConfigEntry(dirName, effectiveCfg)
		entries = append(entries, entry)
		refMap[ref.ConfigMapName] = ref
	}

	// Add server overrides for paths not in plugin defaults.
	if overrides, exists := serverOverrides[plugin.Name]; exists {
		for path, cfg := range overrides {
			if !pluginHasConfigPath(plugin, path) {
				entry, ref := buildPluginConfigEntry(dirName, cfg)
				entries = append(entries, entry)
				refMap[ref.ConfigMapName] = ref
			}
		}
	}

	return entries
}

// pluginHasConfigPath checks if a plugin has a config file at the given path.
func pluginHasConfigPath(plugin *mcv1beta1.Plugin, path string) bool {
	for _, cfg := range plugin.Spec.Configs {
		if cfg.Path == path {
			return true
		}
	}

	return false
}

// collectUnmatchedOverrideEntries processes server plugin config overrides for plugins
// not in the matched set. Returns entries and warning messages.
func collectUnmatchedOverrideEntries(
	serverOverrides map[string]map[string]mcv1beta1.PluginConfigFile,
	consumedOverrides map[string]bool,
	refMap map[string]configMapVolumeRef,
) ([]configEntry, []string) {
	var entries []configEntry
	var warnings []string

	for pluginName, overrides := range serverOverrides {
		if consumedOverrides[pluginName] {
			continue
		}

		warnings = append(warnings,
			fmt.Sprintf("pluginConfigs references plugin %q which is not matched to this server", pluginName))

		for _, cfg := range overrides {
			entry, ref := buildPluginConfigEntry(pluginName, cfg)
			entries = append(entries, entry)
			refMap[ref.ConfigMapName] = ref
		}
	}

	return entries, warnings
}

// collectServerConfigEntries processes server-level config files.
func collectServerConfigEntries(
	server *mcv1beta1.PaperMCServer,
	refMap map[string]configMapVolumeRef,
) []configEntry {
	entries := make([]configEntry, 0, len(server.Spec.ServerConfigs))

	for _, cfg := range server.Spec.ServerConfigs {
		volName := configMapVolumeName(cfg.ConfigMapRef.Name)
		sourcePath := fmt.Sprintf("%s/%s/%s", configMountBase, volName, cfg.ConfigMapRef.Key)
		targetPath := fmt.Sprintf("%s/%s", dataMountPath, cfg.Path)
		overwrite := cfg.Overwrite
		if overwrite == "" {
			overwrite = "always"
		}

		entries = append(entries, configEntry{
			sourcePath: sourcePath,
			targetPath: targetPath,
			overwrite:  overwrite,
			comment:    fmt.Sprintf("Server config: %s (overwrite=%s)", cfg.Path, overwrite),
		})

		ref := configMapVolumeRef{
			VolumeName:    volName,
			ConfigMapName: cfg.ConfigMapRef.Name,
			MountPath:     fmt.Sprintf("%s/%s", configMountBase, volName),
		}
		refMap[ref.ConfigMapName] = ref
	}

	return entries
}

// renderConfigScript generates the shell script from config entries.
func renderConfigScript(entries []configEntry) string {
	var sb strings.Builder

	sb.WriteString("#!/bin/sh\nset -e\n")

	for _, entry := range entries {
		fmt.Fprintf(&sb, "# %s\n", entry.comment)

		dir := filepath.Dir(entry.targetPath)
		if dir != "." && dir != "/" {
			fmt.Fprintf(&sb, "mkdir -p %s\n", dir)
		}

		if entry.overwrite == "ifNotExists" {
			fmt.Fprintf(&sb, "if [ ! -f %s ]; then\n", entry.targetPath)
			fmt.Fprintf(&sb, "  cp %s %s\n", entry.sourcePath, entry.targetPath)
			sb.WriteString("fi\n")
		} else {
			fmt.Fprintf(&sb, "cp %s %s\n", entry.sourcePath, entry.targetPath)
		}
	}

	return sb.String()
}

// sortedConfigMapRefs extracts and sorts ConfigMap volume refs from the map.
func sortedConfigMapRefs(refMap map[string]configMapVolumeRef) []configMapVolumeRef {
	refs := make([]configMapVolumeRef, 0, len(refMap))
	for _, ref := range refMap {
		refs = append(refs, ref)
	}

	sort.Slice(refs, func(i, j int) bool {
		return refs[i].ConfigMapName < refs[j].ConfigMapName
	})

	return refs
}

// buildConfigInjection constructs the init container, volumes, and script ConfigMap
// needed for config injection. Returns nil for all values if no configs are defined.
func buildConfigInjection(
	server *mcv1beta1.PaperMCServer,
	matchedPlugins []mcv1beta1.Plugin,
) (*corev1.Container, []corev1.Volume, *corev1.ConfigMap) {
	script, refs, _ := buildConfigScript(server, matchedPlugins)
	if script == "" {
		return nil, nil, nil
	}

	scriptCMName := server.Name + "-config-script"
	scriptCM := buildScriptConfigMap(scriptCMName, server.Namespace, server.Name, script)
	volumes := buildConfigVolumes(scriptCMName, refs)
	mounts := buildConfigVolumeMounts(refs)

	initContainer := &corev1.Container{
		Name:         "config-injector",
		Image:        "busybox:1.37",
		Command:      []string{"sh", configScriptPath + "/" + configScriptKey},
		VolumeMounts: mounts,
	}

	return initContainer, volumes, scriptCM
}

// buildScriptConfigMap creates the ConfigMap containing the config injection script.
func buildScriptConfigMap(name, namespace, serverName, script string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    standardLabels(serverName, "config"),
		},
		Data: map[string]string{
			configScriptKey: script,
		},
	}
}

// buildConfigVolumes creates volumes for the script ConfigMap and referenced ConfigMaps.
func buildConfigVolumes(scriptCMName string, refs []configMapVolumeRef) []corev1.Volume {
	volumes := make([]corev1.Volume, 0, len(refs)+1)
	defaultMode := int32(0o755)

	volumes = append(volumes, corev1.Volume{
		Name: "config-script",
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: scriptCMName},
				DefaultMode:          &defaultMode,
			},
		},
	})

	for _, ref := range refs {
		volumes = append(volumes, corev1.Volume{
			Name: ref.VolumeName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: ref.ConfigMapName},
				},
			},
		})
	}

	return volumes
}

// buildConfigVolumeMounts creates volume mounts for the config injector init container.
func buildConfigVolumeMounts(refs []configMapVolumeRef) []corev1.VolumeMount {
	mounts := make([]corev1.VolumeMount, 0, len(refs)+2)

	mounts = append(mounts,
		corev1.VolumeMount{Name: "data", MountPath: dataMountPath},
		corev1.VolumeMount{Name: "config-script", MountPath: configScriptPath},
	)

	for _, ref := range refs {
		mounts = append(mounts, corev1.VolumeMount{
			Name:      ref.VolumeName,
			MountPath: ref.MountPath,
		})
	}

	return mounts
}

// collectReferencedConfigMaps returns all unique ConfigMapKeyRef entries referenced by
// plugin configs, server plugin config overrides, and server configs.
// Used for pre-flight validation of ConfigMap existence.
func collectReferencedConfigMaps(
	server *mcv1beta1.PaperMCServer,
	matchedPlugins []mcv1beta1.Plugin,
) []mcv1beta1.ConfigMapKeyRef {
	seen := make(map[string]bool)
	var refs []mcv1beta1.ConfigMapKeyRef

	addRef := func(ref mcv1beta1.ConfigMapKeyRef) {
		key := ref.Name + "/" + ref.Key
		if !seen[key] {
			seen[key] = true
			refs = append(refs, ref)
		}
	}

	for i := range matchedPlugins {
		for _, cfg := range matchedPlugins[i].Spec.Configs {
			addRef(cfg.ConfigMapRef)
		}
	}

	for _, pc := range server.Spec.PluginConfigs {
		for _, cfg := range pc.Configs {
			addRef(cfg.ConfigMapRef)
		}
	}

	for _, cfg := range server.Spec.ServerConfigs {
		addRef(cfg.ConfigMapRef)
	}

	return refs
}

// buildPluginConfigEntry creates a configEntry and configMapVolumeRef for a plugin config file.
func buildPluginConfigEntry(
	dirName string,
	cfg mcv1beta1.PluginConfigFile,
) (configEntry, configMapVolumeRef) {
	volName := configMapVolumeName(cfg.ConfigMapRef.Name)
	sourcePath := fmt.Sprintf("%s/%s/%s", configMountBase, volName, cfg.ConfigMapRef.Key)
	targetPath := fmt.Sprintf("%s/plugins/%s/%s", dataMountPath, dirName, cfg.Path)

	overwrite := cfg.Overwrite
	if overwrite == "" {
		overwrite = "always"
	}

	entry := configEntry{
		sourcePath: sourcePath,
		targetPath: targetPath,
		overwrite:  overwrite,
		comment:    fmt.Sprintf("Plugin: %s, file: %s (overwrite=%s)", dirName, cfg.Path, overwrite),
	}

	ref := configMapVolumeRef{
		VolumeName:    volName,
		ConfigMapName: cfg.ConfigMapRef.Name,
		MountPath:     fmt.Sprintf("%s/%s", configMountBase, volName),
	}

	return entry, ref
}
