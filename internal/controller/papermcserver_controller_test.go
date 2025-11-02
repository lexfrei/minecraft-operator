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
						PaperVersion:   "latest",
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
			Expect(resource.Spec.PaperVersion).To(Equal("latest"))
			// TODO(user): Add integration tests with full reconciler setup including PaperClient and Solver.
			// Example: If you expect a certain status condition after reconciliation, verify it here.
		})
	})
})
