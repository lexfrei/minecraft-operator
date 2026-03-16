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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	sourceTypeURL       = "url"
	strategyLatest      = "latest"
	strategyAuto        = "auto"
	strategyPin         = "pin"
	strategyBuildPin    = "build-pin"
	testPluginVersion   = "2.5.0"
	testExampleJARURL   = "https://example.com/plugin.jar"
	testExampleChecksum = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	testPluginDirName   = "BlueMap"
)

func validPlugin() *Plugin {
	return &Plugin{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-plugin",
			Namespace: "default",
		},
		Spec: PluginSpec{
			Source: PluginSource{
				Type:    "hangar",
				Project: "BlueMap",
			},
			UpdateStrategy:   "latest",
			InstanceSelector: metav1.LabelSelector{},
		},
	}
}

func TestPluginValidateCreate_ValidHangar(t *testing.T) {
	v := &PluginValidator{}
	p := validPlugin()

	warnings, err := v.ValidateCreate(context.Background(), p)
	require.NoError(t, err)
	assert.Empty(t, warnings)
}

func TestPluginValidateCreate_ValidURL(t *testing.T) {
	v := &PluginValidator{}
	p := validPlugin()
	p.Spec.Source.Type = sourceTypeURL
	p.Spec.Source.Project = ""
	p.Spec.Source.URL = testExampleJARURL
	p.Spec.Source.Checksum = testExampleChecksum

	warnings, err := v.ValidateCreate(context.Background(), p)
	require.NoError(t, err)
	assert.Empty(t, warnings)
}

func TestPluginValidateCreate_HangarMissingProject(t *testing.T) {
	v := &PluginValidator{}
	p := validPlugin()
	p.Spec.Source.Project = ""

	_, err := v.ValidateCreate(context.Background(), p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "project")
}

func TestPluginValidateCreate_URLMissingURL(t *testing.T) {
	v := &PluginValidator{}
	p := validPlugin()
	p.Spec.Source.Type = sourceTypeURL
	p.Spec.Source.Project = ""

	_, err := v.ValidateCreate(context.Background(), p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "url")
}

func TestPluginValidateCreate_URLNotHTTPS(t *testing.T) {
	v := &PluginValidator{}
	p := validPlugin()
	p.Spec.Source.Type = sourceTypeURL
	p.Spec.Source.Project = ""
	p.Spec.Source.URL = "http://example.com/plugin.jar"

	_, err := v.ValidateCreate(context.Background(), p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "https")
}

func TestPluginValidateCreate_PinMissingVersion(t *testing.T) {
	v := &PluginValidator{}
	p := validPlugin()
	p.Spec.UpdateStrategy = strategyPin

	_, err := v.ValidateCreate(context.Background(), p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "version")
}

func TestPluginValidateCreate_PinWithVersion(t *testing.T) {
	v := &PluginValidator{}
	p := validPlugin()
	p.Spec.UpdateStrategy = strategyPin
	p.Spec.Version = testPluginVersion

	warnings, err := v.ValidateCreate(context.Background(), p)
	require.NoError(t, err)
	assert.Empty(t, warnings)
}

func TestPluginValidateCreate_BuildPinMissingVersion(t *testing.T) {
	v := &PluginValidator{}
	p := validPlugin()
	p.Spec.UpdateStrategy = strategyBuildPin

	_, err := v.ValidateCreate(context.Background(), p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "version")
}

func TestPluginValidateCreate_BuildPinMissingBuild(t *testing.T) {
	v := &PluginValidator{}
	p := validPlugin()
	p.Spec.UpdateStrategy = strategyBuildPin
	p.Spec.Version = testPluginVersion

	_, err := v.ValidateCreate(context.Background(), p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "build")
}

func TestPluginValidateCreate_BuildPinValid(t *testing.T) {
	v := &PluginValidator{}
	p := validPlugin()
	p.Spec.UpdateStrategy = strategyBuildPin
	p.Spec.Version = testPluginVersion
	build := 42
	p.Spec.Build = &build

	warnings, err := v.ValidateCreate(context.Background(), p)
	require.NoError(t, err)
	assert.Empty(t, warnings)
}

func TestPluginValidateCreate_URLWithoutChecksumWarns(t *testing.T) {
	v := &PluginValidator{}
	p := validPlugin()
	p.Spec.Source.Type = sourceTypeURL
	p.Spec.Source.Project = ""
	p.Spec.Source.URL = testExampleJARURL
	p.Spec.Source.Checksum = ""

	warnings, err := v.ValidateCreate(context.Background(), p)
	require.NoError(t, err)
	assert.NotEmpty(t, warnings, "should warn about missing checksum")
}

func TestPluginValidateUpdate_Valid(t *testing.T) {
	v := &PluginValidator{}
	oldP := validPlugin()
	newP := validPlugin()
	newP.Spec.Source.Project = "OtherPlugin"

	warnings, err := v.ValidateUpdate(context.Background(), oldP, newP)
	require.NoError(t, err)
	assert.Empty(t, warnings)
}

func TestPluginValidateUpdate_InvalidNewSpec(t *testing.T) {
	v := &PluginValidator{}
	oldP := validPlugin()
	newP := validPlugin()
	newP.Spec.Source.Project = ""

	_, err := v.ValidateUpdate(context.Background(), oldP, newP)
	require.Error(t, err)
}

// Issue 2: url.Parse accepts "https://" (no host) as valid.
func TestPluginValidateCreate_URLHttpsNoHost(t *testing.T) {
	v := &PluginValidator{}
	p := validPlugin()
	p.Spec.Source.Type = sourceTypeURL
	p.Spec.Source.Project = ""
	p.Spec.Source.URL = "https://"

	_, err := v.ValidateCreate(context.Background(), p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "host")
}

// Issue 5: URL without .jar extension should produce a warning.
func TestPluginValidateCreate_URLWithoutJarExtensionWarns(t *testing.T) {
	v := &PluginValidator{}
	p := validPlugin()
	p.Spec.Source.Type = sourceTypeURL
	p.Spec.Source.Project = ""
	p.Spec.Source.URL = "https://example.com/plugin"
	p.Spec.Source.Checksum = testExampleChecksum

	warnings, err := v.ValidateCreate(context.Background(), p)
	require.NoError(t, err)
	assert.NotEmpty(t, warnings, "should warn when URL path does not end in .jar")
}

// Issue 6: auto strategy with version should be valid (no error).
func TestPluginValidateCreate_AutoStrategyWithVersion(t *testing.T) {
	v := &PluginValidator{}
	p := validPlugin()
	p.Spec.UpdateStrategy = strategyAuto
	p.Spec.Version = testPluginVersion

	warnings, err := v.ValidateCreate(context.Background(), p)
	require.NoError(t, err)
	assert.Empty(t, warnings)
}

// Issue 6: latest strategy should be valid (no error).
func TestPluginValidateCreate_LatestStrategyValid(t *testing.T) {
	v := &PluginValidator{}
	p := validPlugin()
	p.Spec.UpdateStrategy = strategyLatest

	warnings, err := v.ValidateCreate(context.Background(), p)
	require.NoError(t, err)
	assert.Empty(t, warnings)
}

// Issue 3 (round 2): invalid checksum format should be rejected.
func TestPluginValidateCreate_URLInvalidChecksumFormat(t *testing.T) {
	v := &PluginValidator{}
	p := validPlugin()
	p.Spec.Source.Type = sourceTypeURL
	p.Spec.Source.Project = ""
	p.Spec.Source.URL = testExampleJARURL
	p.Spec.Source.Checksum = "not-a-sha256"

	_, err := v.ValidateCreate(context.Background(), p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "checksum")
}

// Valid checksum should pass.
func TestPluginValidateCreate_URLValidChecksumFormat(t *testing.T) {
	v := &PluginValidator{}
	p := validPlugin()
	p.Spec.Source.Type = sourceTypeURL
	p.Spec.Source.Project = ""
	p.Spec.Source.URL = testExampleJARURL
	p.Spec.Source.Checksum = testExampleChecksum

	warnings, err := v.ValidateCreate(context.Background(), p)
	require.NoError(t, err)
	assert.Empty(t, warnings)
}

// Uppercase checksum should be accepted (normalized to lowercase).
func TestPluginValidateCreate_URLUppercaseChecksumAccepted(t *testing.T) {
	v := &PluginValidator{}
	p := validPlugin()
	p.Spec.Source.Type = sourceTypeURL
	p.Spec.Source.Project = ""
	p.Spec.Source.URL = testExampleJARURL
	p.Spec.Source.Checksum = "E3B0C44298FC1C149AFBF4C8996FB92427AE41E4649B934CA495991B7852B855"

	warnings, err := v.ValidateCreate(context.Background(), p)
	require.NoError(t, err)
	assert.Empty(t, warnings)
}

// Hangar source with extra url/checksum fields should still pass (fields are ignored).
func TestPluginValidateCreate_HangarWithExtraURLFields(t *testing.T) {
	v := &PluginValidator{}
	p := validPlugin()
	p.Spec.Source.URL = testExampleJARURL
	p.Spec.Source.Checksum = testExampleChecksum

	warnings, err := v.ValidateCreate(context.Background(), p)
	require.NoError(t, err)
	assert.Empty(t, warnings)
}

func TestPluginValidateDelete_AlwaysAllowed(t *testing.T) {
	v := &PluginValidator{}
	p := validPlugin()

	warnings, err := v.ValidateDelete(context.Background(), p)
	require.NoError(t, err)
	assert.Empty(t, warnings)
}

// --- Config validation tests ---

func TestPluginValidateCreate_ConfigsValid(t *testing.T) {
	v := &PluginValidator{}
	p := validPlugin()
	p.Spec.PluginDirName = testPluginDirName
	p.Spec.Configs = []PluginConfigFile{
		{
			ConfigMapRef: ConfigMapKeyRef{Name: "bluemap-defaults", Key: "core.conf"},
			Path:         "core.conf",
			Overwrite:    "always",
		},
	}

	warnings, err := v.ValidateCreate(context.Background(), p)
	require.NoError(t, err)
	assert.Empty(t, warnings)
}

func TestPluginValidateCreate_ConfigsWithoutPluginDirName(t *testing.T) {
	v := &PluginValidator{}
	p := validPlugin()
	p.Spec.Configs = []PluginConfigFile{
		{
			ConfigMapRef: ConfigMapKeyRef{Name: "test", Key: "test"},
			Path:         "core.conf",
		},
	}

	_, err := v.ValidateCreate(context.Background(), p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pluginDirName")
}

func TestPluginValidateCreate_ConfigPathTraversal(t *testing.T) {
	v := &PluginValidator{}
	p := validPlugin()
	p.Spec.PluginDirName = testPluginDirName
	p.Spec.Configs = []PluginConfigFile{
		{
			ConfigMapRef: ConfigMapKeyRef{Name: "test", Key: "test"},
			Path:         "../../../etc/passwd",
		},
	}

	_, err := v.ValidateCreate(context.Background(), p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path")
}

func TestPluginValidateCreate_ConfigAbsolutePath(t *testing.T) {
	v := &PluginValidator{}
	p := validPlugin()
	p.Spec.PluginDirName = testPluginDirName
	p.Spec.Configs = []PluginConfigFile{
		{
			ConfigMapRef: ConfigMapKeyRef{Name: "test", Key: "test"},
			Path:         "/etc/passwd",
		},
	}

	_, err := v.ValidateCreate(context.Background(), p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path")
}

func TestPluginValidateCreate_ConfigEmptyPath(t *testing.T) {
	v := &PluginValidator{}
	p := validPlugin()
	p.Spec.PluginDirName = testPluginDirName
	p.Spec.Configs = []PluginConfigFile{
		{
			ConfigMapRef: ConfigMapKeyRef{Name: "test", Key: "test"},
			Path:         "",
		},
	}

	_, err := v.ValidateCreate(context.Background(), p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path")
}

func TestPluginValidateCreate_ConfigEmptyConfigMapRef(t *testing.T) {
	v := &PluginValidator{}
	p := validPlugin()
	p.Spec.PluginDirName = testPluginDirName
	p.Spec.Configs = []PluginConfigFile{
		{
			ConfigMapRef: ConfigMapKeyRef{Name: "", Key: "test"},
			Path:         "core.conf",
		},
	}

	_, err := v.ValidateCreate(context.Background(), p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "name")
}

func TestPluginValidateCreate_PluginDirNameTraversal(t *testing.T) {
	v := &PluginValidator{}
	p := validPlugin()
	p.Spec.PluginDirName = "../escape"
	p.Spec.Configs = []PluginConfigFile{
		{
			ConfigMapRef: ConfigMapKeyRef{Name: "test", Key: "test"},
			Path:         "core.conf",
		},
	}

	_, err := v.ValidateCreate(context.Background(), p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pluginDirName")
}

func TestPluginValidateCreate_PluginDirNameWithSlash(t *testing.T) {
	v := &PluginValidator{}
	p := validPlugin()
	p.Spec.PluginDirName = "some/path"
	p.Spec.Configs = []PluginConfigFile{
		{
			ConfigMapRef: ConfigMapKeyRef{Name: "test", Key: "test"},
			Path:         "core.conf",
		},
	}

	_, err := v.ValidateCreate(context.Background(), p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pluginDirName")
}

func TestPluginValidateCreate_ConfigDuplicatePaths(t *testing.T) {
	v := &PluginValidator{}
	p := validPlugin()
	p.Spec.PluginDirName = testPluginDirName
	p.Spec.Configs = []PluginConfigFile{
		{
			ConfigMapRef: ConfigMapKeyRef{Name: "a", Key: "x"},
			Path:         "core.conf",
		},
		{
			ConfigMapRef: ConfigMapKeyRef{Name: "b", Key: "y"},
			Path:         "core.conf",
		},
	}

	_, err := v.ValidateCreate(context.Background(), p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "core.conf")
}

func TestPluginValidateCreate_ConfigNestedPath(t *testing.T) {
	v := &PluginValidator{}
	p := validPlugin()
	p.Spec.PluginDirName = testPluginDirName
	p.Spec.Configs = []PluginConfigFile{
		{
			ConfigMapRef: ConfigMapKeyRef{Name: "test", Key: "overworld"},
			Path:         "maps/overworld.conf",
			Overwrite:    "ifNotExists",
		},
	}

	warnings, err := v.ValidateCreate(context.Background(), p)
	require.NoError(t, err)
	assert.Empty(t, warnings)
}

func TestPluginValidateCreate_NoConfigsNoPluginDirNameOK(t *testing.T) {
	v := &PluginValidator{}
	p := validPlugin()
	// No configs, no pluginDirName — valid

	warnings, err := v.ValidateCreate(context.Background(), p)
	require.NoError(t, err)
	assert.Empty(t, warnings)
}

// --- Endpoint validation tests ---

func TestPluginValidateCreate_EndpointsValid(t *testing.T) {
	v := &PluginValidator{}
	p := validPlugin()
	p.Spec.Endpoints = []PluginEndpoint{
		{Name: "web-ui", Port: 8123, Protocol: "HTTP"},
		{Name: "metrics", Port: 9100, Protocol: "TCP"},
	}

	warnings, err := v.ValidateCreate(context.Background(), p)
	require.NoError(t, err)
	assert.Empty(t, warnings)
}

func TestPluginValidateCreate_EndpointsEmptyIsValid(t *testing.T) {
	v := &PluginValidator{}
	p := validPlugin()
	p.Spec.Endpoints = nil

	warnings, err := v.ValidateCreate(context.Background(), p)
	require.NoError(t, err)
	assert.Empty(t, warnings)
}

func TestPluginValidateCreate_EndpointsDuplicateNames(t *testing.T) {
	v := &PluginValidator{}
	p := validPlugin()
	p.Spec.Endpoints = []PluginEndpoint{
		{Name: "web-ui", Port: 8123, Protocol: "HTTP"},
		{Name: "web-ui", Port: 9100, Protocol: "TCP"},
	}

	_, err := v.ValidateCreate(context.Background(), p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "web-ui")
}

func TestPluginValidateCreate_EndpointsDuplicatePortProtocol(t *testing.T) {
	v := &PluginValidator{}
	p := validPlugin()
	p.Spec.Endpoints = []PluginEndpoint{
		{Name: "web-ui", Port: 8123, Protocol: "TCP"},
		{Name: "other", Port: 8123, Protocol: "TCP"},
	}

	_, err := v.ValidateCreate(context.Background(), p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "8123")
}

func TestPluginValidateCreate_EndpointsSamePortDifferentProtocol(t *testing.T) {
	v := &PluginValidator{}
	p := validPlugin()
	p.Spec.Endpoints = []PluginEndpoint{
		{Name: "game-tcp", Port: 8123, Protocol: "TCP"},
		{Name: "game-udp", Port: 8123, Protocol: "UDP"},
	}

	warnings, err := v.ValidateCreate(context.Background(), p)
	require.NoError(t, err)
	assert.Empty(t, warnings)
}

func TestPluginValidateCreate_EndpointInvalidProtocol(t *testing.T) {
	v := &PluginValidator{}
	p := validPlugin()
	p.Spec.Endpoints = []PluginEndpoint{
		{Name: "bad", Port: 8123, Protocol: "SCTP"},
	}

	_, err := v.ValidateCreate(context.Background(), p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "protocol")
}

func TestPluginValidateCreate_EndpointDefaultProtocol(t *testing.T) {
	v := &PluginValidator{}
	p := validPlugin()
	p.Spec.Endpoints = []PluginEndpoint{
		{Name: "basic", Port: 8123}, // Protocol empty → defaults to TCP
	}

	warnings, err := v.ValidateCreate(context.Background(), p)
	require.NoError(t, err)
	assert.Empty(t, warnings)
}

func TestPluginValidateCreate_EndpointPortZero(t *testing.T) {
	v := &PluginValidator{}
	p := validPlugin()
	p.Spec.Endpoints = []PluginEndpoint{
		{Name: "bad", Port: 0, Protocol: "TCP"},
	}

	_, err := v.ValidateCreate(context.Background(), p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "port")
}

func TestPluginValidateCreate_EndpointPortTooHigh(t *testing.T) {
	v := &PluginValidator{}
	p := validPlugin()
	p.Spec.Endpoints = []PluginEndpoint{
		{Name: "bad", Port: 70000, Protocol: "TCP"},
	}

	_, err := v.ValidateCreate(context.Background(), p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "port")
}

func TestPluginValidateCreate_EndpointEmptyName(t *testing.T) {
	v := &PluginValidator{}
	p := validPlugin()
	p.Spec.Endpoints = []PluginEndpoint{
		{Name: "", Port: 8123, Protocol: "TCP"},
	}

	_, err := v.ValidateCreate(context.Background(), p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "name")
}
