/*
Copyright 2026, Aleksei Sviridkin.

SPDX-License-Identifier: BSD-3-Clause
*/

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	mck8slexlav1beta1 "github.com/lexfrei/minecraft-operator/api/v1beta1"
)

// --- Test fixtures ---

func makeTestPlugin(name, namespace string) *mck8slexlav1beta1.Plugin {
	return &mck8slexlav1beta1.Plugin{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: mck8slexlav1beta1.PluginSpec{
			Source: mck8slexlav1beta1.PluginSource{
				Type:    testSourceHangar,
				Project: "test-project",
			},
			UpdateStrategy: updateStrategyLatest,
		},
	}
}

// --- ListPlugins tests ---

func TestPluginService_ListPlugins_AllNamespaces(t *testing.T) {
	t.Parallel()

	plugin1 := makeTestPlugin("plugin1", "ns1")
	plugin2 := makeTestPlugin("plugin2", "ns2")

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestScheme()).
		WithObjects(plugin1, plugin2).
		Build()

	svc := NewPluginService(fakeClient)

	plugins, err := svc.ListPlugins(context.Background(), "")

	require.NoError(t, err)
	assert.Len(t, plugins, 2)
}

func TestPluginService_ListPlugins_FilterByNamespace(t *testing.T) {
	t.Parallel()

	plugin1 := makeTestPlugin("plugin1", "ns1")
	plugin2 := makeTestPlugin("plugin2", "ns2")
	plugin3 := makeTestPlugin("plugin3", "ns1")

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestScheme()).
		WithObjects(plugin1, plugin2, plugin3).
		Build()

	svc := NewPluginService(fakeClient)

	plugins, err := svc.ListPlugins(context.Background(), "ns1")

	require.NoError(t, err)
	assert.Len(t, plugins, 2)
	for _, p := range plugins {
		assert.Equal(t, "ns1", p.Namespace)
	}
}

func TestPluginService_ListPlugins_Empty(t *testing.T) {
	t.Parallel()

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestScheme()).
		Build()

	svc := NewPluginService(fakeClient)

	plugins, err := svc.ListPlugins(context.Background(), "")

	require.NoError(t, err)
	assert.Empty(t, plugins)
}

// --- GetPlugin tests ---

func TestPluginService_GetPlugin_Found(t *testing.T) {
	t.Parallel()

	plugin := makeTestPlugin(testPluginName, testNamespaceDefault)
	plugin.Spec.Version = testPluginVer100
	plugin.Status.RepositoryStatus = "available"
	plugin.Status.MatchedInstances = []mck8slexlav1beta1.MatchedInstance{
		{Name: testServerName1, Namespace: testNamespaceDefault, Version: testServerVersion, Compatible: true},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestScheme()).
		WithObjects(plugin).
		Build()

	svc := NewPluginService(fakeClient)

	result, err := svc.GetPlugin(context.Background(), testNamespaceDefault, testPluginName)

	require.NoError(t, err)
	assert.Equal(t, testPluginName, result.Name)
	assert.Equal(t, testNamespaceDefault, result.Namespace)
	assert.Equal(t, testSourceHangar, result.SourceType)
	assert.Equal(t, "test-project", result.Project)
	assert.Equal(t, updateStrategyLatest, result.UpdateStrategy)
	assert.Equal(t, 1, result.MatchedServers)
	assert.Equal(t, "available", result.RepositoryStatus)
}

func TestPluginService_GetPlugin_NotFound(t *testing.T) {
	t.Parallel()

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestScheme()).
		Build()

	svc := NewPluginService(fakeClient)

	result, err := svc.GetPlugin(context.Background(), testNamespaceDefault, "nonexistent")

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "failed to get Plugin")
}

func TestPluginService_GetPlugin_WithUpdateDelay(t *testing.T) {
	t.Parallel()

	plugin := makeTestPlugin(testPluginName, testNamespaceDefault)
	plugin.Spec.UpdateDelay = &metav1.Duration{Duration: 168 * time.Hour}

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestScheme()).
		WithObjects(plugin).
		Build()

	svc := NewPluginService(fakeClient)

	result, err := svc.GetPlugin(context.Background(), testNamespaceDefault, testPluginName)

	require.NoError(t, err)
	require.NotNil(t, result.UpdateDelay)
	assert.Equal(t, 168*time.Hour, *result.UpdateDelay)
}

func TestPluginService_GetPlugin_PinnedStrategy(t *testing.T) {
	t.Parallel()

	plugin := makeTestPlugin(testPluginName, testNamespaceDefault)
	plugin.Spec.UpdateStrategy = "pin"
	plugin.Spec.Version = "1.5.0"

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestScheme()).
		WithObjects(plugin).
		Build()

	svc := NewPluginService(fakeClient)

	result, err := svc.GetPlugin(context.Background(), testNamespaceDefault, testPluginName)

	require.NoError(t, err)
	assert.Equal(t, "1.5.0", result.ResolvedVersion)
}

// --- CreatePlugin tests ---

func TestPluginService_CreatePlugin_Success(t *testing.T) {
	t.Parallel()

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestScheme()).
		Build()

	svc := NewPluginService(fakeClient)

	data := PluginCreateData{
		Name:      "new-plugin",
		Namespace: testNamespaceDefault,
		Source: PluginSourceData{
			Type:    testSourceHangar,
			Project: testProjectName,
		},
		UpdateStrategy: updateStrategyLatest,
		UpdateDelay:    "168h",
		Build:          42,
		Endpoints: []PluginEndpointData{
			{Name: "web-ui", Port: 8123, Protocol: "HTTP"},
		},
	}

	err := svc.CreatePlugin(context.Background(), data)

	require.NoError(t, err)

	// Verify plugin was created
	var plugin mck8slexlav1beta1.Plugin
	err = fakeClient.Get(
		context.Background(),
		client.ObjectKey{Namespace: testNamespaceDefault, Name: "new-plugin"},
		&plugin,
	)
	require.NoError(t, err)
	assert.Equal(t, "new-plugin", plugin.Name)
	assert.Equal(t, updateStrategyLatest, plugin.Spec.UpdateStrategy)
	assert.NotNil(t, plugin.Spec.UpdateDelay)
	assert.Equal(t, 168*time.Hour, plugin.Spec.UpdateDelay.Duration)
	assert.NotNil(t, plugin.Spec.Build)
	assert.Equal(t, 42, *plugin.Spec.Build)
	require.Len(t, plugin.Spec.Endpoints, 1)
	assert.Equal(t, "web-ui", plugin.Spec.Endpoints[0].Name)
	assert.Equal(t, int32(8123), plugin.Spec.Endpoints[0].Port)
}

func TestPluginService_CreatePlugin_MinimalData(t *testing.T) {
	t.Parallel()

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestScheme()).
		Build()

	svc := NewPluginService(fakeClient)

	data := PluginCreateData{
		Name:      "minimal-plugin",
		Namespace: testNamespaceDefault,
		Source: PluginSourceData{
			Type:    testSourceHangar,
			Project: testProjectName,
		},
	}

	err := svc.CreatePlugin(context.Background(), data)

	require.NoError(t, err)

	var plugin mck8slexlav1beta1.Plugin
	err = fakeClient.Get(
		context.Background(),
		client.ObjectKey{Namespace: testNamespaceDefault, Name: "minimal-plugin"},
		&plugin,
	)
	require.NoError(t, err)
	assert.Nil(t, plugin.Spec.Build)
	assert.Empty(t, plugin.Spec.Endpoints)
	assert.Nil(t, plugin.Spec.UpdateDelay)
}

// --- DeletePlugin tests ---

func TestPluginService_DeletePlugin_Success(t *testing.T) {
	t.Parallel()

	plugin := makeTestPlugin("to-delete", testNamespaceDefault)

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestScheme()).
		WithObjects(plugin).
		Build()

	svc := NewPluginService(fakeClient)

	err := svc.DeletePlugin(context.Background(), testNamespaceDefault, "to-delete")

	require.NoError(t, err)

	// Verify plugin was deleted
	var deleted mck8slexlav1beta1.Plugin
	err = fakeClient.Get(
		context.Background(),
		client.ObjectKey{Namespace: testNamespaceDefault, Name: "to-delete"},
		&deleted,
	)
	require.Error(t, err)
}

func TestPluginService_DeletePlugin_NotFound(t *testing.T) {
	t.Parallel()

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestScheme()).
		Build()

	svc := NewPluginService(fakeClient)

	err := svc.DeletePlugin(context.Background(), testNamespaceDefault, "nonexistent")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get plugin")
}

// --- UpdatePlugin tests ---

//nolint:dupl // Test code intentionally similar to servers_test.go
func TestPluginService_UpdatePlugin_Success(t *testing.T) {
	t.Parallel()

	plugin := makeTestPlugin(testPluginName, testNamespaceDefault)

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestScheme()).
		WithObjects(plugin).
		Build()

	svc := NewPluginService(fakeClient)

	newStrategy := "pin"
	newVersion := testPluginVer200
	newBuild := 10
	data := PluginUpdateData{
		Name:           testPluginName,
		Namespace:      testNamespaceDefault,
		UpdateStrategy: &newStrategy,
		Version:        &newVersion,
		Build:          &newBuild,
	}

	err := svc.UpdatePlugin(context.Background(), data)

	require.NoError(t, err)

	var updated mck8slexlav1beta1.Plugin
	err = fakeClient.Get(
		context.Background(),
		client.ObjectKey{Namespace: testNamespaceDefault, Name: testPluginName},
		&updated,
	)
	require.NoError(t, err)
	assert.Equal(t, "pin", updated.Spec.UpdateStrategy)
	assert.Equal(t, testPluginVer200, updated.Spec.Version)
	assert.NotNil(t, updated.Spec.Build)
	assert.Equal(t, 10, *updated.Spec.Build)
}

func TestPluginService_UpdatePlugin_PartialUpdate(t *testing.T) {
	t.Parallel()

	plugin := makeTestPlugin(testPluginName, testNamespaceDefault)
	plugin.Spec.UpdateStrategy = updateStrategyLatest
	plugin.Spec.Version = testPluginVer100

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestScheme()).
		WithObjects(plugin).
		Build()

	svc := NewPluginService(fakeClient)

	// Only update version
	newVersion := testPluginVer200
	data := PluginUpdateData{
		Name:      testPluginName,
		Namespace: testNamespaceDefault,
		Version:   &newVersion,
	}

	err := svc.UpdatePlugin(context.Background(), data)

	require.NoError(t, err)

	var updated mck8slexlav1beta1.Plugin
	err = fakeClient.Get(
		context.Background(),
		client.ObjectKey{Namespace: testNamespaceDefault, Name: testPluginName},
		&updated,
	)
	require.NoError(t, err)
	assert.Equal(t, updateStrategyLatest, updated.Spec.UpdateStrategy) // Unchanged
	assert.Equal(t, testPluginVer200, updated.Spec.Version)            // Updated
}

func TestPluginService_UpdatePlugin_NotFound(t *testing.T) {
	t.Parallel()

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestScheme()).
		Build()

	svc := NewPluginService(fakeClient)

	data := PluginUpdateData{
		Name:      "nonexistent",
		Namespace: testNamespaceDefault,
	}

	err := svc.UpdatePlugin(context.Background(), data)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get plugin")
}

// --- TriggerReconciliation tests ---

func TestPluginService_TriggerReconciliation_AddsAnnotation(t *testing.T) {
	t.Parallel()

	plugin := makeTestPlugin(testPluginName, testNamespaceDefault)

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestScheme()).
		WithObjects(plugin).
		Build()

	svc := NewPluginService(fakeClient)

	err := svc.TriggerReconciliation(context.Background(), testNamespaceDefault, testPluginName)

	require.NoError(t, err)

	var updated mck8slexlav1beta1.Plugin
	err = fakeClient.Get(
		context.Background(),
		client.ObjectKey{Namespace: testNamespaceDefault, Name: testPluginName},
		&updated,
	)
	require.NoError(t, err)
	assert.Contains(t, updated.Annotations, AnnotationReconcile)
}

func TestPluginService_TriggerReconciliation_NotFound(t *testing.T) {
	t.Parallel()

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestScheme()).
		Build()

	svc := NewPluginService(fakeClient)

	err := svc.TriggerReconciliation(context.Background(), testNamespaceDefault, "nonexistent")

	require.Error(t, err)
}

// --- GetPluginNamespaces tests ---

func TestPluginService_GetPluginNamespaces(t *testing.T) {
	t.Parallel()

	plugin1 := makeTestPlugin("plugin1", "ns1")
	plugin2 := makeTestPlugin("plugin2", "ns2")
	plugin3 := makeTestPlugin("plugin3", "ns1")

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestScheme()).
		WithObjects(plugin1, plugin2, plugin3).
		Build()

	svc := NewPluginService(fakeClient)

	namespaces, err := svc.GetPluginNamespaces(context.Background())

	require.NoError(t, err)
	assert.Len(t, namespaces, 2)
	assert.Contains(t, namespaces, "ns1")
	assert.Contains(t, namespaces, "ns2")
}

func TestPluginService_GetPluginNamespaces_Empty(t *testing.T) {
	t.Parallel()

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestScheme()).
		Build()

	svc := NewPluginService(fakeClient)

	namespaces, err := svc.GetPluginNamespaces(context.Background())

	require.NoError(t, err)
	assert.Empty(t, namespaces)
}

// --- Constructor test ---

func TestNewPluginService(t *testing.T) {
	t.Parallel()

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestScheme()).
		Build()

	svc := NewPluginService(fakeClient)

	assert.NotNil(t, svc)
}

// --- pluginToData conversion tests ---

func TestPluginToData_WithAvailableVersions(t *testing.T) {
	t.Parallel()

	plugin := makeTestPlugin(testPluginName, testNamespaceDefault)
	now := metav1.Now()
	plugin.Status.AvailableVersions = []mck8slexlav1beta1.PluginVersionInfo{
		{
			Version:           testPluginVer100,
			MinecraftVersions: []string{testServerVersion},
			DownloadURL:       "https://example.com/v1.jar",
			ReleasedAt:        now,
		},
	}
	plugin.Status.LastFetched = &now

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestScheme()).
		WithObjects(plugin).
		Build()

	svc := NewPluginService(fakeClient)

	result, err := svc.GetPlugin(context.Background(), testNamespaceDefault, testPluginName)

	require.NoError(t, err)
	require.Len(t, result.AvailableVersions, 1)
	assert.Equal(t, testPluginVer100, result.AvailableVersions[0].Version)
	assert.Equal(t, []string{testServerVersion}, result.AvailableVersions[0].SupportedVersions)
	assert.NotNil(t, result.LastFetched)
}

// --- Invalid duration tests ---

func TestPluginService_CreatePlugin_InvalidUpdateDelay(t *testing.T) {
	// BUG: CreatePlugin silently ignores invalid UpdateDelay strings.
	t.Parallel()

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestScheme()).
		Build()

	svc := NewPluginService(fakeClient)

	data := PluginCreateData{
		Name:      "bad-delay-plugin",
		Namespace: testNamespaceDefault,
		Source: PluginSourceData{
			Type:    testSourceHangar,
			Project: "EssentialsX",
		},
		UpdateStrategy: updateStrategyLatest,
		UpdateDelay:    "not-a-duration",
	}

	err := svc.CreatePlugin(context.Background(), data)
	require.Error(t, err, "CreatePlugin should return error for invalid UpdateDelay")
}

func TestPluginService_UpdatePlugin_InvalidUpdateDelay(t *testing.T) {
	// BUG: UpdatePlugin silently ignores invalid UpdateDelay strings.
	t.Parallel()

	existingPlugin := &mck8slexlav1beta1.Plugin{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "delay-test-plugin",
			Namespace: testNamespaceDefault,
		},
		Spec: mck8slexlav1beta1.PluginSpec{
			Source: mck8slexlav1beta1.PluginSource{
				Type:    testSourceHangar,
				Project: "EssentialsX",
			},
			UpdateStrategy: updateStrategyLatest,
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestScheme()).
		WithObjects(existingPlugin).
		Build()

	svc := NewPluginService(fakeClient)

	badDelay := "invalid"
	data := PluginUpdateData{
		Namespace:   testNamespaceDefault,
		Name:        "delay-test-plugin",
		UpdateDelay: &badDelay,
	}

	err := svc.UpdatePlugin(context.Background(), data)
	require.Error(t, err, "UpdatePlugin should return error for invalid UpdateDelay")
}
