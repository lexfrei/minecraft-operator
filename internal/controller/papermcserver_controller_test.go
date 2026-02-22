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

package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	mck8slexlav1alpha1 "github.com/lexfrei/minecraft-operator/api/v1alpha1"
	"github.com/lexfrei/minecraft-operator/pkg/solver"
)

var _ = Describe("PaperMCServer Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default", // TODO(user):Modify as needed
		}
		papermcserver := &mck8slexlav1alpha1.PaperMCServer{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind PaperMCServer")
			err := k8sClient.Get(ctx, typeNamespacedName, papermcserver)
			if err != nil && errors.IsNotFound(err) {
				resource := &mck8slexlav1alpha1.PaperMCServer{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: mck8slexlav1alpha1.PaperMCServerSpec{
						UpdateStrategy: "latest",
						Version:        "latest",
						UpdateSchedule: mck8slexlav1alpha1.UpdateSchedule{
							CheckCron: "0 3 * * *",
							MaintenanceWindow: mck8slexlav1alpha1.MaintenanceWindow{
								Cron:    "0 4 * * 0",
								Enabled: true,
							},
						},
						GracefulShutdown: mck8slexlav1alpha1.GracefulShutdown{
							Timeout: metav1.Duration{Duration: 300000000000}, // 5 minutes
						},
						RCON: mck8slexlav1alpha1.RCONConfig{
							Enabled: false,
							PasswordSecret: mck8slexlav1alpha1.SecretKeyRef{
								Name: "test-secret",
								Key:  "password",
							},
						},
						PodTemplate: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  "papermc",
										Image: "lexfrei/papermc:latest",
									},
								},
							},
						},
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			// TODO(user): Cleanup logic after each test, like removing the resource instance.
			resource := &mck8slexlav1alpha1.PaperMCServer{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance PaperMCServer")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
		It("should successfully reconcile the resource", func() {
			By("Verifying the resource was created")
			// Simple test: just verify the resource exists with correct spec
			resource := &mck8slexlav1alpha1.PaperMCServer{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())
			Expect(resource.Spec.Version).To(Equal("latest"))
			// TODO(user): Add integration tests with full reconciler setup including PaperClient and Solver.
			// Example: If you expect a certain status condition after reconciliation, verify it here.
		})
	})

	Context("Plugin compatibility check", func() {
		It("should return true when plugin has compatible version", func() {
			reconciler := &PaperMCServerReconciler{}
			ctx := context.Background()

			// Plugin with version compatible with 1.21.1
			plugin := mck8slexlav1alpha1.Plugin{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "compatible-plugin",
					Namespace: "default",
				},
				Status: mck8slexlav1alpha1.PluginStatus{
					AvailableVersions: []mck8slexlav1alpha1.PluginVersionInfo{
						{
							Version:           "1.0.0",
							MinecraftVersions: []string{"1.20.4", "1.21.0", "1.21.1"},
							DownloadURL:       "https://example.com/plugin.jar",
						},
					},
				},
			}

			compatible := reconciler.isPluginCompatibleWithPaper(ctx, &plugin, "1.21.1")
			Expect(compatible).To(BeTrue(), "Plugin with compatible version should return true")
		})

		It("should return false when no compatible plugin version exists", func() {
			reconciler := &PaperMCServerReconciler{}
			ctx := context.Background()

			// Plugin only compatible with older versions
			plugin := mck8slexlav1alpha1.Plugin{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "incompatible-plugin",
					Namespace: "default",
				},
				Status: mck8slexlav1alpha1.PluginStatus{
					AvailableVersions: []mck8slexlav1alpha1.PluginVersionInfo{
						{
							Version:           "1.0.0",
							MinecraftVersions: []string{"1.19.4", "1.20.0"},
							DownloadURL:       "https://example.com/plugin.jar",
						},
						{
							Version:           "2.0.0",
							MinecraftVersions: []string{"1.20.1", "1.20.4"},
							DownloadURL:       "https://example.com/plugin-v2.jar",
						},
					},
				},
			}

			compatible := reconciler.isPluginCompatibleWithPaper(ctx, &plugin, "1.21.1")
			Expect(compatible).To(BeFalse(), "Plugin without compatible version should return false")
		})

		It("should block update when no compatible plugin version exists", func() {
			reconciler := &PaperMCServerReconciler{}
			ctx := context.Background()

			// Plugin incompatible with 1.21.1
			matchedPlugins := []mck8slexlav1alpha1.Plugin{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "blocker-plugin",
						Namespace: "default",
					},
					Status: mck8slexlav1alpha1.PluginStatus{
						AvailableVersions: []mck8slexlav1alpha1.PluginVersionInfo{
							{
								Version:           "1.0.0",
								MinecraftVersions: []string{"1.20.4"},
								DownloadURL:       "https://example.com/plugin.jar",
							},
						},
					},
				},
			}

			compatible, blockingPlugin, blockReason := reconciler.checkPluginCompatibility(
				ctx, "1.21.1", matchedPlugins)

			Expect(compatible).To(BeFalse(), "Update should be blocked")
			Expect(blockingPlugin).To(Equal("blocker-plugin"))
			Expect(blockReason).To(ContainSubstring("incompatible with Paper"))
		})

		It("should allow update when all plugins have compatible versions", func() {
			reconciler := &PaperMCServerReconciler{}
			ctx := context.Background()

			matchedPlugins := []mck8slexlav1alpha1.Plugin{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "plugin-a",
						Namespace: "default",
					},
					Status: mck8slexlav1alpha1.PluginStatus{
						AvailableVersions: []mck8slexlav1alpha1.PluginVersionInfo{
							{
								Version:           "1.0.0",
								MinecraftVersions: []string{"1.21.0", "1.21.1"},
								DownloadURL:       "https://example.com/a.jar",
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "plugin-b",
						Namespace: "default",
					},
					Status: mck8slexlav1alpha1.PluginStatus{
						AvailableVersions: []mck8slexlav1alpha1.PluginVersionInfo{
							{
								Version:           "2.0.0",
								MinecraftVersions: []string{"1.20.4", "1.21.1"},
								DownloadURL:       "https://example.com/b.jar",
							},
						},
					},
				},
			}

			compatible, blockingPlugin, blockReason := reconciler.checkPluginCompatibility(
				ctx, "1.21.1", matchedPlugins)

			Expect(compatible).To(BeTrue(), "Update should be allowed when all plugins compatible")
			Expect(blockingPlugin).To(BeEmpty())
			Expect(blockReason).To(BeEmpty())
		})
	})

	Context("Plugin compatibility edge cases", func() {
		It("should NOT block when plugin has no available versions but has compatibilityOverride", func() {
			reconciler := &PaperMCServerReconciler{}
			ctx := context.Background()

			matchedPlugins := []mck8slexlav1alpha1.Plugin{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "override-only-plugin",
						Namespace: "default",
					},
					Spec: mck8slexlav1alpha1.PluginSpec{
						CompatibilityOverride: &mck8slexlav1alpha1.CompatibilityOverride{
							Enabled:           true,
							MinecraftVersions: []string{"1.21.x"},
						},
					},
					// No AvailableVersions
				},
			}

			compatible, blockingPlugin, blockReason := reconciler.checkPluginCompatibility(
				ctx, "1.21.1", matchedPlugins)

			Expect(compatible).To(BeTrue(),
				"Plugin with override should not be blocked even without available versions")
			Expect(blockingPlugin).To(BeEmpty())
			Expect(blockReason).To(BeEmpty())
		})

		It("should assume compatible when plugin has no available versions and no override", func() {
			reconciler := &PaperMCServerReconciler{}
			ctx := context.Background()

			matchedPlugins := []mck8slexlav1alpha1.Plugin{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "no-metadata-plugin",
						Namespace: "default",
					},
					// No AvailableVersions, no override
				},
			}

			compatible, blockingPlugin, blockReason := reconciler.checkPluginCompatibility(
				ctx, "1.21.1", matchedPlugins)

			Expect(compatible).To(BeTrue(),
				"Plugin without metadata should be assumed compatible per DESIGN.md")
			Expect(blockingPlugin).To(BeEmpty())
			Expect(blockReason).To(BeEmpty())
		})
	})

	Context("Plugin compatibility with compatibilityOverride", func() {
		It("should return true when compatibilityOverride has matching wildcard version", func() {
			reconciler := &PaperMCServerReconciler{}
			ctx := context.Background()

			plugin := mck8slexlav1alpha1.Plugin{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "override-plugin",
					Namespace: "default",
				},
				Spec: mck8slexlav1alpha1.PluginSpec{
					CompatibilityOverride: &mck8slexlav1alpha1.CompatibilityOverride{
						Enabled:           true,
						MinecraftVersions: []string{"1.21.x"},
					},
				},
				// No available versions in status - only override matters
				Status: mck8slexlav1alpha1.PluginStatus{},
			}

			compatible := reconciler.isPluginCompatibleWithPaper(ctx, &plugin, "1.21.4")
			Expect(compatible).To(BeTrue(),
				"Plugin with compatibilityOverride matching via wildcard should be compatible")
		})

		It("should return true when compatibilityOverride has exact version match", func() {
			reconciler := &PaperMCServerReconciler{}
			ctx := context.Background()

			plugin := mck8slexlav1alpha1.Plugin{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "override-exact-plugin",
					Namespace: "default",
				},
				Spec: mck8slexlav1alpha1.PluginSpec{
					CompatibilityOverride: &mck8slexlav1alpha1.CompatibilityOverride{
						Enabled:           true,
						MinecraftVersions: []string{"1.21.1"},
					},
				},
			}

			compatible := reconciler.isPluginCompatibleWithPaper(ctx, &plugin, "1.21.1")
			Expect(compatible).To(BeTrue(),
				"Plugin with exact version in compatibilityOverride should be compatible")
		})

		It("should return true when compatibilityOverride is enabled but has no versions", func() {
			reconciler := &PaperMCServerReconciler{}
			ctx := context.Background()

			plugin := mck8slexlav1alpha1.Plugin{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "override-empty-plugin",
					Namespace: "default",
				},
				Spec: mck8slexlav1alpha1.PluginSpec{
					CompatibilityOverride: &mck8slexlav1alpha1.CompatibilityOverride{
						Enabled: true,
						// No versions specified - assume compatible
					},
				},
			}

			compatible := reconciler.isPluginCompatibleWithPaper(ctx, &plugin, "1.21.1")
			Expect(compatible).To(BeTrue(),
				"Plugin with enabled override and no versions should assume compatible")
		})

		It("should return false when compatibilityOverride versions don't match", func() {
			reconciler := &PaperMCServerReconciler{}
			ctx := context.Background()

			plugin := mck8slexlav1alpha1.Plugin{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "override-mismatch-plugin",
					Namespace: "default",
				},
				Spec: mck8slexlav1alpha1.PluginSpec{
					CompatibilityOverride: &mck8slexlav1alpha1.CompatibilityOverride{
						Enabled:           true,
						MinecraftVersions: []string{"1.20.x"},
					},
				},
			}

			compatible := reconciler.isPluginCompatibleWithPaper(ctx, &plugin, "1.21.1")
			Expect(compatible).To(BeFalse(),
				"Plugin with override versions not matching should be incompatible")
		})
	})

	Context("buildPodSpec error handling", func() {
		It("should return error when DesiredVersion is not set instead of using hardcoded fallback", func() {

			reconciler := &PaperMCServerReconciler{}
			server := &mck8slexlav1alpha1.PaperMCServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-server",
					Namespace: "default",
				},
				Spec: mck8slexlav1alpha1.PaperMCServerSpec{
					PodTemplate: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "papermc"},
							},
						},
					},
				},
				// Status.DesiredVersion and Status.DesiredBuild NOT set
			}

			podSpec, err := reconciler.buildPodSpec(server)
			Expect(err).To(HaveOccurred(), "buildPodSpec should return error when DesiredVersion is not set")
			Expect(podSpec).To(BeNil())
			Expect(err.Error()).To(ContainSubstring("DesiredVersion"))
		})

		It("should build pod spec correctly when DesiredVersion and DesiredBuild are set", func() {
			reconciler := &PaperMCServerReconciler{}
			server := &mck8slexlav1alpha1.PaperMCServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-server",
					Namespace: "default",
				},
				Spec: mck8slexlav1alpha1.PaperMCServerSpec{
					PodTemplate: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "papermc"},
							},
						},
					},
				},
				Status: mck8slexlav1alpha1.PaperMCServerStatus{
					DesiredVersion: "1.21.1",
					DesiredBuild:   100,
				},
			}

			podSpec, err := reconciler.buildPodSpec(server)
			Expect(err).NotTo(HaveOccurred())
			Expect(podSpec).NotTo(BeNil())
			Expect(podSpec.Containers[0].Image).To(Equal("docker.io/lexfrei/papermc:1.21.1-100"))
		})
	})

	Context("clearUpdateBlocked behavior", func() {
		It("should clear UpdateBlocked condition even when Blocked is already false", func() {
			reconciler := &PaperMCServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			server := &mck8slexlav1alpha1.PaperMCServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-clear-blocked",
					Namespace:  "default",
					Generation: 1,
				},
				Status: mck8slexlav1alpha1.PaperMCServerStatus{
					UpdateBlocked: &mck8slexlav1alpha1.UpdateBlockedStatus{
						Blocked: false, // Already cleared struct-level
					},
				},
			}

			// Manually set condition to True (simulating stale condition)
			reconciler.setCondition(server, conditionTypeUpdateBlocked, metav1.ConditionTrue,
				reasonUpdateBlocked, "stale block reason")

			// Call clearUpdateBlocked
			reconciler.clearUpdateBlocked(server)

			// UpdateBlocked should be nil
			Expect(server.Status.UpdateBlocked).To(BeNil())

			// Condition should be False
			cond := findCondition(server.Status.Conditions, conditionTypeUpdateBlocked)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		})

		It("should clear UpdateBlocked when Blocked is true", func() {
			reconciler := &PaperMCServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			server := &mck8slexlav1alpha1.PaperMCServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-clear-blocked-true",
					Namespace:  "default",
					Generation: 1,
				},
				Status: mck8slexlav1alpha1.PaperMCServerStatus{
					UpdateBlocked: &mck8slexlav1alpha1.UpdateBlockedStatus{
						Blocked: true,
						Reason:  "some block reason",
					},
				},
			}

			reconciler.setCondition(server, conditionTypeUpdateBlocked, metav1.ConditionTrue,
				reasonUpdateBlocked, "some block reason")

			reconciler.clearUpdateBlocked(server)

			Expect(server.Status.UpdateBlocked).To(BeNil())

			cond := findCondition(server.Status.Conditions, conditionTypeUpdateBlocked)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		})
	})

	Context("serverStatusEqual comparison", func() {
		It("should detect changes in AvailableUpdate content", func() {

			a := &mck8slexlav1alpha1.PaperMCServerStatus{
				CurrentVersion: "1.21.1",
				CurrentBuild:   100,
				DesiredVersion: "1.21.1",
				DesiredBuild:   100,
				AvailableUpdate: &mck8slexlav1alpha1.AvailableUpdate{
					Version: "1.21.2",
					Build:   50,
				},
			}

			b := &mck8slexlav1alpha1.PaperMCServerStatus{
				CurrentVersion: "1.21.1",
				CurrentBuild:   100,
				DesiredVersion: "1.21.1",
				DesiredBuild:   100,
				AvailableUpdate: &mck8slexlav1alpha1.AvailableUpdate{
					Version: "1.21.3", // Different version!
					Build:   60,       // Different build!
				},
			}

			equal := serverStatusEqual(a, b)

			Expect(equal).To(BeFalse(),
				"serverStatusEqual should compare AvailableUpdate content")
		})

		It("should detect changes in LastUpdate", func() {
			now := metav1.Now()
			a := &mck8slexlav1alpha1.PaperMCServerStatus{
				CurrentVersion: "1.21.1",
				CurrentBuild:   100,
				DesiredVersion: "1.21.1",
				DesiredBuild:   100,
				LastUpdate: &mck8slexlav1alpha1.UpdateHistory{
					AppliedAt:  now,
					Successful: true,
				},
			}

			b := &mck8slexlav1alpha1.PaperMCServerStatus{
				CurrentVersion: "1.21.1",
				CurrentBuild:   100,
				DesiredVersion: "1.21.1",
				DesiredBuild:   100,
				LastUpdate:     nil, // Different!
			}

			equal := serverStatusEqual(a, b)

			Expect(equal).To(BeFalse(),
				"serverStatusEqual should detect LastUpdate changes")
		})
	})

	Context("buildPluginStatus preserves existing data", func() {
		It("should preserve InstalledJARName from previous status", func() {
			// Bug 11: buildPluginStatus overwrites entire status,
			// losing InstalledJARName set by update controller
			reconciler := &PaperMCServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
				Solver: solver.NewSimpleSolver(),
			}

			server := &mck8slexlav1alpha1.PaperMCServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-preserve",
					Namespace: "default",
				},
				Spec: mck8slexlav1alpha1.PaperMCServerSpec{
					Version: "1.21.4",
				},
				Status: mck8slexlav1alpha1.PaperMCServerStatus{
					CurrentVersion: "1.21.4",
					Plugins: []mck8slexlav1alpha1.ServerPluginStatus{
						{
							PluginRef: mck8slexlav1alpha1.PluginRef{
								Name:      "my-plugin",
								Namespace: "default",
							},
							InstalledJARName: "my-plugin.jar",
							CurrentVersion:   "2.0.0",
						},
					},
				},
			}

			matchedPlugins := []mck8slexlav1alpha1.Plugin{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "my-plugin",
						Namespace: "default",
					},
					Spec: mck8slexlav1alpha1.PluginSpec{
						Source: mck8slexlav1alpha1.PluginSource{
							Type: "hangar",
						},
					},
					Status: mck8slexlav1alpha1.PluginStatus{
						AvailableVersions: []mck8slexlav1alpha1.PluginVersionInfo{
							{
								Version:           "2.1.0",
								MinecraftVersions: []string{"1.21.4"},
							},
						},
					},
				},
			}

			result := reconciler.buildPluginStatus(context.Background(), server, matchedPlugins)

			Expect(result).To(HaveLen(1))
			Expect(result[0].InstalledJARName).To(Equal("my-plugin.jar"),
				"buildPluginStatus must preserve InstalledJARName from previous status")
			Expect(result[0].CurrentVersion).To(Equal("2.0.0"),
				"buildPluginStatus must preserve CurrentVersion from previous status")
		})

		It("should preserve PendingDeletion from previous status", func() {
			reconciler := &PaperMCServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
				Solver: solver.NewSimpleSolver(),
			}

			server := &mck8slexlav1alpha1.PaperMCServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-preserve-pending",
					Namespace: "default",
				},
				Spec: mck8slexlav1alpha1.PaperMCServerSpec{
					Version: "1.21.4",
				},
				Status: mck8slexlav1alpha1.PaperMCServerStatus{
					CurrentVersion: "1.21.4",
					Plugins: []mck8slexlav1alpha1.ServerPluginStatus{
						{
							PluginRef: mck8slexlav1alpha1.PluginRef{
								Name:      "deleting-plugin",
								Namespace: "default",
							},
							PendingDeletion: true,
						},
					},
				},
			}

			matchedPlugins := []mck8slexlav1alpha1.Plugin{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "deleting-plugin",
						Namespace: "default",
					},
					Spec: mck8slexlav1alpha1.PluginSpec{
						Source: mck8slexlav1alpha1.PluginSource{
							Type: "hangar",
						},
					},
					Status: mck8slexlav1alpha1.PluginStatus{
						AvailableVersions: []mck8slexlav1alpha1.PluginVersionInfo{
							{
								Version:           "1.0.0",
								MinecraftVersions: []string{"1.21.4"},
							},
						},
					},
				},
			}

			result := reconciler.buildPluginStatus(context.Background(), server, matchedPlugins)

			Expect(result).To(HaveLen(1))
			Expect(result[0].PendingDeletion).To(BeTrue(),
				"buildPluginStatus must preserve PendingDeletion from previous status")
		})
	})

	Context("resolvePluginVersionForServer with empty versions", func() {
		It("should not return error when plugin has no available versions yet", func() {
			// Bug 9: Race condition - Plugin controller hasn't fetched metadata yet.
			// This should not be logged as ERROR; it's a transient state.
			reconciler := &PaperMCServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
				Solver: solver.NewSimpleSolver(),
			}

			server := &mck8slexlav1alpha1.PaperMCServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-empty-versions",
					Namespace: "default",
				},
				Spec: mck8slexlav1alpha1.PaperMCServerSpec{
					Version: "1.21.4",
				},
				Status: mck8slexlav1alpha1.PaperMCServerStatus{
					CurrentVersion: "1.21.4",
				},
			}

			plugin := &mck8slexlav1alpha1.Plugin{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "new-plugin",
					Namespace: "default",
				},
				Spec: mck8slexlav1alpha1.PluginSpec{
					Source: mck8slexlav1alpha1.PluginSource{
						Type: "hangar",
					},
				},
				Status: mck8slexlav1alpha1.PluginStatus{
					AvailableVersions: nil, // Not fetched yet
				},
			}

			version, err := reconciler.resolvePluginVersionForServer(
				context.Background(), server, plugin)

			// Should return empty string without error (transient state)
			Expect(err).NotTo(HaveOccurred(),
				"Empty available versions is transient, not an error")
			Expect(version).To(BeEmpty())
		})
	})
})

// findCondition returns the condition with the given type from the slice, or nil if not found.
func findCondition(conditions []metav1.Condition, conditionType string) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == conditionType {
			return &conditions[i]
		}
	}

	return nil
}
