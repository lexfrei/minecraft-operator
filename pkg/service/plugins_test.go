/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
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
				Type:    "hangar",
				Project: "test-project",
			},
			UpdateStrategy: "latest",
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

	plugin := makeTestPlugin("test-plugin", "default")
	plugin.Spec.Version = "1.0.0"
	plugin.Status.RepositoryStatus = "available"
	plugin.Status.MatchedInstances = []mck8slexlav1beta1.MatchedInstance{
		{Name: "server1", Namespace: "default", Version: "1.21.1", Compatible: true},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestScheme()).
		WithObjects(plugin).
		Build()

	svc := NewPluginService(fakeClient)

	result, err := svc.GetPlugin(context.Background(), "default", "test-plugin")

	require.NoError(t, err)
	assert.Equal(t, "test-plugin", result.Name)
	assert.Equal(t, "default", result.Namespace)
	assert.Equal(t, "hangar", result.SourceType)
	assert.Equal(t, "test-project", result.Project)
	assert.Equal(t, "latest", result.UpdateStrategy)
	assert.Equal(t, 1, result.MatchedServers)
	assert.Equal(t, "available", result.RepositoryStatus)
}

func TestPluginService_GetPlugin_NotFound(t *testing.T) {
	t.Parallel()

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestScheme()).
		Build()

	svc := NewPluginService(fakeClient)

	result, err := svc.GetPlugin(context.Background(), "default", "nonexistent")

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "failed to get Plugin")
}

func TestPluginService_GetPlugin_WithUpdateDelay(t *testing.T) {
	t.Parallel()

	plugin := makeTestPlugin("test-plugin", "default")
	plugin.Spec.UpdateDelay = &metav1.Duration{Duration: 168 * time.Hour}

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestScheme()).
		WithObjects(plugin).
		Build()

	svc := NewPluginService(fakeClient)

	result, err := svc.GetPlugin(context.Background(), "default", "test-plugin")

	require.NoError(t, err)
	require.NotNil(t, result.UpdateDelay)
	assert.Equal(t, 168*time.Hour, *result.UpdateDelay)
}

func TestPluginService_GetPlugin_PinnedStrategy(t *testing.T) {
	t.Parallel()

	plugin := makeTestPlugin("test-plugin", "default")
	plugin.Spec.UpdateStrategy = "pinned"
	plugin.Spec.Version = "1.5.0"

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestScheme()).
		WithObjects(plugin).
		Build()

	svc := NewPluginService(fakeClient)

	result, err := svc.GetPlugin(context.Background(), "default", "test-plugin")

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
		Namespace: "default",
		Source: PluginSourceData{
			Type:    "hangar",
			Project: "test",
		},
		UpdateStrategy: "latest",
		UpdateDelay:    "168h",
		Build:          42,
		Port:           25565,
	}

	err := svc.CreatePlugin(context.Background(), data)

	require.NoError(t, err)

	// Verify plugin was created
	var plugin mck8slexlav1beta1.Plugin
	err = fakeClient.Get(context.Background(), client.ObjectKey{Namespace: "default", Name: "new-plugin"}, &plugin)
	require.NoError(t, err)
	assert.Equal(t, "new-plugin", plugin.Name)
	assert.Equal(t, "latest", plugin.Spec.UpdateStrategy)
	assert.NotNil(t, plugin.Spec.UpdateDelay)
	assert.Equal(t, 168*time.Hour, plugin.Spec.UpdateDelay.Duration)
	assert.NotNil(t, plugin.Spec.Build)
	assert.Equal(t, 42, *plugin.Spec.Build)
	assert.NotNil(t, plugin.Spec.Port)
	assert.Equal(t, int32(25565), *plugin.Spec.Port)
}

func TestPluginService_CreatePlugin_MinimalData(t *testing.T) {
	t.Parallel()

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestScheme()).
		Build()

	svc := NewPluginService(fakeClient)

	data := PluginCreateData{
		Name:      "minimal-plugin",
		Namespace: "default",
		Source: PluginSourceData{
			Type:    "hangar",
			Project: "test",
		},
	}

	err := svc.CreatePlugin(context.Background(), data)

	require.NoError(t, err)

	var plugin mck8slexlav1beta1.Plugin
	err = fakeClient.Get(context.Background(), client.ObjectKey{Namespace: "default", Name: "minimal-plugin"}, &plugin)
	require.NoError(t, err)
	assert.Nil(t, plugin.Spec.Build)
	assert.Nil(t, plugin.Spec.Port)
	assert.Nil(t, plugin.Spec.UpdateDelay)
}

// --- DeletePlugin tests ---

func TestPluginService_DeletePlugin_Success(t *testing.T) {
	t.Parallel()

	plugin := makeTestPlugin("to-delete", "default")

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestScheme()).
		WithObjects(plugin).
		Build()

	svc := NewPluginService(fakeClient)

	err := svc.DeletePlugin(context.Background(), "default", "to-delete")

	require.NoError(t, err)

	// Verify plugin was deleted
	var deleted mck8slexlav1beta1.Plugin
	err = fakeClient.Get(context.Background(), client.ObjectKey{Namespace: "default", Name: "to-delete"}, &deleted)
	require.Error(t, err)
}

func TestPluginService_DeletePlugin_NotFound(t *testing.T) {
	t.Parallel()

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestScheme()).
		Build()

	svc := NewPluginService(fakeClient)

	err := svc.DeletePlugin(context.Background(), "default", "nonexistent")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get plugin")
}

// --- UpdatePlugin tests ---

//nolint:dupl // Test code intentionally similar to servers_test.go
func TestPluginService_UpdatePlugin_Success(t *testing.T) {
	t.Parallel()

	plugin := makeTestPlugin("test-plugin", "default")

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestScheme()).
		WithObjects(plugin).
		Build()

	svc := NewPluginService(fakeClient)

	newStrategy := "pinned"
	newVersion := "2.0.0"
	newBuild := 10
	data := PluginUpdateData{
		Name:           "test-plugin",
		Namespace:      "default",
		UpdateStrategy: &newStrategy,
		Version:        &newVersion,
		Build:          &newBuild,
	}

	err := svc.UpdatePlugin(context.Background(), data)

	require.NoError(t, err)

	var updated mck8slexlav1beta1.Plugin
	err = fakeClient.Get(context.Background(), client.ObjectKey{Namespace: "default", Name: "test-plugin"}, &updated)
	require.NoError(t, err)
	assert.Equal(t, "pinned", updated.Spec.UpdateStrategy)
	assert.Equal(t, "2.0.0", updated.Spec.Version)
	assert.NotNil(t, updated.Spec.Build)
	assert.Equal(t, 10, *updated.Spec.Build)
}

func TestPluginService_UpdatePlugin_PartialUpdate(t *testing.T) {
	t.Parallel()

	plugin := makeTestPlugin("test-plugin", "default")
	plugin.Spec.UpdateStrategy = "latest"
	plugin.Spec.Version = "1.0.0"

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestScheme()).
		WithObjects(plugin).
		Build()

	svc := NewPluginService(fakeClient)

	// Only update version
	newVersion := "2.0.0"
	data := PluginUpdateData{
		Name:      "test-plugin",
		Namespace: "default",
		Version:   &newVersion,
	}

	err := svc.UpdatePlugin(context.Background(), data)

	require.NoError(t, err)

	var updated mck8slexlav1beta1.Plugin
	err = fakeClient.Get(context.Background(), client.ObjectKey{Namespace: "default", Name: "test-plugin"}, &updated)
	require.NoError(t, err)
	assert.Equal(t, "latest", updated.Spec.UpdateStrategy) // Unchanged
	assert.Equal(t, "2.0.0", updated.Spec.Version)         // Updated
}

func TestPluginService_UpdatePlugin_NotFound(t *testing.T) {
	t.Parallel()

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestScheme()).
		Build()

	svc := NewPluginService(fakeClient)

	data := PluginUpdateData{
		Name:      "nonexistent",
		Namespace: "default",
	}

	err := svc.UpdatePlugin(context.Background(), data)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get plugin")
}

// --- TriggerReconciliation tests ---

func TestPluginService_TriggerReconciliation_AddsAnnotation(t *testing.T) {
	t.Parallel()

	plugin := makeTestPlugin("test-plugin", "default")

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestScheme()).
		WithObjects(plugin).
		Build()

	svc := NewPluginService(fakeClient)

	err := svc.TriggerReconciliation(context.Background(), "default", "test-plugin")

	require.NoError(t, err)

	var updated mck8slexlav1beta1.Plugin
	err = fakeClient.Get(context.Background(), client.ObjectKey{Namespace: "default", Name: "test-plugin"}, &updated)
	require.NoError(t, err)
	assert.Contains(t, updated.Annotations, AnnotationReconcile)
}

func TestPluginService_TriggerReconciliation_NotFound(t *testing.T) {
	t.Parallel()

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestScheme()).
		Build()

	svc := NewPluginService(fakeClient)

	err := svc.TriggerReconciliation(context.Background(), "default", "nonexistent")

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

	plugin := makeTestPlugin("test-plugin", "default")
	now := metav1.Now()
	plugin.Status.AvailableVersions = []mck8slexlav1beta1.PluginVersionInfo{
		{
			Version:           "1.0.0",
			MinecraftVersions: []string{"1.21.1"},
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

	result, err := svc.GetPlugin(context.Background(), "default", "test-plugin")

	require.NoError(t, err)
	require.Len(t, result.AvailableVersions, 1)
	assert.Equal(t, "1.0.0", result.AvailableVersions[0].Version)
	assert.Equal(t, []string{"1.21.1"}, result.AvailableVersions[0].SupportedVersions)
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
		Namespace: "default",
		Source: PluginSourceData{
			Type:    "hangar",
			Project: "EssentialsX",
		},
		UpdateStrategy: "latest",
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
			Namespace: "default",
		},
		Spec: mck8slexlav1beta1.PluginSpec{
			Source: mck8slexlav1beta1.PluginSource{
				Type:    "hangar",
				Project: "EssentialsX",
			},
			UpdateStrategy: "latest",
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestScheme()).
		WithObjects(existingPlugin).
		Build()

	svc := NewPluginService(fakeClient)

	badDelay := "invalid"
	data := PluginUpdateData{
		Namespace:   "default",
		Name:        "delay-test-plugin",
		UpdateDelay: &badDelay,
	}

	err := svc.UpdatePlugin(context.Background(), data)
	require.Error(t, err, "UpdatePlugin should return error for invalid UpdateDelay")
}
