// Package selector implements label selector matching for Plugin-to-PaperMCServer relationships.
package selector

import (
	"context"

	"github.com/cockroachdb/errors"
	mcv1alpha1 "github.com/lexfrei/minecraft-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// MatchesSelector checks if a set of labels matches a label selector.
// Returns true if the labels match the selector, false otherwise.
func MatchesSelector(labelSet map[string]string, selector metav1.LabelSelector) (bool, error) {
	// Convert metav1.LabelSelector to labels.Selector
	s, err := metav1.LabelSelectorAsSelector(&selector)
	if err != nil {
		return false, errors.Wrap(err, "failed to convert label selector")
	}

	// Check if the label set matches the selector
	return s.Matches(labels.Set(labelSet)), nil
}

// FindMatchingServers finds all PaperMCServer resources in a namespace that match the given selector.
// Returns a list of matching servers or an error if the query fails.
func FindMatchingServers(
	ctx context.Context,
	c client.Client,
	namespace string,
	selector metav1.LabelSelector,
) ([]mcv1alpha1.PaperMCServer, error) {
	// List all servers in the namespace
	var serverList mcv1alpha1.PaperMCServerList
	if err := c.List(ctx, &serverList, client.InNamespace(namespace)); err != nil {
		return nil, errors.Wrap(err, "failed to list PaperMCServer resources")
	}

	// Convert selector to labels.Selector for efficient matching
	s, err := metav1.LabelSelectorAsSelector(&selector)
	if err != nil {
		return nil, errors.Wrap(err, "failed to convert label selector")
	}

	// Filter servers by selector
	matched := make([]mcv1alpha1.PaperMCServer, 0)
	for i := range serverList.Items {
		server := &serverList.Items[i]
		if s.Matches(labels.Set(server.Labels)) {
			matched = append(matched, *server)
		}
	}

	return matched, nil
}

// FindMatchingPlugins finds all Plugin resources in a namespace whose instanceSelector matches the server labels.
// Returns a list of matching plugins or an error if the query fails.
func FindMatchingPlugins(
	ctx context.Context,
	c client.Client,
	namespace string,
	serverLabels map[string]string,
) ([]mcv1alpha1.Plugin, error) {
	// List all plugins in the namespace
	var pluginList mcv1alpha1.PluginList
	if err := c.List(ctx, &pluginList, client.InNamespace(namespace)); err != nil {
		return nil, errors.Wrap(err, "failed to list Plugin resources")
	}

	// Filter plugins by their instanceSelector
	matched := make([]mcv1alpha1.Plugin, 0)
	for i := range pluginList.Items {
		plugin := &pluginList.Items[i]

		// Check if plugin's instanceSelector matches the server labels
		s, err := metav1.LabelSelectorAsSelector(&plugin.Spec.InstanceSelector)
		if err != nil {
			// Log error but continue with other plugins
			continue
		}

		if s.Matches(labels.Set(serverLabels)) {
			matched = append(matched, *plugin)
		}
	}

	return matched, nil
}
