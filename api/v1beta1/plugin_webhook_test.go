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
	strategyPin         = "pin"
	strategyBuildPin    = "build-pin"
	testPluginVersion   = "2.5.0"
	testExampleJARURL   = "https://example.com/plugin.jar"
	testExampleChecksum = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
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

func TestPluginValidateDelete_AlwaysAllowed(t *testing.T) {
	v := &PluginValidator{}
	p := validPlugin()

	warnings, err := v.ValidateDelete(context.Background(), p)
	require.NoError(t, err)
	assert.Empty(t, warnings)
}
