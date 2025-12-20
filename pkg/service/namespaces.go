package service

import (
	"context"

	"github.com/cockroachdb/errors"
	mck8slexlav1alpha1 "github.com/lexfrei/minecraft-operator/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NamespaceService provides operations for namespace-related queries.
type NamespaceService struct {
	client client.Client
}

// NewNamespaceService creates a new NamespaceService.
func NewNamespaceService(c client.Client) *NamespaceService {
	return &NamespaceService{client: c}
}

// ListNamespaces returns unique namespaces that have plugins or servers.
func (s *NamespaceService) ListNamespaces(ctx context.Context) ([]string, error) {
	namespaceSet := make(map[string]bool)

	// Get namespaces from plugins
	var pluginList mck8slexlav1alpha1.PluginList
	if err := s.client.List(ctx, &pluginList); err != nil {
		return nil, errors.Wrap(err, "failed to list Plugins")
	}
	for _, p := range pluginList.Items {
		namespaceSet[p.Namespace] = true
	}

	// Get namespaces from servers
	var serverList mck8slexlav1alpha1.PaperMCServerList
	if err := s.client.List(ctx, &serverList); err != nil {
		return nil, errors.Wrap(err, "failed to list PaperMCServers")
	}
	for _, s := range serverList.Items {
		namespaceSet[s.Namespace] = true
	}

	// Always include default namespace
	namespaceSet["default"] = true

	namespaces := make([]string, 0, len(namespaceSet))
	for ns := range namespaceSet {
		namespaces = append(namespaces, ns)
	}

	return namespaces, nil
}
