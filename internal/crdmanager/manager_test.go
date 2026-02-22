package crdmanager

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestEmbeddedCRDsExist(t *testing.T) {
	t.Parallel()

	entries, err := crdFS.ReadDir("crds")
	require.NoError(t, err, "should read embedded crds directory")

	yamlFiles := make([]string, 0)
	for _, e := range entries {
		if !e.IsDir() {
			yamlFiles = append(yamlFiles, e.Name())
		}
	}

	assert.Len(t, yamlFiles, 2, "should have exactly 2 embedded CRD files")
	assert.Contains(t, yamlFiles, "mc.k8s.lex.la_plugins.yaml")
	assert.Contains(t, yamlFiles, "mc.k8s.lex.la_papermcservers.yaml")
}

func TestParseCRDs(t *testing.T) {
	t.Parallel()

	crds, err := parseCRDs()
	require.NoError(t, err, "should parse all embedded CRDs")
	require.Len(t, crds, 2, "should parse exactly 2 CRDs")

	expectedNames := map[string]bool{
		"plugins.mc.k8s.lex.la":        false,
		"papermcservers.mc.k8s.lex.la": false,
	}

	for _, crd := range crds {
		assert.Equal(t, "CustomResourceDefinition", crd.GetKind(),
			"each embedded object should be a CRD")
		assert.Equal(t, "apiextensions.k8s.io/v1", crd.GetAPIVersion(),
			"CRD should be apiextensions v1")

		name := crd.GetName()
		_, ok := expectedNames[name]
		assert.True(t, ok, "unexpected CRD name: %s", name)
		expectedNames[name] = true
	}

	for name, found := range expectedNames {
		assert.True(t, found, "expected CRD %s not found in embedded files", name)
	}
}

func TestNewCRDManager(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, apiextensionsv1.AddToScheme(scheme))

	cli := fake.NewClientBuilder().WithScheme(scheme).Build()
	mgr := NewCRDManager(cli)

	require.NotNil(t, mgr, "NewCRDManager should return non-nil manager")
}

func TestApply(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, apiextensionsv1.AddToScheme(scheme))

	cli := fake.NewClientBuilder().WithScheme(scheme).Build()
	mgr := NewCRDManager(cli)

	ctx := context.Background()
	err := mgr.Apply(ctx)
	require.NoError(t, err, "Apply should succeed")

	// Verify CRDs were created
	var crdList apiextensionsv1.CustomResourceDefinitionList
	require.NoError(t, cli.List(ctx, &crdList))
	assert.Len(t, crdList.Items, 2, "should have 2 CRDs after Apply")

	names := make([]string, 0, len(crdList.Items))
	for _, crd := range crdList.Items {
		names = append(names, crd.Name)
	}

	assert.Contains(t, names, "plugins.mc.k8s.lex.la")
	assert.Contains(t, names, "papermcservers.mc.k8s.lex.la")
}

func TestApplyIdempotent(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, apiextensionsv1.AddToScheme(scheme))

	cli := fake.NewClientBuilder().WithScheme(scheme).Build()
	mgr := NewCRDManager(cli)

	ctx := context.Background()

	// Apply twice — second call should not error
	require.NoError(t, mgr.Apply(ctx), "first Apply should succeed")
	require.NoError(t, mgr.Apply(ctx), "second Apply should succeed (idempotent)")

	var crdList apiextensionsv1.CustomResourceDefinitionList
	require.NoError(t, cli.List(ctx, &crdList))
	assert.Len(t, crdList.Items, 2, "should still have exactly 2 CRDs")
}

func TestWaitEstablished(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, apiextensionsv1.AddToScheme(scheme))

	// Pre-create CRDs with Established condition
	pluginCRD := &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: "plugins.mc.k8s.lex.la"},
		Status: apiextensionsv1.CustomResourceDefinitionStatus{
			Conditions: []apiextensionsv1.CustomResourceDefinitionCondition{
				{
					Type:   apiextensionsv1.Established,
					Status: apiextensionsv1.ConditionTrue,
				},
			},
		},
	}

	serverCRD := &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: "papermcservers.mc.k8s.lex.la"},
		Status: apiextensionsv1.CustomResourceDefinitionStatus{
			Conditions: []apiextensionsv1.CustomResourceDefinitionCondition{
				{
					Type:   apiextensionsv1.Established,
					Status: apiextensionsv1.ConditionTrue,
				},
			},
		},
	}

	cli := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(pluginCRD, serverCRD).
		WithObjects(pluginCRD, serverCRD).
		Build()

	mgr := NewCRDManager(cli)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := mgr.WaitEstablished(ctx)
	require.NoError(t, err, "WaitEstablished should succeed when CRDs are Established")
}

func TestWaitEstablishedTimeout(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, apiextensionsv1.AddToScheme(scheme))

	// CRDs exist but NOT Established
	pluginCRD := &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: "plugins.mc.k8s.lex.la"},
	}

	serverCRD := &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: "papermcservers.mc.k8s.lex.la"},
	}

	cli := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(pluginCRD, serverCRD).
		Build()

	mgr := NewCRDManager(cli)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err := mgr.WaitEstablished(ctx)
	require.Error(t, err, "WaitEstablished should fail when CRDs are not Established")
}

func TestEnsureCRDs(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, apiextensionsv1.AddToScheme(scheme))

	cli := fake.NewClientBuilder().WithScheme(scheme).Build()
	mgr := NewCRDManager(cli)

	// Use short timeout: fake client Apply creates CRDs without Established
	// condition, so WaitEstablished should time out.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	ensureErr := mgr.EnsureCRDs(ctx)
	require.Error(t, ensureErr, "EnsureCRDs should fail because fake client CRDs lack Established condition")
}

// parseCRDs is a test helper that reads and decodes all embedded CRD YAMLs.
func parseCRDs() ([]*unstructured.Unstructured, error) {
	entries, err := crdFS.ReadDir("crds")
	if err != nil {
		return nil, err
	}

	var result []*unstructured.Unstructured

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		data, err := crdFS.ReadFile("crds/" + entry.Name())
		if err != nil {
			return nil, err
		}

		decoder := yaml.NewYAMLOrJSONDecoder(
			bytes.NewReader(data),
			4096,
		)

		for {
			u := &unstructured.Unstructured{}
			if err := decoder.Decode(u); err != nil {
				if err == io.EOF {
					break
				}

				return nil, err
			}

			if u.GetName() == "" {
				continue
			}

			result = append(result, u)
		}
	}

	return result, nil
}
