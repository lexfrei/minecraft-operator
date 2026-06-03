/*
Copyright 2026, Aleksei Sviridkin.

SPDX-License-Identifier: BSD-3-Clause
*/

package controller

// Update strategy constants (unified for both Plugin and PaperMCServer).
const (
	updateStrategyLatest   = "latest"
	updateStrategyAuto     = "auto"
	updateStrategyPin      = "pin"
	updateStrategyBuildPin = "build-pin"
	// Legacy constants for backward compatibility.
	versionPolicyLatest = "latest"
	versionPolicyPinned = "pinned"
)

// Finalizer and annotation constants.
const (
	// PluginFinalizer is the finalizer added to Plugin resources to ensure JAR cleanup.
	PluginFinalizer = "mc.k8s.lex.la/plugin-cleanup"

	// AnnotationApplyNow triggers immediate update application, bypassing maintenance window.
	// Value should be a Unix timestamp (seconds since epoch) of when the annotation was set.
	// Annotations older than 5 minutes are ignored to prevent stale triggers.
	AnnotationApplyNow = "mc.k8s.lex.la/apply-now"

	// AnnotationBackupNow triggers immediate VolumeSnapshot backup.
	// Value should be a Unix timestamp (seconds since epoch) of when the annotation was set.
	// Annotations older than 5 minutes are ignored to prevent stale triggers.
	AnnotationBackupNow = "mc.k8s.lex.la/backup-now"
)

// Shared string literals used across production code and tests. Extracted to
// satisfy goconst (repeated literals) and to keep recurring values in one place.
const (
	gcPluginUpdateDir        = "/data/plugins/update"
	gcCronEvery6h            = "0 */6 * * *"
	gcCronDaily3am           = "0 3 * * *"
	gcCronWeekly             = "0 4 * * 0"
	gcVersion100             = "1.0.0"
	gcVersion120             = "1.20"
	gcVersion1204            = "1.20.4"
	gcVersion121             = "1.21"
	gcVersion1210            = "1.21.0"
	gcVersion1211            = "1.21.1"
	gcVersion1211Build91     = "1.21.1-91"
	gcVersion1213            = "1.21.3"
	gcVersion1214            = "1.21.4"
	gcCIDR10                 = "10.0.0.0/8"
	gcPodIP                  = "10.0.0.1"
	gcVersion200             = "2.0.0"
	gcVersion2212            = "2.21.2"
	gcChecksumAAA            = "aaa"
	gcChecksumABC            = "abc123"
	gcAllGood                = "AllGood"
	gcOverwriteAlways        = "always"
	gcLabelApp               = "app"
	gcPluginBluemap          = "bluemap"
	gcBluemapDefaults        = "bluemap-defaults"
	gcConfigScript           = "config-script"
	gcConfigYML              = "config.yml"
	gcCoreConf               = "core.conf"
	gcVolumeData             = "data"
	gcDataTest0              = "data-test-0"
	gcNamespaceDefault       = "default"
	gcImageDocker1211Build91 = "docker.io/lexfrei/papermc:1.21.1-91"
	gcPluginDynmap           = "dynmap"
	gcGameGateway            = "game-gateway"
	gcGatewayNS              = "gw-ns"
	gcGatewaySystemNS        = "gateway-system"
	gcMapHostname            = "map.example.com"
	gcCmdMkdir               = "mkdir"
	gcSourceHangar           = "hangar"
	gcProtocolHTTPLower      = "http"
	gcProtocolHTTP           = "HTTP"
	gcURLPluginV2            = "https://example.com/plugin-v2.jar"
	gcURLPlugin              = "https://example.com/plugin.jar"
	gcURLV1                  = "https://example.com/v1.jar"
	gcOverwriteIfNotExists   = "ifNotExists"
	gcLabelMetadataName      = "kubernetes.io/metadata.name"
	gcImage1211Build100      = "lexfrei/papermc:1.21.1-100"
	gcImage1211Build91       = "lexfrei/papermc:1.21.1-91"
	gcImageLatest            = "lexfrei/papermc:latest"
	gcMCConfig               = "mc-config"
	gcLabelServerName        = "mc.k8s.lex.la/server-name"
	gcNamespaceMinecraft     = "minecraft"
	gcMinecraftOperator      = "minecraft-operator"
	gcMyGateway              = "my-gateway"
	gcMyPlugin               = "my-plugin"
	gcMyServer               = "my-server"
	gcNonexistent            = "nonexistent"
	gcNamespace2             = "ns2"
	gcPassword               = "password"
	gcPluginA                = "plugin-a"
	gcPluginB                = "plugin-b"
	gcRCON                   = "rcon"
	gcRCONSecret             = "rcon-secret"
	gcRenderConf             = "render.conf"
	gcServerA                = "server-a"
	gcServerB                = "server-b"
	gcServerBackup100        = "server-backup-100"
	gcServerProperties       = "server.properties"
	gcServer1                = "server1"
	gcSharedConfig           = "shared-config"
	gcTest                   = "test"
	gcTestPlugin             = "test-plugin"
	gcTestPluginCamel        = "TestPlugin"
	gcTrue                   = "true"
	gcProtocolUDP            = "UDP"
	gcSourceURL              = "url"
	gcVersionResolvedMsg     = "Version resolved"
	gcWebUI                  = "web-ui"
)
