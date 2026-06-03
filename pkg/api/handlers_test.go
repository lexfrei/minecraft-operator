/*
Copyright 2026, Aleksei Sviridkin.

SPDX-License-Identifier: BSD-3-Clause
*/

package api

import (
	"context"
	"fmt"
	"testing"

	"github.com/lexfrei/minecraft-operator/api/openapi/generated"
	mcv1beta1 "github.com/lexfrei/minecraft-operator/api/v1beta1"
	"github.com/lexfrei/minecraft-operator/pkg/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const testProject = "TestProject"

// Test fixture constants.
const (
	testValue            = "test"
	testNamespaceDefault = "default"
	testServerName       = "test-server"
	testMyServerName     = "my-server"
	testMyPluginName     = "my-plugin"
	testDeleteMeName     = "delete-me"
	testNonexistentName  = "nonexistent"
	testServerVersion    = "1.21.1"
	testSourceHangar     = "hangar"
	testStatusRunning    = "running"
	testEndpointWebUI    = "web-ui"
	labelKeyApp          = "app"
	labelValuePapermc    = "papermc"
)

func newTestScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	_ = mcv1beta1.AddToScheme(scheme)

	return scheme
}

func newTestServer() *Server {
	scheme := newTestScheme()

	c := fake.NewClientBuilder().WithScheme(scheme).Build()

	return NewServer(c, VersionInfo{
		Version:   "v0.1.0-test",
		GitCommit: "abc123",
		BuildDate: "2025-01-01T00:00:00Z",
	})
}

// --- GetHealth tests ---

func TestGetHealth_Healthy(t *testing.T) {
	srv := newTestServer()
	ctx := context.Background()

	resp, err := srv.GetHealth(ctx, generated.GetHealthRequestObject{})
	require.NoError(t, err)

	healthResp, ok := resp.(generated.GetHealth200JSONResponse)
	require.True(t, ok, "Expected 200 response, got %T", resp)
	assert.Equal(t, generated.Healthy, healthResp.Status)
}

// --- GetVersion tests ---

func TestGetVersion_ReturnsVersionInfo(t *testing.T) {
	srv := newTestServer()
	ctx := context.Background()

	resp, err := srv.GetVersion(ctx, generated.GetVersionRequestObject{})
	require.NoError(t, err)

	versionResp, ok := resp.(generated.GetVersion200JSONResponse)
	require.True(t, ok, "Expected 200 response, got %T", resp)
	assert.Equal(t, "v0.1.0-test", versionResp.Version)
	assert.Equal(t, "v1", versionResp.ApiVersion)
	assert.NotNil(t, versionResp.GitCommit)
	assert.Equal(t, "abc123", *versionResp.GitCommit)
	assert.NotNil(t, versionResp.BuildDate)
	assert.NotNil(t, versionResp.GoVersion)
}

// --- ListNamespaces tests ---

func TestListNamespaces_IncludesDefault(t *testing.T) {
	srv := newTestServer()
	ctx := context.Background()

	resp, err := srv.ListNamespaces(ctx, generated.ListNamespacesRequestObject{})
	require.NoError(t, err)

	nsResp, ok := resp.(generated.ListNamespaces200JSONResponse)
	require.True(t, ok, "Expected 200 response, got %T", resp)
	assert.Contains(t, nsResp.Namespaces, testNamespaceDefault,
		"ListNamespaces should always include 'default' namespace")
}

func TestListNamespaces_IncludesServerNamespaces(t *testing.T) {
	server := &mcv1beta1.PaperMCServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testServerName,
			Namespace: "minecraft",
		},
		Spec: mcv1beta1.PaperMCServerSpec{
			Version:        testServerVersion,
			UpdateStrategy: updateStrategyLatest,
		},
	}

	scheme := newTestScheme()
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(server).
		Build()

	srv := NewServer(c, VersionInfo{Version: testValue})
	ctx := context.Background()

	resp, err := srv.ListNamespaces(ctx, generated.ListNamespacesRequestObject{})
	require.NoError(t, err)

	nsResp, ok := resp.(generated.ListNamespaces200JSONResponse)
	require.True(t, ok)
	assert.Contains(t, nsResp.Namespaces, "minecraft",
		"Should include namespace from existing server")
}

// --- ListServers tests ---

func TestListServers_Empty(t *testing.T) {
	srv := newTestServer()
	ctx := context.Background()

	resp, err := srv.ListServers(ctx, generated.ListServersRequestObject{})
	require.NoError(t, err)

	listResp, ok := resp.(generated.ListServers200JSONResponse)
	require.True(t, ok, "Expected 200 response, got %T", resp)
	assert.Empty(t, listResp.Servers)
}

func TestListServers_WithServer(t *testing.T) {
	server := &mcv1beta1.PaperMCServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testMyServerName,
			Namespace: testNamespaceDefault,
		},
		Spec: mcv1beta1.PaperMCServerSpec{
			Version:        testServerVersion,
			UpdateStrategy: updateStrategyLatest,
		},
	}

	scheme := newTestScheme()
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(server).
		Build()

	srv := NewServer(c, VersionInfo{Version: testValue})
	ctx := context.Background()

	resp, err := srv.ListServers(ctx, generated.ListServersRequestObject{})
	require.NoError(t, err)

	listResp, ok := resp.(generated.ListServers200JSONResponse)
	require.True(t, ok)
	assert.Len(t, listResp.Servers, 1)
	assert.Equal(t, testMyServerName, listResp.Servers[0].Name)
	assert.Equal(t, testNamespaceDefault, listResp.Servers[0].Namespace)
}

func TestListServers_FilterByNamespace(t *testing.T) {
	server1 := &mcv1beta1.PaperMCServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "server1",
			Namespace: "ns1",
		},
		Spec: mcv1beta1.PaperMCServerSpec{
			Version:        testServerVersion,
			UpdateStrategy: updateStrategyLatest,
		},
	}
	server2 := &mcv1beta1.PaperMCServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "server2",
			Namespace: "ns2",
		},
		Spec: mcv1beta1.PaperMCServerSpec{
			Version:        testServerVersion,
			UpdateStrategy: updateStrategyLatest,
		},
	}

	scheme := newTestScheme()
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(server1, server2).
		Build()

	srv := NewServer(c, VersionInfo{Version: testValue})
	ctx := context.Background()

	ns := "ns1"
	resp, err := srv.ListServers(ctx, generated.ListServersRequestObject{
		Params: generated.ListServersParams{Namespace: &ns},
	})
	require.NoError(t, err)

	listResp, ok := resp.(generated.ListServers200JSONResponse)
	require.True(t, ok)
	assert.Len(t, listResp.Servers, 1)
	assert.Equal(t, "server1", listResp.Servers[0].Name)
}

// --- CreateServer tests ---

func TestCreateServer_NilBody(t *testing.T) {
	srv := newTestServer()
	ctx := context.Background()

	resp, err := srv.CreateServer(ctx, generated.CreateServerRequestObject{
		Body: nil,
	})
	require.NoError(t, err)

	_, ok := resp.(generated.CreateServer400JSONResponse)
	assert.True(t, ok, "Expected 400 response for nil body, got %T", resp)
}

func TestCreateServer_Success(t *testing.T) {
	srv := newTestServer()
	ctx := context.Background()

	resp, err := srv.CreateServer(ctx, generated.CreateServerRequestObject{
		Body: &generated.CreateServerJSONRequestBody{
			Name:           testServerName,
			Namespace:      testNamespaceDefault,
			UpdateStrategy: updateStrategyLatest,
		},
	})
	require.NoError(t, err)

	createResp, ok := resp.(generated.CreateServer201JSONResponse)
	require.True(t, ok, "Expected 201 response, got %T", resp)
	assert.Equal(t, testServerName, createResp.Name)
	assert.Equal(t, testNamespaceDefault, createResp.Namespace)
}

func TestCreateServer_Duplicate(t *testing.T) {
	server := &mcv1beta1.PaperMCServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "existing-server",
			Namespace: testNamespaceDefault,
		},
		Spec: mcv1beta1.PaperMCServerSpec{
			Version:        testServerVersion,
			UpdateStrategy: updateStrategyLatest,
		},
	}

	scheme := newTestScheme()
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(server).
		Build()

	srv := NewServer(c, VersionInfo{Version: testValue})
	ctx := context.Background()

	resp, err := srv.CreateServer(ctx, generated.CreateServerRequestObject{
		Body: &generated.CreateServerJSONRequestBody{
			Name:           "existing-server",
			Namespace:      testNamespaceDefault,
			UpdateStrategy: updateStrategyLatest,
		},
	})
	require.NoError(t, err)

	_, ok := resp.(generated.CreateServer409JSONResponse)
	assert.True(t, ok, "Expected 409 response for duplicate server, got %T", resp)
}

// --- GetServer tests ---

func TestGetServer_Found(t *testing.T) {
	server := &mcv1beta1.PaperMCServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testMyServerName,
			Namespace: testNamespaceDefault,
		},
		Spec: mcv1beta1.PaperMCServerSpec{
			Version:        testServerVersion,
			UpdateStrategy: updateStrategyLatest,
		},
	}

	scheme := newTestScheme()
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(server).
		Build()

	srv := NewServer(c, VersionInfo{Version: testValue})
	ctx := context.Background()

	resp, err := srv.GetServer(ctx, generated.GetServerRequestObject{
		Namespace: testNamespaceDefault,
		Name:      testMyServerName,
	})
	require.NoError(t, err)

	getResp, ok := resp.(generated.GetServer200JSONResponse)
	require.True(t, ok, "Expected 200 response, got %T", resp)
	assert.Equal(t, testMyServerName, getResp.Name)
}

func TestGetServer_NotFound(t *testing.T) {
	srv := newTestServer()
	ctx := context.Background()

	resp, err := srv.GetServer(ctx, generated.GetServerRequestObject{
		Namespace: testNamespaceDefault,
		Name:      testNonexistentName,
	})
	require.NoError(t, err)

	_, ok := resp.(generated.GetServer404JSONResponse)
	assert.True(t, ok, "Expected 404 response for nonexistent server, got %T", resp)
}

// --- DeleteServer tests ---

func TestDeleteServer_Success(t *testing.T) {
	server := &mcv1beta1.PaperMCServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testDeleteMeName,
			Namespace: testNamespaceDefault,
		},
		Spec: mcv1beta1.PaperMCServerSpec{
			Version:        testServerVersion,
			UpdateStrategy: updateStrategyLatest,
		},
	}

	scheme := newTestScheme()
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(server).
		Build()

	srv := NewServer(c, VersionInfo{Version: testValue})
	ctx := context.Background()

	resp, err := srv.DeleteServer(ctx, generated.DeleteServerRequestObject{
		Namespace: testNamespaceDefault,
		Name:      testDeleteMeName,
	})
	require.NoError(t, err)

	_, ok := resp.(generated.DeleteServer204Response)
	assert.True(t, ok, "Expected 204 response, got %T", resp)
}

func TestDeleteServer_NotFound(t *testing.T) {
	srv := newTestServer()
	ctx := context.Background()

	resp, err := srv.DeleteServer(ctx, generated.DeleteServerRequestObject{
		Namespace: testNamespaceDefault,
		Name:      testNonexistentName,
	})
	require.NoError(t, err)

	_, ok := resp.(generated.DeleteServer404JSONResponse)
	assert.True(t, ok, "Expected 404 response, got %T", resp)
}

// --- UpdateServer tests ---

func TestUpdateServer_NilBody(t *testing.T) {
	srv := newTestServer()
	ctx := context.Background()

	resp, err := srv.UpdateServer(ctx, generated.UpdateServerRequestObject{
		Namespace: testNamespaceDefault,
		Name:      testValue,
		Body:      nil,
	})
	require.NoError(t, err)

	_, ok := resp.(generated.UpdateServer400JSONResponse)
	assert.True(t, ok, "Expected 400 response for nil body, got %T", resp)
}

func TestUpdateServer_NotFound(t *testing.T) {
	srv := newTestServer()
	ctx := context.Background()

	strategy := generated.UpdateStrategy(updateStrategyLatest)
	resp, err := srv.UpdateServer(ctx, generated.UpdateServerRequestObject{
		Namespace: testNamespaceDefault,
		Name:      testNonexistentName,
		Body: &generated.ServerUpdateRequest{
			UpdateStrategy: &strategy,
		},
	})
	require.NoError(t, err)

	_, ok := resp.(generated.UpdateServer404JSONResponse)
	assert.True(t, ok, "Expected 404 response, got %T", resp)
}

// --- ResolveServer tests ---

func TestResolveServer_NotFound(t *testing.T) {
	srv := newTestServer()
	ctx := context.Background()

	resp, err := srv.ResolveServer(ctx, generated.ResolveServerRequestObject{
		Namespace: testNamespaceDefault,
		Name:      testNonexistentName,
	})
	require.NoError(t, err)

	_, ok := resp.(generated.ResolveServer404JSONResponse)
	assert.True(t, ok, "Expected 404 response, got %T", resp)
}

// --- ApplyNowServer tests ---

func TestApplyNowServer_NotFound(t *testing.T) {
	srv := newTestServer()
	ctx := context.Background()

	resp, err := srv.ApplyNowServer(ctx, generated.ApplyNowServerRequestObject{
		Namespace: testNamespaceDefault,
		Name:      testNonexistentName,
	})
	require.NoError(t, err)

	_, ok := resp.(generated.ApplyNowServer404JSONResponse)
	assert.True(t, ok, "Expected 404 response, got %T", resp)
}

// --- ListPlugins tests ---

func TestListPlugins_Empty(t *testing.T) {
	srv := newTestServer()
	ctx := context.Background()

	resp, err := srv.ListPlugins(ctx, generated.ListPluginsRequestObject{})
	require.NoError(t, err)

	listResp, ok := resp.(generated.ListPlugins200JSONResponse)
	require.True(t, ok, "Expected 200 response, got %T", resp)
	assert.Empty(t, listResp.Plugins)
}

func TestListPlugins_WithPlugins(t *testing.T) {
	plugin := &mcv1beta1.Plugin{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-plugin",
			Namespace: testNamespaceDefault,
		},
		Spec: mcv1beta1.PluginSpec{
			Source: mcv1beta1.PluginSource{
				Type:    testSourceHangar,
				Project: testProject,
			},
			UpdateStrategy: updateStrategyLatest,
			InstanceSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{labelKeyApp: labelValuePapermc},
			},
		},
	}

	scheme := newTestScheme()
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(plugin).
		Build()

	srv := NewServer(c, VersionInfo{Version: testValue})
	ctx := context.Background()

	resp, err := srv.ListPlugins(ctx, generated.ListPluginsRequestObject{})
	require.NoError(t, err)

	listResp, ok := resp.(generated.ListPlugins200JSONResponse)
	require.True(t, ok)
	assert.Len(t, listResp.Plugins, 1)
	assert.Equal(t, "test-plugin", listResp.Plugins[0].Name)
}

// --- CreatePlugin tests ---

func TestCreatePlugin_NilBody(t *testing.T) {
	srv := newTestServer()
	ctx := context.Background()

	resp, err := srv.CreatePlugin(ctx, generated.CreatePluginRequestObject{
		Body: nil,
	})
	require.NoError(t, err)

	_, ok := resp.(generated.CreatePlugin400JSONResponse)
	assert.True(t, ok, "Expected 400 response for nil body, got %T", resp)
}

func TestCreatePlugin_Success(t *testing.T) {
	srv := newTestServer()
	ctx := context.Background()

	project := testProject
	resp, err := srv.CreatePlugin(ctx, generated.CreatePluginRequestObject{
		Body: &generated.CreatePluginJSONRequestBody{
			Name:      testMyPluginName,
			Namespace: testNamespaceDefault,
			Source: generated.PluginSource{
				Type:    testSourceHangar,
				Project: &project,
			},
			InstanceSelector: generated.LabelSelector{
				MatchLabels: &map[string]string{labelKeyApp: labelValuePapermc},
			},
		},
	})
	require.NoError(t, err)

	createResp, ok := resp.(generated.CreatePlugin201JSONResponse)
	require.True(t, ok, "Expected 201 response, got %T", resp)
	assert.Equal(t, testMyPluginName, createResp.Name)
	assert.Equal(t, testNamespaceDefault, createResp.Namespace)
}

func TestCreatePlugin_Duplicate(t *testing.T) {
	plugin := &mcv1beta1.Plugin{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "existing-plugin",
			Namespace: testNamespaceDefault,
		},
		Spec: mcv1beta1.PluginSpec{
			Source: mcv1beta1.PluginSource{
				Type:    testSourceHangar,
				Project: "ExistingProject",
			},
			UpdateStrategy: updateStrategyLatest,
			InstanceSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{labelKeyApp: labelValuePapermc},
			},
		},
	}

	scheme := newTestScheme()
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(plugin).
		Build()

	srv := NewServer(c, VersionInfo{Version: testValue})
	ctx := context.Background()

	project := "ExistingProject"
	resp, err := srv.CreatePlugin(ctx, generated.CreatePluginRequestObject{
		Body: &generated.CreatePluginJSONRequestBody{
			Name:      "existing-plugin",
			Namespace: testNamespaceDefault,
			Source: generated.PluginSource{
				Type:    testSourceHangar,
				Project: &project,
			},
			InstanceSelector: generated.LabelSelector{
				MatchLabels: &map[string]string{labelKeyApp: labelValuePapermc},
			},
		},
	})
	require.NoError(t, err)

	_, ok := resp.(generated.CreatePlugin409JSONResponse)
	assert.True(t, ok, "Expected 409 response for duplicate plugin, got %T", resp)
}

// --- GetPlugin tests ---

func TestGetPlugin_Found(t *testing.T) {
	plugin := &mcv1beta1.Plugin{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testMyPluginName,
			Namespace: testNamespaceDefault,
		},
		Spec: mcv1beta1.PluginSpec{
			Source: mcv1beta1.PluginSource{
				Type:    testSourceHangar,
				Project: testProject,
			},
			UpdateStrategy: updateStrategyLatest,
			InstanceSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{labelKeyApp: labelValuePapermc},
			},
		},
	}

	scheme := newTestScheme()
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(plugin).
		Build()

	srv := NewServer(c, VersionInfo{Version: testValue})
	ctx := context.Background()

	resp, err := srv.GetPlugin(ctx, generated.GetPluginRequestObject{
		Namespace: testNamespaceDefault,
		Name:      testMyPluginName,
	})
	require.NoError(t, err)

	getResp, ok := resp.(generated.GetPlugin200JSONResponse)
	require.True(t, ok, "Expected 200 response, got %T", resp)
	assert.Equal(t, testMyPluginName, getResp.Name)
}

func TestGetPlugin_ReturnsEndpoints(t *testing.T) {
	plugin := &mcv1beta1.Plugin{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "plugin-with-endpoints",
			Namespace: testNamespaceDefault,
		},
		Spec: mcv1beta1.PluginSpec{
			Source: mcv1beta1.PluginSource{
				Type:    testSourceHangar,
				Project: testProject,
			},
			UpdateStrategy: updateStrategyLatest,
			Endpoints: []mcv1beta1.PluginEndpoint{
				{Name: testEndpointWebUI, Port: 8100, Protocol: "HTTP"},
				{Name: "metrics", Port: 9100, Protocol: "TCP"},
			},
			InstanceSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{labelKeyApp: labelValuePapermc},
			},
		},
	}

	scheme := newTestScheme()
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(plugin).
		Build()

	srv := NewServer(c, VersionInfo{Version: testValue})
	ctx := context.Background()

	resp, err := srv.GetPlugin(ctx, generated.GetPluginRequestObject{
		Namespace: testNamespaceDefault,
		Name:      "plugin-with-endpoints",
	})
	require.NoError(t, err)

	getResp, ok := resp.(generated.GetPlugin200JSONResponse)
	require.True(t, ok, "Expected 200 response, got %T", resp)
	require.NotNil(t, getResp.Endpoints, "Endpoints should not be nil")
	require.Len(t, *getResp.Endpoints, 2)
	assert.Equal(t, testEndpointWebUI, (*getResp.Endpoints)[0].Name)
	assert.Equal(t, 8100, (*getResp.Endpoints)[0].Port)
	assert.Equal(t, "metrics", (*getResp.Endpoints)[1].Name)
	assert.Equal(t, 9100, (*getResp.Endpoints)[1].Port)
}

func TestGetPlugin_NotFound(t *testing.T) {
	srv := newTestServer()
	ctx := context.Background()

	resp, err := srv.GetPlugin(ctx, generated.GetPluginRequestObject{
		Namespace: testNamespaceDefault,
		Name:      testNonexistentName,
	})
	require.NoError(t, err)

	_, ok := resp.(generated.GetPlugin404JSONResponse)
	assert.True(t, ok, "Expected 404 response, got %T", resp)
}

// --- UpdatePlugin tests ---

func TestUpdatePlugin_NilBody(t *testing.T) {
	srv := newTestServer()
	ctx := context.Background()

	resp, err := srv.UpdatePlugin(ctx, generated.UpdatePluginRequestObject{
		Namespace: testNamespaceDefault,
		Name:      testValue,
		Body:      nil,
	})
	require.NoError(t, err)

	_, ok := resp.(generated.UpdatePlugin400JSONResponse)
	assert.True(t, ok, "Expected 400 response for nil body, got %T", resp)
}

func TestUpdatePlugin_NotFound(t *testing.T) {
	srv := newTestServer()
	ctx := context.Background()

	strategy := generated.UpdateStrategy(updateStrategyLatest)
	resp, err := srv.UpdatePlugin(ctx, generated.UpdatePluginRequestObject{
		Namespace: testNamespaceDefault,
		Name:      testNonexistentName,
		Body: &generated.PluginUpdateRequest{
			UpdateStrategy: &strategy,
		},
	})
	require.NoError(t, err)

	_, ok := resp.(generated.UpdatePlugin404JSONResponse)
	assert.True(t, ok, "Expected 404 response, got %T", resp)
}

// --- DeletePlugin tests ---

func TestDeletePlugin_Success(t *testing.T) {
	plugin := &mcv1beta1.Plugin{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testDeleteMeName,
			Namespace: testNamespaceDefault,
		},
		Spec: mcv1beta1.PluginSpec{
			Source: mcv1beta1.PluginSource{
				Type:    testSourceHangar,
				Project: testProject,
			},
			UpdateStrategy: updateStrategyLatest,
			InstanceSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{labelKeyApp: labelValuePapermc},
			},
		},
	}

	scheme := newTestScheme()
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(plugin).
		Build()

	srv := NewServer(c, VersionInfo{Version: testValue})
	ctx := context.Background()

	resp, err := srv.DeletePlugin(ctx, generated.DeletePluginRequestObject{
		Namespace: testNamespaceDefault,
		Name:      testDeleteMeName,
	})
	require.NoError(t, err)

	_, ok := resp.(generated.DeletePlugin204Response)
	assert.True(t, ok, "Expected 204 response, got %T", resp)
}

func TestDeletePlugin_NotFound(t *testing.T) {
	srv := newTestServer()
	ctx := context.Background()

	resp, err := srv.DeletePlugin(ctx, generated.DeletePluginRequestObject{
		Namespace: testNamespaceDefault,
		Name:      testNonexistentName,
	})
	require.NoError(t, err)

	_, ok := resp.(generated.DeletePlugin404JSONResponse)
	assert.True(t, ok, "Expected 404 response, got %T", resp)
}

// --- ResolvePlugin tests ---

func TestResolvePlugin_NotFound(t *testing.T) {
	srv := newTestServer()
	ctx := context.Background()

	resp, err := srv.ResolvePlugin(ctx, generated.ResolvePluginRequestObject{
		Namespace: testNamespaceDefault,
		Name:      testNonexistentName,
	})
	require.NoError(t, err)

	_, ok := resp.(generated.ResolvePlugin404JSONResponse)
	assert.True(t, ok, "Expected 404 response, got %T", resp)
}

// --- Helper function tests ---

func TestIsNotFoundError(t *testing.T) {
	assert.False(t, isNotFoundError(nil),
		"nil error should not be 'not found'")
	assert.False(t, isNotFoundError(assert.AnError),
		"Generic error without 'not found' should return false")

	assert.True(t, isNotFoundError(fmt.Errorf("resource not found")),
		"Error containing 'not found' should be detected")
	assert.True(t, isNotFoundError(fmt.Errorf("NotFound")),
		"Error containing 'NotFound' should be detected")
}

func TestContainsString(t *testing.T) {
	assert.True(t, containsString("not found", "not found"))
	assert.True(t, containsString("resource NotFound", "NotFound"))
	assert.False(t, containsString("success", "error"))
	assert.False(t, containsString("", "something"))
}

// --- Conversion function tests ---

func TestServerDataToSummary_MinimalFields(t *testing.T) {
	// Verify conversion handles minimal data without panicking
	data := service.ServerData{
		Name:           testServerName,
		Namespace:      testNamespaceDefault,
		UpdateStrategy: updateStrategyLatest,
		Status:         testStatusRunning,
	}
	summary := serverDataToSummary(data)
	assert.Equal(t, testServerName, summary.Name)
	assert.Equal(t, testNamespaceDefault, summary.Namespace)
	assert.Equal(t, generated.UpdateStrategy(updateStrategyLatest), summary.UpdateStrategy)
}

func TestServerDataToSummary_WithAllFields(t *testing.T) {
	data := service.ServerData{
		Name:           "full-server",
		Namespace:      "minecraft",
		CurrentVersion: testServerVersion,
		DesiredVersion: "1.21.2",
		UpdateStrategy: "auto",
		Status:         testStatusRunning,
		PluginCount:    3,
		Labels:         map[string]string{"env": "production"},
		AvailableUpdate: &service.AvailableUpdateData{
			Version: "1.21.2",
			Build:   100,
		},
	}
	summary := serverDataToSummary(data)
	assert.Equal(t, "full-server", summary.Name)
	require.NotNil(t, summary.CurrentVersion)
	assert.Equal(t, testServerVersion, *summary.CurrentVersion)
	require.NotNil(t, summary.DesiredVersion)
	assert.Equal(t, "1.21.2", *summary.DesiredVersion)
	require.NotNil(t, summary.PluginCount)
	assert.Equal(t, 3, *summary.PluginCount)
	require.NotNil(t, summary.Labels)
	require.NotNil(t, summary.AvailableUpdate)
}

func TestPluginDataToSummary_MinimalFields(t *testing.T) {
	data := service.PluginData{
		Name:           "test-plugin",
		Namespace:      testNamespaceDefault,
		SourceType:     testSourceHangar,
		UpdateStrategy: updateStrategyLatest,
	}
	summary := pluginDataToSummary(data)
	assert.Equal(t, "test-plugin", summary.Name)
	assert.Equal(t, testNamespaceDefault, summary.Namespace)
	assert.Equal(t, generated.PluginSourceType(testSourceHangar), summary.SourceType)
}

func TestServerDataToDetail_WithPlugins(t *testing.T) {
	data := service.ServerData{
		Name:           "detail-server",
		Namespace:      testNamespaceDefault,
		UpdateStrategy: updateStrategyLatest,
		Status:         testStatusRunning,
		Plugins: []service.ServerPluginData{
			{
				Name:       "plugin1",
				Namespace:  testNamespaceDefault,
				Compatible: true,
				SourceType: testSourceHangar,
			},
		},
	}
	detail := serverDataToDetail(data)
	assert.Equal(t, "detail-server", detail.Name)
	require.NotNil(t, detail.Plugins)
	assert.Len(t, *detail.Plugins, 1)
	assert.Equal(t, "plugin1", (*detail.Plugins)[0].Name)
}

func TestPluginDataToDetail_WithVersions(t *testing.T) {
	data := service.PluginData{
		Name:           "detail-plugin",
		Namespace:      testNamespaceDefault,
		SourceType:     testSourceHangar,
		UpdateStrategy: updateStrategyLatest,
		AvailableVersions: []service.PluginVersionData{
			{
				Version:           "1.0.0",
				SupportedVersions: []string{testServerVersion},
				DownloadURL:       "https://example.com/plugin-1.0.0.jar",
			},
		},
		MatchedInstances: []service.MatchedInstanceData{
			{
				Name:       "server1",
				Namespace:  testNamespaceDefault,
				Compatible: true,
			},
		},
	}
	detail := pluginDataToDetail(data)
	assert.Equal(t, "detail-plugin", detail.Name)
	require.NotNil(t, detail.AvailableVersions)
	assert.Len(t, *detail.AvailableVersions, 1)
	require.NotNil(t, detail.MatchedInstances)
	assert.Len(t, *detail.MatchedInstances, 1)
}

// --- Request conversion function tests ---

func TestServerCreateRequestToData(t *testing.T) {
	version := testServerVersion
	req := generated.ServerCreateRequest{
		Name:           "new-server",
		Namespace:      testNamespaceDefault,
		UpdateStrategy: updateStrategyPin,
		Version:        &version,
	}

	data := serverCreateRequestToData(req)
	assert.Equal(t, "new-server", data.Name)
	assert.Equal(t, testNamespaceDefault, data.Namespace)
	assert.Equal(t, updateStrategyPin, data.UpdateStrategy)
	assert.Equal(t, testServerVersion, data.Version)
}

func TestPluginCreateRequestToData(t *testing.T) {
	project := testProject
	req := generated.PluginCreateRequest{
		Name:      "new-plugin",
		Namespace: testNamespaceDefault,
		Source: generated.PluginSource{
			Type:    testSourceHangar,
			Project: &project,
		},
		InstanceSelector: generated.LabelSelector{
			MatchLabels: &map[string]string{labelKeyApp: labelValuePapermc},
		},
	}

	data := pluginCreateRequestToData(req)
	assert.Equal(t, "new-plugin", data.Name)
	assert.Equal(t, testNamespaceDefault, data.Namespace)
	assert.Equal(t, testSourceHangar, data.Source.Type)
	assert.Equal(t, testProject, data.Source.Project)
}

func TestLabelSelectorToK8s(t *testing.T) {
	sel := generated.LabelSelector{
		MatchLabels: &map[string]string{
			labelKeyApp: labelValuePapermc,
			"tier":      "game",
		},
	}

	result := labelSelectorToK8s(sel)
	assert.Equal(t, labelValuePapermc, result.MatchLabels[labelKeyApp])
	assert.Equal(t, "game", result.MatchLabels["tier"])
}

func TestCreatePlugin_EndpointInvalidPort_ShouldReturn400(t *testing.T) {
	srv := newTestServer()
	ctx := context.Background()

	project := testProject
	resp, err := srv.CreatePlugin(ctx, generated.CreatePluginRequestObject{
		Body: &generated.CreatePluginJSONRequestBody{
			Name:      "bad-endpoint-plugin",
			Namespace: testNamespaceDefault,
			Endpoints: &[]generated.PluginEndpoint{
				{Name: testEndpointWebUI, Port: -1},
			},
			Source: generated.PluginSource{
				Type:    testSourceHangar,
				Project: &project,
			},
			InstanceSelector: generated.LabelSelector{
				MatchLabels: &map[string]string{labelKeyApp: labelValuePapermc},
			},
		},
	})
	require.NoError(t, err)

	_, ok := resp.(generated.CreatePlugin400JSONResponse)
	assert.True(t, ok, "Expected 400 response for negative endpoint port, got %T", resp)
}

func TestCreatePlugin_EndpointOverflowPort_ShouldReturn400(t *testing.T) {
	srv := newTestServer()
	ctx := context.Background()

	project := testProject
	resp, err := srv.CreatePlugin(ctx, generated.CreatePluginRequestObject{
		Body: &generated.CreatePluginJSONRequestBody{
			Name:      "overflow-endpoint-plugin",
			Namespace: testNamespaceDefault,
			Endpoints: &[]generated.PluginEndpoint{
				{Name: testEndpointWebUI, Port: 100000},
			},
			Source: generated.PluginSource{
				Type:    testSourceHangar,
				Project: &project,
			},
			InstanceSelector: generated.LabelSelector{
				MatchLabels: &map[string]string{labelKeyApp: labelValuePapermc},
			},
		},
	})
	require.NoError(t, err)

	_, ok := resp.(generated.CreatePlugin400JSONResponse)
	assert.True(t, ok, "Expected 400 response for endpoint port > 65535, got %T", resp)
}

func TestUpdatePlugin_EndpointInvalidPort_ShouldReturn400(t *testing.T) {
	plugin := &mcv1beta1.Plugin{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "endpoint-test-plugin",
			Namespace: testNamespaceDefault,
		},
		Spec: mcv1beta1.PluginSpec{
			Source: mcv1beta1.PluginSource{
				Type:    testSourceHangar,
				Project: testProject,
			},
			UpdateStrategy: updateStrategyLatest,
			InstanceSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{labelKeyApp: labelValuePapermc},
			},
		},
	}

	scheme := newTestScheme()
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(plugin).
		Build()

	srv := NewServer(c, VersionInfo{Version: testValue})
	ctx := context.Background()

	resp, err := srv.UpdatePlugin(ctx, generated.UpdatePluginRequestObject{
		Namespace: testNamespaceDefault,
		Name:      "endpoint-test-plugin",
		Body: &generated.PluginUpdateRequest{
			Endpoints: &[]generated.PluginEndpoint{
				{Name: testEndpointWebUI, Port: -1},
			},
		},
	})
	require.NoError(t, err)

	_, ok := resp.(generated.UpdatePlugin400JSONResponse)
	assert.True(t, ok, "Expected 400 response for negative endpoint port, got %T", resp)
}

func TestCreatePlugin_DuplicateEndpointNames_ShouldReturn400(t *testing.T) {
	srv := newTestServer()
	ctx := context.Background()

	project := testProject
	proto := generated.TCP
	resp, err := srv.CreatePlugin(ctx, generated.CreatePluginRequestObject{
		Body: &generated.CreatePluginJSONRequestBody{
			Name:      "dup-name-plugin",
			Namespace: testNamespaceDefault,
			Endpoints: &[]generated.PluginEndpoint{
				{Name: testEndpointWebUI, Port: 8100, Protocol: &proto},
				{Name: testEndpointWebUI, Port: 9100, Protocol: &proto},
			},
			Source: generated.PluginSource{
				Type:    testSourceHangar,
				Project: &project,
			},
			InstanceSelector: generated.LabelSelector{
				MatchLabels: &map[string]string{labelKeyApp: labelValuePapermc},
			},
		},
	})
	require.NoError(t, err)

	badResp, ok := resp.(generated.CreatePlugin400JSONResponse)
	require.True(t, ok, "Expected 400 response for duplicate endpoint names, got %T", resp)
	assert.Contains(t, badResp.Error, "Duplicate endpoint name")
}

func TestCreatePlugin_DuplicatePortProtocol_ShouldReturn400(t *testing.T) {
	srv := newTestServer()
	ctx := context.Background()

	project := testProject
	proto := generated.TCP
	resp, err := srv.CreatePlugin(ctx, generated.CreatePluginRequestObject{
		Body: &generated.CreatePluginJSONRequestBody{
			Name:      "dup-port-plugin",
			Namespace: testNamespaceDefault,
			Endpoints: &[]generated.PluginEndpoint{
				{Name: "endpoint-a", Port: 8100, Protocol: &proto},
				{Name: "endpoint-b", Port: 8100, Protocol: &proto},
			},
			Source: generated.PluginSource{
				Type:    testSourceHangar,
				Project: &project,
			},
			InstanceSelector: generated.LabelSelector{
				MatchLabels: &map[string]string{labelKeyApp: labelValuePapermc},
			},
		},
	})
	require.NoError(t, err)

	badResp, ok := resp.(generated.CreatePlugin400JSONResponse)
	require.True(t, ok, "Expected 400 response for duplicate port+protocol, got %T", resp)
	assert.Contains(t, badResp.Error, "Duplicate port+protocol")
}

func TestCreatePlugin_InvalidProtocol_ShouldReturn400(t *testing.T) {
	srv := newTestServer()
	ctx := context.Background()

	project := testProject
	badProto := generated.PluginEndpointProtocol("INVALID")
	resp, err := srv.CreatePlugin(ctx, generated.CreatePluginRequestObject{
		Body: &generated.CreatePluginJSONRequestBody{
			Name:      "bad-proto-plugin",
			Namespace: testNamespaceDefault,
			Endpoints: &[]generated.PluginEndpoint{
				{Name: testEndpointWebUI, Port: 8100, Protocol: &badProto},
			},
			Source: generated.PluginSource{
				Type:    testSourceHangar,
				Project: &project,
			},
			InstanceSelector: generated.LabelSelector{
				MatchLabels: &map[string]string{labelKeyApp: labelValuePapermc},
			},
		},
	})
	require.NoError(t, err)

	badResp, ok := resp.(generated.CreatePlugin400JSONResponse)
	require.True(t, ok, "Expected 400 response for invalid protocol, got %T", resp)
	assert.Contains(t, badResp.Error, "Invalid protocol")
}

func TestCreatePlugin_InvalidEndpointName_ShouldReturn400(t *testing.T) {
	srv := newTestServer()
	ctx := context.Background()

	project := testProject
	resp, err := srv.CreatePlugin(ctx, generated.CreatePluginRequestObject{
		Body: &generated.CreatePluginJSONRequestBody{
			Name:      "bad-name-plugin",
			Namespace: testNamespaceDefault,
			Endpoints: &[]generated.PluginEndpoint{
				{Name: "INVALID_NAME", Port: 8100},
			},
			Source: generated.PluginSource{
				Type:    testSourceHangar,
				Project: &project,
			},
			InstanceSelector: generated.LabelSelector{
				MatchLabels: &map[string]string{labelKeyApp: labelValuePapermc},
			},
		},
	})
	require.NoError(t, err)

	badResp, ok := resp.(generated.CreatePlugin400JSONResponse)
	require.True(t, ok, "Expected 400 response for invalid endpoint name, got %T", resp)
	assert.Contains(t, badResp.Error, "DNS label")
}

func TestLabelSelectorToK8s_WithExpressions(t *testing.T) {
	key := labelKeyApp
	operator := generated.LabelSelectorRequirementOperator("In")
	values := []string{labelValuePapermc, "vanilla"}
	sel := generated.LabelSelector{
		MatchExpressions: &[]generated.LabelSelectorRequirement{
			{
				Key:      key,
				Operator: operator,
				Values:   &values,
			},
		},
	}

	result := labelSelectorToK8s(sel)
	require.Len(t, result.MatchExpressions, 1)
	assert.Equal(t, labelKeyApp, result.MatchExpressions[0].Key)
	assert.Equal(t, metav1.LabelSelectorOperator("In"), result.MatchExpressions[0].Operator)
	assert.Equal(t, []string{labelValuePapermc, "vanilla"}, result.MatchExpressions[0].Values)
}

func TestValidateServerCreateRequest_InvalidName(t *testing.T) {
	body := &generated.ServerCreateRequest{
		Name:           "INVALID_NAME!!!",
		Namespace:      testNamespaceDefault,
		UpdateStrategy: updateStrategyLatest,
	}
	msg := validateServerCreateRequest(body)
	assert.Contains(t, msg, "Name")
	assert.Contains(t, msg, "DNS subdomain")
}

func TestValidatePluginCreateRequest_InvalidNamespace(t *testing.T) {
	sel := generated.LabelSelector{}
	src := generated.PluginSource{Type: generated.Hangar, Project: ptr("BlueMap")}
	body := &generated.PluginCreateRequest{
		Name:             "valid",
		Namespace:        "BAD NAMESPACE",
		Source:           src,
		InstanceSelector: sel,
	}
	resp := validatePluginCreateRequest(body)
	assert.NotNil(t, resp, "should reject invalid namespace")
}

func TestValidateServerCreateRequest_InvalidStrategy(t *testing.T) {
	body := &generated.ServerCreateRequest{
		Name:           testValue,
		Namespace:      testNamespaceDefault,
		UpdateStrategy: "garbage",
	}
	msg := validateServerCreateRequest(body)
	assert.Contains(t, msg, "Invalid update strategy")
}

func TestValidateServerCreateRequest_PinRequiresVersion(t *testing.T) {
	body := &generated.ServerCreateRequest{
		Name:           testValue,
		Namespace:      testNamespaceDefault,
		UpdateStrategy: updateStrategyPin,
	}
	msg := validateServerCreateRequest(body)
	assert.Contains(t, msg, "Version is required")
}

func TestValidateServerCreateRequest_BuildPinRequiresBuild(t *testing.T) {
	v := testServerVersion
	body := &generated.ServerCreateRequest{
		Name:           testValue,
		Namespace:      testNamespaceDefault,
		UpdateStrategy: updateStrategyBuildPin,
		Version:        &v,
	}
	msg := validateServerCreateRequest(body)
	assert.Contains(t, msg, "Build is required")
}

func TestValidateServerCreateRequest_Valid(t *testing.T) {
	body := &generated.ServerCreateRequest{
		Name:           testValue,
		Namespace:      testNamespaceDefault,
		UpdateStrategy: updateStrategyLatest,
	}
	msg := validateServerCreateRequest(body)
	assert.Empty(t, msg)
}

func TestValidatePluginSource_HangarRequiresProject(t *testing.T) {
	src := generated.PluginSource{Type: generated.Hangar}
	msg := validatePluginSource(src)
	assert.Contains(t, msg, "Project is required")
}

func TestValidatePluginSource_HangarValid(t *testing.T) {
	project := "BlueMap"
	src := generated.PluginSource{Type: generated.Hangar, Project: &project}
	msg := validatePluginSource(src)
	assert.Empty(t, msg)
}

func TestValidatePluginSource_URLRequiresURL(t *testing.T) {
	src := generated.PluginSource{Type: generated.Url}
	msg := validatePluginSource(src)
	assert.Contains(t, msg, "URL is required")
}

func TestValidatePluginSource_URLRejectsHTTP(t *testing.T) {
	u := "http://evil.com/plugin.jar"
	src := generated.PluginSource{Type: generated.Url, Url: &u}
	msg := validatePluginSource(src)
	assert.NotEmpty(t, msg, "should reject HTTP URL")
}

func TestValidatePluginSource_URLRejectsSSRF(t *testing.T) {
	u := "https://127.0.0.1/plugin.jar"
	src := generated.PluginSource{Type: generated.Url, Url: &u}
	msg := validatePluginSource(src)
	assert.NotEmpty(t, msg, "should reject SSRF address")
}

func TestValidatePluginSource_URLValid(t *testing.T) {
	u := "https://example.com/plugin.jar"
	src := generated.PluginSource{Type: generated.Url, Url: &u}
	msg := validatePluginSource(src)
	assert.Empty(t, msg)
}

func TestValidatePluginSource_UnknownType(t *testing.T) {
	src := generated.PluginSource{Type: "modrinth"}
	msg := validatePluginSource(src)
	assert.Contains(t, msg, "Unsupported source type")
}
