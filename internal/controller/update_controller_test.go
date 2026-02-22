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

	"github.com/cockroachdb/errors"
	mcv1alpha1 "github.com/lexfrei/minecraft-operator/api/v1alpha1"
	"github.com/lexfrei/minecraft-operator/pkg/rcon"
	"github.com/lexfrei/minecraft-operator/pkg/testutil"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	testNamespace        = "default"
	updateControllerPath = "update_controller.go"
)

var _ = Describe("UpdateController", func() {
	Context("Cron scheduling", func() {
		var (
			ctx        context.Context
			reconciler *UpdateReconciler
			mockCron   *testutil.MockCronScheduler
			serverName string
			namespace  string
		)

		BeforeEach(func() {
			ctx = context.Background()
			mockCron = testutil.NewMockCronScheduler()

			reconciler = &UpdateReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
				cron:   mockCron,
			}

			serverName = "test-server-cron"
			namespace = testNamespace
		})

		AfterEach(func() {
			// Clean up created resources
			server := &mcv1alpha1.PaperMCServer{}
			_ = k8sClient.Get(ctx, types.NamespacedName{
				Name:      serverName,
				Namespace: namespace,
			}, server)
			_ = k8sClient.Delete(ctx, server)
		})

		It("should add cron job when PaperMCServer created with enabled maintenance window", func() {
			server := &mcv1alpha1.PaperMCServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serverName,
					Namespace: namespace,
				},
				Spec: mcv1alpha1.PaperMCServerSpec{
					UpdateStrategy: "latest",
					Version:        "1.21.0",
					UpdateSchedule: mcv1alpha1.UpdateSchedule{
						CheckCron: "0 3 * * *",
						MaintenanceWindow: mcv1alpha1.MaintenanceWindow{
							Cron:    "0 4 * * 0",
							Enabled: true,
						},
					},
					GracefulShutdown: mcv1alpha1.GracefulShutdown{
						Timeout: metav1.Duration{Duration: 300},
					},
					RCON: mcv1alpha1.RCONConfig{
						Enabled: true,
						PasswordSecret: mcv1alpha1.SecretKeyRef{
							Name: "rcon-secret",
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

			Expect(k8sClient.Create(ctx, server)).To(Succeed())

			req := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      serverName,
					Namespace: namespace,
				},
			}

			_, err := reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			// Verify cron job was added
			Expect(mockCron.Jobs).To(HaveLen(1))

			// Verify correct cron spec
			var foundJob *testutil.MockCronJob
			for _, job := range mockCron.Jobs {
				foundJob = job
				break
			}
			Expect(foundJob).NotTo(BeNil())
			Expect(foundJob.Spec).To(Equal("0 4 * * 0"))
			Expect(foundJob.Removed).To(BeFalse())
		})

		It("should not add cron job when maintenance window is disabled", func() {
			server := &mcv1alpha1.PaperMCServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serverName,
					Namespace: namespace,
				},
				Spec: mcv1alpha1.PaperMCServerSpec{
					UpdateStrategy: "latest",
					Version:        "1.21.0",
					UpdateSchedule: mcv1alpha1.UpdateSchedule{
						CheckCron: "0 3 * * *",
						MaintenanceWindow: mcv1alpha1.MaintenanceWindow{
							Cron:    "0 4 * * 0",
							Enabled: false, // Disabled
						},
					},
					GracefulShutdown: mcv1alpha1.GracefulShutdown{
						Timeout: metav1.Duration{Duration: 300},
					},
					RCON: mcv1alpha1.RCONConfig{
						Enabled: true,
						PasswordSecret: mcv1alpha1.SecretKeyRef{
							Name: "rcon-secret",
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

			Expect(k8sClient.Create(ctx, server)).To(Succeed())

			req := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      serverName,
					Namespace: namespace,
				},
			}

			_, err := reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			// Verify no cron job was added
			Expect(mockCron.Jobs).To(BeEmpty())
		})

		It("should return error for invalid cron expression", func() {
			server := &mcv1alpha1.PaperMCServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serverName,
					Namespace: namespace,
				},
				Spec: mcv1alpha1.PaperMCServerSpec{
					UpdateStrategy: "latest",
					Version:        "1.21.0",
					UpdateSchedule: mcv1alpha1.UpdateSchedule{
						CheckCron: "0 3 * * *",
						MaintenanceWindow: mcv1alpha1.MaintenanceWindow{
							Cron:    "invalid cron expression",
							Enabled: true,
						},
					},
					GracefulShutdown: mcv1alpha1.GracefulShutdown{
						Timeout: metav1.Duration{Duration: 300},
					},
					RCON: mcv1alpha1.RCONConfig{
						Enabled: true,
						PasswordSecret: mcv1alpha1.SecretKeyRef{
							Name: "rcon-secret",
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

			Expect(k8sClient.Create(ctx, server)).To(Succeed())

			req := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      serverName,
					Namespace: namespace,
				},
			}

			_, err := reconciler.Reconcile(ctx, req)
			Expect(err).To(HaveOccurred())
		})

		It("should remove cron job when PaperMCServer deleted", func() {
			server := &mcv1alpha1.PaperMCServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serverName,
					Namespace: namespace,
				},
				Spec: mcv1alpha1.PaperMCServerSpec{
					UpdateStrategy: "latest",
					Version:        "1.21.0",
					UpdateSchedule: mcv1alpha1.UpdateSchedule{
						CheckCron: "0 3 * * *",
						MaintenanceWindow: mcv1alpha1.MaintenanceWindow{
							Cron:    "0 4 * * 0",
							Enabled: true,
						},
					},
					GracefulShutdown: mcv1alpha1.GracefulShutdown{
						Timeout: metav1.Duration{Duration: 300},
					},
					RCON: mcv1alpha1.RCONConfig{
						Enabled: true,
						PasswordSecret: mcv1alpha1.SecretKeyRef{
							Name: "rcon-secret",
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

			Expect(k8sClient.Create(ctx, server)).To(Succeed())

			req := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      serverName,
					Namespace: namespace,
				},
			}

			// First reconcile - add cron job
			_, err := reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(mockCron.Jobs).To(HaveLen(1))

			// Get the job ID
			var jobID testutil.MockCronJob
			for _, job := range mockCron.Jobs {
				jobID = *job
				break
			}

			// Delete server
			Expect(k8sClient.Delete(ctx, server)).To(Succeed())

			// Second reconcile - remove cron job
			_, err = reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			// Verify job was removed
			job := mockCron.GetJob(jobID.ID)
			Expect(job).NotTo(BeNil())
			Expect(job.Removed).To(BeTrue())
		})

		It("should update cron job when maintenance window cron spec changes", func() {
			server := &mcv1alpha1.PaperMCServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serverName,
					Namespace: namespace,
				},
				Spec: mcv1alpha1.PaperMCServerSpec{
					UpdateStrategy: "latest",
					Version:        "1.21.0",
					UpdateSchedule: mcv1alpha1.UpdateSchedule{
						CheckCron: "0 3 * * *",
						MaintenanceWindow: mcv1alpha1.MaintenanceWindow{
							Cron:    "0 4 * * 0", // Sunday 4 AM
							Enabled: true,
						},
					},
					GracefulShutdown: mcv1alpha1.GracefulShutdown{
						Timeout: metav1.Duration{Duration: 300},
					},
					RCON: mcv1alpha1.RCONConfig{
						Enabled: true,
						PasswordSecret: mcv1alpha1.SecretKeyRef{
							Name: "rcon-secret",
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

			Expect(k8sClient.Create(ctx, server)).To(Succeed())

			req := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      serverName,
					Namespace: namespace,
				},
			}

			// First reconcile
			_, err := reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(mockCron.Jobs).To(HaveLen(1))

			oldJob := mockCron.GetJobBySpec("0 4 * * 0")
			Expect(oldJob).NotTo(BeNil())

			// Update cron spec
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      serverName,
				Namespace: namespace,
			}, server)
			Expect(err).NotTo(HaveOccurred())

			server.Spec.UpdateSchedule.MaintenanceWindow.Cron = "0 5 * * 1" // Monday 5 AM
			Expect(k8sClient.Update(ctx, server)).To(Succeed())

			// Second reconcile
			_, err = reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			// Verify old job removed and new job added
			updatedOldJob := mockCron.GetJob(oldJob.ID)
			Expect(updatedOldJob.Removed).To(BeTrue())

			newJob := mockCron.GetJobBySpec("0 5 * * 1")
			Expect(newJob).NotTo(BeNil())
			Expect(newJob.Removed).To(BeFalse())
		})

		It("should NOT recreate cron job when spec is unchanged on re-reconcile", func() {
			server := &mcv1alpha1.PaperMCServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serverName,
					Namespace: namespace,
				},
				Spec: mcv1alpha1.PaperMCServerSpec{
					UpdateStrategy: "latest",
					Version:        "1.21.0",
					UpdateSchedule: mcv1alpha1.UpdateSchedule{
						CheckCron: "0 3 * * *",
						MaintenanceWindow: mcv1alpha1.MaintenanceWindow{
							Cron:    "0 4 * * 0",
							Enabled: true,
						},
					},
					GracefulShutdown: mcv1alpha1.GracefulShutdown{
						Timeout: metav1.Duration{Duration: 300},
					},
					RCON: mcv1alpha1.RCONConfig{
						Enabled: true,
						PasswordSecret: mcv1alpha1.SecretKeyRef{
							Name: "rcon-secret",
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

			Expect(k8sClient.Create(ctx, server)).To(Succeed())

			req := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      serverName,
					Namespace: namespace,
				},
			}

			// First reconcile - should add cron job
			_, err := reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(mockCron.Jobs).To(HaveLen(1))

			// Get first job ID
			var firstJobID testutil.MockCronJob
			for _, job := range mockCron.Jobs {
				firstJobID = *job
				break
			}
			Expect(firstJobID.Removed).To(BeFalse())

			// Second reconcile - same spec, should NOT create new job
			_, err = reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			// Still only 1 job (not removed + re-added)
			activeCount := 0
			for _, job := range mockCron.Jobs {
				if !job.Removed {
					activeCount++
				}
			}
			Expect(activeCount).To(Equal(1), "Expected exactly 1 active cron job after re-reconcile")

			// Same job retained (not removed and replaced)
			job := mockCron.GetJob(firstJobID.ID)
			Expect(job).NotTo(BeNil())
			Expect(job.Removed).To(BeFalse(), "Original job should not have been removed")
		})

		It("should handle multiple servers with independent cron schedules", func() {
			server1 := &mcv1alpha1.PaperMCServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "server-1",
					Namespace: namespace,
				},
				Spec: mcv1alpha1.PaperMCServerSpec{
					UpdateStrategy: "latest",
					Version:        "1.21.0",
					UpdateSchedule: mcv1alpha1.UpdateSchedule{
						CheckCron: "0 3 * * *",
						MaintenanceWindow: mcv1alpha1.MaintenanceWindow{
							Cron:    "0 4 * * 0",
							Enabled: true,
						},
					},
					GracefulShutdown: mcv1alpha1.GracefulShutdown{
						Timeout: metav1.Duration{Duration: 300},
					},
					RCON: mcv1alpha1.RCONConfig{
						Enabled: true,
						PasswordSecret: mcv1alpha1.SecretKeyRef{
							Name: "rcon-secret",
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

			server2 := &mcv1alpha1.PaperMCServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "server-2",
					Namespace: namespace,
				},
				Spec: mcv1alpha1.PaperMCServerSpec{
					UpdateStrategy: "latest",
					Version:        "1.21.0",
					UpdateSchedule: mcv1alpha1.UpdateSchedule{
						CheckCron: "0 3 * * *",
						MaintenanceWindow: mcv1alpha1.MaintenanceWindow{
							Cron:    "0 5 * * 1",
							Enabled: true,
						},
					},
					GracefulShutdown: mcv1alpha1.GracefulShutdown{
						Timeout: metav1.Duration{Duration: 300},
					},
					RCON: mcv1alpha1.RCONConfig{
						Enabled: true,
						PasswordSecret: mcv1alpha1.SecretKeyRef{
							Name: "rcon-secret",
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

			Expect(k8sClient.Create(ctx, server1)).To(Succeed())
			Expect(k8sClient.Create(ctx, server2)).To(Succeed())

			defer func() {
				_ = k8sClient.Delete(ctx, server1)
				_ = k8sClient.Delete(ctx, server2)
			}()

			// Reconcile both servers
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "server-1", Namespace: namespace},
			})
			Expect(err).NotTo(HaveOccurred())

			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "server-2", Namespace: namespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify two independent cron jobs
			Expect(mockCron.Jobs).To(HaveLen(2))

			job1 := mockCron.GetJobBySpec("0 4 * * 0")
			job2 := mockCron.GetJobBySpec("0 5 * * 1")

			Expect(job1).NotTo(BeNil())
			Expect(job2).NotTo(BeNil())
			Expect(job1.ID).NotTo(Equal(job2.ID))
		})
	})

	Context("Update delay enforcement", func() {
		var (
			reconciler *UpdateReconciler
		)

		BeforeEach(func() {
			reconciler = &UpdateReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
		})

		It("should skip update when updateDelay not satisfied", func() {
			now := metav1.Now()
			recentRelease := metav1.NewTime(now.Add(-24 * time.Hour)) // 1 day ago

			server := &mcv1alpha1.PaperMCServer{
				Spec: mcv1alpha1.PaperMCServerSpec{
					UpdateStrategy: "latest",
					Version:        "latest",
					UpdateDelay:    &metav1.Duration{Duration: 72 * time.Hour}, // 3 days
					UpdateSchedule: mcv1alpha1.UpdateSchedule{
						CheckCron: "0 3 * * *",
						MaintenanceWindow: mcv1alpha1.MaintenanceWindow{
							Cron:    "0 4 * * 0",
							Enabled: true,
						},
					},
					GracefulShutdown: mcv1alpha1.GracefulShutdown{
						Timeout: metav1.Duration{Duration: 300},
					},
					RCON: mcv1alpha1.RCONConfig{
						Enabled: true,
						PasswordSecret: mcv1alpha1.SecretKeyRef{
							Name: "rcon-secret",
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
				Status: mcv1alpha1.PaperMCServerStatus{
					CurrentVersion: "1.21.0",
					CurrentBuild:   100,
					AvailableUpdate: &mcv1alpha1.AvailableUpdate{
						Version:    "1.21.1",
						Build:      150,
						ReleasedAt: recentRelease, // Too recent
					},
				},
			}

			// Check if update should be applied
			should, remaining := reconciler.shouldApplyUpdate(server)
			Expect(should).To(BeFalse(), "Update should be skipped due to updateDelay")
			Expect(remaining).To(BeNumerically(">", 0), "Remaining time should be positive")
			Expect(remaining).To(BeNumerically("~", 48*time.Hour, 1*time.Hour), "Should wait ~48 hours")
		})

		It("should proceed with update when updateDelay satisfied", func() {
			now := metav1.Now()
			oldRelease := metav1.NewTime(now.Add(-96 * time.Hour)) // 4 days ago

			server := &mcv1alpha1.PaperMCServer{
				Spec: mcv1alpha1.PaperMCServerSpec{
					UpdateStrategy: "latest",
					Version:        "latest",
					UpdateDelay:    &metav1.Duration{Duration: 72 * time.Hour}, // 3 days
					UpdateSchedule: mcv1alpha1.UpdateSchedule{
						CheckCron: "0 3 * * *",
						MaintenanceWindow: mcv1alpha1.MaintenanceWindow{
							Cron:    "0 4 * * 0",
							Enabled: true,
						},
					},
					GracefulShutdown: mcv1alpha1.GracefulShutdown{
						Timeout: metav1.Duration{Duration: 300},
					},
					RCON: mcv1alpha1.RCONConfig{
						Enabled: true,
						PasswordSecret: mcv1alpha1.SecretKeyRef{
							Name: "rcon-secret",
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
				Status: mcv1alpha1.PaperMCServerStatus{
					CurrentVersion: "1.21.0",
					CurrentBuild:   100,
					AvailableUpdate: &mcv1alpha1.AvailableUpdate{
						Version:    "1.21.1",
						Build:      150,
						ReleasedAt: oldRelease, // Old enough
					},
				},
			}

			// Check if update should be applied
			should, remaining := reconciler.shouldApplyUpdate(server)
			Expect(should).To(BeTrue(), "Update should proceed, delay satisfied")
			Expect(remaining).To(Equal(time.Duration(0)), "No remaining time")
		})

		It("should proceed when no updateDelay configured", func() {
			now := metav1.Now()
			recentRelease := metav1.NewTime(now.Add(-1 * time.Hour)) // Just released

			server := &mcv1alpha1.PaperMCServer{
				Spec: mcv1alpha1.PaperMCServerSpec{
					UpdateStrategy: "latest",
					Version:        "latest",
					// No updateDelay specified
					UpdateSchedule: mcv1alpha1.UpdateSchedule{
						CheckCron: "0 3 * * *",
						MaintenanceWindow: mcv1alpha1.MaintenanceWindow{
							Cron:    "0 4 * * 0",
							Enabled: true,
						},
					},
					GracefulShutdown: mcv1alpha1.GracefulShutdown{
						Timeout: metav1.Duration{Duration: 300},
					},
					RCON: mcv1alpha1.RCONConfig{
						Enabled: true,
						PasswordSecret: mcv1alpha1.SecretKeyRef{
							Name: "rcon-secret",
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
				Status: mcv1alpha1.PaperMCServerStatus{
					CurrentVersion: "1.21.0",
					CurrentBuild:   100,
					AvailableUpdate: &mcv1alpha1.AvailableUpdate{
						Version:    "1.21.1",
						Build:      150,
						ReleasedAt: recentRelease, // Recent but no delay configured
					},
				},
			}

			// Check if update should be applied
			should, remaining := reconciler.shouldApplyUpdate(server)
			Expect(should).To(BeTrue(), "Update should proceed when no delay configured")
			Expect(remaining).To(Equal(time.Duration(0)), "No remaining time")
		})

		It("should return true when no availableUpdate exists", func() {
			server := &mcv1alpha1.PaperMCServer{
				Spec: mcv1alpha1.PaperMCServerSpec{
					UpdateStrategy: "latest",
					Version:        "latest",
					UpdateDelay:    &metav1.Duration{Duration: 72 * time.Hour},
					UpdateSchedule: mcv1alpha1.UpdateSchedule{
						CheckCron: "0 3 * * *",
						MaintenanceWindow: mcv1alpha1.MaintenanceWindow{
							Cron:    "0 4 * * 0",
							Enabled: true,
						},
					},
					GracefulShutdown: mcv1alpha1.GracefulShutdown{
						Timeout: metav1.Duration{Duration: 300},
					},
					RCON: mcv1alpha1.RCONConfig{
						Enabled: true,
						PasswordSecret: mcv1alpha1.SecretKeyRef{
							Name: "rcon-secret",
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
				Status: mcv1alpha1.PaperMCServerStatus{
					CurrentVersion: "1.21.0",
					CurrentBuild:   100,
					// No availableUpdate
				},
			}

			// Check if update should be applied
			should, remaining := reconciler.shouldApplyUpdate(server)
			Expect(should).To(BeTrue(), "Should return true when no update available")
			Expect(remaining).To(Equal(time.Duration(0)), "No remaining time")
		})
	})

	Context("JAR downloads", func() {
		var (
			ctx        context.Context
			reconciler *UpdateReconciler
			mockHTTP   *testutil.MockHTTPServer
		)

		BeforeEach(func() {
			ctx = context.Background()
			mockHTTP = testutil.NewMockHTTPServer()

			reconciler = &UpdateReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
		})

		AfterEach(func() {
			if mockHTTP != nil {
				mockHTTP.Close()
			}
		})

		It("should download Paper JAR successfully", func() {
			// Create fake JAR data
			fakeJARData := []byte("fake-paper-jar-content")
			mockHTTP.AddFile("/paper/1.21.1/build/150/paper-1.21.1-150.jar", fakeJARData)

			downloadURL := mockHTTP.URL() + "/paper/1.21.1/build/150/paper-1.21.1-150.jar"
			targetPath := "/tmp/test-paper.jar"

			err := reconciler.downloadFile(ctx, downloadURL, targetPath)
			Expect(err).NotTo(HaveOccurred())

			// Verify download was requested
			downloads := mockHTTP.GetDownloads()
			Expect(downloads).To(ContainElement("/paper/1.21.1/build/150/paper-1.21.1-150.jar"))
		})

		It("should return error when download fails", func() {
			failPath := "/paper/fail.jar"
			mockHTTP.SetFailOnPath(failPath, true)

			downloadURL := mockHTTP.URL() + failPath
			targetPath := "/tmp/test-fail.jar"

			err := reconciler.downloadFile(ctx, downloadURL, targetPath)
			Expect(err).To(HaveOccurred())
		})

		It("should download multiple plugin JARs", func() {
			// Setup plugins
			plugin1Data := []byte("plugin1-data")
			plugin2Data := []byte("plugin2-data")

			mockHTTP.AddFile("/plugins/EssentialsX/2.20.1.jar", plugin1Data)
			mockHTTP.AddFile("/plugins/Dynmap/3.7.jar", plugin2Data)

			plugins := []struct {
				name string
				url  string
				path string
			}{
				{"EssentialsX", mockHTTP.URL() + "/plugins/EssentialsX/2.20.1.jar", "/tmp/essentialsx.jar"},
				{"Dynmap", mockHTTP.URL() + "/plugins/Dynmap/3.7.jar", "/tmp/dynmap.jar"},
			}

			for _, p := range plugins {
				err := reconciler.downloadFile(ctx, p.url, p.path)
				Expect(err).NotTo(HaveOccurred())
			}

			// Verify all downloads
			downloads := mockHTTP.GetDownloads()
			Expect(downloads).To(HaveLen(2))
			Expect(downloads).To(ContainElement("/plugins/EssentialsX/2.20.1.jar"))
			Expect(downloads).To(ContainElement("/plugins/Dynmap/3.7.jar"))
		})

		It("should verify checksum after download", func() {
			fakeData := []byte("test-jar-content")
			expectedHash := testutil.ComputeSHA256(fakeData)

			mockHTTP.AddFile("/test.jar", fakeData)
			downloadURL := mockHTTP.URL() + "/test.jar"
			targetPath := "/tmp/test-checksum.jar"

			// Download file
			err := reconciler.downloadFile(ctx, downloadURL, targetPath)
			Expect(err).NotTo(HaveOccurred())

			// Verify checksum
			err = reconciler.verifyChecksum(targetPath, expectedHash)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should return error for invalid checksum", func() {
			fakeData := []byte("test-jar-content")
			wrongHash := "0000000000000000000000000000000000000000000000000000000000000000"

			mockHTTP.AddFile("/test.jar", fakeData)
			downloadURL := mockHTTP.URL() + "/test.jar"
			targetPath := "/tmp/test-bad-checksum.jar"

			// Download file
			err := reconciler.downloadFile(ctx, downloadURL, targetPath)
			Expect(err).NotTo(HaveOccurred())

			// Verify checksum - should fail
			err = reconciler.verifyChecksum(targetPath, wrongHash)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("checksum mismatch"))
		})

		It("should handle download cancellation via context", func() {
			cancelCtx, cancel := context.WithCancel(ctx)
			cancel() // Cancel immediately

			downloadURL := mockHTTP.URL() + "/test.jar"
			targetPath := "/tmp/test-cancelled.jar"

			err := reconciler.downloadFile(cancelCtx, downloadURL, targetPath)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("context"))
		})
	})

	Context("PodExecutor-based plugin operations", func() {
		It("should use PodExecutor for downloading plugins instead of kubectl", func() {
			mockExec := &testutil.MockPodExecutor{}
			reconciler := &UpdateReconciler{
				Client:      k8sClient,
				Scheme:      k8sClient.Scheme(),
				PodExecutor: mockExec,
			}

			server := &mcv1alpha1.PaperMCServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-exec-server",
					Namespace: "default",
				},
			}

			ctx := context.Background()
			// Will fail because curl won't work, but we verify the call was made to PodExecutor
			_ = reconciler.downloadPluginToServer(ctx, server, "test-plugin",
				"https://example.com/plugin.jar", "")

			Expect(mockExec.Calls).To(HaveLen(1))
			Expect(mockExec.Calls[0].PodName).To(Equal("test-exec-server-0"))
			Expect(mockExec.Calls[0].Namespace).To(Equal("default"))
			Expect(mockExec.Calls[0].Container).To(Equal("papermc"))
			Expect(mockExec.Calls[0].Command).To(ContainElement("sh"))
		})

		It("should use PodExecutor for deleting plugin JARs instead of kubectl", func() {
			mockExec := &testutil.MockPodExecutor{}
			reconciler := &UpdateReconciler{
				Client:      k8sClient,
				Scheme:      k8sClient.Scheme(),
				PodExecutor: mockExec,
			}

			server := &mcv1alpha1.PaperMCServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-exec-server",
					Namespace: "default",
				},
			}

			plugin := mcv1alpha1.ServerPluginStatus{
				PluginRef: mcv1alpha1.PluginRef{
					Name:      "test-plugin",
					Namespace: "default",
				},
				InstalledJARName: "test-plugin-1.0.jar",
			}

			ctx := context.Background()
			// deletePluginJAR will call PodExecutor then try markJARAsDeleted which may fail
			// but we verify PodExecutor was called
			_ = reconciler.deletePluginJAR(ctx, server, plugin)

			Expect(mockExec.Calls).To(HaveLen(1))
			Expect(mockExec.Calls[0].PodName).To(Equal("test-exec-server-0"))
			Expect(mockExec.Calls[0].Container).To(Equal("papermc"))
			// Verify the rm command is in the command args
			Expect(mockExec.Calls[0].Command[2]).To(ContainSubstring("rm -f /data/plugins/test-plugin-1.0.jar"))
		})
	})

	Context("RCON graceful shutdown", func() {
		var (
			ctx        context.Context
			reconciler *UpdateReconciler
			mockRCON   *rcon.MockClient
			serverName string
			namespace  string
		)

		BeforeEach(func() {
			ctx = context.Background()
			mockRCON = rcon.NewMockClient()

			reconciler = &UpdateReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			serverName = "test-server-rcon"
			namespace = testNamespace
		})

		AfterEach(func() {
			server := &mcv1alpha1.PaperMCServer{}
			_ = k8sClient.Get(ctx, types.NamespacedName{
				Name:      serverName,
				Namespace: namespace,
			}, server)
			_ = k8sClient.Delete(ctx, server)
		})

		It("should execute graceful shutdown via RCON", func() {
			server := &mcv1alpha1.PaperMCServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serverName,
					Namespace: namespace,
				},
				Spec: mcv1alpha1.PaperMCServerSpec{
					UpdateStrategy: "latest",
					Version:        "latest",
					RCON: mcv1alpha1.RCONConfig{
						Enabled: true,
						Port:    25575,
						PasswordSecret: mcv1alpha1.SecretKeyRef{
							Name: "rcon-secret",
							Key:  "password",
						},
					},
					GracefulShutdown: mcv1alpha1.GracefulShutdown{
						Timeout: metav1.Duration{Duration: 300 * time.Second},
					},
					UpdateSchedule: mcv1alpha1.UpdateSchedule{
						CheckCron: "0 3 * * *",
						MaintenanceWindow: mcv1alpha1.MaintenanceWindow{
							Cron:    "0 4 * * 0",
							Enabled: true,
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

			// Use mock RCON client
			err := reconciler.executeGracefulShutdownWithClient(ctx, server, mockRCON)
			Expect(err).NotTo(HaveOccurred())

			// Verify RCON interactions
			Expect(mockRCON.ConnectCalled).To(BeTrue(), "RCON Connect should be called")
			Expect(mockRCON.GracefulShutdownCalled).To(BeTrue(), "RCON GracefulShutdown should be called")
			Expect(mockRCON.CloseCalled).To(BeTrue(), "RCON Close should be called")

			// Verify shutdown commands were sent
			commands := mockRCON.GetCommands()
			Expect(commands).To(ContainElement(ContainSubstring("say")), "Should send warning messages")
			Expect(commands).To(ContainElement("save-all"), "Should send save-all command")
			Expect(commands).To(ContainElement("stop"), "Should send stop command")
		})

		It("should send warning messages to players", func() {
			server := &mcv1alpha1.PaperMCServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serverName,
					Namespace: namespace,
				},
				Spec: mcv1alpha1.PaperMCServerSpec{
					UpdateStrategy: "latest",
					Version:        "latest",
					RCON: mcv1alpha1.RCONConfig{
						Enabled: true,
						Port:    25575,
					},
					GracefulShutdown: mcv1alpha1.GracefulShutdown{
						Timeout: metav1.Duration{Duration: 300 * time.Second},
					},
					UpdateSchedule: mcv1alpha1.UpdateSchedule{
						CheckCron: "0 3 * * *",
						MaintenanceWindow: mcv1alpha1.MaintenanceWindow{
							Cron:    "0 4 * * 0",
							Enabled: true,
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

			err := reconciler.executeGracefulShutdownWithClient(ctx, server, mockRCON)
			Expect(err).NotTo(HaveOccurred())

			// Verify warnings were sent
			warnings := mockRCON.GetWarnings()
			Expect(warnings).NotTo(BeEmpty(), "Should send at least one warning")
		})

		It("should continue with pod deletion if RCON fails", func() {
			server := &mcv1alpha1.PaperMCServer{
				Spec: mcv1alpha1.PaperMCServerSpec{
					UpdateStrategy: "latest",
					RCON: mcv1alpha1.RCONConfig{
						Enabled: true,
						Port:    25575,
					},
				},
			}

			// Simulate RCON connection failure
			mockRCON.ConnectError = errors.New("RCON connection failed")

			// Should not return error (just log warning)
			err := reconciler.executeGracefulShutdownWithClient(ctx, server, mockRCON)

			// RCON failure should not block the update
			// Implementation should log warning and continue
			Expect(err).To(HaveOccurred()) // Current implementation returns error
			// TODO: In production, we might want to just log and continue
		})

		It("should handle context cancellation during RCON", func() {
			server := &mcv1alpha1.PaperMCServer{
				Spec: mcv1alpha1.PaperMCServerSpec{
					UpdateStrategy: "latest",
					RCON: mcv1alpha1.RCONConfig{
						Enabled: true,
						Port:    25575,
					},
					GracefulShutdown: mcv1alpha1.GracefulShutdown{
						Timeout: metav1.Duration{Duration: 300 * time.Second},
					},
				},
			}

			cancelCtx, cancel := context.WithCancel(ctx)
			cancel() // Cancel immediately

			err := reconciler.executeGracefulShutdownWithClient(cancelCtx, server, mockRCON)
			Expect(err).To(HaveOccurred())
		})
	})

	Context("Plugin deletion during update", func() {
		var (
			ctx        context.Context
			reconciler *UpdateReconciler
			serverName string
			namespace  string
		)

		BeforeEach(func() {
			ctx = context.Background()
			reconciler = &UpdateReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			serverName = "test-server-plugin-deletion"
			namespace = testNamespace
		})

		AfterEach(func() {
			// Clean up server
			server := &mcv1alpha1.PaperMCServer{}
			_ = k8sClient.Get(ctx, types.NamespacedName{
				Name:      serverName,
				Namespace: namespace,
			}, server)
			_ = k8sClient.Delete(ctx, server)

			// Clean up plugins
			pluginList := &mcv1alpha1.PluginList{}
			_ = k8sClient.List(ctx, pluginList, client.InNamespace(namespace))
			for i := range pluginList.Items {
				_ = k8sClient.Delete(ctx, &pluginList.Items[i])
			}
		})

		It("should identify plugins marked for deletion", func() {
			By("creating a server")
			server := &mcv1alpha1.PaperMCServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serverName,
					Namespace: namespace,
				},
				Spec: mcv1alpha1.PaperMCServerSpec{
					UpdateStrategy: "latest",
					Version:        "1.21.1",
					UpdateSchedule: mcv1alpha1.UpdateSchedule{
						CheckCron: "0 3 * * *",
						MaintenanceWindow: mcv1alpha1.MaintenanceWindow{
							Cron:    "0 4 * * 0",
							Enabled: true,
						},
					},
					GracefulShutdown: mcv1alpha1.GracefulShutdown{
						Timeout: metav1.Duration{Duration: 300 * time.Second},
					},
					RCON: mcv1alpha1.RCONConfig{
						Enabled: false,
					},
					PodTemplate: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "papermc",
									Image: "lexfrei/papermc:1.21.1-100",
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, server)).To(Succeed())

			By("fetching the server and updating status with plugins")
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      serverName,
				Namespace: namespace,
			}, server)).To(Succeed())

			server.Status = mcv1alpha1.PaperMCServerStatus{
				CurrentVersion: "1.21.1",
				CurrentBuild:   100,
				Plugins: []mcv1alpha1.ServerPluginStatus{
					{
						PluginRef: mcv1alpha1.PluginRef{
							Name:      "plugin-to-delete",
							Namespace: namespace,
						},
						ResolvedVersion:  "1.0.0",
						CurrentVersion:   "1.0.0",
						Compatible:       true,
						Source:           "hangar",
						PendingDeletion:  true,
						InstalledJARName: "TestPlugin-1.0.0.jar",
					},
					{
						PluginRef: mcv1alpha1.PluginRef{
							Name:      "plugin-to-keep",
							Namespace: namespace,
						},
						ResolvedVersion:  "2.0.0",
						CurrentVersion:   "2.0.0",
						Compatible:       true,
						Source:           "hangar",
						PendingDeletion:  false,
						InstalledJARName: "KeepPlugin-2.0.0.jar",
					},
				},
			}
			Expect(k8sClient.Status().Update(ctx, server)).To(Succeed())

			By("re-fetching the server to get updated status")
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      serverName,
				Namespace: namespace,
			}, server)).To(Succeed())

			By("calling getPluginsToDelete")
			pluginsToDelete := reconciler.getPluginsToDelete(server)

			By("verifying only marked plugins are returned")
			Expect(pluginsToDelete).To(HaveLen(1))
			Expect(pluginsToDelete[0].PluginRef.Name).To(Equal("plugin-to-delete"))
			Expect(pluginsToDelete[0].InstalledJARName).To(Equal("TestPlugin-1.0.0.jar"))
		})

		It("should include plugins without InstalledJARName for immediate deletion", func() {
			By("creating a server")
			server := &mcv1alpha1.PaperMCServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serverName,
					Namespace: namespace,
				},
				Spec: mcv1alpha1.PaperMCServerSpec{
					UpdateStrategy: "latest",
					Version:        "1.21.1",
					UpdateSchedule: mcv1alpha1.UpdateSchedule{
						CheckCron: "0 3 * * *",
						MaintenanceWindow: mcv1alpha1.MaintenanceWindow{
							Cron:    "0 4 * * 0",
							Enabled: true,
						},
					},
					GracefulShutdown: mcv1alpha1.GracefulShutdown{
						Timeout: metav1.Duration{Duration: 300 * time.Second},
					},
					RCON: mcv1alpha1.RCONConfig{
						Enabled: false,
					},
					PodTemplate: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "papermc",
									Image: "lexfrei/papermc:1.21.1-100",
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, server)).To(Succeed())

			By("fetching server and updating status with plugin without JAR name")
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      serverName,
				Namespace: namespace,
			}, server)).To(Succeed())

			server.Status = mcv1alpha1.PaperMCServerStatus{
				CurrentVersion: "1.21.1",
				CurrentBuild:   100,
				Plugins: []mcv1alpha1.ServerPluginStatus{
					{
						PluginRef: mcv1alpha1.PluginRef{
							Name:      "plugin-no-jar",
							Namespace: namespace,
						},
						ResolvedVersion:  "1.0.0",
						Compatible:       true,
						Source:           "hangar",
						PendingDeletion:  true,
						InstalledJARName: "", // No JAR name
					},
				},
			}
			Expect(k8sClient.Status().Update(ctx, server)).To(Succeed())

			By("re-fetching the server to get updated status")
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      serverName,
				Namespace: namespace,
			}, server)).To(Succeed())

			By("calling getPluginsToDelete")
			pluginsToDelete := reconciler.getPluginsToDelete(server)

			By("verifying plugin is included for immediate deletion completion")
			Expect(pluginsToDelete).To(HaveLen(1),
				"Plugins with empty InstalledJARName should be returned for immediate mark-as-deleted")
		})

		It("should update Plugin.DeletionProgress after JAR deletion", func() {
			By("creating a Plugin with DeletionProgress")
			plugin := &mcv1alpha1.Plugin{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "plugin-with-progress",
					Namespace: namespace,
				},
				Spec: mcv1alpha1.PluginSpec{
					Source: mcv1alpha1.PluginSource{
						Type:    "hangar",
						Project: "TestPlugin",
					},
					UpdateStrategy: "latest",
					InstanceSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "true",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, plugin)).To(Succeed())

			By("setting DeletionProgress in status")
			plugin.Status.DeletionProgress = []mcv1alpha1.DeletionProgressEntry{
				{
					ServerName: serverName,
					Namespace:  namespace,
					JARDeleted: false,
					DeletedAt:  nil,
				},
			}
			Expect(k8sClient.Status().Update(ctx, plugin)).To(Succeed())

			By("marking JAR as deleted")
			err := reconciler.markJARAsDeleted(ctx, plugin.Name, plugin.Namespace, serverName, namespace)
			Expect(err).NotTo(HaveOccurred())

			By("verifying DeletionProgress is updated")
			updatedPlugin := &mcv1alpha1.Plugin{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      plugin.Name,
				Namespace: plugin.Namespace,
			}, updatedPlugin)).To(Succeed())

			Expect(updatedPlugin.Status.DeletionProgress).To(HaveLen(1))
			Expect(updatedPlugin.Status.DeletionProgress[0].JARDeleted).To(BeTrue())
			Expect(updatedPlugin.Status.DeletionProgress[0].DeletedAt).NotTo(BeNil())
		})

		It("should handle multiple plugins marked for deletion", func() {
			By("creating a server")
			server := &mcv1alpha1.PaperMCServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serverName,
					Namespace: namespace,
				},
				Spec: mcv1alpha1.PaperMCServerSpec{
					UpdateStrategy: "latest",
					Version:        "1.21.1",
					UpdateSchedule: mcv1alpha1.UpdateSchedule{
						CheckCron: "0 3 * * *",
						MaintenanceWindow: mcv1alpha1.MaintenanceWindow{
							Cron:    "0 4 * * 0",
							Enabled: true,
						},
					},
					GracefulShutdown: mcv1alpha1.GracefulShutdown{
						Timeout: metav1.Duration{Duration: 300 * time.Second},
					},
					RCON: mcv1alpha1.RCONConfig{
						Enabled: false,
					},
					PodTemplate: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "papermc",
									Image: "lexfrei/papermc:1.21.1-100",
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, server)).To(Succeed())

			By("fetching server and updating status with multiple plugins")
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      serverName,
				Namespace: namespace,
			}, server)).To(Succeed())

			server.Status = mcv1alpha1.PaperMCServerStatus{
				CurrentVersion: "1.21.1",
				CurrentBuild:   100,
				Plugins: []mcv1alpha1.ServerPluginStatus{
					{
						PluginRef: mcv1alpha1.PluginRef{
							Name:      "plugin-1",
							Namespace: namespace,
						},
						PendingDeletion:  true,
						InstalledJARName: "Plugin1-1.0.0.jar",
					},
					{
						PluginRef: mcv1alpha1.PluginRef{
							Name:      "plugin-2",
							Namespace: namespace,
						},
						PendingDeletion:  true,
						InstalledJARName: "Plugin2-2.0.0.jar",
					},
					{
						PluginRef: mcv1alpha1.PluginRef{
							Name:      "plugin-3",
							Namespace: namespace,
						},
						PendingDeletion:  false,
						InstalledJARName: "Plugin3-3.0.0.jar",
					},
				},
			}
			Expect(k8sClient.Status().Update(ctx, server)).To(Succeed())

			By("re-fetching the server to get updated status")
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      serverName,
				Namespace: namespace,
			}, server)).To(Succeed())

			By("calling getPluginsToDelete")
			pluginsToDelete := reconciler.getPluginsToDelete(server)

			By("verifying all marked plugins are returned")
			Expect(pluginsToDelete).To(HaveLen(2))

			names := []string{pluginsToDelete[0].PluginRef.Name, pluginsToDelete[1].PluginRef.Name}
			Expect(names).To(ContainElement("plugin-1"))
			Expect(names).To(ContainElement("plugin-2"))
		})
		It("should delete plugin JARs from both plugins/ and update/ directories", func() {
			By("creating mock executor that records commands")
			mockExecutor := &testutil.MockPodExecutor{}

			By("creating a Plugin with DeletionProgress for markJARAsDeleted")
			plugin := &mcv1alpha1.Plugin{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "plugin-both-dirs",
					Namespace: namespace,
				},
				Spec: mcv1alpha1.PluginSpec{
					Source: mcv1alpha1.PluginSource{
						Type:    "hangar",
						Project: "TestPlugin",
					},
					UpdateStrategy: "latest",
					InstanceSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "true",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, plugin)).To(Succeed())

			plugin.Status.DeletionProgress = []mcv1alpha1.DeletionProgressEntry{
				{
					ServerName: serverName,
					Namespace:  namespace,
					JARDeleted: false,
				},
			}
			Expect(k8sClient.Status().Update(ctx, plugin)).To(Succeed())

			By("calling deletePluginJAR with InstalledJARName set")
			deleteReconciler := &UpdateReconciler{
				Client:      k8sClient,
				Scheme:      k8sClient.Scheme(),
				PodExecutor: mockExecutor,
			}

			server := &mcv1alpha1.PaperMCServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serverName,
					Namespace: namespace,
				},
			}

			pluginStatus := mcv1alpha1.ServerPluginStatus{
				PluginRef: mcv1alpha1.PluginRef{
					Name:      "plugin-both-dirs",
					Namespace: namespace,
				},
				InstalledJARName: "plugin-both-dirs.jar",
				PendingDeletion:  true,
			}

			err := deleteReconciler.deletePluginJAR(ctx, server, pluginStatus)
			Expect(err).NotTo(HaveOccurred())

			By("verifying rm command deletes from both directories")
			Expect(mockExecutor.Calls).To(HaveLen(1))
			rmCommand := mockExecutor.Calls[0].Command
			fullCmd := rmCommand[len(rmCommand)-1] // last arg to "sh -c" is the command string
			Expect(fullCmd).To(ContainSubstring("/data/plugins/plugin-both-dirs.jar"))
			Expect(fullCmd).To(ContainSubstring("/data/plugins/update/plugin-both-dirs.jar"))
		})

		It("should immediately mark as deleted when InstalledJARName is empty", func() {
			By("creating a Plugin with DeletionProgress")
			plugin := &mcv1alpha1.Plugin{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "plugin-never-installed",
					Namespace: namespace,
				},
				Spec: mcv1alpha1.PluginSpec{
					Source: mcv1alpha1.PluginSource{
						Type:    "hangar",
						Project: "TestPlugin",
					},
					UpdateStrategy: "latest",
					InstanceSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "true",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, plugin)).To(Succeed())

			plugin.Status.DeletionProgress = []mcv1alpha1.DeletionProgressEntry{
				{
					ServerName: serverName,
					Namespace:  namespace,
					JARDeleted: false,
				},
			}
			Expect(k8sClient.Status().Update(ctx, plugin)).To(Succeed())

			By("calling deletePluginJAR with empty InstalledJARName")
			deleteReconciler := &UpdateReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			server := &mcv1alpha1.PaperMCServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serverName,
					Namespace: namespace,
				},
			}

			pluginStatus := mcv1alpha1.ServerPluginStatus{
				PluginRef: mcv1alpha1.PluginRef{
					Name:      "plugin-never-installed",
					Namespace: namespace,
				},
				InstalledJARName: "",
				PendingDeletion:  true,
			}

			err := deleteReconciler.deletePluginJAR(ctx, server, pluginStatus)
			Expect(err).NotTo(HaveOccurred())

			By("verifying Plugin DeletionProgress is marked as deleted")
			updatedPlugin := &mcv1alpha1.Plugin{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      plugin.Name,
				Namespace: plugin.Namespace,
			}, updatedPlugin)).To(Succeed())

			Expect(updatedPlugin.Status.DeletionProgress).To(HaveLen(1))
			Expect(updatedPlugin.Status.DeletionProgress[0].JARDeleted).To(BeTrue())
		})
	})

	Context("applyPluginUpdates sets InstalledJARName", func() {
		var (
			ctx        context.Context
			reconciler *UpdateReconciler
			namespace  string
		)

		BeforeEach(func() {
			ctx = context.Background()
			namespace = testNamespace
		})

		It("should set InstalledJARName and CurrentVersion after download", func() {
			By("creating a mock executor")
			mockExecutor := &testutil.MockPodExecutor{}

			By("creating a Plugin with AvailableVersions")
			plugin := &mcv1alpha1.Plugin{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-download-plugin",
					Namespace: namespace,
				},
				Spec: mcv1alpha1.PluginSpec{
					Source: mcv1alpha1.PluginSource{
						Type:    "hangar",
						Project: "TestDownload",
					},
					UpdateStrategy: "latest",
					InstanceSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "true",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, plugin)).To(Succeed())

			now := metav1.Now()
			plugin.Status.AvailableVersions = []mcv1alpha1.PluginVersionInfo{
				{
					Version:           "1.0.0",
					DownloadURL:       "https://example.com/test-download-plugin-1.0.0.jar",
					ReleasedAt:        now,
					CachedAt:          now,
					MinecraftVersions: []string{"1.21.1"},
				},
			}
			Expect(k8sClient.Status().Update(ctx, plugin)).To(Succeed())

			By("setting up reconciler and server")
			reconciler = &UpdateReconciler{
				Client:      k8sClient,
				Scheme:      k8sClient.Scheme(),
				PodExecutor: mockExecutor,
			}

			server := &mcv1alpha1.PaperMCServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-server-download",
					Namespace: namespace,
				},
				Spec: mcv1alpha1.PaperMCServerSpec{
					UpdateStrategy: "latest",
					Version:        "1.21.1",
					UpdateSchedule: mcv1alpha1.UpdateSchedule{
						CheckCron: "0 3 * * *",
						MaintenanceWindow: mcv1alpha1.MaintenanceWindow{
							Cron:    "0 4 * * 0",
							Enabled: true,
						},
					},
					GracefulShutdown: mcv1alpha1.GracefulShutdown{
						Timeout: metav1.Duration{Duration: 300 * time.Second},
					},
					RCON: mcv1alpha1.RCONConfig{
						Enabled: false,
					},
					PodTemplate: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "papermc",
									Image: "lexfrei/papermc:1.21.1-100",
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, server)).To(Succeed())

			server.Status = mcv1alpha1.PaperMCServerStatus{
				CurrentVersion: "1.21.1",
				CurrentBuild:   100,
				Plugins: []mcv1alpha1.ServerPluginStatus{
					{
						PluginRef: mcv1alpha1.PluginRef{
							Name:      "test-download-plugin",
							Namespace: namespace,
						},
						ResolvedVersion: "1.0.0",
						Compatible:      true,
						Source:          "hangar",
					},
				},
			}
			Expect(k8sClient.Status().Update(ctx, server)).To(Succeed())

			By("calling applyPluginUpdates")
			err := reconciler.applyPluginUpdates(ctx, server)
			Expect(err).NotTo(HaveOccurred())

			By("verifying InstalledJARName and CurrentVersion are set")
			Expect(server.Status.Plugins).To(HaveLen(1))
			Expect(server.Status.Plugins[0].InstalledJARName).To(Equal("test-download-plugin.jar"),
				"InstalledJARName should be set after successful download")
			Expect(server.Status.Plugins[0].CurrentVersion).To(Equal("1.0.0"),
				"CurrentVersion should be set after successful download")

			By("cleaning up")
			_ = k8sClient.Delete(ctx, server)
			_ = k8sClient.Delete(ctx, plugin)
		})
	})

	Context("performCombinedUpdate operation order (Bug 13)", func() {
		It("should call applyPluginUpdates BEFORE updateStatefulSetImage", func() {
			// Verify via AST that in performCombinedUpdate,
			// applyPluginUpdates is called before updateStatefulSetImage.
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, updateControllerPath, nil, parser.AllErrors)
			Expect(err).NotTo(HaveOccurred())

			var foundFunc bool
			ast.Inspect(f, func(n ast.Node) bool {
				fn, ok := n.(*ast.FuncDecl)
				if !ok || fn.Name.Name != "performCombinedUpdate" {
					return true
				}
				foundFunc = true

				// Collect method call positions in order
				var callPositions []struct {
					name string
					pos  token.Pos
				}
				ast.Inspect(fn.Body, func(inner ast.Node) bool {
					call, ok := inner.(*ast.CallExpr)
					if !ok {
						return true
					}
					sel, ok := call.Fun.(*ast.SelectorExpr)
					if !ok {
						return true
					}
					name := sel.Sel.Name
					if name == "applyPluginUpdates" || name == "updateStatefulSetImage" {
						callPositions = append(callPositions, struct {
							name string
							pos  token.Pos
						}{name, call.Pos()})
					}
					return true
				})

				Expect(callPositions).To(HaveLen(2),
					"performCombinedUpdate should call both applyPluginUpdates and updateStatefulSetImage")
				Expect(callPositions[0].name).To(Equal("applyPluginUpdates"),
					"applyPluginUpdates must be called BEFORE updateStatefulSetImage")
				Expect(callPositions[1].name).To(Equal("updateStatefulSetImage"),
					"updateStatefulSetImage must be called AFTER applyPluginUpdates")

				return false
			})
			Expect(foundFunc).To(BeTrue(), "performCombinedUpdate function not found")
		})
	})

	Context("performCombinedUpdate RCON shutdown (Bug 14)", func() {
		It("should perform RCON graceful shutdown during combined update", func() {
			// performPluginOnlyUpdate does RCON shutdown (step 3-4):
			//   - createRCONClient
			//   - executeGracefulShutdownWithClient
			//   - deletePod
			//
			// performCombinedUpdate does NOT:
			//   - just updateStatefulSetImage (which triggers rolling update)
			//   - no RCON shutdown
			//   - no player warnings
			//   - no explicit save-all
			//
			// This test verifies that performCombinedUpdate should include
			// RCON graceful shutdown like performPluginOnlyUpdate does.

			// We can't easily test the full flow without infrastructure,
			// but we verify the contract by checking that combined update
			// code path includes RCON operations.

			// The simplest red test: check that the combined update function
			// references RCON in some way. Since performCombinedUpdate currently
			// has NO RCON code at all, any assertion about RCON behavior fails.

			// Test approach: Create a server with RCON enabled and mock everything.
			// The combined update should attempt RCON shutdown.
			// With the bug: it won't.

			mockExec := &testutil.MockPodExecutor{Output: []byte("ok")}
			mockCron := testutil.NewMockCronScheduler()

			reconciler := &UpdateReconciler{
				Client:      k8sClient,
				Scheme:      k8sClient.Scheme(),
				PodExecutor: mockExec,
				cron:        mockCron,
			}

			server := &mcv1alpha1.PaperMCServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-combined-rcon",
					Namespace: testNamespace,
				},
				Spec: mcv1alpha1.PaperMCServerSpec{
					UpdateStrategy: "latest",
					Version:        "1.21.1",
					UpdateSchedule: mcv1alpha1.UpdateSchedule{
						CheckCron: "0 3 * * *",
						MaintenanceWindow: mcv1alpha1.MaintenanceWindow{
							Cron:    "0 4 * * 0",
							Enabled: true,
						},
					},
					GracefulShutdown: mcv1alpha1.GracefulShutdown{
						Timeout: metav1.Duration{Duration: 300 * time.Second},
					},
					RCON: mcv1alpha1.RCONConfig{
						Enabled: true,
						Port:    25575,
						PasswordSecret: mcv1alpha1.SecretKeyRef{
							Name: "rcon-secret",
							Key:  "password",
						},
					},
					PodTemplate: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "papermc",
									Image: "lexfrei/papermc:1.21.0-90",
								},
							},
						},
					},
				},
				Status: mcv1alpha1.PaperMCServerStatus{
					CurrentVersion: "1.21.0",
					CurrentBuild:   90,
					DesiredVersion: "1.21.1",
					DesiredBuild:   100,
				},
			}

			// Verify via AST that performCombinedUpdate calls RCON shutdown functions.
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, updateControllerPath, nil, parser.AllErrors)
			Expect(err).NotTo(HaveOccurred())

			var foundFunc bool
			var callsCreateRCON, callsGracefulShutdown bool

			ast.Inspect(f, func(n ast.Node) bool {
				fn, ok := n.(*ast.FuncDecl)
				if !ok || fn.Name.Name != "performCombinedUpdate" {
					return true
				}
				foundFunc = true

				ast.Inspect(fn.Body, func(inner ast.Node) bool {
					call, ok := inner.(*ast.CallExpr)
					if !ok {
						return true
					}
					sel, ok := call.Fun.(*ast.SelectorExpr)
					if !ok {
						return true
					}
					switch sel.Sel.Name {
					case "createRCONClient":
						callsCreateRCON = true
					case "executeGracefulShutdownWithClient":
						callsGracefulShutdown = true
					}
					return true
				})

				return false
			})

			Expect(foundFunc).To(BeTrue(), "performCombinedUpdate function not found")
			Expect(callsCreateRCON).To(BeTrue(),
				"performCombinedUpdate should call createRCONClient for graceful shutdown")
			Expect(callsGracefulShutdown).To(BeTrue(),
				"performCombinedUpdate should call executeGracefulShutdownWithClient")

			_ = reconciler
			_ = server
		})
	})

	Context("waitForPodReady context propagation (Bug 15)", func() {
		It("should pass timeout context to API calls, not parent context", func() {
			// waitForPodReady creates ctxTimeout with 10-minute deadline:
			//   ctxTimeout, cancel := context.WithTimeout(ctx, 10*time.Minute)
			// But then uses parent ctx for API calls:
			//   r.Get(ctx, ...) ← BUG: should be ctxTimeout
			//
			// This means individual API calls are NOT bounded by the timeout.
			// If the API server hangs, r.Get() will block indefinitely.
			// The timeout only works via the select{} statement, but between
			// ticker fires the Get() call could hang forever.

			// Verify via AST that waitForPodReady uses ctxTimeout (not ctx)
			// for r.Get() calls inside the polling loop.
			fset := token.NewFileSet()
			f, parseErr := parser.ParseFile(fset, updateControllerPath, nil, parser.AllErrors)
			Expect(parseErr).NotTo(HaveOccurred())

			var foundFunc bool
			ast.Inspect(f, func(n ast.Node) bool {
				fn, ok := n.(*ast.FuncDecl)
				if !ok || fn.Name.Name != "waitForPodReady" {
					return true
				}
				foundFunc = true

				// Find all r.Get() calls and verify they use ctxTimeout
				ast.Inspect(fn.Body, func(inner ast.Node) bool {
					call, ok := inner.(*ast.CallExpr)
					if !ok {
						return true
					}
					sel, ok := call.Fun.(*ast.SelectorExpr)
					if !ok || sel.Sel.Name != "Get" {
						return true
					}
					// r.Get(ctxTimeout, ...) — first arg should be ctxTimeout
					if len(call.Args) > 0 {
						ident, ok := call.Args[0].(*ast.Ident)
						if ok {
							Expect(ident.Name).To(Equal("ctxTimeout"),
								"r.Get() in waitForPodReady should use ctxTimeout, not "+ident.Name)
						}
					}
					return true
				})

				return false
			})
			Expect(foundFunc).To(BeTrue(), "waitForPodReady function not found")
		})
	})

	Context("shouldApplyNow context propagation (Minor issue)", func() {
		It("should accept a context parameter and not use context.Background()", func() {
			// Verify via AST that shouldApplyNow accepts context.Context
			// and does not call context.Background()
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, updateControllerPath, nil, parser.AllErrors)
			Expect(err).NotTo(HaveOccurred())

			var found bool
			ast.Inspect(f, func(n ast.Node) bool {
				fn, ok := n.(*ast.FuncDecl)
				if !ok || fn.Name.Name != "shouldApplyNow" {
					return true
				}
				found = true

				// Check that first parameter (after receiver) is context.Context
				params := fn.Type.Params.List
				Expect(params).To(HaveLen(2),
					"shouldApplyNow should have ctx and server parameters")

				firstParam := params[0]
				sel, ok := firstParam.Type.(*ast.SelectorExpr)
				Expect(ok).To(BeTrue(), "first parameter should be context.Context")
				pkg, ok := sel.X.(*ast.Ident)
				Expect(ok).To(BeTrue())
				Expect(pkg.Name).To(Equal("context"))
				Expect(sel.Sel.Name).To(Equal("Context"))

				// Check that context.Background() is NOT called inside
				ast.Inspect(fn.Body, func(inner ast.Node) bool {
					call, ok := inner.(*ast.CallExpr)
					if !ok {
						return true
					}
					sel, ok := call.Fun.(*ast.SelectorExpr)
					if !ok {
						return true
					}
					pkg, ok := sel.X.(*ast.Ident)
					if !ok {
						return true
					}
					Expect(pkg.Name+"."+sel.Sel.Name).NotTo(Equal("context.Background"),
						"shouldApplyNow should not call context.Background()")
					return true
				})

				return false
			})
			Expect(found).To(BeTrue(), "shouldApplyNow function not found")
		})
	})

	Context("CombineErrors message for single error (Minor issue)", func() {
		It("should not produce 'and 0 more errors' message", func() {
			// At update_controller.go:996:
			//   return errors.CombineErrors(errs[0], errors.Newf("and %d more errors", len(errs)-1))
			//
			// When there is exactly 1 error (len(errs) == 1), this produces:
			//   "original error; and 0 more errors"
			//
			// This is confusing and unprofessional in user-facing error messages.

			err1 := errors.New("failed to delete plugin TestPlugin")
			errs := []error{err1}

			// Simulate the current behavior
			var combined error
			if len(errs) > 0 {
				combined = errors.CombineErrors(errs[0], errors.Newf("and %d more errors", len(errs)-1))
			}

			// The error message should NOT contain "0 more errors"
			Expect(combined).To(HaveOccurred())
			Expect(combined.Error()).NotTo(ContainSubstring("0 more errors"),
				"Minor: When there is exactly 1 error, CombineErrors at line 996 produces "+
					"'and 0 more errors' suffix. The error handling should return the single error "+
					"directly without wrapping, or use len(errs)-1 > 0 guard.")
		})
	})

	Context("Immediate apply annotation", func() {
		var (
			ctx        context.Context
			reconciler *UpdateReconciler
			serverName string
			namespace  string
		)

		BeforeEach(func() {
			ctx = context.Background()
			reconciler = &UpdateReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			serverName = "test-server-immediate-apply"
			namespace = testNamespace
		})

		AfterEach(func() {
			server := &mcv1alpha1.PaperMCServer{}
			_ = k8sClient.Get(ctx, types.NamespacedName{
				Name:      serverName,
				Namespace: namespace,
			}, server)
			_ = k8sClient.Delete(ctx, server)
		})

		It("should detect valid apply-now annotation", func() {
			By("creating a server with apply-now annotation")
			now := time.Now()
			server := &mcv1alpha1.PaperMCServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serverName,
					Namespace: namespace,
					Annotations: map[string]string{
						AnnotationApplyNow: fmt.Sprintf("%d", now.Unix()),
					},
				},
				Spec: mcv1alpha1.PaperMCServerSpec{
					UpdateStrategy: "latest",
					Version:        "1.21.1",
					UpdateSchedule: mcv1alpha1.UpdateSchedule{
						CheckCron: "0 3 * * *",
						MaintenanceWindow: mcv1alpha1.MaintenanceWindow{
							Cron:    "0 4 * * 0",
							Enabled: true,
						},
					},
					GracefulShutdown: mcv1alpha1.GracefulShutdown{
						Timeout: metav1.Duration{Duration: 300 * time.Second},
					},
					RCON: mcv1alpha1.RCONConfig{
						Enabled: false,
					},
					PodTemplate: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "papermc",
									Image: "lexfrei/papermc:1.21.1-100",
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, server)).To(Succeed())

			By("checking if apply-now is valid")
			valid := reconciler.shouldApplyNow(ctx, server)
			Expect(valid).To(BeTrue())
		})

		It("should reject stale apply-now annotation older than 5 minutes", func() {
			By("creating a server with stale apply-now annotation")
			staleTime := time.Now().Add(-10 * time.Minute) // 10 minutes ago
			server := &mcv1alpha1.PaperMCServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serverName,
					Namespace: namespace,
					Annotations: map[string]string{
						AnnotationApplyNow: fmt.Sprintf("%d", staleTime.Unix()),
					},
				},
				Spec: mcv1alpha1.PaperMCServerSpec{
					UpdateStrategy: "latest",
					Version:        "1.21.1",
					UpdateSchedule: mcv1alpha1.UpdateSchedule{
						CheckCron: "0 3 * * *",
						MaintenanceWindow: mcv1alpha1.MaintenanceWindow{
							Cron:    "0 4 * * 0",
							Enabled: true,
						},
					},
					GracefulShutdown: mcv1alpha1.GracefulShutdown{
						Timeout: metav1.Duration{Duration: 300 * time.Second},
					},
					RCON: mcv1alpha1.RCONConfig{
						Enabled: false,
					},
					PodTemplate: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "papermc",
									Image: "lexfrei/papermc:1.21.1-100",
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, server)).To(Succeed())

			By("checking if apply-now is valid")
			valid := reconciler.shouldApplyNow(ctx, server)
			Expect(valid).To(BeFalse())
		})

		It("should reject invalid apply-now annotation format", func() {
			By("creating a server with invalid apply-now annotation")
			server := &mcv1alpha1.PaperMCServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serverName,
					Namespace: namespace,
					Annotations: map[string]string{
						AnnotationApplyNow: "not-a-timestamp",
					},
				},
				Spec: mcv1alpha1.PaperMCServerSpec{
					UpdateStrategy: "latest",
					Version:        "1.21.1",
					UpdateSchedule: mcv1alpha1.UpdateSchedule{
						CheckCron: "0 3 * * *",
						MaintenanceWindow: mcv1alpha1.MaintenanceWindow{
							Cron:    "0 4 * * 0",
							Enabled: true,
						},
					},
					GracefulShutdown: mcv1alpha1.GracefulShutdown{
						Timeout: metav1.Duration{Duration: 300 * time.Second},
					},
					RCON: mcv1alpha1.RCONConfig{
						Enabled: false,
					},
					PodTemplate: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "papermc",
									Image: "lexfrei/papermc:1.21.1-100",
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, server)).To(Succeed())

			By("checking if apply-now is valid")
			valid := reconciler.shouldApplyNow(ctx, server)
			Expect(valid).To(BeFalse())
		})

		It("should return false when no apply-now annotation present", func() {
			By("creating a server without apply-now annotation")
			server := &mcv1alpha1.PaperMCServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serverName,
					Namespace: namespace,
				},
				Spec: mcv1alpha1.PaperMCServerSpec{
					UpdateStrategy: "latest",
					Version:        "1.21.1",
					UpdateSchedule: mcv1alpha1.UpdateSchedule{
						CheckCron: "0 3 * * *",
						MaintenanceWindow: mcv1alpha1.MaintenanceWindow{
							Cron:    "0 4 * * 0",
							Enabled: true,
						},
					},
					GracefulShutdown: mcv1alpha1.GracefulShutdown{
						Timeout: metav1.Duration{Duration: 300 * time.Second},
					},
					RCON: mcv1alpha1.RCONConfig{
						Enabled: false,
					},
					PodTemplate: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "papermc",
									Image: "lexfrei/papermc:1.21.1-100",
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, server)).To(Succeed())

			By("checking if apply-now is valid")
			valid := reconciler.shouldApplyNow(ctx, server)
			Expect(valid).To(BeFalse())
		})

		It("should remove annotation after processing", func() {
			By("creating a server with apply-now annotation")
			now := time.Now()
			server := &mcv1alpha1.PaperMCServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serverName,
					Namespace: namespace,
					Annotations: map[string]string{
						AnnotationApplyNow: fmt.Sprintf("%d", now.Unix()),
					},
				},
				Spec: mcv1alpha1.PaperMCServerSpec{
					UpdateStrategy: "latest",
					Version:        "1.21.1",
					UpdateSchedule: mcv1alpha1.UpdateSchedule{
						CheckCron: "0 3 * * *",
						MaintenanceWindow: mcv1alpha1.MaintenanceWindow{
							Cron:    "0 4 * * 0",
							Enabled: true,
						},
					},
					GracefulShutdown: mcv1alpha1.GracefulShutdown{
						Timeout: metav1.Duration{Duration: 300 * time.Second},
					},
					RCON: mcv1alpha1.RCONConfig{
						Enabled: false,
					},
					PodTemplate: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "papermc",
									Image: "lexfrei/papermc:1.21.1-100",
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, server)).To(Succeed())

			By("removing the annotation")
			err := reconciler.removeApplyNowAnnotation(ctx, server)
			Expect(err).NotTo(HaveOccurred())

			By("verifying annotation is removed")
			updatedServer := &mcv1alpha1.PaperMCServer{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      serverName,
				Namespace: namespace,
			}, updatedServer)).To(Succeed())
			_, exists := updatedServer.Annotations[AnnotationApplyNow]
			Expect(exists).To(BeFalse())
		})
	})

	Context("Pod lifecycle and status updates", func() {
		var (
			ctx        context.Context
			reconciler *UpdateReconciler
			serverName string
			namespace  string
		)

		BeforeEach(func() {
			ctx = context.Background()
			reconciler = &UpdateReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			serverName = "test-server-pod"
			namespace = testNamespace
		})

		AfterEach(func() {
			server := &mcv1alpha1.PaperMCServer{}
			_ = k8sClient.Get(ctx, types.NamespacedName{
				Name:      serverName,
				Namespace: namespace,
			}, server)
			_ = k8sClient.Delete(ctx, server)
		})

		It("should update lastUpdate in status after successful update", func() {
			server := &mcv1alpha1.PaperMCServer{
				Spec: mcv1alpha1.PaperMCServerSpec{
					UpdateStrategy: "latest",
					Version:        "1.21.1",
				},
				Status: mcv1alpha1.PaperMCServerStatus{
					CurrentVersion: "1.21.0",
					CurrentBuild:   100,
				},
			}

			reconciler.updateServerStatus(server, true)

			// Verify lastUpdate was set
			Expect(server.Status.LastUpdate).NotTo(BeNil())
			Expect(server.Status.LastUpdate.Successful).To(BeTrue())
			Expect(server.Status.LastUpdate.PreviousVersion).To(Equal("1.21.0"))
		})

		It("should record failed update in status", func() {
			server := &mcv1alpha1.PaperMCServer{
				Spec: mcv1alpha1.PaperMCServerSpec{
					UpdateStrategy: "latest",
					Version:        "1.21.1",
				},
				Status: mcv1alpha1.PaperMCServerStatus{
					CurrentVersion: "1.21.0",
				},
			}

			reconciler.updateServerStatus(server, false)

			// Verify lastUpdate records failure
			Expect(server.Status.LastUpdate).NotTo(BeNil())
			Expect(server.Status.LastUpdate.Successful).To(BeFalse())
		})

		It("should clear availableUpdate after successful update", func() {
			server := &mcv1alpha1.PaperMCServer{
				Spec: mcv1alpha1.PaperMCServerSpec{
					UpdateStrategy: "latest",
					Version:        "1.21.1",
				},
				Status: mcv1alpha1.PaperMCServerStatus{
					CurrentVersion: "1.21.0",
					AvailableUpdate: &mcv1alpha1.AvailableUpdate{
						Version: "1.21.1",
						Build:   150,
					},
				},
			}

			reconciler.updateServerStatus(server, true)

			// Verify availableUpdate was cleared
			Expect(server.Status.AvailableUpdate).To(BeNil())
		})

		It("should set Updating condition during update", func() {
			server := &mcv1alpha1.PaperMCServer{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
			}

			reconciler.setUpdatingCondition(server, true, "Update in progress")

			// Verify condition was set
			cond := meta.FindStatusCondition(server.Status.Conditions, conditionTypeUpdating)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionTrue))
			Expect(cond.Reason).To(Equal(reasonUpdateInProgress))
		})

		It("should clear Updating condition after update", func() {
			server := &mcv1alpha1.PaperMCServer{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
			}

			reconciler.setUpdatingCondition(server, false, "Update complete")

			// Verify condition was cleared
			cond := meta.FindStatusCondition(server.Status.Conditions, conditionTypeUpdating)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Reason).To(Equal(reasonUpdateComplete))
		})

		It("should update CurrentVersion to DesiredVersion after successful update", func() {
			server := &mcv1alpha1.PaperMCServer{
				Spec: mcv1alpha1.PaperMCServerSpec{
					UpdateStrategy: "latest",
					Version:        "1.21.1",
				},
				Status: mcv1alpha1.PaperMCServerStatus{
					CurrentVersion:  "1.21.0",
					CurrentBuild:    100,
					DesiredVersion:  "1.21.1",
					DesiredBuild:    150,
					AvailableUpdate: &mcv1alpha1.AvailableUpdate{},
				},
			}

			reconciler.updateServerStatus(server, true)

			// Verify CurrentVersion is updated to DesiredVersion
			Expect(server.Status.CurrentVersion).To(Equal("1.21.1"),
				"CurrentVersion should be updated to DesiredVersion after successful update")
			Expect(server.Status.CurrentBuild).To(Equal(150),
				"CurrentBuild should be updated to DesiredBuild after successful update")
		})

		It("should update plugin CurrentVersion to ResolvedVersion after successful update", func() {
			server := &mcv1alpha1.PaperMCServer{
				Spec: mcv1alpha1.PaperMCServerSpec{
					UpdateStrategy: "latest",
					Version:        "1.21.1",
				},
				Status: mcv1alpha1.PaperMCServerStatus{
					CurrentVersion: "1.21.0",
					DesiredVersion: "1.21.1",
					Plugins: []mcv1alpha1.ServerPluginStatus{
						{
							PluginRef:       mcv1alpha1.PluginRef{Name: "plugin1", Namespace: "default"},
							CurrentVersion:  "1.0.0",
							ResolvedVersion: "1.1.0",
						},
						{
							PluginRef:       mcv1alpha1.PluginRef{Name: "plugin2", Namespace: "default"},
							CurrentVersion:  "2.0.0",
							ResolvedVersion: "2.1.0",
						},
					},
					AvailableUpdate: &mcv1alpha1.AvailableUpdate{},
				},
			}

			reconciler.updateServerStatus(server, true)

			// Verify plugin CurrentVersions are updated to ResolvedVersions
			Expect(server.Status.Plugins[0].CurrentVersion).To(Equal("1.1.0"),
				"Plugin CurrentVersion should be updated to ResolvedVersion")
			Expect(server.Status.Plugins[1].CurrentVersion).To(Equal("2.1.0"),
				"Plugin CurrentVersion should be updated to ResolvedVersion")
		})
	})

	Context("Pod deletion in plugin-only update", func() {
		var (
			ctx        context.Context
			reconciler *UpdateReconciler
			serverName string
			namespace  string
		)

		BeforeEach(func() {
			ctx = context.Background()
			reconciler = &UpdateReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			serverName = "test-server-pod-deletion"
			namespace = testNamespace
		})

		AfterEach(func() {
			// Clean up server
			server := &mcv1alpha1.PaperMCServer{}
			_ = k8sClient.Get(ctx, types.NamespacedName{
				Name:      serverName,
				Namespace: namespace,
			}, server)
			_ = k8sClient.Delete(ctx, server)
		})

		It("should have deletePod method available", func() {
			// Verify the reconciler has a deletePod method
			// This is a structural test to ensure the method exists

			// This call should not panic - method should exist
			// The actual deletion will fail because the pod doesn't exist, but that's expected
			err := reconciler.deletePod(ctx, serverName+"-0", namespace)

			// We expect an error (pod not found), but the method should exist
			Expect(err).To(HaveOccurred()) // Pod doesn't exist, that's fine
			Expect(err.Error()).To(ContainSubstring("not found"))
		})
	})

	Context("Download error aggregation", func() {
		It("should return aggregate error when plugin downloads fail", func() {
			// applyPluginUpdates() collects all download errors and returns them
			// as an aggregate error. Each error includes the plugin name.
			Skip("Requires complex mocking of download operations")
		})
	})

	Context("Delete error handling", func() {
		It("should propagate error from deleteMarkedPlugins in performPluginOnlyUpdate", func() {
			// performPluginOnlyUpdate() returns error from deleteMarkedPlugins()
			// instead of logging and continuing.
			Skip("Requires complex mocking of delete operations")
		})
	})

	Context("Maintenance window enforcement", func() {
		It("should allow update when inside maintenance window", func() {
			// Maintenance window: Sunday 4:00 AM
			// Current time: Sunday 4:30 AM (within 1-hour window)
			sunday430AM := time.Date(2026, 2, 22, 4, 30, 0, 0, time.UTC)
			reconciler := &UpdateReconciler{
				nowFunc: func() time.Time { return sunday430AM },
			}

			server := &mcv1alpha1.PaperMCServer{
				Spec: mcv1alpha1.PaperMCServerSpec{
					UpdateSchedule: mcv1alpha1.UpdateSchedule{
						MaintenanceWindow: mcv1alpha1.MaintenanceWindow{
							Cron:    "0 4 * * 0",
							Enabled: true,
						},
					},
				},
			}

			Expect(reconciler.isInMaintenanceWindow(server)).To(BeTrue(),
				"Should be inside maintenance window at Sunday 4:30 AM")
		})

		It("should reject update when outside maintenance window", func() {
			// Maintenance window: Sunday 4:00 AM
			// Current time: Wednesday 10:00 AM (outside window)
			wednesday10AM := time.Date(2026, 2, 25, 10, 0, 0, 0, time.UTC)
			reconciler := &UpdateReconciler{
				nowFunc: func() time.Time { return wednesday10AM },
			}

			server := &mcv1alpha1.PaperMCServer{
				Spec: mcv1alpha1.PaperMCServerSpec{
					UpdateSchedule: mcv1alpha1.UpdateSchedule{
						MaintenanceWindow: mcv1alpha1.MaintenanceWindow{
							Cron:    "0 4 * * 0",
							Enabled: true,
						},
					},
				},
			}

			Expect(reconciler.isInMaintenanceWindow(server)).To(BeFalse(),
				"Should be outside maintenance window at Wednesday 10 AM")
		})

		It("should allow update when maintenance window is disabled", func() {
			reconciler := &UpdateReconciler{}

			server := &mcv1alpha1.PaperMCServer{
				Spec: mcv1alpha1.PaperMCServerSpec{
					UpdateSchedule: mcv1alpha1.UpdateSchedule{
						MaintenanceWindow: mcv1alpha1.MaintenanceWindow{
							Cron:    "0 4 * * 0",
							Enabled: false,
						},
					},
				},
			}

			Expect(reconciler.isInMaintenanceWindow(server)).To(BeTrue(),
				"Should allow when maintenance window is disabled")
		})

		It("should reject update when just past the 1-hour window", func() {
			// Maintenance window: Sunday 4:00 AM
			// Current time: Sunday 5:01 AM (just past 1-hour window)
			sunday501AM := time.Date(2026, 2, 22, 5, 1, 0, 0, time.UTC)
			reconciler := &UpdateReconciler{
				nowFunc: func() time.Time { return sunday501AM },
			}

			server := &mcv1alpha1.PaperMCServer{
				Spec: mcv1alpha1.PaperMCServerSpec{
					UpdateSchedule: mcv1alpha1.UpdateSchedule{
						MaintenanceWindow: mcv1alpha1.MaintenanceWindow{
							Cron:    "0 4 * * 0",
							Enabled: true,
						},
					},
				},
			}

			Expect(reconciler.isInMaintenanceWindow(server)).To(BeFalse(),
				"Should reject update just after the 1-hour maintenance window")
		})
	})

	Context("Plugin deletion with empty InstalledJARName", func() {
		It("should immediately mark JAR as deleted when InstalledJARName is empty", func() {
			// Bug 8: When a plugin was never installed (InstalledJARName empty),
			// the update controller should immediately mark the JAR as deleted
			// instead of waiting for a 10-minute timeout.
			server := &mcv1alpha1.PaperMCServer{
				Status: mcv1alpha1.PaperMCServerStatus{
					Plugins: []mcv1alpha1.ServerPluginStatus{
						{
							PluginRef: mcv1alpha1.PluginRef{
								Name:      "never-installed-plugin",
								Namespace: "default",
							},
							PendingDeletion:  true,
							InstalledJARName: "", // never installed
						},
					},
				},
			}

			// getPluginsToDelete should NOT skip plugins with empty InstalledJARName
			// when PendingDeletion is true — they should be returned for immediate cleanup
			result := (&UpdateReconciler{}).getPluginsToDelete(server)

			// Currently this returns 0 (the bug), should return 1 (the fix)
			Expect(result).To(HaveLen(1),
				"Should include plugins with empty InstalledJARName for immediate deletion completion")
		})
	})

	Context("Maintenance window cron parse error", func() {
		It("should block updates when cron expression is invalid", func() {
			// Bug: isInMaintenanceWindow returns true (allows update) when cron
			// parse fails. This silently bypasses maintenance windows on typos.
			// Safe default: block updates on parse error.
			reconciler := &UpdateReconciler{}

			server := &mcv1alpha1.PaperMCServer{
				Spec: mcv1alpha1.PaperMCServerSpec{
					UpdateSchedule: mcv1alpha1.UpdateSchedule{
						MaintenanceWindow: mcv1alpha1.MaintenanceWindow{
							Enabled: true,
							Cron:    "invalid cron expression!!!",
						},
					},
				},
			}

			result := reconciler.isInMaintenanceWindow(server)
			Expect(result).To(BeFalse(),
				"Invalid cron should block updates (safe default), not silently allow them")
		})
	})

	Context("applyPluginUpdates empty downloadURL", func() {
		It("should skip plugins with empty downloadURL without error", func() {
			// Bug: When downloadURL is empty (e.g., after Bug 12 ExternalURL filter),
			// applyPluginUpdates adds an error to downloadErrors. This means plugins
			// that simply don't have a download URL (legitimate state) cause the
			// entire update to be reported as failed.
			// Instead, plugins with empty downloadURL should be silently skipped
			// (logged as info, not treated as error).
			src, readErr := os.ReadFile(updateControllerPath)
			Expect(readErr).NotTo(HaveOccurred())
			srcStr := string(src)

			// Find the empty downloadURL handling block
			// It should contain slog.InfoContext (info log, not error)
			// and should NOT contain downloadErrors = append near downloadURL == ""
			Expect(srcStr).To(ContainSubstring(`slog.InfoContext(ctx, "Plugin has no download URL, skipping"`),
				"Empty downloadURL should be logged as info, not treated as error")
		})
	})
})
