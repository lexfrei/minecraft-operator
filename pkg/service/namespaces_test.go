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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	mck8slexlav1alpha1 "github.com/lexfrei/minecraft-operator/api/v1alpha1"
)

func newTestScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	_ = mck8slexlav1alpha1.AddToScheme(scheme)
	return scheme
}

// --- ListNamespaces tests ---

func TestNamespaceService_ListNamespaces_IncludesDefault(t *testing.T) {
	t.Parallel()

	// Empty cluster
	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestScheme()).
		Build()

	svc := NewNamespaceService(fakeClient)

	namespaces, err := svc.ListNamespaces(context.Background())

	require.NoError(t, err)
	assert.Contains(t, namespaces, "default")
	assert.Len(t, namespaces, 1)
}

func TestNamespaceService_ListNamespaces_FromPlugins(t *testing.T) {
	t.Parallel()

	plugin := &mck8slexlav1alpha1.Plugin{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-plugin",
			Namespace: "minecraft",
		},
		Spec: mck8slexlav1alpha1.PluginSpec{
			Source: mck8slexlav1alpha1.PluginSource{
				Type:    "hangar",
				Project: "test",
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestScheme()).
		WithObjects(plugin).
		Build()

	svc := NewNamespaceService(fakeClient)

	namespaces, err := svc.ListNamespaces(context.Background())

	require.NoError(t, err)
	assert.Contains(t, namespaces, "minecraft")
	assert.Contains(t, namespaces, "default")
	assert.Len(t, namespaces, 2)
}

func TestNamespaceService_ListNamespaces_FromServers(t *testing.T) {
	t.Parallel()

	server := &mck8slexlav1alpha1.PaperMCServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-server",
			Namespace: "games",
		},
		Spec: mck8slexlav1alpha1.PaperMCServerSpec{
			Version: "1.21.1",
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestScheme()).
		WithObjects(server).
		Build()

	svc := NewNamespaceService(fakeClient)

	namespaces, err := svc.ListNamespaces(context.Background())

	require.NoError(t, err)
	assert.Contains(t, namespaces, "games")
	assert.Contains(t, namespaces, "default")
	assert.Len(t, namespaces, 2)
}

func TestNamespaceService_ListNamespaces_Deduplicates(t *testing.T) {
	t.Parallel()

	// Plugin and server in the same namespace
	plugin := &mck8slexlav1alpha1.Plugin{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-plugin",
			Namespace: "shared",
		},
		Spec: mck8slexlav1alpha1.PluginSpec{
			Source: mck8slexlav1alpha1.PluginSource{
				Type:    "hangar",
				Project: "test",
			},
		},
	}

	server := &mck8slexlav1alpha1.PaperMCServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-server",
			Namespace: "shared",
		},
		Spec: mck8slexlav1alpha1.PaperMCServerSpec{
			Version: "1.21.1",
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestScheme()).
		WithObjects(plugin, server).
		Build()

	svc := NewNamespaceService(fakeClient)

	namespaces, err := svc.ListNamespaces(context.Background())

	require.NoError(t, err)
	assert.Contains(t, namespaces, "shared")
	assert.Contains(t, namespaces, "default")
	assert.Len(t, namespaces, 2) // No duplicates
}

func TestNamespaceService_ListNamespaces_MultipleNamespaces(t *testing.T) {
	t.Parallel()

	plugin1 := &mck8slexlav1alpha1.Plugin{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "plugin1",
			Namespace: "ns1",
		},
		Spec: mck8slexlav1alpha1.PluginSpec{
			Source: mck8slexlav1alpha1.PluginSource{
				Type:    "hangar",
				Project: "test1",
			},
		},
	}

	plugin2 := &mck8slexlav1alpha1.Plugin{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "plugin2",
			Namespace: "ns2",
		},
		Spec: mck8slexlav1alpha1.PluginSpec{
			Source: mck8slexlav1alpha1.PluginSource{
				Type:    "hangar",
				Project: "test2",
			},
		},
	}

	server := &mck8slexlav1alpha1.PaperMCServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "server1",
			Namespace: "ns3",
		},
		Spec: mck8slexlav1alpha1.PaperMCServerSpec{
			Version: "1.21.1",
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestScheme()).
		WithObjects(plugin1, plugin2, server).
		Build()

	svc := NewNamespaceService(fakeClient)

	namespaces, err := svc.ListNamespaces(context.Background())

	require.NoError(t, err)
	assert.Contains(t, namespaces, "ns1")
	assert.Contains(t, namespaces, "ns2")
	assert.Contains(t, namespaces, "ns3")
	assert.Contains(t, namespaces, "default")
	assert.Len(t, namespaces, 4)
}

func TestNamespaceService_ListNamespaces_DefaultNamespaceResources(t *testing.T) {
	t.Parallel()

	// Resources in default namespace
	plugin := &mck8slexlav1alpha1.Plugin{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-plugin",
			Namespace: "default",
		},
		Spec: mck8slexlav1alpha1.PluginSpec{
			Source: mck8slexlav1alpha1.PluginSource{
				Type:    "hangar",
				Project: "test",
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestScheme()).
		WithObjects(plugin).
		Build()

	svc := NewNamespaceService(fakeClient)

	namespaces, err := svc.ListNamespaces(context.Background())

	require.NoError(t, err)
	assert.Contains(t, namespaces, "default")
	assert.Len(t, namespaces, 1) // Only default, no duplicates
}

// --- Constructor test ---

func TestNewNamespaceService(t *testing.T) {
	t.Parallel()

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestScheme()).
		Build()

	svc := NewNamespaceService(fakeClient)

	assert.NotNil(t, svc)
}
