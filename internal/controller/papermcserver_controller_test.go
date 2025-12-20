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
			Expect(blockReason).To(ContainSubstring("incompatible"))
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
})
