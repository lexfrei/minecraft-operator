package crdmanager

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
)

func TestEmbeddedCRDsExist(t *testing.T) {
	t.Parallel()

	entries, err := crdFS.ReadDir("crds")
	require.NoError(t, err, "should read embedded crds directory")

	// We have exactly 2 CRDs: Plugin and PaperMCServer
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
