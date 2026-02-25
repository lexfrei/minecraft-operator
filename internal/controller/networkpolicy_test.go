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
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	mck8slexlav1beta1 "github.com/lexfrei/minecraft-operator/api/v1beta1"
	"github.com/lexfrei/minecraft-operator/pkg/paper"
	"github.com/lexfrei/minecraft-operator/pkg/solver"
	"github.com/lexfrei/minecraft-operator/pkg/testutil"
)

var _ = Describe("NetworkPolicy for PaperMCServer", func() {
	var (
		ns         string
		reconciler *PaperMCServerReconciler
	)

	BeforeEach(func() {
		ns = fmt.Sprintf("np-test-%d", time.Now().UnixNano())
		nsObj := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: ns},
		}
		Expect(k8sClient.Create(ctx, nsObj)).To(Succeed())

		reconciler = &PaperMCServerReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
			Solver: solver.NewSimpleSolver(),
			RegistryClient: &testutil.MockRegistryAPI{
				Tags:       []string{"1.21.1-91"},
				ImageExist: true,
			},
			PaperClient: &testutil.MockPaperAPI{
				Versions:     []string{"1.21.1"},
				BuildInfo:    &paper.BuildInfo{Version: "1.21.1", Build: 91},
				BuildNumbers: []int{91},
			},
		}
	})

	createServer := func(name string, network *mck8slexlav1beta1.NetworkConfig) *mck8slexlav1beta1.PaperMCServer {
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
				Network: network,
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

		var created mck8slexlav1beta1.PaperMCServer
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: ns}, &created)).To(Succeed())

		return &created
	}

	createServerWithRCON := func(name string, network *mck8slexlav1beta1.NetworkConfig) *mck8slexlav1beta1.PaperMCServer {
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
					Enabled:        true,
					PasswordSecret: mck8slexlav1beta1.SecretKeyRef{Name: "rcon-secret", Key: "password"},
					Port:           25575,
				},
				Service: mck8slexlav1beta1.ServiceConfig{
					Type: corev1.ServiceTypeClusterIP,
				},
				Network: network,
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

		var created mck8slexlav1beta1.PaperMCServer
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: ns}, &created)).To(Succeed())

		return &created
	}

	Context("when network policy is enabled", func() {
		It("should create a NetworkPolicy for the server", func() {
			server := createServer("np-basic", &mck8slexlav1beta1.NetworkConfig{
				NetworkPolicy: &mck8slexlav1beta1.ServerNetworkPolicy{
					Enabled: true,
				},
			})

			err := reconciler.ensureNetworkPolicy(ctx, server)
			Expect(err).NotTo(HaveOccurred())

			var np networkingv1.NetworkPolicy
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: server.Name + "-minecraft", Namespace: ns,
			}, &np)).To(Succeed())

			// Verify pod selector matches StatefulSet pods
			Expect(np.Spec.PodSelector.MatchLabels).To(HaveKeyWithValue("app", "papermc"))
			Expect(np.Spec.PodSelector.MatchLabels).To(HaveKeyWithValue("mc.k8s.lex.la/server-name", server.Name))
		})

		It("should allow Minecraft port 25565 ingress", func() {
			server := createServer("np-mc-port", &mck8slexlav1beta1.NetworkConfig{
				NetworkPolicy: &mck8slexlav1beta1.ServerNetworkPolicy{
					Enabled: true,
				},
			})

			err := reconciler.ensureNetworkPolicy(ctx, server)
			Expect(err).NotTo(HaveOccurred())

			var np networkingv1.NetworkPolicy
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: server.Name + "-minecraft", Namespace: ns,
			}, &np)).To(Succeed())

			// Should have ingress rule for port 25565
			mcPort := intstr.FromInt32(25565)
			tcpProto := corev1.ProtocolTCP
			Expect(np.Spec.Ingress).To(ContainElement(
				networkingv1.NetworkPolicyIngressRule{
					Ports: []networkingv1.NetworkPolicyPort{
						{Protocol: &tcpProto, Port: &mcPort},
					},
				},
			))
		})

		It("should allow DNS egress when restrictEgress is true", func() {
			restrictEgress := true
			server := createServer("np-egress-dns", &mck8slexlav1beta1.NetworkConfig{
				NetworkPolicy: &mck8slexlav1beta1.ServerNetworkPolicy{
					Enabled:        true,
					RestrictEgress: &restrictEgress,
				},
			})

			err := reconciler.ensureNetworkPolicy(ctx, server)
			Expect(err).NotTo(HaveOccurred())

			var np networkingv1.NetworkPolicy
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: server.Name + "-minecraft", Namespace: ns,
			}, &np)).To(Succeed())

			// Should have Egress policy type
			Expect(np.Spec.PolicyTypes).To(ContainElement(networkingv1.PolicyTypeEgress))

			// Should have DNS egress rule
			dnsPort := intstr.FromInt32(53)
			udpProto := corev1.ProtocolUDP
			tcpProto := corev1.ProtocolTCP
			Expect(np.Spec.Egress).To(ContainElement(
				networkingv1.NetworkPolicyEgressRule{
					Ports: []networkingv1.NetworkPolicyPort{
						{Protocol: &udpProto, Port: &dnsPort},
						{Protocol: &tcpProto, Port: &dnsPort},
					},
				},
			))
		})

		It("should have both Ingress and Egress policy types", func() {
			restrictEgress := true
			server := createServer("np-types", &mck8slexlav1beta1.NetworkConfig{
				NetworkPolicy: &mck8slexlav1beta1.ServerNetworkPolicy{
					Enabled:        true,
					RestrictEgress: &restrictEgress,
				},
			})

			err := reconciler.ensureNetworkPolicy(ctx, server)
			Expect(err).NotTo(HaveOccurred())

			var np networkingv1.NetworkPolicy
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: server.Name + "-minecraft", Namespace: ns,
			}, &np)).To(Succeed())

			Expect(np.Spec.PolicyTypes).To(ContainElement(networkingv1.PolicyTypeIngress))
			Expect(np.Spec.PolicyTypes).To(ContainElement(networkingv1.PolicyTypeEgress))
		})

		It("should set standard labels on NetworkPolicy", func() {
			server := createServer("np-labels", &mck8slexlav1beta1.NetworkConfig{
				NetworkPolicy: &mck8slexlav1beta1.ServerNetworkPolicy{
					Enabled: true,
				},
			})

			err := reconciler.ensureNetworkPolicy(ctx, server)
			Expect(err).NotTo(HaveOccurred())

			var np networkingv1.NetworkPolicy
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: server.Name + "-minecraft", Namespace: ns,
			}, &np)).To(Succeed())

			Expect(np.Labels).To(HaveKeyWithValue("app.kubernetes.io/name", "papermc"))
			Expect(np.Labels).To(HaveKeyWithValue("app.kubernetes.io/instance", server.Name))
			Expect(np.Labels).To(HaveKeyWithValue("app.kubernetes.io/component", "network-policy"))
			Expect(np.Labels).To(HaveKeyWithValue("app.kubernetes.io/part-of", "minecraft-operator"))
		})

		It("should set owner reference for garbage collection", func() {
			server := createServer("np-owner", &mck8slexlav1beta1.NetworkConfig{
				NetworkPolicy: &mck8slexlav1beta1.ServerNetworkPolicy{
					Enabled: true,
				},
			})

			err := reconciler.ensureNetworkPolicy(ctx, server)
			Expect(err).NotTo(HaveOccurred())

			var np networkingv1.NetworkPolicy
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: server.Name + "-minecraft", Namespace: ns,
			}, &np)).To(Succeed())

			Expect(np.OwnerReferences).To(HaveLen(1))
			Expect(np.OwnerReferences[0].Name).To(Equal(server.Name))
		})
	})

	Context("when network policy is disabled", func() {
		It("should not create a NetworkPolicy", func() {
			_ = createServer("np-disabled", nil)

			var npList networkingv1.NetworkPolicyList
			Expect(k8sClient.List(ctx, &npList, client.InNamespace(ns))).To(Succeed())
			Expect(npList.Items).To(BeEmpty())
		})

		It("should delete existing NetworkPolicy when disabled", func() {
			server := createServer("np-cleanup", &mck8slexlav1beta1.NetworkConfig{
				NetworkPolicy: &mck8slexlav1beta1.ServerNetworkPolicy{
					Enabled: true,
				},
			})

			err := reconciler.ensureNetworkPolicy(ctx, server)
			Expect(err).NotTo(HaveOccurred())

			// Verify it exists
			var np networkingv1.NetworkPolicy
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: server.Name + "-minecraft", Namespace: ns,
			}, &np)).To(Succeed())

			// Disable network policy
			server.Spec.Network = nil
			err = reconciler.ensureNetworkPolicy(ctx, server)
			Expect(err).NotTo(HaveOccurred())

			// Should be deleted
			var npList networkingv1.NetworkPolicyList
			Expect(k8sClient.List(ctx, &npList, client.InNamespace(ns))).To(Succeed())
			Expect(npList.Items).To(BeEmpty())
		})
	})

	Context("when network policy has custom allowFrom", func() {
		It("should include additional ingress sources for Minecraft port", func() {
			server := createServer("np-allowfrom", &mck8slexlav1beta1.NetworkConfig{
				NetworkPolicy: &mck8slexlav1beta1.ServerNetworkPolicy{
					Enabled: true,
					AllowFrom: []mck8slexlav1beta1.NetworkPolicySource{
						{CIDR: "10.0.0.0/8"},
					},
				},
			})

			err := reconciler.ensureNetworkPolicy(ctx, server)
			Expect(err).NotTo(HaveOccurred())

			var np networkingv1.NetworkPolicy
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: server.Name + "-minecraft", Namespace: ns,
			}, &np)).To(Succeed())

			// The Minecraft port rule should have From entries
			mcPort := intstr.FromInt32(25565)
			found := false
			for _, rule := range np.Spec.Ingress {
				for _, port := range rule.Ports {
					if port.Port != nil && port.Port.IntVal == mcPort.IntVal {
						found = true
						Expect(rule.From).To(ContainElement(
							networkingv1.NetworkPolicyPeer{
								IPBlock: &networkingv1.IPBlock{CIDR: "10.0.0.0/8"},
							},
						))
					}
				}
			}
			Expect(found).To(BeTrue(), "Should have Minecraft port rule with custom allowFrom")
		})
	})

	// Issue #2: convertToPeer must reject CIDR + selector combination
	Context("when allowFrom has both CIDR and podSelector", func() {
		It("should return error for invalid peer with CIDR and selector", func() {
			_, err := convertToPeer(mck8slexlav1beta1.NetworkPolicySource{
				CIDR:        "10.0.0.0/8",
				PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "proxy"}},
			})
			Expect(err).To(HaveOccurred(), "convertToPeer must reject CIDR combined with PodSelector")
		})

		It("should return error for CIDR and namespaceSelector", func() {
			_, err := convertToPeer(mck8slexlav1beta1.NetworkPolicySource{
				CIDR:              "10.0.0.0/8",
				NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"env": "prod"}},
			})
			Expect(err).To(HaveOccurred(), "convertToPeer must reject CIDR combined with NamespaceSelector")
		})
	})

	// Issue #5: Default egress must include HTTPS for Mojang auth
	Context("when restrictEgress is true (default)", func() {
		It("should allow HTTPS egress for Mojang authentication", func() {
			restrictEgress := true
			server := createServer("np-mojang", &mck8slexlav1beta1.NetworkConfig{
				NetworkPolicy: &mck8slexlav1beta1.ServerNetworkPolicy{
					Enabled:        true,
					RestrictEgress: &restrictEgress,
				},
			})

			err := reconciler.ensureNetworkPolicy(ctx, server)
			Expect(err).NotTo(HaveOccurred())

			var np networkingv1.NetworkPolicy
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: server.Name + "-minecraft", Namespace: ns,
			}, &np)).To(Succeed())

			httpsPort := intstr.FromInt32(443)
			tcpProto := corev1.ProtocolTCP
			Expect(np.Spec.Egress).To(ContainElement(
				networkingv1.NetworkPolicyEgressRule{
					Ports: []networkingv1.NetworkPolicyPort{
						{Protocol: &tcpProto, Port: &httpsPort},
					},
				},
			))
		})
	})

	// RCON namespace restriction must use operator namespace, not server namespace
	Context("when RCON is enabled and operator runs in a different namespace", func() {
		It("should restrict RCON port to operator namespace, not server namespace", func() {
			operatorNS := "minecraft-operator-system"
			reconciler.OperatorNamespace = operatorNS

			server := createServerWithRCON("np-rcon-opns", &mck8slexlav1beta1.NetworkConfig{
				NetworkPolicy: &mck8slexlav1beta1.ServerNetworkPolicy{
					Enabled: true,
				},
			})

			err := reconciler.ensureNetworkPolicy(ctx, server)
			Expect(err).NotTo(HaveOccurred())

			var np networkingv1.NetworkPolicy
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: server.Name + "-minecraft", Namespace: ns,
			}, &np)).To(Succeed())

			rconPort := intstr.FromInt32(25575)
			tcpProto := corev1.ProtocolTCP
			found := false
			for _, rule := range np.Spec.Ingress {
				for _, port := range rule.Ports {
					if port.Port != nil && port.Port.IntVal == rconPort.IntVal && *port.Protocol == tcpProto {
						found = true
						Expect(rule.From).To(ContainElement(
							networkingv1.NetworkPolicyPeer{
								NamespaceSelector: &metav1.LabelSelector{
									MatchLabels: map[string]string{
										"kubernetes.io/metadata.name": operatorNS,
									},
								},
							},
						), "RCON should be restricted to operator namespace, not server namespace")
					}
				}
			}
			Expect(found).To(BeTrue(), "Should have RCON port rule")
		})

		It("should fall back to server namespace when OperatorNamespace is empty", func() {
			reconciler.OperatorNamespace = ""

			server := createServerWithRCON("np-rcon-fallback", &mck8slexlav1beta1.NetworkConfig{
				NetworkPolicy: &mck8slexlav1beta1.ServerNetworkPolicy{
					Enabled: true,
				},
			})

			err := reconciler.ensureNetworkPolicy(ctx, server)
			Expect(err).NotTo(HaveOccurred())

			var np networkingv1.NetworkPolicy
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: server.Name + "-minecraft", Namespace: ns,
			}, &np)).To(Succeed())

			rconPort := intstr.FromInt32(25575)
			tcpProto := corev1.ProtocolTCP
			found := false
			for _, rule := range np.Spec.Ingress {
				for _, port := range rule.Ports {
					if port.Port != nil && port.Port.IntVal == rconPort.IntVal && *port.Protocol == tcpProto {
						found = true
						Expect(rule.From).To(ContainElement(
							networkingv1.NetworkPolicyPeer{
								NamespaceSelector: &metav1.LabelSelector{
									MatchLabels: map[string]string{
										"kubernetes.io/metadata.name": ns,
									},
								},
							},
						), "Should fall back to server namespace when OperatorNamespace is empty")
					}
				}
			}
			Expect(found).To(BeTrue(), "Should have RCON port rule")
		})
	})

	// Issue #9: restrictEgress: false must produce Ingress-only policy
	Context("when restrictEgress is false", func() {
		It("should only have Ingress policy type and no egress rules", func() {
			restrictEgress := false
			server := createServer("np-no-egress", &mck8slexlav1beta1.NetworkConfig{
				NetworkPolicy: &mck8slexlav1beta1.ServerNetworkPolicy{
					Enabled:        true,
					RestrictEgress: &restrictEgress,
				},
			})

			err := reconciler.ensureNetworkPolicy(ctx, server)
			Expect(err).NotTo(HaveOccurred())

			var np networkingv1.NetworkPolicy
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: server.Name + "-minecraft", Namespace: ns,
			}, &np)).To(Succeed())

			Expect(np.Spec.PolicyTypes).To(ConsistOf(networkingv1.PolicyTypeIngress))
			Expect(np.Spec.Egress).To(BeEmpty())
		})
	})

	// Empty allowEgressTo entry must be rejected (opens all egress)
	Context("when allowEgressTo has an empty entry", func() {
		It("should return error for empty NetworkPolicyDestination", func() {
			restrictEgress := true
			server := createServer("np-egress-empty", &mck8slexlav1beta1.NetworkConfig{
				NetworkPolicy: &mck8slexlav1beta1.ServerNetworkPolicy{
					Enabled:        true,
					RestrictEgress: &restrictEgress,
					AllowEgressTo: []mck8slexlav1beta1.NetworkPolicyDestination{
						{}, // empty entry — would open all egress
					},
				},
			})

			err := reconciler.ensureNetworkPolicy(ctx, server)
			Expect(err).To(HaveOccurred(), "Empty allowEgressTo entry must be rejected")
		})
	})

	// Empty NetworkPolicySource must be rejected (opens all ingress)
	Context("when allowFrom has an empty entry", func() {
		It("should return error for empty NetworkPolicySource", func() {
			_, err := convertToPeer(mck8slexlav1beta1.NetworkPolicySource{})
			Expect(err).To(HaveOccurred(), "Empty NetworkPolicySource must be rejected")
		})
	})

	// Update path must skip API call when nothing changed
	Context("when ensureNetworkPolicy is called twice with same config", func() {
		It("should not issue unnecessary update", func() {
			server := createServer("np-noop-update", &mck8slexlav1beta1.NetworkConfig{
				NetworkPolicy: &mck8slexlav1beta1.ServerNetworkPolicy{
					Enabled: true,
				},
			})

			// First call — create
			err := reconciler.ensureNetworkPolicy(ctx, server)
			Expect(err).NotTo(HaveOccurred())

			var np1 networkingv1.NetworkPolicy
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: server.Name + "-minecraft", Namespace: ns,
			}, &np1)).To(Succeed())
			rv1 := np1.ResourceVersion

			// Second call — should not update (same config)
			err = reconciler.ensureNetworkPolicy(ctx, server)
			Expect(err).NotTo(HaveOccurred())

			var np2 networkingv1.NetworkPolicy
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: server.Name + "-minecraft", Namespace: ns,
			}, &np2)).To(Succeed())

			Expect(np2.ResourceVersion).To(Equal(rv1),
				"ResourceVersion must not change when config is unchanged")
		})
	})

	// Update path must actually update when config changes
	Context("when NetworkPolicy config changes", func() {
		It("should update existing NetworkPolicy spec", func() {
			server := createServer("np-update", &mck8slexlav1beta1.NetworkConfig{
				NetworkPolicy: &mck8slexlav1beta1.ServerNetworkPolicy{
					Enabled: true,
					AllowFrom: []mck8slexlav1beta1.NetworkPolicySource{
						{CIDR: "10.0.0.0/8"},
					},
				},
			})

			err := reconciler.ensureNetworkPolicy(ctx, server)
			Expect(err).NotTo(HaveOccurred())

			// Modify config
			server.Spec.Network.NetworkPolicy.AllowFrom = []mck8slexlav1beta1.NetworkPolicySource{
				{CIDR: "192.168.0.0/16"},
			}

			err = reconciler.ensureNetworkPolicy(ctx, server)
			Expect(err).NotTo(HaveOccurred())

			var np networkingv1.NetworkPolicy
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: server.Name + "-minecraft", Namespace: ns,
			}, &np)).To(Succeed())

			// Verify the Minecraft port rule has updated allowFrom
			mcPort := intstr.FromInt32(25565)
			found := false
			for _, rule := range np.Spec.Ingress {
				for _, port := range rule.Ports {
					if port.Port != nil && port.Port.IntVal == mcPort.IntVal {
						found = true
						Expect(rule.From).To(ContainElement(
							networkingv1.NetworkPolicyPeer{
								IPBlock: &networkingv1.IPBlock{CIDR: "192.168.0.0/16"},
							},
						))
						Expect(rule.From).NotTo(ContainElement(
							networkingv1.NetworkPolicyPeer{
								IPBlock: &networkingv1.IPBlock{CIDR: "10.0.0.0/8"},
							},
						), "Old CIDR should be replaced")
					}
				}
			}
			Expect(found).To(BeTrue())
		})
	})

	// allowEgressTo custom destinations
	Context("when allowEgressTo has custom destinations", func() {
		It("should include CIDR-based egress rule", func() {
			restrictEgress := true
			port443 := int32(443)
			tcpProto := corev1.ProtocolTCP
			server := createServer("np-egress-cidr", &mck8slexlav1beta1.NetworkConfig{
				NetworkPolicy: &mck8slexlav1beta1.ServerNetworkPolicy{
					Enabled:        true,
					RestrictEgress: &restrictEgress,
					AllowEgressTo: []mck8slexlav1beta1.NetworkPolicyDestination{
						{CIDR: "203.0.113.0/24", Port: &port443, Protocol: &tcpProto},
					},
				},
			})

			err := reconciler.ensureNetworkPolicy(ctx, server)
			Expect(err).NotTo(HaveOccurred())

			var np networkingv1.NetworkPolicy
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: server.Name + "-minecraft", Namespace: ns,
			}, &np)).To(Succeed())

			egressPort := intstr.FromInt32(443)
			Expect(np.Spec.Egress).To(ContainElement(
				networkingv1.NetworkPolicyEgressRule{
					To: []networkingv1.NetworkPolicyPeer{
						{IPBlock: &networkingv1.IPBlock{CIDR: "203.0.113.0/24"}},
					},
					Ports: []networkingv1.NetworkPolicyPort{
						{Protocol: &tcpProto, Port: &egressPort},
					},
				},
			))
		})

		It("should handle port-only egress (no CIDR)", func() {
			restrictEgress := true
			port8080 := int32(8080)
			server := createServer("np-egress-port", &mck8slexlav1beta1.NetworkConfig{
				NetworkPolicy: &mck8slexlav1beta1.ServerNetworkPolicy{
					Enabled:        true,
					RestrictEgress: &restrictEgress,
					AllowEgressTo: []mck8slexlav1beta1.NetworkPolicyDestination{
						{Port: &port8080},
					},
				},
			})

			err := reconciler.ensureNetworkPolicy(ctx, server)
			Expect(err).NotTo(HaveOccurred())

			var np networkingv1.NetworkPolicy
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: server.Name + "-minecraft", Namespace: ns,
			}, &np)).To(Succeed())

			egressPort := intstr.FromInt32(8080)
			tcpDefault := corev1.ProtocolTCP
			// Should have DNS + custom port rules
			Expect(len(np.Spec.Egress)).To(BeNumerically(">=", 3),
				"Should have DNS + HTTPS + custom port egress rules")
			Expect(np.Spec.Egress).To(ContainElement(
				networkingv1.NetworkPolicyEgressRule{
					Ports: []networkingv1.NetworkPolicyPort{
						{Protocol: &tcpDefault, Port: &egressPort},
					},
				},
			))
		})
	})

})
