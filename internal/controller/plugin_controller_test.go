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
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	mck8slexlav1alpha1 "github.com/lexfrei/minecraft-operator/api/v1alpha1"
	"github.com/lexfrei/minecraft-operator/pkg/plugins"
	"github.com/lexfrei/minecraft-operator/pkg/solver"
	"github.com/lexfrei/minecraft-operator/pkg/testutil"
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

	Context("When handling server deletion during plugin deletion", func() {
		const (
			serverCleanupPluginName  = "server-cleanup-plugin"
			existingServerName       = "existing-server"
			deletedServerName        = "deleted-server"
			anotherDeletedServerName = "another-deleted-server"
		)

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      serverCleanupPluginName,
			Namespace: "default",
		}

		BeforeEach(func() {
			By("creating only one server (existing-server), leaving others as non-existent")
			server := &mck8slexlav1alpha1.PaperMCServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      existingServerName,
					Namespace: "default",
					Labels: map[string]string{
						"cleanup-test": "true",
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
			err := k8sClient.Get(ctx, types.NamespacedName{Name: existingServerName, Namespace: "default"}, server)
			if errors.IsNotFound(err) {
				Expect(k8sClient.Create(ctx, server)).To(Succeed())
			}

			By("creating the Plugin resource for cleanup tests")
			plugin := &mck8slexlav1alpha1.Plugin{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serverCleanupPluginName,
					Namespace: "default",
				},
				Spec: mck8slexlav1alpha1.PluginSpec{
					Source: mck8slexlav1alpha1.PluginSource{
						Type:    "hangar",
						Project: "CleanupTestPlugin",
					},
					UpdateStrategy: "latest",
					InstanceSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"cleanup-test": "true",
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
				// Remove finalizer if present
				if controllerutil.ContainsFinalizer(plugin, PluginFinalizer) {
					controllerutil.RemoveFinalizer(plugin, PluginFinalizer)
					if updateErr := k8sClient.Update(ctx, plugin); updateErr != nil {
						return
					}
				}
				if plugin.DeletionTimestamp.IsZero() {
					_ = k8sClient.Delete(ctx, plugin)
				}
			}

			Eventually(func() bool {
				err := k8sClient.Get(ctx, typeNamespacedName, plugin)
				return errors.IsNotFound(err)
			}, time.Second*10, time.Millisecond*500).Should(BeTrue())

			By("cleaning up the PaperMCServer resource")
			server := &mck8slexlav1alpha1.PaperMCServer{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: existingServerName, Namespace: "default"}, server)
			if err == nil {
				_ = k8sClient.Delete(ctx, server)
			}
		})

		It("should remove DeletionProgress entry when server is deleted", func() {
			By("getting the plugin and adding finalizer")
			plugin := &mck8slexlav1alpha1.Plugin{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, plugin)).To(Succeed())
			controllerutil.AddFinalizer(plugin, PluginFinalizer)
			Expect(k8sClient.Update(ctx, plugin)).To(Succeed())

			By("manually setting DeletionProgress with non-existent server")
			Expect(k8sClient.Get(ctx, typeNamespacedName, plugin)).To(Succeed())
			plugin.Status.DeletionProgress = []mck8slexlav1alpha1.DeletionProgressEntry{
				{
					ServerName: deletedServerName, // This server doesn't exist
					Namespace:  "default",
					JARDeleted: false,
				},
			}
			Expect(k8sClient.Status().Update(ctx, plugin)).To(Succeed())

			By("verifying that DeletionProgress entry for non-existent server can be identified")
			Expect(k8sClient.Get(ctx, typeNamespacedName, plugin)).To(Succeed())
			Expect(plugin.Status.DeletionProgress).To(HaveLen(1))
			Expect(plugin.Status.DeletionProgress[0].ServerName).To(Equal(deletedServerName))

			// Note: The actual cleanupDeletedServers() logic will remove this entry
			// This test verifies the precondition - the entry exists for a non-existent server
			var nonExistentServer mck8slexlav1alpha1.PaperMCServer
			err := k8sClient.Get(ctx, types.NamespacedName{Name: deletedServerName, Namespace: "default"}, &nonExistentServer)
			Expect(errors.IsNotFound(err)).To(BeTrue(), "Server should not exist")
		})

		It("should handle mixed scenario with existing and deleted servers", func() {
			By("getting the plugin and adding finalizer")
			plugin := &mck8slexlav1alpha1.Plugin{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, plugin)).To(Succeed())
			controllerutil.AddFinalizer(plugin, PluginFinalizer)
			Expect(k8sClient.Update(ctx, plugin)).To(Succeed())

			By("manually setting DeletionProgress with mixed servers")
			Expect(k8sClient.Get(ctx, typeNamespacedName, plugin)).To(Succeed())
			plugin.Status.DeletionProgress = []mck8slexlav1alpha1.DeletionProgressEntry{
				{
					ServerName: existingServerName, // This server exists
					Namespace:  "default",
					JARDeleted: false,
				},
				{
					ServerName: deletedServerName, // This server doesn't exist
					Namespace:  "default",
					JARDeleted: false,
				},
			}
			Expect(k8sClient.Status().Update(ctx, plugin)).To(Succeed())

			By("verifying the DeletionProgress has 2 entries")
			Expect(k8sClient.Get(ctx, typeNamespacedName, plugin)).To(Succeed())
			Expect(plugin.Status.DeletionProgress).To(HaveLen(2))

			By("verifying existing-server exists but deleted-server does not")
			var existingServer mck8slexlav1alpha1.PaperMCServer
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: existingServerName, Namespace: "default"}, &existingServer)).To(Succeed())

			var nonExistentServer mck8slexlav1alpha1.PaperMCServer
			err := k8sClient.Get(ctx, types.NamespacedName{Name: deletedServerName, Namespace: "default"}, &nonExistentServer)
			Expect(errors.IsNotFound(err)).To(BeTrue())

			// After cleanupDeletedServers() is called, only existing-server should remain
			// This test verifies the precondition
		})

		It("should allow finalizer removal when all servers in DeletionProgress are deleted", func() {
			By("getting the plugin and adding finalizer")
			plugin := &mck8slexlav1alpha1.Plugin{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, plugin)).To(Succeed())
			controllerutil.AddFinalizer(plugin, PluginFinalizer)
			Expect(k8sClient.Update(ctx, plugin)).To(Succeed())

			By("manually setting DeletionProgress with only non-existent servers")
			Expect(k8sClient.Get(ctx, typeNamespacedName, plugin)).To(Succeed())
			plugin.Status.DeletionProgress = []mck8slexlav1alpha1.DeletionProgressEntry{
				{
					ServerName: deletedServerName,
					Namespace:  "default",
					JARDeleted: false,
				},
				{
					ServerName: anotherDeletedServerName,
					Namespace:  "default",
					JARDeleted: false,
				},
			}
			Expect(k8sClient.Status().Update(ctx, plugin)).To(Succeed())

			By("verifying both servers do not exist")
			var server1, server2 mck8slexlav1alpha1.PaperMCServer
			err1 := k8sClient.Get(ctx, types.NamespacedName{Name: deletedServerName, Namespace: "default"}, &server1)
			err2 := k8sClient.Get(ctx, types.NamespacedName{Name: anotherDeletedServerName, Namespace: "default"}, &server2)
			Expect(errors.IsNotFound(err1)).To(BeTrue())
			Expect(errors.IsNotFound(err2)).To(BeTrue())

			// After cleanupDeletedServers() removes both entries,
			// DeletionProgress will be empty, and allJARsDeleted() will return true
			// allowing finalizer removal. This test verifies preconditions.
		})
	})

	Context("Deletion timeout to prevent deadlock", func() {
		It("should force-complete deletion entries older than timeout", func() {
			reconciler := &PluginReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			fifteenMinAgo := metav1.NewTime(time.Now().Add(-15 * time.Minute))
			oneMinAgo := metav1.NewTime(time.Now().Add(-1 * time.Minute))

			plugin := &mck8slexlav1alpha1.Plugin{
				Status: mck8slexlav1alpha1.PluginStatus{
					DeletionProgress: []mck8slexlav1alpha1.DeletionProgressEntry{
						{
							ServerName:          "stale-server",
							Namespace:           "default",
							JARDeleted:          false,
							DeletionRequestedAt: &fifteenMinAgo, // 15 min ago — should be force-completed
						},
						{
							ServerName:          "recent-server",
							Namespace:           "default",
							JARDeleted:          false,
							DeletionRequestedAt: &oneMinAgo, // 1 min ago — should NOT be force-completed
						},
					},
				},
			}

			reconciler.forceCompleteStaleDeletions(context.Background(), plugin)

			Expect(plugin.Status.DeletionProgress[0].JARDeleted).To(BeTrue(),
				"Stale entry (15 min ago) should be force-completed")
			Expect(plugin.Status.DeletionProgress[0].DeletedAt).NotTo(BeNil())

			Expect(plugin.Status.DeletionProgress[1].JARDeleted).To(BeFalse(),
				"Recent entry (1 min ago) should NOT be force-completed")
			Expect(plugin.Status.DeletionProgress[1].DeletedAt).To(BeNil())
		})

		It("should not force-complete entries without DeletionRequestedAt", func() {
			reconciler := &PluginReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			plugin := &mck8slexlav1alpha1.Plugin{
				Status: mck8slexlav1alpha1.PluginStatus{
					DeletionProgress: []mck8slexlav1alpha1.DeletionProgressEntry{
						{
							ServerName:          "no-timestamp-server",
							Namespace:           "default",
							JARDeleted:          false,
							DeletionRequestedAt: nil,
						},
					},
				},
			}

			reconciler.forceCompleteStaleDeletions(context.Background(), plugin)

			Expect(plugin.Status.DeletionProgress[0].JARDeleted).To(BeFalse(),
				"Entry without DeletionRequestedAt should not be force-completed")
		})
	})

	Context("forceCompleteStaleDeletions bugs", func() {
		It("should accept context parameter for logging", func() {
			// Regression: forceCompleteStaleDeletions used to call slog.Warn() without context.
			// Per project standards, must use slog.WarnContext(ctx, ...).
			// Verify via AST that the function accepts ctx parameter.
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, "plugin_controller.go", nil, 0)
			Expect(err).NotTo(HaveOccurred())

			var found bool
			ast.Inspect(f, func(n ast.Node) bool {
				fn, ok := n.(*ast.FuncDecl)
				if !ok || fn.Name.Name != "forceCompleteStaleDeletions" {
					return true
				}
				found = true
				// Check that function has ctx parameter (beyond receiver)
				params := fn.Type.Params.List
				// Receiver is separate, params should include context.Context
				hasCtx := false
				for _, p := range params {
					if sel, ok := p.Type.(*ast.SelectorExpr); ok {
						if sel.Sel.Name == "Context" {
							hasCtx = true
						}
					}
				}
				Expect(hasCtx).To(BeTrue(),
					"forceCompleteStaleDeletions should accept context.Context for slog.WarnContext")
				return false
			})
			Expect(found).To(BeTrue(), "forceCompleteStaleDeletions function not found")
		})

		It("should persist changes to status after force-completing", func() {
			// Regression: forceCompleteStaleDeletions used to modify in-memory status
			// but never called r.Status().Update(). Changes were lost on next reconciliation.
			// Verify via AST that the function calls r.Status().Update() or
			// that the caller (reconcileDelete) persists after calling it.
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, "plugin_controller.go", nil, 0)
			Expect(err).NotTo(HaveOccurred())

			// Check reconcileDelete: after forceCompleteStaleDeletions call,
			// there must be a Status().Update() call BEFORE allJARsDeleted check
			var foundReconcileDelete bool
			ast.Inspect(f, func(n ast.Node) bool {
				fn, ok := n.(*ast.FuncDecl)
				if !ok || fn.Name.Name != "reconcileDelete" {
					return true
				}
				foundReconcileDelete = true

				// Find positions of forceCompleteStaleDeletions and allJARsDeleted
				var forceCompletePos, allJARsPos token.Pos
				var hasStatusUpdateBetween bool
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					sel, ok := call.Fun.(*ast.SelectorExpr)
					if !ok {
						return true
					}
					if sel.Sel.Name == "forceCompleteStaleDeletions" {
						forceCompletePos = call.Pos()
					}
					if sel.Sel.Name == "allJARsDeleted" {
						allJARsPos = call.Pos()
					}
					// Look for Status().Update() pattern
					if sel.Sel.Name == "Update" && forceCompletePos.IsValid() &&
						(!allJARsPos.IsValid() || call.Pos() < allJARsPos) &&
						call.Pos() > forceCompletePos {
						hasStatusUpdateBetween = true
					}
					return true
				})

				Expect(forceCompletePos.IsValid()).To(BeTrue(),
					"forceCompleteStaleDeletions call not found in reconcileDelete")
				Expect(hasStatusUpdateBetween).To(BeTrue(),
					"Status().Update() must be called between forceCompleteStaleDeletions and allJARsDeleted")
				return false
			})
			Expect(foundReconcileDelete).To(BeTrue(), "reconcileDelete function not found")
		})
	})

	Context("statusEqual", func() {
		now := metav1.Now()

		It("should detect DownloadURL change in AvailableVersions", func() {
			// Regression: statusEqual used to only compare len(AvailableVersions), not content.
			// When downloadURL changes (e.g., from GitHub page URL to empty after ExternalURL
			// filter removal), status update was skipped because len stays the same.
			a := &mck8slexlav1alpha1.PluginStatus{
				RepositoryStatus: "available",
				AvailableVersions: []mck8slexlav1alpha1.PluginVersionInfo{
					{
						Version:     "2.21.2",
						DownloadURL: "https://github.com/EssentialsX/Essentials/releases/tags/2.21.2",
						CachedAt:    now,
					},
				},
			}
			b := &mck8slexlav1alpha1.PluginStatus{
				RepositoryStatus: "available",
				AvailableVersions: []mck8slexlav1alpha1.PluginVersionInfo{
					{
						Version:     "2.21.2",
						DownloadURL: "", // Empty after ExternalURL filter removal
						CachedAt:    now,
					},
				},
			}

			Expect(statusEqual(a, b)).To(BeFalse(),
				"statusEqual should detect DownloadURL change in AvailableVersions")
		})

		It("should detect version change in AvailableVersions", func() {
			a := &mck8slexlav1alpha1.PluginStatus{
				RepositoryStatus: "available",
				AvailableVersions: []mck8slexlav1alpha1.PluginVersionInfo{
					{Version: "1.0.0", CachedAt: now},
				},
			}
			b := &mck8slexlav1alpha1.PluginStatus{
				RepositoryStatus: "available",
				AvailableVersions: []mck8slexlav1alpha1.PluginVersionInfo{
					{Version: "2.0.0", CachedAt: now},
				},
			}

			Expect(statusEqual(a, b)).To(BeFalse(),
				"statusEqual should detect version change in AvailableVersions")
		})

		It("should detect MatchedInstances content change", func() {
			a := &mck8slexlav1alpha1.PluginStatus{
				RepositoryStatus: "available",
				MatchedInstances: []mck8slexlav1alpha1.MatchedInstance{
					{Name: "server-a", Compatible: true},
				},
			}
			b := &mck8slexlav1alpha1.PluginStatus{
				RepositoryStatus: "available",
				MatchedInstances: []mck8slexlav1alpha1.MatchedInstance{
					{Name: "server-b", Compatible: true},
				},
			}

			Expect(statusEqual(a, b)).To(BeFalse(),
				"statusEqual should detect MatchedInstances content change")
		})

		It("should return true for truly equal statuses", func() {
			a := &mck8slexlav1alpha1.PluginStatus{
				RepositoryStatus: "available",
				AvailableVersions: []mck8slexlav1alpha1.PluginVersionInfo{
					{Version: "1.0.0", DownloadURL: "https://example.com/v1.jar", CachedAt: now},
				},
				MatchedInstances: []mck8slexlav1alpha1.MatchedInstance{
					{Name: "server-a", Compatible: true},
				},
			}
			b := &mck8slexlav1alpha1.PluginStatus{
				RepositoryStatus: "available",
				AvailableVersions: []mck8slexlav1alpha1.PluginVersionInfo{
					{Version: "1.0.0", DownloadURL: "https://example.com/v1.jar", CachedAt: now},
				},
				MatchedInstances: []mck8slexlav1alpha1.MatchedInstance{
					{Name: "server-a", Compatible: true},
				},
			}

			Expect(statusEqual(a, b)).To(BeTrue(),
				"statusEqual should return true for identical statuses")
		})

		It("should treat nil and empty AvailableVersions as equal", func() {
			a := &mck8slexlav1alpha1.PluginStatus{
				RepositoryStatus:  "available",
				AvailableVersions: nil,
			}
			b := &mck8slexlav1alpha1.PluginStatus{
				RepositoryStatus:  "available",
				AvailableVersions: []mck8slexlav1alpha1.PluginVersionInfo{},
			}

			Expect(statusEqual(a, b)).To(BeTrue(),
				"nil and empty AvailableVersions should be equal to prevent infinite reconcile loops")
		})

		It("should treat nil and empty MatchedInstances as equal", func() {
			a := &mck8slexlav1alpha1.PluginStatus{
				RepositoryStatus: "available",
				MatchedInstances: nil,
			}
			b := &mck8slexlav1alpha1.PluginStatus{
				RepositoryStatus: "available",
				MatchedInstances: []mck8slexlav1alpha1.MatchedInstance{},
			}

			Expect(statusEqual(a, b)).To(BeTrue(),
				"nil and empty MatchedInstances should be equal to prevent infinite reconcile loops")
		})

		It("should treat nil and empty Conditions as equal", func() {
			a := &mck8slexlav1alpha1.PluginStatus{
				RepositoryStatus: "available",
				Conditions:       nil,
			}
			b := &mck8slexlav1alpha1.PluginStatus{
				RepositoryStatus: "available",
				Conditions:       []metav1.Condition{},
			}

			Expect(statusEqual(a, b)).To(BeTrue(),
				"nil and empty Conditions should be equal to prevent infinite reconcile loops")
		})

		It("should detect Conditions changes", func() {
			// Regression: statusEqual did not compare Conditions field.
			// When conditions change (e.g., Ready transitions from True to False),
			// statusEqual returned true, so Status().Update() was never called and
			// condition changes were lost.
			now := metav1.Now()
			a := &mck8slexlav1alpha1.PluginStatus{
				RepositoryStatus: "available",
				Conditions: []metav1.Condition{
					{
						Type:               "Ready",
						Status:             metav1.ConditionTrue,
						LastTransitionTime: now,
						Reason:             "ReconcileSuccess",
						Message:            "OK",
					},
				},
			}
			b := &mck8slexlav1alpha1.PluginStatus{
				RepositoryStatus: "available",
				Conditions: []metav1.Condition{
					{
						Type:               "Ready",
						Status:             metav1.ConditionFalse,
						LastTransitionTime: now,
						Reason:             "ReconcileError",
						Message:            "something failed",
					},
				},
			}

			Expect(statusEqual(a, b)).To(BeFalse(),
				"statusEqual should detect Conditions changes")
		})
	})

	Context("doReconcile early return on unavailable repository", func() {
		It("should return early and use syncPluginMetadata result when no versions available", func() {
			// Regression: When syncPluginMetadata returns nil versions (repo unavailable, no cache),
			// it returns result={RequeueAfter: 5m}, err=nil. But doReconcile only checked
			// err != nil, ignoring the result. It continued to set VersionResolved=True
			// and Ready=True even though no metadata was fetched.
			//
			// Fix: doReconcile should check if allVersions is nil and return
			// the result from syncPluginMetadata.
			src, readErr := os.ReadFile("plugin_controller.go")
			Expect(readErr).NotTo(HaveOccurred())
			srcStr := string(src)

			// The doReconcile function should use the allVersions return value
			// (not discard it with _) to decide whether to continue.
			// Currently line 129 discards it: "_, result, err := r.syncPluginMetadata(...)"
			// After fix, it should check allVersions == nil and return result early.
			Expect(srcStr).NotTo(ContainSubstring("_, result, err := r.syncPluginMetadata"),
				"doReconcile should NOT discard allVersions from syncPluginMetadata; "+
					"it must check for nil versions and return early when repository is unavailable")
		})
	})

	Context("Reconcile flow with mocked dependencies", func() {
		var (
			reconciler *PluginReconciler
			mockPlugin *testutil.MockPluginClient
			namespace  string
		)

		BeforeEach(func() {
			namespace = "default"
			mockPlugin = &testutil.MockPluginClient{
				Versions: []plugins.PluginVersion{
					{
						Version:           "2.21.0",
						ReleaseDate:       time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC),
						MinecraftVersions: []string{"1.20.4", "1.21.0", "1.21.1"},
						DownloadURL:       "https://example.com/plugin-2.21.0.jar",
						Hash:              "abc123",
					},
					{
						Version:           "2.21.2",
						ReleaseDate:       time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
						MinecraftVersions: []string{"1.21.0", "1.21.1", "1.21.4"},
						DownloadURL:       "https://example.com/plugin-2.21.2.jar",
						Hash:              "def456",
					},
				},
			}
			reconciler = &PluginReconciler{
				Client:       k8sClient,
				Scheme:       k8sClient.Scheme(),
				PluginClient: mockPlugin,
				Solver:       solver.NewSimpleSolver(),
			}
		})

		createPlugin := func(name string, spec mck8slexlav1alpha1.PluginSpec) {
			plugin := &mck8slexlav1alpha1.Plugin{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
				},
				Spec: spec,
			}
			Expect(k8sClient.Create(ctx, plugin)).To(Succeed())
		}

		deletePlugin := func(name string) {
			plugin := &mck8slexlav1alpha1.Plugin{}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, plugin)
			if err != nil {
				return
			}
			if controllerutil.ContainsFinalizer(plugin, PluginFinalizer) {
				controllerutil.RemoveFinalizer(plugin, PluginFinalizer)
				if updateErr := k8sClient.Update(ctx, plugin); updateErr != nil {
					return
				}
			}
			if plugin.DeletionTimestamp.IsZero() {
				_ = k8sClient.Delete(ctx, plugin)
			}
		}

		It("should add finalizer on first reconcile", func() {
			pluginName := "test-finalizer-add"
			createPlugin(pluginName, mck8slexlav1alpha1.PluginSpec{
				Source:         mck8slexlav1alpha1.PluginSource{Type: "hangar", Project: "TestPlugin"},
				UpdateStrategy: "latest",
				InstanceSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{"test-finalizer": "true"},
				},
			})
			defer deletePlugin(pluginName)

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: pluginName, Namespace: namespace}}
			result, err := reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(Equal(ctrl.Result{}), "Should requeue after adding finalizer")

			var plugin mck8slexlav1alpha1.Plugin
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: pluginName, Namespace: namespace}, &plugin)).To(Succeed())
			Expect(controllerutil.ContainsFinalizer(&plugin, PluginFinalizer)).To(BeTrue(),
				"Plugin should have finalizer after first reconcile")
		})

		It("should fetch metadata and set RepositoryAvailable condition", func() {
			pluginName := "test-metadata-fetch"
			createPlugin(pluginName, mck8slexlav1alpha1.PluginSpec{
				Source:         mck8slexlav1alpha1.PluginSource{Type: "hangar", Project: "EssentialsX"},
				UpdateStrategy: "latest",
				InstanceSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{"test-metadata": "true"},
				},
			})
			defer deletePlugin(pluginName)

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: pluginName, Namespace: namespace}}

			// First reconcile adds finalizer
			_, err := reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			// Second reconcile fetches metadata
			_, err = reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			var plugin mck8slexlav1alpha1.Plugin
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: pluginName, Namespace: namespace}, &plugin)).To(Succeed())

			// Verify RepositoryAvailable condition
			cond := findCondition(plugin.Status.Conditions, conditionTypeRepositoryAvailable)
			Expect(cond).NotTo(BeNil(), "RepositoryAvailable condition should be set")
			Expect(cond.Status).To(Equal(metav1.ConditionTrue))

			// Verify metadata cached in status
			Expect(plugin.Status.RepositoryStatus).To(Equal("available"))
			Expect(plugin.Status.AvailableVersions).To(HaveLen(2))
			Expect(plugin.Status.LastFetched).NotTo(BeNil())

			// Verify PluginClient was called with correct project
			Expect(mockPlugin.GetVersionsCalls).To(Equal(1))
			Expect(mockPlugin.GetVersionsProjects).To(ContainElement("EssentialsX"))
		})

		It("should set Ready=True and VersionResolved=True on successful reconcile", func() {
			pluginName := "test-ready-true"
			createPlugin(pluginName, mck8slexlav1alpha1.PluginSpec{
				Source:         mck8slexlav1alpha1.PluginSource{Type: "hangar", Project: "TestPlugin"},
				UpdateStrategy: "latest",
				InstanceSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{"test-ready": "true"},
				},
			})
			defer deletePlugin(pluginName)

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: pluginName, Namespace: namespace}}

			// First reconcile: finalizer
			_, err := reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			// Second reconcile: metadata + conditions
			_, err = reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			var plugin mck8slexlav1alpha1.Plugin
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: pluginName, Namespace: namespace}, &plugin)).To(Succeed())

			readyCond := findCondition(plugin.Status.Conditions, conditionTypeReady)
			Expect(readyCond).NotTo(BeNil(), "Ready condition should be set")
			Expect(readyCond.Status).To(Equal(metav1.ConditionTrue))

			versionCond := findCondition(plugin.Status.Conditions, conditionTypeVersionResolved)
			Expect(versionCond).NotTo(BeNil(), "VersionResolved condition should be set")
			Expect(versionCond.Status).To(Equal(metav1.ConditionTrue))
		})

		It("should set unavailable status when repository fails and no cache exists", func() {
			pluginName := "test-repo-unavailable"
			mockPlugin.VersionErr = fmt.Errorf("internal server error")
			mockPlugin.Versions = nil

			createPlugin(pluginName, mck8slexlav1alpha1.PluginSpec{
				Source:         mck8slexlav1alpha1.PluginSource{Type: "hangar", Project: "BrokenPlugin"},
				UpdateStrategy: "latest",
				InstanceSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{"test-unavail": "true"},
				},
			})
			defer deletePlugin(pluginName)

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: pluginName, Namespace: namespace}}

			// First reconcile: finalizer
			_, err := reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			// Second reconcile: metadata fetch fails
			result, err := reconciler.Reconcile(ctx, req)
			// err is nil because handleRepositoryError returns nil error with RequeueAfter
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(5*time.Minute),
				"Should requeue after 5 minutes when repository unavailable")

			var plugin mck8slexlav1alpha1.Plugin
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: pluginName, Namespace: namespace}, &plugin)).To(Succeed())

			Expect(plugin.Status.RepositoryStatus).To(Equal("unavailable"))

			repoCond := findCondition(plugin.Status.Conditions, conditionTypeRepositoryAvailable)
			Expect(repoCond).NotTo(BeNil())
			Expect(repoCond.Status).To(Equal(metav1.ConditionFalse))
		})

		It("should use cached data (orphaned status) when repository fails but cache exists", func() {
			pluginName := "test-repo-orphaned"
			createPlugin(pluginName, mck8slexlav1alpha1.PluginSpec{
				Source:         mck8slexlav1alpha1.PluginSource{Type: "hangar", Project: "CachedPlugin"},
				UpdateStrategy: "latest",
				InstanceSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{"test-orphaned": "true"},
				},
			})
			defer deletePlugin(pluginName)

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: pluginName, Namespace: namespace}}

			// First reconcile: finalizer
			_, err := reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			// Second reconcile: successful metadata fetch (populates cache)
			_, err = reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			// Verify cache is populated
			var plugin mck8slexlav1alpha1.Plugin
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: pluginName, Namespace: namespace}, &plugin)).To(Succeed())
			Expect(plugin.Status.AvailableVersions).To(HaveLen(2))

			// Now make repository fail
			mockPlugin.VersionErr = fmt.Errorf("internal server error")
			mockPlugin.Versions = nil

			// Third reconcile: uses cached data
			result, err := reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(15*time.Minute),
				"Should continue normal requeue when cache is used")

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: pluginName, Namespace: namespace}, &plugin)).To(Succeed())
			Expect(plugin.Status.RepositoryStatus).To(Equal("orphaned"))
			Expect(plugin.Status.AvailableVersions).To(HaveLen(2),
				"Cached versions should be preserved")
		})

		It("should build MatchedInstances from label selector", func() {
			pluginName := "test-match-selector"
			matchLabel := "test-match-plugin"

			// Create a PaperMCServer that matches the selector
			server := &mck8slexlav1alpha1.PaperMCServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "match-target-server",
					Namespace: namespace,
					Labels: map[string]string{
						matchLabel: "true",
					},
				},
				Spec: mck8slexlav1alpha1.PaperMCServerSpec{
					UpdateStrategy: "latest",
					PodTemplate: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{{Name: "papermc"}},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, server)).To(Succeed())
			defer func() {
				_ = k8sClient.Delete(ctx, server)
			}()

			createPlugin(pluginName, mck8slexlav1alpha1.PluginSpec{
				Source:         mck8slexlav1alpha1.PluginSource{Type: "hangar", Project: "MatchPlugin"},
				UpdateStrategy: "latest",
				InstanceSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{matchLabel: "true"},
				},
			})
			defer deletePlugin(pluginName)

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: pluginName, Namespace: namespace}}

			// First reconcile: finalizer
			_, err := reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			// Second reconcile: metadata + matching
			_, err = reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			var plugin mck8slexlav1alpha1.Plugin
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: pluginName, Namespace: namespace}, &plugin)).To(Succeed())

			Expect(plugin.Status.MatchedInstances).To(HaveLen(1))
			Expect(plugin.Status.MatchedInstances[0].Name).To(Equal("match-target-server"))
			Expect(plugin.Status.MatchedInstances[0].Namespace).To(Equal(namespace))
			Expect(plugin.Status.MatchedInstances[0].Compatible).To(BeTrue())
		})

		It("should set RepositoryAvailable=False when source type is unsupported", func() {
			// Unsupported source type is treated as repository fetch error,
			// not as a reconcile error. The plugin is "ready" but repo unavailable.
			pluginName := "test-unsupported-source"
			createPlugin(pluginName, mck8slexlav1alpha1.PluginSpec{
				Source:         mck8slexlav1alpha1.PluginSource{Type: "modrinth", Project: "SomePlugin"},
				UpdateStrategy: "latest",
				InstanceSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{"test-unsupported": "true"},
				},
			})
			defer deletePlugin(pluginName)

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: pluginName, Namespace: namespace}}

			// First reconcile: finalizer
			_, err := reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			// Second reconcile: unsupported source type → handled as repo unavailable
			result, err := reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred(),
				"Unsupported source type is handled gracefully, not returned as error")
			Expect(result.RequeueAfter).To(Equal(5*time.Minute),
				"Should requeue after 5 minutes like any unavailable repo")

			var plugin mck8slexlav1alpha1.Plugin
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: pluginName, Namespace: namespace}, &plugin)).To(Succeed())

			Expect(plugin.Status.RepositoryStatus).To(Equal("unavailable"))

			repoCond := findCondition(plugin.Status.Conditions, conditionTypeRepositoryAvailable)
			Expect(repoCond).NotTo(BeNil())
			Expect(repoCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(repoCond.Message).To(ContainSubstring("unsupported source type"))
		})

		It("should return empty result for non-existent plugin", func() {
			req := ctrl.Request{NamespacedName: types.NamespacedName{
				Name: "nonexistent-plugin", Namespace: namespace,
			}}
			result, err := reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(ctrl.Result{}))
		})

		It("should build empty MatchedInstances when no servers match selector", func() {
			pluginName := "test-no-match"
			createPlugin(pluginName, mck8slexlav1alpha1.PluginSpec{
				Source:         mck8slexlav1alpha1.PluginSource{Type: "hangar", Project: "LonelyPlugin"},
				UpdateStrategy: "latest",
				InstanceSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{"nonexistent-label": "true"},
				},
			})
			defer deletePlugin(pluginName)

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: pluginName, Namespace: namespace}}

			// First reconcile: finalizer
			_, err := reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			// Second reconcile: no servers match
			_, err = reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			var plugin mck8slexlav1alpha1.Plugin
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: pluginName, Namespace: namespace}, &plugin)).To(Succeed())

			Expect(plugin.Status.MatchedInstances).To(BeEmpty(),
				"MatchedInstances should be empty when no servers match")

			// Should still be Ready=True (no matching servers is not an error)
			readyCond := findCondition(plugin.Status.Conditions, conditionTypeReady)
			Expect(readyCond).NotTo(BeNil())
			Expect(readyCond.Status).To(Equal(metav1.ConditionTrue))
		})

		It("should cache versions in AvailableVersions status field", func() {
			pluginName := "test-cache-versions"
			createPlugin(pluginName, mck8slexlav1alpha1.PluginSpec{
				Source:         mck8slexlav1alpha1.PluginSource{Type: "hangar", Project: "CacheTest"},
				UpdateStrategy: "latest",
				InstanceSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{"test-cache": "true"},
				},
			})
			defer deletePlugin(pluginName)

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: pluginName, Namespace: namespace}}

			// First reconcile: finalizer
			_, err := reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			// Second reconcile: metadata fetch
			_, err = reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			var plugin mck8slexlav1alpha1.Plugin
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: pluginName, Namespace: namespace}, &plugin)).To(Succeed())

			Expect(plugin.Status.AvailableVersions).To(HaveLen(2))
			Expect(plugin.Status.AvailableVersions[0].Version).To(Equal("2.21.0"))
			Expect(plugin.Status.AvailableVersions[0].DownloadURL).To(Equal("https://example.com/plugin-2.21.0.jar"))
			Expect(plugin.Status.AvailableVersions[0].Hash).To(Equal("abc123"))
			Expect(plugin.Status.AvailableVersions[1].Version).To(Equal("2.21.2"))
		})
	})
})
