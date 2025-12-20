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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	mck8slexlav1alpha1 "github.com/lexfrei/minecraft-operator/api/v1alpha1"
)

const testServerVersion = "1.21.1"

func newTestSchemeWithAppsV1() *runtime.Scheme {
	scheme := runtime.NewScheme()
	_ = mck8slexlav1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	return scheme
}

// --- Test fixtures ---

func makeTestServer(name, namespace string) *mck8slexlav1alpha1.PaperMCServer {
	return &mck8slexlav1alpha1.PaperMCServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: mck8slexlav1alpha1.PaperMCServerSpec{
			Version:        testServerVersion,
			UpdateStrategy: "latest",
		},
	}
}

func makeTestStatefulSet(name, namespace string, ready bool) *appsv1.StatefulSet {
	var replicas int32 = 1
	var readyReplicas int32
	if ready {
		readyReplicas = 1
	}

	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
		},
		Status: appsv1.StatefulSetStatus{
			Replicas:        replicas,
			ReadyReplicas:   readyReplicas,
			CurrentRevision: "rev1",
			UpdateRevision:  "rev1",
		},
	}
}

// --- ListServers tests ---

func TestServerService_ListServers_AllNamespaces(t *testing.T) {
	t.Parallel()

	server1 := makeTestServer("server1", "ns1")
	server2 := makeTestServer("server2", "ns2")

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestSchemeWithAppsV1()).
		WithObjects(server1, server2).
		Build()

	svc := NewServerService(fakeClient)

	servers, err := svc.ListServers(context.Background(), "")

	require.NoError(t, err)
	assert.Len(t, servers, 2)
}

func TestServerService_ListServers_FilterByNamespace(t *testing.T) {
	t.Parallel()

	server1 := makeTestServer("server1", "ns1")
	server2 := makeTestServer("server2", "ns2")
	server3 := makeTestServer("server3", "ns1")

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestSchemeWithAppsV1()).
		WithObjects(server1, server2, server3).
		Build()

	svc := NewServerService(fakeClient)

	servers, err := svc.ListServers(context.Background(), "ns1")

	require.NoError(t, err)
	assert.Len(t, servers, 2)
	for _, s := range servers {
		assert.Equal(t, "ns1", s.Namespace)
	}
}

func TestServerService_ListServers_Empty(t *testing.T) {
	t.Parallel()

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestSchemeWithAppsV1()).
		Build()

	svc := NewServerService(fakeClient)

	servers, err := svc.ListServers(context.Background(), "")

	require.NoError(t, err)
	assert.Empty(t, servers)
}

// --- GetServer tests ---

func TestServerService_GetServer_Found(t *testing.T) {
	t.Parallel()

	server := makeTestServer("test-server", "default")
	server.Status.CurrentVersion = testServerVersion
	server.Status.CurrentBuild = 91

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestSchemeWithAppsV1()).
		WithObjects(server).
		Build()

	svc := NewServerService(fakeClient)

	result, err := svc.GetServer(context.Background(), "default", "test-server")

	require.NoError(t, err)
	assert.Equal(t, "test-server", result.Name)
	assert.Equal(t, "default", result.Namespace)
	assert.Equal(t, testServerVersion, result.CurrentVersion)
	assert.Equal(t, 91, result.CurrentBuild)
}

func TestServerService_GetServer_NotFound(t *testing.T) {
	t.Parallel()

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestSchemeWithAppsV1()).
		Build()

	svc := NewServerService(fakeClient)

	result, err := svc.GetServer(context.Background(), "default", "nonexistent")

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "failed to get PaperMCServer")
}

// --- GetServerByName tests ---

func TestServerService_GetServerByName_Found(t *testing.T) {
	t.Parallel()

	server := makeTestServer("unique-server", "custom-ns")
	server.Status.CurrentVersion = testServerVersion

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestSchemeWithAppsV1()).
		WithObjects(server).
		Build()

	svc := NewServerService(fakeClient)

	result, err := svc.GetServerByName(context.Background(), "unique-server")

	require.NoError(t, err)
	assert.Equal(t, "unique-server", result.Name)
	assert.Equal(t, "custom-ns", result.Namespace)
}

func TestServerService_GetServerByName_NotFound(t *testing.T) {
	t.Parallel()

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestSchemeWithAppsV1()).
		Build()

	svc := NewServerService(fakeClient)

	result, err := svc.GetServerByName(context.Background(), "nonexistent")

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "not found")
}

// --- DetermineStatus tests ---

func TestServerService_DetermineStatus_Running(t *testing.T) {
	t.Parallel()

	server := makeTestServer("test", "default")
	server.Status.Conditions = []metav1.Condition{
		{
			Type:   "StatefulSetReady",
			Status: metav1.ConditionTrue,
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestSchemeWithAppsV1()).
		Build()

	svc := NewServerService(fakeClient)

	status := svc.DetermineStatus(server)

	assert.Equal(t, StatusRunning, status)
}

func TestServerService_DetermineStatus_Updating(t *testing.T) {
	t.Parallel()

	server := makeTestServer("test", "default")
	server.Status.Conditions = []metav1.Condition{
		{
			Type:   "StatefulSetReady",
			Status: metav1.ConditionFalse,
			Reason: "Updating",
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestSchemeWithAppsV1()).
		Build()

	svc := NewServerService(fakeClient)

	status := svc.DetermineStatus(server)

	assert.Equal(t, StatusUpdating, status)
}

func TestServerService_DetermineStatus_FallbackToCurrentVersion(t *testing.T) {
	t.Parallel()

	server := makeTestServer("test", "default")
	server.Status.CurrentVersion = testServerVersion // No conditions, but has version

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestSchemeWithAppsV1()).
		Build()

	svc := NewServerService(fakeClient)

	status := svc.DetermineStatus(server)

	assert.Equal(t, StatusRunning, status)
}

func TestServerService_DetermineStatus_Unknown(t *testing.T) {
	t.Parallel()

	server := makeTestServer("test", "default")
	// No conditions and no current version

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestSchemeWithAppsV1()).
		Build()

	svc := NewServerService(fakeClient)

	status := svc.DetermineStatus(server)

	assert.Equal(t, StatusUnknown, status)
}

// --- CheckStatefulSetStatus tests ---

func TestServerService_CheckStatefulSetStatus_Ready(t *testing.T) {
	t.Parallel()

	sts := makeTestStatefulSet("test-server", "default", true)

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestSchemeWithAppsV1()).
		WithObjects(sts).
		Build()

	svc := NewServerService(fakeClient)

	status := svc.CheckStatefulSetStatus(context.Background(), "test-server", "default")

	assert.Equal(t, StatusRunning, status)
}

func TestServerService_CheckStatefulSetStatus_Updating(t *testing.T) {
	t.Parallel()

	sts := makeTestStatefulSet("test-server", "default", false)
	sts.Status.CurrentRevision = "rev1"
	sts.Status.UpdateRevision = "rev2" // Different revisions

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestSchemeWithAppsV1()).
		WithObjects(sts).
		Build()

	svc := NewServerService(fakeClient)

	status := svc.CheckStatefulSetStatus(context.Background(), "test-server", "default")

	assert.Equal(t, StatusUpdating, status)
}

func TestServerService_CheckStatefulSetStatus_NotFound(t *testing.T) {
	t.Parallel()

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestSchemeWithAppsV1()).
		Build()

	svc := NewServerService(fakeClient)

	status := svc.CheckStatefulSetStatus(context.Background(), "nonexistent", "default")

	assert.Equal(t, StatusUnknown, status)
}

// --- CreateServer tests ---

func TestServerService_CreateServer_Success(t *testing.T) {
	t.Parallel()

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestSchemeWithAppsV1()).
		Build()

	svc := NewServerService(fakeClient)

	data := ServerCreateData{
		Name:               "new-server",
		Namespace:          "default",
		UpdateStrategy:     "latest",
		Version:            testServerVersion,
		Build:              91,
		UpdateDelay:        "168h",
		CheckCron:          "0 4 * * *",
		MaintenanceEnabled: true,
		MaintenanceCron:    "0 4 * * 0",
		Labels:             map[string]string{"env": "test"},
	}

	err := svc.CreateServer(context.Background(), data)

	require.NoError(t, err)

	// Verify server was created
	var server mck8slexlav1alpha1.PaperMCServer
	err = fakeClient.Get(context.Background(), client.ObjectKey{Namespace: "default", Name: "new-server"}, &server)
	require.NoError(t, err)
	assert.Equal(t, "new-server", server.Name)
	assert.Equal(t, "latest", server.Spec.UpdateStrategy)
	assert.Equal(t, testServerVersion, server.Spec.Version)
	assert.NotNil(t, server.Spec.Build)
	assert.Equal(t, 91, *server.Spec.Build)
	assert.Equal(t, "0 4 * * *", server.Spec.UpdateSchedule.CheckCron)
	assert.True(t, server.Spec.UpdateSchedule.MaintenanceWindow.Enabled)
	assert.Equal(t, "0 4 * * 0", server.Spec.UpdateSchedule.MaintenanceWindow.Cron)
	assert.Equal(t, "test", server.Labels["env"])
}

func TestServerService_CreateServer_MinimalData(t *testing.T) {
	t.Parallel()

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestSchemeWithAppsV1()).
		Build()

	svc := NewServerService(fakeClient)

	data := ServerCreateData{
		Name:      "minimal-server",
		Namespace: "default",
	}

	err := svc.CreateServer(context.Background(), data)

	require.NoError(t, err)

	var server mck8slexlav1alpha1.PaperMCServer
	err = fakeClient.Get(context.Background(), client.ObjectKey{Namespace: "default", Name: "minimal-server"}, &server)
	require.NoError(t, err)
	assert.Nil(t, server.Spec.Build)
	assert.Nil(t, server.Spec.UpdateDelay)
}

// --- UpdateServer tests ---

//nolint:dupl // Test code intentionally similar to plugins_test.go
func TestServerService_UpdateServer_Success(t *testing.T) {
	t.Parallel()

	server := makeTestServer("test-server", "default")

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestSchemeWithAppsV1()).
		WithObjects(server).
		Build()

	svc := NewServerService(fakeClient)

	newStrategy := "pin"
	newVersion := "1.21.2"
	newBuild := 100
	data := ServerUpdateData{
		Name:           "test-server",
		Namespace:      "default",
		UpdateStrategy: &newStrategy,
		Version:        &newVersion,
		Build:          &newBuild,
	}

	err := svc.UpdateServer(context.Background(), data)

	require.NoError(t, err)

	var updated mck8slexlav1alpha1.PaperMCServer
	err = fakeClient.Get(context.Background(), client.ObjectKey{Namespace: "default", Name: "test-server"}, &updated)
	require.NoError(t, err)
	assert.Equal(t, "pin", updated.Spec.UpdateStrategy)
	assert.Equal(t, "1.21.2", updated.Spec.Version)
	assert.NotNil(t, updated.Spec.Build)
	assert.Equal(t, 100, *updated.Spec.Build)
}

func TestServerService_UpdateServer_NotFound(t *testing.T) {
	t.Parallel()

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestSchemeWithAppsV1()).
		Build()

	svc := NewServerService(fakeClient)

	data := ServerUpdateData{
		Name:      "nonexistent",
		Namespace: "default",
	}

	err := svc.UpdateServer(context.Background(), data)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get server")
}

// --- DeleteServer tests ---

func TestServerService_DeleteServer_Success(t *testing.T) {
	t.Parallel()

	server := makeTestServer("to-delete", "default")

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestSchemeWithAppsV1()).
		WithObjects(server).
		Build()

	svc := NewServerService(fakeClient)

	err := svc.DeleteServer(context.Background(), "default", "to-delete")

	require.NoError(t, err)

	var deleted mck8slexlav1alpha1.PaperMCServer
	err = fakeClient.Get(context.Background(), client.ObjectKey{Namespace: "default", Name: "to-delete"}, &deleted)
	require.Error(t, err)
}

func TestServerService_DeleteServer_NotFound(t *testing.T) {
	t.Parallel()

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestSchemeWithAppsV1()).
		Build()

	svc := NewServerService(fakeClient)

	err := svc.DeleteServer(context.Background(), "default", "nonexistent")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get server")
}

// --- TriggerReconciliation tests ---

func TestServerService_TriggerReconciliation_AddsAnnotation(t *testing.T) {
	t.Parallel()

	server := makeTestServer("test-server", "default")

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestSchemeWithAppsV1()).
		WithObjects(server).
		Build()

	svc := NewServerService(fakeClient)

	err := svc.TriggerReconciliation(context.Background(), "default", "test-server")

	require.NoError(t, err)

	var updated mck8slexlav1alpha1.PaperMCServer
	err = fakeClient.Get(context.Background(), client.ObjectKey{Namespace: "default", Name: "test-server"}, &updated)
	require.NoError(t, err)
	assert.Contains(t, updated.Annotations, AnnotationReconcile)
}

// --- ApplyNow tests ---

func TestServerService_ApplyNow_SetsAnnotation(t *testing.T) {
	t.Parallel()

	server := makeTestServer("test-server", "default")

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestSchemeWithAppsV1()).
		WithObjects(server).
		Build()

	svc := NewServerService(fakeClient)

	err := svc.ApplyNow(context.Background(), "default", "test-server")

	require.NoError(t, err)

	var updated mck8slexlav1alpha1.PaperMCServer
	err = fakeClient.Get(context.Background(), client.ObjectKey{Namespace: "default", Name: "test-server"}, &updated)
	require.NoError(t, err)
	assert.Contains(t, updated.Annotations, AnnotationApplyNow)
}

func TestServerService_ApplyNow_NotFound(t *testing.T) {
	t.Parallel()

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestSchemeWithAppsV1()).
		Build()

	svc := NewServerService(fakeClient)

	err := svc.ApplyNow(context.Background(), "default", "nonexistent")

	require.Error(t, err)
}

// --- GetServerNamespaces tests ---

func TestServerService_GetServerNamespaces(t *testing.T) {
	t.Parallel()

	server1 := makeTestServer("server1", "ns1")
	server2 := makeTestServer("server2", "ns2")
	server3 := makeTestServer("server3", "ns1")

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestSchemeWithAppsV1()).
		WithObjects(server1, server2, server3).
		Build()

	svc := NewServerService(fakeClient)

	namespaces, err := svc.GetServerNamespaces(context.Background())

	require.NoError(t, err)
	assert.Len(t, namespaces, 2)
	assert.Contains(t, namespaces, "ns1")
	assert.Contains(t, namespaces, "ns2")
}

func TestServerService_GetServerNamespaces_Empty(t *testing.T) {
	t.Parallel()

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestSchemeWithAppsV1()).
		Build()

	svc := NewServerService(fakeClient)

	namespaces, err := svc.GetServerNamespaces(context.Background())

	require.NoError(t, err)
	assert.Empty(t, namespaces)
}

// --- serverToData conversion tests ---

func TestServerToData_WithAvailableUpdate(t *testing.T) {
	t.Parallel()

	now := metav1.Now()
	server := makeTestServer("test-server", "default")
	server.Status.AvailableUpdate = &mck8slexlav1alpha1.AvailableUpdate{
		Version:    "1.21.2",
		Build:      92,
		ReleasedAt: now,
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestSchemeWithAppsV1()).
		WithObjects(server).
		Build()

	svc := NewServerService(fakeClient)

	result, err := svc.GetServer(context.Background(), "default", "test-server")

	require.NoError(t, err)
	require.NotNil(t, result.AvailableUpdate)
	assert.Equal(t, "1.21.2", result.AvailableUpdate.Version)
	assert.Equal(t, 92, result.AvailableUpdate.Build)
}

func TestServerToData_WithMaintenanceSchedule(t *testing.T) {
	t.Parallel()

	server := makeTestServer("test-server", "default")
	server.Spec.UpdateSchedule.MaintenanceWindow.Enabled = true
	server.Spec.UpdateSchedule.MaintenanceWindow.Cron = "0 4 * * 0"
	server.Spec.UpdateSchedule.CheckCron = "0 4 * * *"

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestSchemeWithAppsV1()).
		WithObjects(server).
		Build()

	svc := NewServerService(fakeClient)

	result, err := svc.GetServer(context.Background(), "default", "test-server")

	require.NoError(t, err)
	require.NotNil(t, result.MaintenanceSchedule)
	assert.True(t, result.MaintenanceSchedule.Enabled)
	assert.Equal(t, "0 4 * * *", result.MaintenanceSchedule.CheckCron)
	assert.Equal(t, "0 4 * * 0", result.MaintenanceSchedule.WindowCron)
}

func TestServerToData_WithUpdateHistory(t *testing.T) {
	t.Parallel()

	now := metav1.Now()
	server := makeTestServer("test-server", "default")
	server.Status.LastUpdate = &mck8slexlav1alpha1.UpdateHistory{
		AppliedAt:       now,
		PreviousVersion: "1.21.0",
		Successful:      true,
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestSchemeWithAppsV1()).
		WithObjects(server).
		Build()

	svc := NewServerService(fakeClient)

	result, err := svc.GetServer(context.Background(), "default", "test-server")

	require.NoError(t, err)
	require.NotNil(t, result.UpdateHistory)
	assert.Equal(t, "1.21.0", result.UpdateHistory.PreviousVersion)
	assert.True(t, result.UpdateHistory.Successful)
}

func TestServerToData_WithPlugins(t *testing.T) {
	t.Parallel()

	server := makeTestServer("test-server", "default")
	server.Status.Plugins = []mck8slexlav1alpha1.ServerPluginStatus{
		{
			PluginRef:       mck8slexlav1alpha1.PluginRef{Name: "plugin1", Namespace: "default"},
			ResolvedVersion: "1.0.0",
			CurrentVersion:  "0.9.0",
			DesiredVersion:  "1.0.0",
			Compatible:      true,
			Source:          "hangar",
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestSchemeWithAppsV1()).
		WithObjects(server).
		Build()

	svc := NewServerService(fakeClient)

	result, err := svc.GetServer(context.Background(), "default", "test-server")

	require.NoError(t, err)
	assert.Len(t, result.Plugins, 1)
	assert.Equal(t, "plugin1", result.Plugins[0].Name)
	assert.Equal(t, "1.0.0", result.Plugins[0].ResolvedVersion)
	assert.True(t, result.Plugins[0].Compatible)
}

// --- fetchPluginDetails tests ---

func TestFetchPluginDetails_WithExistingPlugin(t *testing.T) {
	t.Parallel()

	server := makeTestServer("test-server", "default")
	server.Status.Plugins = []mck8slexlav1alpha1.ServerPluginStatus{
		{
			PluginRef:       mck8slexlav1alpha1.PluginRef{Name: "test-plugin", Namespace: "default"},
			ResolvedVersion: "1.0.0",
			Compatible:      true,
		},
	}

	plugin := &mck8slexlav1alpha1.Plugin{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-plugin",
			Namespace: "default",
		},
		Spec: mck8slexlav1alpha1.PluginSpec{
			Source: mck8slexlav1alpha1.PluginSource{
				Type:    "hangar",
				Project: "test-project",
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestSchemeWithAppsV1()).
		WithObjects(server, plugin).
		Build()

	svc := NewServerService(fakeClient)

	result, err := svc.GetServer(context.Background(), "default", "test-server")

	require.NoError(t, err)
	require.Len(t, result.Plugins, 1)
	assert.Equal(t, "hangar", result.Plugins[0].SourceType)
	assert.Equal(t, "test-project", result.Plugins[0].Project)
}

func TestFetchPluginDetails_PluginNotFound(t *testing.T) {
	t.Parallel()

	server := makeTestServer("test-server", "default")
	server.Status.Plugins = []mck8slexlav1alpha1.ServerPluginStatus{
		{
			PluginRef:       mck8slexlav1alpha1.PluginRef{Name: "missing-plugin", Namespace: "default"},
			ResolvedVersion: "1.0.0",
			Compatible:      true,
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestSchemeWithAppsV1()).
		WithObjects(server).
		Build()

	svc := NewServerService(fakeClient)

	result, err := svc.GetServer(context.Background(), "default", "test-server")

	// Should still return data with partial plugin info
	require.NoError(t, err)
	require.Len(t, result.Plugins, 1)
	assert.Equal(t, "missing-plugin", result.Plugins[0].Name)
	assert.Equal(t, "1.0.0", result.Plugins[0].ResolvedVersion)
	assert.Empty(t, result.Plugins[0].SourceType)
}

// --- Constructor test ---

func TestNewServerService(t *testing.T) {
	t.Parallel()

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestSchemeWithAppsV1()).
		Build()

	svc := NewServerService(fakeClient)

	assert.NotNil(t, svc)
}
