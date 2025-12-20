package service

import (
	"context"
	"fmt"
	"time"

	"github.com/cockroachdb/errors"
	mck8slexlav1alpha1 "github.com/lexfrei/minecraft-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// PluginService provides operations for Plugin resources.
type PluginService struct {
	client client.Client
}

// NewPluginService creates a new PluginService.
func NewPluginService(c client.Client) *PluginService {
	return &PluginService{client: c}
}

// PluginData represents plugin information for API/UI consumption.
type PluginData struct {
	Name              string
	Namespace         string
	SourceType        string
	Project           string
	URL               string
	UpdateStrategy    string
	Version           string
	Build             *int
	UpdateDelay       *time.Duration
	ResolvedVersion   string
	MatchedServers    int
	RepositoryStatus  string
	LastFetched       *time.Time
	AvailableVersions []PluginVersionData
	MatchedInstances  []MatchedInstanceData
}

// PluginVersionData represents a cached plugin version.
type PluginVersionData struct {
	Version           string
	SupportedVersions []string
	DownloadURL       string
	ReleasedAt        time.Time
}

// MatchedInstanceData represents a matched server instance.
type MatchedInstanceData struct {
	Name       string
	Namespace  string
	Version    string
	Compatible bool
}

// PluginSourceData contains plugin source information.
type PluginSourceData struct {
	Type    string
	Project string
	URL     string
}

// PluginCreateData contains data for creating a plugin.
type PluginCreateData struct {
	Name             string
	Namespace        string
	Source           PluginSourceData
	UpdateStrategy   string
	Version          string
	Build            int
	UpdateDelay      string
	Port             int32
	InstanceSelector metav1.LabelSelector
}

// PluginUpdateData contains data for updating an existing plugin.
type PluginUpdateData struct {
	Namespace        string
	Name             string
	UpdateStrategy   *string
	Version          *string
	Build            *int
	UpdateDelay      *string
	Port             *int32
	InstanceSelector *metav1.LabelSelector
}

// ListPlugins returns all plugins, optionally filtered by namespace.
func (s *PluginService) ListPlugins(ctx context.Context, namespace string) ([]PluginData, error) {
	var pluginList mck8slexlav1alpha1.PluginList

	if err := s.client.List(ctx, &pluginList); err != nil {
		return nil, errors.Wrap(err, "failed to list Plugins")
	}

	plugins := make([]PluginData, 0, len(pluginList.Items))

	for i := range pluginList.Items {
		plugin := &pluginList.Items[i]

		// Skip if filtering by namespace
		if namespace != "" && plugin.Namespace != namespace {
			continue
		}

		data := s.pluginToData(plugin)
		plugins = append(plugins, data)
	}

	return plugins, nil
}

// GetPlugin returns a specific plugin by namespace and name.
func (s *PluginService) GetPlugin(ctx context.Context, namespace, name string) (*PluginData, error) {
	var plugin mck8slexlav1alpha1.Plugin

	if err := s.client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &plugin); err != nil {
		return nil, errors.Wrap(err, "failed to get Plugin")
	}

	data := s.pluginToData(&plugin)
	return &data, nil
}

// CreatePlugin creates a new plugin from the provided data.
func (s *PluginService) CreatePlugin(ctx context.Context, data PluginCreateData) error {
	plugin := &mck8slexlav1alpha1.Plugin{
		ObjectMeta: metav1.ObjectMeta{
			Name:      data.Name,
			Namespace: data.Namespace,
		},
		Spec: mck8slexlav1alpha1.PluginSpec{
			Source: mck8slexlav1alpha1.PluginSource{
				Type:    data.Source.Type,
				Project: data.Source.Project,
				URL:     data.Source.URL,
			},
			UpdateStrategy:   data.UpdateStrategy,
			Version:          data.Version,
			InstanceSelector: data.InstanceSelector,
		},
	}

	if data.Build != 0 {
		plugin.Spec.Build = &data.Build
	}

	if data.UpdateDelay != "" {
		d, err := time.ParseDuration(data.UpdateDelay)
		if err == nil {
			plugin.Spec.UpdateDelay = &metav1.Duration{Duration: d}
		}
	}

	if data.Port != 0 {
		plugin.Spec.Port = &data.Port
	}

	if err := s.client.Create(ctx, plugin); err != nil {
		return errors.Wrap(err, "failed to create Plugin")
	}

	return nil
}

// DeletePlugin deletes a plugin.
func (s *PluginService) DeletePlugin(ctx context.Context, namespace, name string) error {
	var plugin mck8slexlav1alpha1.Plugin

	if err := s.client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &plugin); err != nil {
		return errors.Wrap(err, "failed to get plugin")
	}

	if err := s.client.Delete(ctx, &plugin); err != nil {
		return errors.Wrap(err, "failed to delete plugin")
	}

	return nil
}

// TriggerReconciliation sets the reconcile annotation to trigger reconciliation.
func (s *PluginService) TriggerReconciliation(ctx context.Context, namespace, name string) error {
	var plugin mck8slexlav1alpha1.Plugin

	if err := s.client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &plugin); err != nil {
		return errors.Wrap(err, "failed to get plugin")
	}

	if plugin.Annotations == nil {
		plugin.Annotations = make(map[string]string)
	}
	plugin.Annotations[AnnotationReconcile] = fmt.Sprintf("%d", time.Now().Unix())

	if err := s.client.Update(ctx, &plugin); err != nil {
		return errors.Wrap(err, "failed to update plugin")
	}

	return nil
}

// UpdatePlugin updates an existing Plugin with the provided data.
func (s *PluginService) UpdatePlugin(ctx context.Context, data PluginUpdateData) error {
	var plugin mck8slexlav1alpha1.Plugin

	if err := s.client.Get(ctx, client.ObjectKey{Namespace: data.Namespace, Name: data.Name}, &plugin); err != nil {
		return errors.Wrap(err, "failed to get plugin")
	}

	if data.UpdateStrategy != nil {
		plugin.Spec.UpdateStrategy = *data.UpdateStrategy
	}

	if data.Version != nil {
		plugin.Spec.Version = *data.Version
	}

	if data.Build != nil {
		plugin.Spec.Build = data.Build
	}

	if data.UpdateDelay != nil {
		d, err := time.ParseDuration(*data.UpdateDelay)
		if err == nil {
			plugin.Spec.UpdateDelay = &metav1.Duration{Duration: d}
		}
	}

	if data.Port != nil {
		plugin.Spec.Port = data.Port
	}

	if data.InstanceSelector != nil {
		plugin.Spec.InstanceSelector = *data.InstanceSelector
	}

	if err := s.client.Update(ctx, &plugin); err != nil {
		return errors.Wrap(err, "failed to update plugin")
	}

	return nil
}

// pluginToData converts a Plugin to PluginData.
func (s *PluginService) pluginToData(plugin *mck8slexlav1alpha1.Plugin) PluginData {
	data := PluginData{
		Name:             plugin.Name,
		Namespace:        plugin.Namespace,
		SourceType:       plugin.Spec.Source.Type,
		Project:          plugin.Spec.Source.Project,
		URL:              plugin.Spec.Source.URL,
		UpdateStrategy:   plugin.Spec.UpdateStrategy,
		Version:          plugin.Spec.Version,
		Build:            plugin.Spec.Build,
		MatchedServers:   len(plugin.Status.MatchedInstances),
		RepositoryStatus: plugin.Status.RepositoryStatus,
	}

	if plugin.Spec.UpdateDelay != nil {
		duration := plugin.Spec.UpdateDelay.Duration
		data.UpdateDelay = &duration
	}

	if plugin.Status.LastFetched != nil {
		t := plugin.Status.LastFetched.Time
		data.LastFetched = &t
	}

	// For pinned strategy, show the pinned version as resolved
	if plugin.Spec.UpdateStrategy == "pinned" && plugin.Spec.Version != "" {
		data.ResolvedVersion = plugin.Spec.Version
	}

	// Convert available versions
	data.AvailableVersions = make([]PluginVersionData, 0, len(plugin.Status.AvailableVersions))
	for _, v := range plugin.Status.AvailableVersions {
		vd := PluginVersionData{
			Version:           v.Version,
			SupportedVersions: v.MinecraftVersions,
			DownloadURL:       v.DownloadURL,
			ReleasedAt:        v.ReleasedAt.Time,
		}
		data.AvailableVersions = append(data.AvailableVersions, vd)
	}

	// Convert matched instances
	data.MatchedInstances = make([]MatchedInstanceData, 0, len(plugin.Status.MatchedInstances))
	for _, mi := range plugin.Status.MatchedInstances {
		data.MatchedInstances = append(data.MatchedInstances, MatchedInstanceData{
			Name:       mi.Name,
			Namespace:  mi.Namespace,
			Version:    mi.Version,
			Compatible: mi.Compatible,
		})
	}

	return data
}

// GetPluginNamespaces returns unique namespaces that have plugins.
func (s *PluginService) GetPluginNamespaces(ctx context.Context) ([]string, error) {
	var pluginList mck8slexlav1alpha1.PluginList

	if err := s.client.List(ctx, &pluginList); err != nil {
		return nil, errors.Wrap(err, "failed to list Plugins")
	}

	namespaceSet := make(map[string]bool)
	for _, plugin := range pluginList.Items {
		namespaceSet[plugin.Namespace] = true
	}

	namespaces := make([]string, 0, len(namespaceSet))
	for ns := range namespaceSet {
		namespaces = append(namespaces, ns)
	}

	return namespaces, nil
}
