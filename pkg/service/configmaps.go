package service

import (
	"context"
	"sort"

	"github.com/cockroachdb/errors"
	mck8slexlav1beta1 "github.com/lexfrei/minecraft-operator/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ConfigMapService provides operations for Kubernetes ConfigMaps.
type ConfigMapService struct {
	client client.Client
}

// NewConfigMapService creates a new ConfigMapService.
func NewConfigMapService(c client.Client) *ConfigMapService {
	return &ConfigMapService{client: c}
}

// ConfigMapSummary represents a ConfigMap for API/UI consumption.
type ConfigMapSummary struct {
	Name      string
	Namespace string
	Keys      []string
}

// ListConfigMaps returns ConfigMaps in a namespace.
func (s *ConfigMapService) ListConfigMaps(ctx context.Context, namespace string) ([]ConfigMapSummary, error) {
	var cmList corev1.ConfigMapList

	opts := []client.ListOption{client.InNamespace(namespace)}
	if err := s.client.List(ctx, &cmList, opts...); err != nil {
		return nil, errors.Wrap(err, "failed to list ConfigMaps")
	}

	result := make([]ConfigMapSummary, 0, len(cmList.Items))
	for i := range cmList.Items {
		cm := &cmList.Items[i]
		keys := make([]string, 0, len(cm.Data))
		for k := range cm.Data {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		result = append(result, ConfigMapSummary{
			Name:      cm.Name,
			Namespace: cm.Namespace,
			Keys:      keys,
		})
	}

	return result, nil
}

// CreateConfigMap creates a new ConfigMap.
func (s *ConfigMapService) CreateConfigMap(
	ctx context.Context,
	namespace, name string,
	data map[string]string,
) error {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: data,
	}

	if err := s.client.Create(ctx, cm); err != nil {
		return errors.Wrap(err, "failed to create ConfigMap")
	}

	return nil
}

// configDataToPluginConfigs converts service config data to CRD types.
func configDataToPluginConfigs(data []ServerPluginConfigData) []mck8slexlav1beta1.ServerPluginConfig {
	if len(data) == 0 {
		return nil
	}
	result := make([]mck8slexlav1beta1.ServerPluginConfig, 0, len(data))
	for _, pc := range data {
		result = append(result, mck8slexlav1beta1.ServerPluginConfig{
			PluginName: pc.PluginName,
			Configs:    configDataToPluginConfigFiles(pc.Configs),
		})
	}
	return result
}

// configDataToServerConfigs converts service config data to CRD ServerConfigFile types.
func configDataToServerConfigs(data []ConfigFileData) []mck8slexlav1beta1.ServerConfigFile {
	if len(data) == 0 {
		return nil
	}
	result := make([]mck8slexlav1beta1.ServerConfigFile, 0, len(data))
	for _, cfg := range data {
		result = append(result, mck8slexlav1beta1.ServerConfigFile{
			ConfigMapRef: mck8slexlav1beta1.ConfigMapKeyRef{
				Name: cfg.ConfigMapName,
				Key:  cfg.ConfigMapKey,
			},
			Path:      cfg.Path,
			Overwrite: cfg.Overwrite,
		})
	}
	return result
}

// configDataToPluginConfigFiles converts service config data to CRD PluginConfigFile types.
func configDataToPluginConfigFiles(data []ConfigFileData) []mck8slexlav1beta1.PluginConfigFile {
	if len(data) == 0 {
		return nil
	}
	result := make([]mck8slexlav1beta1.PluginConfigFile, 0, len(data))
	for _, cfg := range data {
		result = append(result, mck8slexlav1beta1.PluginConfigFile{
			ConfigMapRef: mck8slexlav1beta1.ConfigMapKeyRef{
				Name: cfg.ConfigMapName,
				Key:  cfg.ConfigMapKey,
			},
			Path:      cfg.Path,
			Overwrite: cfg.Overwrite,
		})
	}
	return result
}
