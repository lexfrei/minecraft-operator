// Package crdmanager handles embedding and applying CRDs at operator startup.
package crdmanager

import (
	"bytes"
	"context"
	"embed"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

//go:embed crds/*.yaml
var crdFS embed.FS

const (
	fieldManager = "minecraft-operator"
	pollInterval = 500 * time.Millisecond
)

// CRDManager embeds and applies CRDs at operator startup using server-side apply.
type CRDManager struct {
	client client.Client
}

// NewCRDManager creates a new CRD manager.
func NewCRDManager(cli client.Client) *CRDManager {
	return &CRDManager{client: cli}
}

// EnsureCRDs applies all embedded CRDs and waits for them to become Established.
func (m *CRDManager) EnsureCRDs(ctx context.Context) error {
	if err := m.Apply(ctx); err != nil {
		return errors.Wrap(err, "failed to apply CRDs")
	}

	if err := m.WaitEstablished(ctx); err != nil {
		return errors.Wrap(err, "failed waiting for CRDs to become established")
	}

	return nil
}

// Apply reads all embedded CRD YAMLs and applies them via server-side apply.
func (m *CRDManager) Apply(ctx context.Context) error {
	entries, err := crdFS.ReadDir("crds")
	if err != nil {
		return errors.Wrap(err, "failed to read embedded CRDs directory")
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}

		if err := m.applyFile(ctx, entry.Name()); err != nil {
			return errors.Wrapf(err, "failed to apply CRD file %s", entry.Name())
		}
	}

	return nil
}

// applyFile reads a single CRD YAML file and applies it.
func (m *CRDManager) applyFile(ctx context.Context, filename string) error {
	data, err := crdFS.ReadFile("crds/" + filename)
	if err != nil {
		return errors.Wrap(err, "failed to read file")
	}

	decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)

	for {
		u := &unstructured.Unstructured{}
		if err := decoder.Decode(u); err != nil {
			if err == io.EOF {
				break
			}

			return errors.Wrap(err, "failed to decode YAML")
		}

		if u.GetName() == "" {
			continue
		}

		applyConfig := client.ApplyConfigurationFromUnstructured(u)

		if err := m.client.Apply(ctx, applyConfig,
			client.FieldOwner(fieldManager),
			client.ForceOwnership,
		); err != nil {
			return errors.Wrapf(err, "failed to apply %s/%s", u.GetKind(), u.GetName())
		}

		slog.InfoContext(ctx, "CRD applied", "name", u.GetName())
	}

	return nil
}

// WaitEstablished polls until all embedded CRDs have the Established condition.
func (m *CRDManager) WaitEstablished(ctx context.Context) error {
	crds, err := m.embeddedCRDNames()
	if err != nil {
		return err
	}

	for {
		allEstablished := true

		for _, name := range crds {
			established, err := m.isCRDEstablished(ctx, name)
			if err != nil {
				return errors.Wrapf(err, "failed to check CRD %s", name)
			}

			if !established {
				allEstablished = false

				break
			}
		}

		if allEstablished {
			slog.InfoContext(ctx, "all CRDs established", "count", len(crds))

			return nil
		}

		select {
		case <-ctx.Done():
			return errors.Wrap(ctx.Err(), "timed out waiting for CRDs to become established")
		case <-time.After(pollInterval):
		}
	}
}

// isCRDEstablished checks if a CRD has the Established condition set to True.
func (m *CRDManager) isCRDEstablished(ctx context.Context, name string) (bool, error) {
	var crd apiextensionsv1.CustomResourceDefinition
	if err := m.client.Get(ctx, client.ObjectKey{Name: name}, &crd); err != nil {
		return false, errors.Wrap(err, "failed to get CRD")
	}

	for _, cond := range crd.Status.Conditions {
		if cond.Type == apiextensionsv1.Established &&
			cond.Status == apiextensionsv1.ConditionTrue {
			return true, nil
		}
	}

	return false, nil
}

// embeddedCRDNames returns the names of all embedded CRDs.
func (m *CRDManager) embeddedCRDNames() ([]string, error) {
	entries, err := crdFS.ReadDir("crds")
	if err != nil {
		return nil, errors.Wrap(err, "failed to read embedded CRDs directory")
	}

	var names []string

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}

		data, readErr := crdFS.ReadFile("crds/" + entry.Name())
		if readErr != nil {
			return nil, errors.Wrapf(readErr, "failed to read %s", entry.Name())
		}

		decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)

		for {
			u := &unstructured.Unstructured{}
			if decodeErr := decoder.Decode(u); decodeErr != nil {
				if decodeErr == io.EOF {
					break
				}

				return nil, errors.Wrapf(decodeErr, "failed to decode %s", entry.Name())
			}

			if u.GetName() != "" {
				names = append(names, u.GetName())
			}
		}
	}

	return names, nil
}
