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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	mcv1beta1 "github.com/lexfrei/minecraft-operator/api/v1beta1"
)

const minecraftPort int32 = 25565

// ensureGatewayRoutes creates, updates, or deletes Gateway API TCPRoute and UDPRoute
// resources based on the server's gateway configuration.
func (r *PaperMCServerReconciler) ensureGatewayRoutes(
	ctx context.Context,
	server *mcv1beta1.PaperMCServer,
	_ []mcv1beta1.Plugin,
) error {
	gwEnabled := server.Spec.Gateway != nil && server.Spec.Gateway.Enabled

	if err := r.reconcileTCPRoute(ctx, server, gwEnabled); err != nil {
		return errors.Wrap(err, "failed to reconcile TCPRoute")
	}

	if err := r.reconcileUDPRoute(ctx, server, gwEnabled); err != nil {
		return errors.Wrap(err, "failed to reconcile UDPRoute")
	}

	return nil
}

// reconcileTCPRoute creates, updates, or deletes the TCPRoute for game traffic.
func (r *PaperMCServerReconciler) reconcileTCPRoute(
	ctx context.Context,
	server *mcv1beta1.PaperMCServer,
	gwEnabled bool,
) error {
	routeName := server.Name + "-tcp"
	shouldExist := gwEnabled &&
		server.Spec.Gateway.TCPRoute != nil &&
		server.Spec.Gateway.TCPRoute.Enabled

	var existing gatewayv1alpha2.TCPRoute
	err := r.Get(ctx, client.ObjectKey{Name: routeName, Namespace: server.Namespace}, &existing)

	if err != nil && !apierrors.IsNotFound(err) {
		if meta.IsNoMatchError(err) {
			slog.DebugContext(ctx, "Gateway API TCPRoute CRD not installed, skipping")

			return nil
		}

		return errors.Wrap(err, "failed to get TCPRoute")
	}

	exists := err == nil

	if !shouldExist {
		if exists {
			slog.InfoContext(ctx, "Deleting TCPRoute", "name", routeName)

			if deleteErr := r.Delete(ctx, &existing); deleteErr != nil && !apierrors.IsNotFound(deleteErr) {
				return errors.Wrap(deleteErr, "failed to delete TCPRoute")
			}
		}

		return nil
	}

	desired := r.buildTCPRoute(server)

	if !exists {
		slog.InfoContext(ctx, "Creating TCPRoute", "name", routeName)

		if err := controllerutil.SetControllerReference(server, desired, r.Scheme); err != nil {
			return errors.Wrap(err, "failed to set owner reference on TCPRoute")
		}

		return errors.Wrap(r.Create(ctx, desired), "failed to create TCPRoute")
	}

	if reflect.DeepEqual(existing.Spec, desired.Spec) && maps.Equal(existing.Labels, desired.Labels) {
		return nil
	}

	slog.InfoContext(ctx, "Updating TCPRoute", "name", routeName)

	existing.Spec = desired.Spec
	existing.Labels = desired.Labels

	return errors.Wrap(r.Update(ctx, &existing), "failed to update TCPRoute")
}

// reconcileUDPRoute creates, updates, or deletes the UDPRoute for game traffic.
func (r *PaperMCServerReconciler) reconcileUDPRoute(
	ctx context.Context,
	server *mcv1beta1.PaperMCServer,
	gwEnabled bool,
) error {
	routeName := server.Name + "-udp"
	shouldExist := gwEnabled &&
		server.Spec.Gateway.UDPRoute != nil &&
		server.Spec.Gateway.UDPRoute.Enabled

	var existing gatewayv1alpha2.UDPRoute
	err := r.Get(ctx, client.ObjectKey{Name: routeName, Namespace: server.Namespace}, &existing)

	if err != nil && !apierrors.IsNotFound(err) {
		if meta.IsNoMatchError(err) {
			slog.DebugContext(ctx, "Gateway API UDPRoute CRD not installed, skipping")

			return nil
		}

		return errors.Wrap(err, "failed to get UDPRoute")
	}

	exists := err == nil

	if !shouldExist {
		if exists {
			slog.InfoContext(ctx, "Deleting UDPRoute", "name", routeName)

			if deleteErr := r.Delete(ctx, &existing); deleteErr != nil && !apierrors.IsNotFound(deleteErr) {
				return errors.Wrap(deleteErr, "failed to delete UDPRoute")
			}
		}

		return nil
	}

	desired := r.buildUDPRoute(server)

	if !exists {
		slog.InfoContext(ctx, "Creating UDPRoute", "name", routeName)

		if err := controllerutil.SetControllerReference(server, desired, r.Scheme); err != nil {
			return errors.Wrap(err, "failed to set owner reference on UDPRoute")
		}

		return errors.Wrap(r.Create(ctx, desired), "failed to create UDPRoute")
	}

	if reflect.DeepEqual(existing.Spec, desired.Spec) && maps.Equal(existing.Labels, desired.Labels) {
		return nil
	}

	slog.InfoContext(ctx, "Updating UDPRoute", "name", routeName)

	existing.Spec = desired.Spec
	existing.Labels = desired.Labels

	return errors.Wrap(r.Update(ctx, &existing), "failed to update UDPRoute")
}

// buildTCPRoute constructs the desired TCPRoute for a PaperMCServer.
func (r *PaperMCServerReconciler) buildTCPRoute(
	server *mcv1beta1.PaperMCServer,
) *gatewayv1alpha2.TCPRoute {
	parentRefs := convertParentRefs(server.Spec.Gateway.ParentRefs)

	// Only expose the Minecraft game port via Gateway.
	// RCON is an admin interface and should not be exposed through public Gateways;
	// use NetworkPolicy + port-forward or internal Service for RCON access.
	rules := []gatewayv1alpha2.TCPRouteRule{
		{BackendRefs: []gatewayv1alpha2.BackendRef{buildBackendRef(server.Name, minecraftPort)}},
	}

	return &gatewayv1alpha2.TCPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      server.Name + "-tcp",
			Namespace: server.Namespace,
			Labels:    standardLabels(server.Name, "networking"),
		},
		Spec: gatewayv1alpha2.TCPRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{
				ParentRefs: parentRefs,
			},
			Rules: rules,
		},
	}
}

// buildUDPRoute constructs the desired UDPRoute for a PaperMCServer.
func (r *PaperMCServerReconciler) buildUDPRoute(
	server *mcv1beta1.PaperMCServer,
) *gatewayv1alpha2.UDPRoute {
	parentRefs := convertParentRefs(server.Spec.Gateway.ParentRefs)

	return &gatewayv1alpha2.UDPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      server.Name + "-udp",
			Namespace: server.Namespace,
			Labels:    standardLabels(server.Name, "networking"),
		},
		Spec: gatewayv1alpha2.UDPRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{
				ParentRefs: parentRefs,
			},
			Rules: []gatewayv1alpha2.UDPRouteRule{
				{BackendRefs: []gatewayv1alpha2.BackendRef{buildBackendRef(server.Name, minecraftPort)}},
			},
		},
	}
}

// convertParentRefs converts our GatewayParentRef to Gateway API ParentReference.
func convertParentRefs(refs []mcv1beta1.GatewayParentRef) []gatewayv1.ParentReference {
	result := make([]gatewayv1.ParentReference, 0, len(refs))

	for _, ref := range refs {
		pr := gatewayv1.ParentReference{
			Name: gatewayv1.ObjectName(ref.Name),
		}

		if ref.Namespace != "" {
			ns := gatewayv1.Namespace(ref.Namespace)
			pr.Namespace = &ns
		}

		if ref.SectionName != "" {
			sn := gatewayv1.SectionName(ref.SectionName)
			pr.SectionName = &sn
		}

		result = append(result, pr)
	}

	return result
}

// buildBackendRef creates a BackendRef pointing to the server's Service on the given port.
func buildBackendRef(serviceName string, port int32) gatewayv1alpha2.BackendRef {
	p := port

	return gatewayv1alpha2.BackendRef{
		BackendObjectReference: gatewayv1.BackendObjectReference{
			Name: gatewayv1.ObjectName(serviceName),
			Port: &p,
		},
	}
}
