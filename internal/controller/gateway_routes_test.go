/*
Copyright 2026, Aleksei Sviridkin.

SPDX-License-Identifier: BSD-3-Clause
*/

package controller

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	mck8slexlav1beta1 "github.com/lexfrei/minecraft-operator/api/v1beta1"
	"github.com/lexfrei/minecraft-operator/pkg/paper"
	"github.com/lexfrei/minecraft-operator/pkg/solver"
	"github.com/lexfrei/minecraft-operator/pkg/testutil"
)

var _ = Describe("Gateway API Routes", func() {
	const (
		timeout  = 10 * time.Second
		interval = 250 * time.Millisecond
	)

	var (
		ns         string
		reconciler *PaperMCServerReconciler
	)

	BeforeEach(func() {
		// Create a unique namespace per test to avoid collisions
		ns = fmt.Sprintf("gw-test-%d", time.Now().UnixNano())
		nsObj := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: ns},
		}
		Expect(k8sClient.Create(ctx, nsObj)).To(Succeed())

		reconciler = &PaperMCServerReconciler{
			Client:         k8sClient,
			Scheme:         k8sClient.Scheme(),
			Solver:         solver.NewSimpleSolver(),
			RegistryClient: &testutil.MockRegistryAPI{Tags: []string{"1.21.1-91"}, ImageExist: true},
			PaperClient: &testutil.MockPaperAPI{
				Versions:     []string{"1.21.1"},
				BuildInfo:    &paper.BuildInfo{Version: "1.21.1", Build: 91},
				BuildNumbers: []int{91},
			},
		}
	})

	createServer := func(name string, gateway *mck8slexlav1beta1.GatewayConfig) *mck8slexlav1beta1.PaperMCServer {
		server := &mck8slexlav1beta1.PaperMCServer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: ns,
			},
			Spec: mck8slexlav1beta1.PaperMCServerSpec{
				UpdateStrategy: "pin",
				Version:        "1.21.1",
				UpdateSchedule: mck8slexlav1beta1.UpdateSchedule{
					CheckCron: "0 3 * * *",
					MaintenanceWindow: mck8slexlav1beta1.MaintenanceWindow{
						Cron:    "0 4 * * 0",
						Enabled: true,
					},
				},
				GracefulShutdown: mck8slexlav1beta1.GracefulShutdown{
					Timeout: metav1.Duration{Duration: 5 * time.Minute},
				},
				RCON: mck8slexlav1beta1.RCONConfig{
					Enabled:        false,
					PasswordSecret: mck8slexlav1beta1.SecretKeyRef{Name: "rcon-secret", Key: "password"},
				},
				Service: mck8slexlav1beta1.ServiceConfig{
					Type: corev1.ServiceTypeClusterIP,
				},
				Gateway: gateway,
				PodTemplate: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{Name: "papermc", Image: "docker.io/lexfrei/papermc:1.21.1-91"},
						},
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, server)).To(Succeed())

		// Re-read to get UID for owner references
		var created mck8slexlav1beta1.PaperMCServer
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: ns}, &created)).To(Succeed())

		return &created
	}

	Context("when gateway is enabled with TCPRoute", func() {
		It("should create a TCPRoute for game traffic", func() {
			server := createServer("gw-tcp-basic", &mck8slexlav1beta1.GatewayConfig{
				Enabled: true,
				ParentRefs: []mck8slexlav1beta1.GatewayParentRef{
					{Name: "game-gateway", Namespace: "gateway-system"},
				},
				TCPRoute: &mck8slexlav1beta1.RouteConfig{Enabled: true},
			})

			err := reconciler.ensureGatewayRoutes(ctx, server, nil)
			Expect(err).NotTo(HaveOccurred())

			// Verify TCPRoute was created
			var tcpRoute gatewayv1alpha2.TCPRoute
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      server.Name + "-tcp",
					Namespace: ns,
				}, &tcpRoute)
			}, timeout, interval).Should(Succeed())

			// Verify parentRefs
			Expect(tcpRoute.Spec.ParentRefs).To(HaveLen(1))
			gwNamespace := gatewayv1alpha2.Namespace("gateway-system")
			Expect(tcpRoute.Spec.ParentRefs[0].Namespace).To(Equal(&gwNamespace))

			// Verify backend references the server Service on port 25565
			Expect(tcpRoute.Spec.Rules).To(HaveLen(1))
			Expect(tcpRoute.Spec.Rules[0].BackendRefs).To(HaveLen(1))
			Expect(string(tcpRoute.Spec.Rules[0].BackendRefs[0].Name)).To(Equal(server.Name))

			port := gatewayv1alpha2.PortNumber(25565)
			Expect(tcpRoute.Spec.Rules[0].BackendRefs[0].Port).To(Equal(&port))
		})

		It("should include RCON port when RCON is enabled", func() {
			server := createServer("gw-tcp-rcon", &mck8slexlav1beta1.GatewayConfig{
				Enabled: true,
				ParentRefs: []mck8slexlav1beta1.GatewayParentRef{
					{Name: "game-gateway", Namespace: "gateway-system"},
				},
				TCPRoute: &mck8slexlav1beta1.RouteConfig{Enabled: true},
			})
			server.Spec.RCON = mck8slexlav1beta1.RCONConfig{
				Enabled:        true,
				PasswordSecret: mck8slexlav1beta1.SecretKeyRef{Name: "s", Key: "k"},
				Port:           25575,
			}

			err := reconciler.ensureGatewayRoutes(ctx, server, nil)
			Expect(err).NotTo(HaveOccurred())

			var tcpRoute gatewayv1alpha2.TCPRoute
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: server.Name + "-tcp", Namespace: ns,
			}, &tcpRoute)).To(Succeed())

			// Should have 2 rules: minecraft + RCON
			Expect(tcpRoute.Spec.Rules).To(HaveLen(2))

			mcPort := gatewayv1alpha2.PortNumber(25565)
			rconPort := gatewayv1alpha2.PortNumber(25575)
			Expect(tcpRoute.Spec.Rules[0].BackendRefs[0].Port).To(Equal(&mcPort))
			Expect(tcpRoute.Spec.Rules[1].BackendRefs[0].Port).To(Equal(&rconPort))
		})
	})

	Context("when gateway is enabled with UDPRoute", func() {
		It("should create a UDPRoute for game traffic", func() {
			server := createServer("gw-udp-basic", &mck8slexlav1beta1.GatewayConfig{
				Enabled: true,
				ParentRefs: []mck8slexlav1beta1.GatewayParentRef{
					{Name: "game-gateway", Namespace: "gateway-system"},
				},
				UDPRoute: &mck8slexlav1beta1.RouteConfig{Enabled: true},
			})

			err := reconciler.ensureGatewayRoutes(ctx, server, nil)
			Expect(err).NotTo(HaveOccurred())

			var udpRoute gatewayv1alpha2.UDPRoute
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: server.Name + "-udp", Namespace: ns,
			}, &udpRoute)).To(Succeed())

			Expect(udpRoute.Spec.Rules).To(HaveLen(1))
			port := gatewayv1alpha2.PortNumber(25565)
			Expect(udpRoute.Spec.Rules[0].BackendRefs[0].Port).To(Equal(&port))
		})
	})

	Context("when gateway is disabled", func() {
		It("should not create any routes", func() {
			server := createServer("gw-disabled", nil)

			err := reconciler.ensureGatewayRoutes(ctx, server, nil)
			Expect(err).NotTo(HaveOccurred())

			var tcpRouteList gatewayv1alpha2.TCPRouteList
			Expect(k8sClient.List(ctx, &tcpRouteList, client.InNamespace(ns))).To(Succeed())
			Expect(tcpRouteList.Items).To(BeEmpty())

			var udpRouteList gatewayv1alpha2.UDPRouteList
			Expect(k8sClient.List(ctx, &udpRouteList, client.InNamespace(ns))).To(Succeed())
			Expect(udpRouteList.Items).To(BeEmpty())
		})

		It("should clean up existing routes when gateway is disabled", func() {
			// First create routes with gateway enabled
			server := createServer("gw-cleanup", &mck8slexlav1beta1.GatewayConfig{
				Enabled: true,
				ParentRefs: []mck8slexlav1beta1.GatewayParentRef{
					{Name: "gw", Namespace: "gw-ns"},
				},
				TCPRoute: &mck8slexlav1beta1.RouteConfig{Enabled: true},
				UDPRoute: &mck8slexlav1beta1.RouteConfig{Enabled: true},
			})

			err := reconciler.ensureGatewayRoutes(ctx, server, nil)
			Expect(err).NotTo(HaveOccurred())

			// Verify routes exist
			var tcpRoute gatewayv1alpha2.TCPRoute
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: server.Name + "-tcp", Namespace: ns,
			}, &tcpRoute)).To(Succeed())

			// Now disable gateway
			server.Spec.Gateway = nil
			err = reconciler.ensureGatewayRoutes(ctx, server, nil)
			Expect(err).NotTo(HaveOccurred())

			// Routes should be deleted
			var tcpRouteList gatewayv1alpha2.TCPRouteList
			Expect(k8sClient.List(ctx, &tcpRouteList, client.InNamespace(ns))).To(Succeed())
			Expect(tcpRouteList.Items).To(BeEmpty())

			var udpRouteList gatewayv1alpha2.UDPRouteList
			Expect(k8sClient.List(ctx, &udpRouteList, client.InNamespace(ns))).To(Succeed())
			Expect(udpRouteList.Items).To(BeEmpty())
		})
	})

	Context("when gateway has multiple parentRefs", func() {
		It("should attach routes to all specified gateways", func() {
			server := createServer("gw-multi-parent", &mck8slexlav1beta1.GatewayConfig{
				Enabled: true,
				ParentRefs: []mck8slexlav1beta1.GatewayParentRef{
					{Name: "gw-1", Namespace: "ns-1"},
					{Name: "gw-2", Namespace: "ns-2", SectionName: "minecraft"},
				},
				TCPRoute: &mck8slexlav1beta1.RouteConfig{Enabled: true},
			})

			err := reconciler.ensureGatewayRoutes(ctx, server, nil)
			Expect(err).NotTo(HaveOccurred())

			var tcpRoute gatewayv1alpha2.TCPRoute
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: server.Name + "-tcp", Namespace: ns,
			}, &tcpRoute)).To(Succeed())

			Expect(tcpRoute.Spec.ParentRefs).To(HaveLen(2))

			sectionName := gatewayv1alpha2.SectionName("minecraft")
			Expect(tcpRoute.Spec.ParentRefs[1].SectionName).To(Equal(&sectionName))
		})
	})

	Context("when updating existing routes", func() {
		It("should update TCPRoute when parentRefs change", func() {
			server := createServer("gw-update", &mck8slexlav1beta1.GatewayConfig{
				Enabled: true,
				ParentRefs: []mck8slexlav1beta1.GatewayParentRef{
					{Name: "gw-old", Namespace: "ns-1"},
				},
				TCPRoute: &mck8slexlav1beta1.RouteConfig{Enabled: true},
			})

			err := reconciler.ensureGatewayRoutes(ctx, server, nil)
			Expect(err).NotTo(HaveOccurred())

			// Update parentRefs
			server.Spec.Gateway.ParentRefs = []mck8slexlav1beta1.GatewayParentRef{
				{Name: "gw-new", Namespace: "ns-2"},
			}
			err = reconciler.ensureGatewayRoutes(ctx, server, nil)
			Expect(err).NotTo(HaveOccurred())

			var tcpRoute gatewayv1alpha2.TCPRoute
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: server.Name + "-tcp", Namespace: ns,
			}, &tcpRoute)).To(Succeed())

			gwNamespace := gatewayv1alpha2.Namespace("ns-2")
			Expect(tcpRoute.Spec.ParentRefs[0].Namespace).To(Equal(&gwNamespace))
		})
	})

	Context("with owner references", func() {
		It("should set owner reference on created routes", func() {
			server := createServer("gw-owner", &mck8slexlav1beta1.GatewayConfig{
				Enabled: true,
				ParentRefs: []mck8slexlav1beta1.GatewayParentRef{
					{Name: "gw", Namespace: "gw-ns"},
				},
				TCPRoute: &mck8slexlav1beta1.RouteConfig{Enabled: true},
			})

			err := reconciler.ensureGatewayRoutes(ctx, server, nil)
			Expect(err).NotTo(HaveOccurred())

			var tcpRoute gatewayv1alpha2.TCPRoute
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: server.Name + "-tcp", Namespace: ns,
			}, &tcpRoute)).To(Succeed())

			Expect(tcpRoute.OwnerReferences).To(HaveLen(1))
			Expect(tcpRoute.OwnerReferences[0].Name).To(Equal(server.Name))
		})
	})
})
