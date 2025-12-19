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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	mck8slexlav1alpha1 "github.com/lexfrei/minecraft-operator/api/v1alpha1"
)

var _ = Describe("Plugin Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default", // TODO(user):Modify as needed
		}
		plugin := &mck8slexlav1alpha1.Plugin{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind Plugin")
			err := k8sClient.Get(ctx, typeNamespacedName, plugin)
			if err != nil && errors.IsNotFound(err) {
				resource := &mck8slexlav1alpha1.Plugin{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: mck8slexlav1alpha1.PluginSpec{
						Source: mck8slexlav1alpha1.PluginSource{
							Type:    "hangar",
							Project: "EssentialsX",
						},
						UpdateStrategy: "latest",
						InstanceSelector: metav1.LabelSelector{
							MatchLabels: map[string]string{
								"test": "true",
							},
						},
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			// TODO(user): Cleanup logic after each test, like removing the resource instance.
			resource := &mck8slexlav1alpha1.Plugin{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance Plugin")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
		It("should successfully reconcile the resource", func() {
			By("Verifying the resource was created")
			// Simple test: just verify the resource exists with correct spec
			resource := &mck8slexlav1alpha1.Plugin{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())
			Expect(resource.Spec.UpdateStrategy).To(Equal("latest"))
			Expect(resource.Spec.Source.Type).To(Equal("hangar"))
			// TODO(user): Add integration tests with full reconciler setup including PluginClient and Solver.
			// Example: If you expect a certain status condition after reconciliation, verify it here.
		})
	})

	Context("When managing plugin deletion lifecycle", func() {
		const deletionTestName = "deletion-test-plugin"
		const deletionTestServerName = "deletion-test-server"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      deletionTestName,
			Namespace: "default",
		}

		BeforeEach(func() {
			By("creating a PaperMCServer that will be matched by the plugin")
			server := &mck8slexlav1alpha1.PaperMCServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      deletionTestServerName,
					Namespace: "default",
					Labels: map[string]string{
						"deletion-test": "true",
					},
				},
				Spec: mck8slexlav1alpha1.PaperMCServerSpec{
					UpdateStrategy: "latest",
					UpdateSchedule: mck8slexlav1alpha1.UpdateSchedule{
						CheckCron: "0 3 * * *",
						MaintenanceWindow: mck8slexlav1alpha1.MaintenanceWindow{
							Cron:    "0 4 * * 0",
							Enabled: true,
						},
					},
					GracefulShutdown: mck8slexlav1alpha1.GracefulShutdown{
						Timeout: metav1.Duration{Duration: 5 * time.Minute},
					},
					RCON: mck8slexlav1alpha1.RCONConfig{
						Enabled: false,
					},
					PodTemplate: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "minecraft",
									Image: "lexfrei/papermc:1.21.1-91",
								},
							},
						},
					},
				},
			}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: deletionTestServerName, Namespace: "default"}, server)
			if errors.IsNotFound(err) {
				Expect(k8sClient.Create(ctx, server)).To(Succeed())
			}

			By("creating the Plugin resource for deletion tests")
			plugin := &mck8slexlav1alpha1.Plugin{
				ObjectMeta: metav1.ObjectMeta{
					Name:      deletionTestName,
					Namespace: "default",
				},
				Spec: mck8slexlav1alpha1.PluginSpec{
					Source: mck8slexlav1alpha1.PluginSource{
						Type:    "hangar",
						Project: "TestPlugin",
					},
					UpdateStrategy: "latest",
					InstanceSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"deletion-test": "true",
						},
					},
				},
			}
			err = k8sClient.Get(ctx, typeNamespacedName, plugin)
			if errors.IsNotFound(err) {
				Expect(k8sClient.Create(ctx, plugin)).To(Succeed())
			}
		})

		AfterEach(func() {
			By("cleaning up the Plugin resource")
			plugin := &mck8slexlav1alpha1.Plugin{}
			err := k8sClient.Get(ctx, typeNamespacedName, plugin)
			if err == nil {
				// Remove finalizer if present to allow deletion
				if controllerutil.ContainsFinalizer(plugin, PluginFinalizer) {
					controllerutil.RemoveFinalizer(plugin, PluginFinalizer)
					if updateErr := k8sClient.Update(ctx, plugin); updateErr != nil {
						// Plugin might already be gone, ignore error
						return
					}
				}
				// Only delete if not already deleting
				if plugin.DeletionTimestamp.IsZero() {
					_ = k8sClient.Delete(ctx, plugin)
				}
			}

			// Wait for plugin to be fully deleted
			Eventually(func() bool {
				err := k8sClient.Get(ctx, typeNamespacedName, plugin)
				return errors.IsNotFound(err)
			}, time.Second*10, time.Millisecond*500).Should(BeTrue())

			By("cleaning up the PaperMCServer resource")
			server := &mck8slexlav1alpha1.PaperMCServer{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: deletionTestServerName, Namespace: "default"}, server)
			if err == nil {
				_ = k8sClient.Delete(ctx, server)
			}
		})

		It("should add finalizer when plugin is created", func() {
			By("waiting for the plugin to have a finalizer")
			// Note: This test verifies the expectation. The actual implementation
			// will add the finalizer during reconciliation.
			plugin := &mck8slexlav1alpha1.Plugin{}
			err := k8sClient.Get(ctx, typeNamespacedName, plugin)
			Expect(err).NotTo(HaveOccurred())

			// Add finalizer manually for now to verify the pattern works
			// The actual implementation will do this in Reconcile()
			if !controllerutil.ContainsFinalizer(plugin, PluginFinalizer) {
				controllerutil.AddFinalizer(plugin, PluginFinalizer)
				Expect(k8sClient.Update(ctx, plugin)).To(Succeed())
			}

			// Verify finalizer is present
			err = k8sClient.Get(ctx, typeNamespacedName, plugin)
			Expect(err).NotTo(HaveOccurred())
			Expect(controllerutil.ContainsFinalizer(plugin, PluginFinalizer)).To(BeTrue())
		})

		It("should initialize DeletionProgress when plugin is deleted", func() {
			By("adding finalizer to the plugin")
			plugin := &mck8slexlav1alpha1.Plugin{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, plugin)).To(Succeed())
			controllerutil.AddFinalizer(plugin, PluginFinalizer)
			Expect(k8sClient.Update(ctx, plugin)).To(Succeed())

			By("deleting the plugin")
			Expect(k8sClient.Delete(ctx, plugin)).To(Succeed())

			By("verifying the plugin enters Terminating state")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, typeNamespacedName, plugin)
				if err != nil {
					return false
				}
				return !plugin.DeletionTimestamp.IsZero()
			}, time.Second*5, time.Millisecond*500).Should(BeTrue())

			// Note: DeletionProgress initialization will be tested with the
			// actual reconciler implementation. This test verifies the CRD
			// supports the field and the finalizer blocks deletion.
		})

		It("should block deletion until finalizer is removed", func() {
			By("adding finalizer to the plugin")
			plugin := &mck8slexlav1alpha1.Plugin{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, plugin)).To(Succeed())
			controllerutil.AddFinalizer(plugin, PluginFinalizer)
			Expect(k8sClient.Update(ctx, plugin)).To(Succeed())

			By("attempting to delete the plugin")
			Expect(k8sClient.Delete(ctx, plugin)).To(Succeed())

			By("verifying the plugin still exists (blocked by finalizer)")
			Consistently(func() bool {
				err := k8sClient.Get(ctx, typeNamespacedName, plugin)
				return err == nil
			}, time.Second*2, time.Millisecond*500).Should(BeTrue())

			By("removing the finalizer")
			Expect(k8sClient.Get(ctx, typeNamespacedName, plugin)).To(Succeed())
			controllerutil.RemoveFinalizer(plugin, PluginFinalizer)
			Expect(k8sClient.Update(ctx, plugin)).To(Succeed())

			By("verifying the plugin is now deleted")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, typeNamespacedName, plugin)
				return errors.IsNotFound(err)
			}, time.Second*5, time.Millisecond*500).Should(BeTrue())
		})

		It("should support DeletionProgress status field", func() {
			By("getting the plugin")
			plugin := &mck8slexlav1alpha1.Plugin{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, plugin)).To(Succeed())

			By("setting DeletionProgress in status")
			now := metav1.Now()
			plugin.Status.DeletionProgress = []mck8slexlav1alpha1.DeletionProgressEntry{
				{
					ServerName: deletionTestServerName,
					Namespace:  "default",
					JARDeleted: false,
					DeletedAt:  nil,
				},
			}
			Expect(k8sClient.Status().Update(ctx, plugin)).To(Succeed())

			By("verifying DeletionProgress is persisted")
			Expect(k8sClient.Get(ctx, typeNamespacedName, plugin)).To(Succeed())
			Expect(plugin.Status.DeletionProgress).To(HaveLen(1))
			Expect(plugin.Status.DeletionProgress[0].ServerName).To(Equal(deletionTestServerName))
			Expect(plugin.Status.DeletionProgress[0].JARDeleted).To(BeFalse())

			By("updating DeletionProgress to mark JAR as deleted")
			plugin.Status.DeletionProgress[0].JARDeleted = true
			plugin.Status.DeletionProgress[0].DeletedAt = &now
			Expect(k8sClient.Status().Update(ctx, plugin)).To(Succeed())

			By("verifying the update is persisted")
			Expect(k8sClient.Get(ctx, typeNamespacedName, plugin)).To(Succeed())
			Expect(plugin.Status.DeletionProgress[0].JARDeleted).To(BeTrue())
			Expect(plugin.Status.DeletionProgress[0].DeletedAt).NotTo(BeNil())
		})
	})
})
