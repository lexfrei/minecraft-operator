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

package selector

import (
	"context"
	"testing"

	mcv1alpha1 "github.com/lexfrei/minecraft-operator/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestMatchesSelector(t *testing.T) {
	t.Run("should match labels with matching selector", func(t *testing.T) {
		labels := map[string]string{
			"app":  "papermc",
			"tier": "game",
		}
		selector := metav1.LabelSelector{
			MatchLabels: map[string]string{
				"app": "papermc",
			},
		}

		matched, err := MatchesSelector(labels, selector)
		require.NoError(t, err)
		assert.True(t, matched)
	})

	t.Run("should not match labels with non-matching selector", func(t *testing.T) {
		labels := map[string]string{
			"app": "papermc",
		}
		selector := metav1.LabelSelector{
			MatchLabels: map[string]string{
				"app": "vanilla",
			},
		}

		matched, err := MatchesSelector(labels, selector)
		require.NoError(t, err)
		assert.False(t, matched)
	})

	t.Run("should match empty selector to any labels", func(t *testing.T) {
		labels := map[string]string{
			"app": "papermc",
		}
		selector := metav1.LabelSelector{}

		matched, err := MatchesSelector(labels, selector)
		require.NoError(t, err)
		assert.True(t, matched)
	})

	t.Run("should return error for invalid selector", func(t *testing.T) {
		labels := map[string]string{"app": "papermc"}
		selector := metav1.LabelSelector{
			MatchExpressions: []metav1.LabelSelectorRequirement{
				{
					Key:      "app",
					Operator: "InvalidOperator",
				},
			},
		}

		_, err := MatchesSelector(labels, selector)
		assert.Error(t, err, "Invalid selector should return error")
	})
}

func TestFindMatchingPlugins_InvalidSelector(t *testing.T) {
	t.Run("should return error when plugin has invalid instanceSelector", func(t *testing.T) {
		scheme := runtime.NewScheme()
		require.NoError(t, mcv1alpha1.AddToScheme(scheme))

		invalidPlugin := &mcv1alpha1.Plugin{
			ObjectMeta: metav1.ObjectMeta{Name: "plugin-invalid", Namespace: "default"},
			Spec: mcv1alpha1.PluginSpec{
				Source:         mcv1alpha1.PluginSource{Type: "hangar", Project: "TestPlugin"},
				UpdateStrategy: "latest",
				InstanceSelector: metav1.LabelSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{
						{Key: "app", Operator: "InvalidOperator"},
					},
				},
			},
		}
		validPlugin := &mcv1alpha1.Plugin{
			ObjectMeta: metav1.ObjectMeta{Name: "plugin-valid", Namespace: "default"},
			Spec: mcv1alpha1.PluginSpec{
				Source:         mcv1alpha1.PluginSource{Type: "hangar", Project: "TestPlugin2"},
				UpdateStrategy: "latest",
				InstanceSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "papermc"},
				},
			},
		}

		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(invalidPlugin, validPlugin).
			Build()

		_, err := FindMatchingPlugins(
			context.Background(), fakeClient, "default", map[string]string{"app": "papermc"},
		)
		require.Error(t, err, "should return error for invalid instanceSelector")
	})
}
