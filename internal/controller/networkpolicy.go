/*
Copyright 2026, Aleksei Sviridkin.

SPDX-License-Identifier: BSD-3-Clause
*/

package controller

import (
	"context"
	"log/slog"
	"maps"
	"reflect"

	"github.com/cockroachdb/errors"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	corev1 "k8s.io/api/core/v1"

	mcv1beta1 "github.com/lexfrei/minecraft-operator/api/v1beta1"
)

const (
	minecraftGamePort = 25565
	dnsPort           = 53
)

// ensureNetworkPolicy creates, updates, or deletes a NetworkPolicy for a PaperMCServer
// based on its network configuration.
func (r *PaperMCServerReconciler) ensureNetworkPolicy(
	ctx context.Context,
	server *mcv1beta1.PaperMCServer,
) error {
	npName := server.Name + "-minecraft"
	shouldExist := server.Spec.Network != nil &&
		server.Spec.Network.NetworkPolicy != nil &&
		server.Spec.Network.NetworkPolicy.Enabled

	var existing networkingv1.NetworkPolicy
	err := r.Get(ctx, client.ObjectKey{Name: npName, Namespace: server.Namespace}, &existing)

	if err != nil && !apierrors.IsNotFound(err) {
		return errors.Wrap(err, "failed to get NetworkPolicy")
	}

	exists := err == nil

	if !shouldExist {
		if exists {
			slog.InfoContext(ctx, "Deleting NetworkPolicy", "name", npName)

			return errors.Wrap(
				r.Delete(ctx, &existing),
				"failed to delete NetworkPolicy",
			)
		}

		return nil
	}

	desired, err := r.buildNetworkPolicy(server)
	if err != nil {
		return errors.Wrap(err, "failed to build NetworkPolicy")
	}

	if !exists {
		slog.InfoContext(ctx, "Creating NetworkPolicy", "name", npName)

		if err := controllerutil.SetControllerReference(server, desired, r.Scheme); err != nil {
			return errors.Wrap(err, "failed to set owner reference on NetworkPolicy")
		}

		return errors.Wrap(r.Create(ctx, desired), "failed to create NetworkPolicy")
	}

	// Skip update if nothing changed
	if reflect.DeepEqual(existing.Spec, desired.Spec) && maps.Equal(existing.Labels, desired.Labels) {
		return nil
	}

	slog.InfoContext(ctx, "Updating NetworkPolicy", "name", npName)

	existing.Spec = desired.Spec
	existing.Labels = desired.Labels

	return errors.Wrap(r.Update(ctx, &existing), "failed to update NetworkPolicy")
}

// buildNetworkPolicy constructs the desired NetworkPolicy for a PaperMCServer.
func (r *PaperMCServerReconciler) buildNetworkPolicy(
	server *mcv1beta1.PaperMCServer,
) (*networkingv1.NetworkPolicy, error) {
	npSpec := server.Spec.Network.NetworkPolicy

	policyTypes := []networkingv1.PolicyType{networkingv1.PolicyTypeIngress}

	// Build ingress rules
	ingress, err := r.buildNetworkPolicyIngress(server, npSpec)
	if err != nil {
		return nil, errors.Wrap(err, "failed to build ingress rules")
	}

	// Build egress rules
	var egress []networkingv1.NetworkPolicyEgressRule

	restrictEgress := npSpec.RestrictEgress == nil || *npSpec.RestrictEgress
	if restrictEgress {
		policyTypes = append(policyTypes, networkingv1.PolicyTypeEgress)

		var egressErr error

		egress, egressErr = r.buildNetworkPolicyEgress(npSpec)
		if egressErr != nil {
			return nil, errors.Wrap(egressErr, "failed to build egress rules")
		}
	}

	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      server.Name + "-minecraft",
			Namespace: server.Namespace,
			Labels:    standardLabels(server.Name, "network-policy"),
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app":                       "papermc",
					"mc.k8s.lex.la/server-name": server.Name,
				},
			},
			PolicyTypes: policyTypes,
			Ingress:     ingress,
			Egress:      egress,
		},
	}, nil
}

// buildNetworkPolicyIngress constructs ingress rules for the NetworkPolicy.
func (r *PaperMCServerReconciler) buildNetworkPolicyIngress(
	server *mcv1beta1.PaperMCServer,
	npSpec *mcv1beta1.ServerNetworkPolicy,
) ([]networkingv1.NetworkPolicyIngressRule, error) {
	tcpProto := corev1.ProtocolTCP
	mcPort := intstr.FromInt32(minecraftGamePort)

	// Minecraft port rule
	mcRule := networkingv1.NetworkPolicyIngressRule{
		Ports: []networkingv1.NetworkPolicyPort{
			{Protocol: &tcpProto, Port: &mcPort},
		},
	}

	if len(npSpec.AllowFrom) == 0 {
		// Default: restrict to same namespace when no explicit sources specified
		mcRule.From = []networkingv1.NetworkPolicyPeer{
			{
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"kubernetes.io/metadata.name": server.Namespace,
					},
				},
			},
		}
	}

	// Add custom allowFrom sources
	for _, source := range npSpec.AllowFrom {
		peer, err := convertToPeer(source)
		if err != nil {
			return nil, errors.Wrap(err, "invalid allowFrom source")
		}

		mcRule.From = append(mcRule.From, peer)
	}

	rules := []networkingv1.NetworkPolicyIngressRule{mcRule}

	// RCON port rule — restricted to operator namespace
	if server.Spec.RCON.Enabled && server.Spec.RCON.Port > 0 {
		rconNS := r.OperatorNamespace
		if rconNS == "" {
			rconNS = server.Namespace
		}

		rconPort := intstr.FromInt32(server.Spec.RCON.Port)
		rconRule := networkingv1.NetworkPolicyIngressRule{
			Ports: []networkingv1.NetworkPolicyPort{
				{Protocol: &tcpProto, Port: &rconPort},
			},
			From: []networkingv1.NetworkPolicyPeer{
				{
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"kubernetes.io/metadata.name": rconNS,
						},
					},
				},
			},
		}
		rules = append(rules, rconRule)
	}

	return rules, nil
}

// buildNetworkPolicyEgress constructs egress rules for the NetworkPolicy.
func (r *PaperMCServerReconciler) buildNetworkPolicyEgress(
	npSpec *mcv1beta1.ServerNetworkPolicy,
) ([]networkingv1.NetworkPolicyEgressRule, error) {
	udpProto := corev1.ProtocolUDP
	tcpProto := corev1.ProtocolTCP
	dnsPortVal := intstr.FromInt32(dnsPort)

	httpsPortVal := intstr.FromInt32(443)

	rules := []networkingv1.NetworkPolicyEgressRule{
		// DNS resolution
		{
			Ports: []networkingv1.NetworkPolicyPort{
				{Protocol: &udpProto, Port: &dnsPortVal},
				{Protocol: &tcpProto, Port: &dnsPortVal},
			},
		},
		// HTTPS (Mojang authentication, plugin downloads)
		{
			Ports: []networkingv1.NetworkPolicyPort{
				{Protocol: &tcpProto, Port: &httpsPortVal},
			},
		},
	}

	// Additional egress destinations
	for _, dest := range npSpec.AllowEgressTo {
		if dest.CIDR == "" && dest.Port == nil {
			return nil, errors.New(
				"NetworkPolicyDestination must specify at least CIDR or Port",
			)
		}

		rule := networkingv1.NetworkPolicyEgressRule{}

		if dest.CIDR != "" {
			rule.To = []networkingv1.NetworkPolicyPeer{
				{IPBlock: &networkingv1.IPBlock{CIDR: dest.CIDR}},
			}
		}

		if dest.Port != nil {
			proto := corev1.ProtocolTCP
			if dest.Protocol != nil {
				proto = *dest.Protocol
			}

			port := intstr.FromInt32(*dest.Port)
			rule.Ports = []networkingv1.NetworkPolicyPort{
				{Protocol: &proto, Port: &port},
			}
		}

		rules = append(rules, rule)
	}

	return rules, nil
}

// convertToPeer converts a NetworkPolicySource to a Kubernetes NetworkPolicyPeer.
// Returns an error if CIDR is combined with PodSelector or NamespaceSelector,
// as the Kubernetes API does not allow IPBlock with other peer fields.
func convertToPeer(source mcv1beta1.NetworkPolicySource) (networkingv1.NetworkPolicyPeer, error) {
	if source.CIDR == "" && source.PodSelector == nil && source.NamespaceSelector == nil {
		return networkingv1.NetworkPolicyPeer{}, errors.New(
			"NetworkPolicySource must specify at least one of CIDR, PodSelector, or NamespaceSelector",
		)
	}

	if source.CIDR != "" && (source.PodSelector != nil || source.NamespaceSelector != nil) {
		return networkingv1.NetworkPolicyPeer{}, errors.New(
			"NetworkPolicySource cannot combine CIDR with PodSelector or NamespaceSelector",
		)
	}

	peer := networkingv1.NetworkPolicyPeer{}

	if source.CIDR != "" {
		peer.IPBlock = &networkingv1.IPBlock{CIDR: source.CIDR}
	}

	if source.PodSelector != nil {
		peer.PodSelector = source.PodSelector
	}

	if source.NamespaceSelector != nil {
		peer.NamespaceSelector = source.NamespaceSelector
	}

	return peer, nil
}
