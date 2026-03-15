/*
Copyright 2026, Aleksei Sviridkin.

SPDX-License-Identifier: BSD-3-Clause
*/

package controller

import (
	"context"
	"fmt"
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

// ensureGatewayRoutes creates, updates, or deletes Gateway API TCPRoute, UDPRoute,
// and HTTPRoute resources based on the server's gateway configuration.
func (r *PaperMCServerReconciler) ensureGatewayRoutes(
	ctx context.Context,
	server *mcv1beta1.PaperMCServer,
	matchedPlugins []mcv1beta1.Plugin,
) error {
	gwEnabled := server.Spec.Gateway != nil && server.Spec.Gateway.Enabled

	if err := r.reconcileTCPRoute(ctx, server, gwEnabled); err != nil {
		return errors.Wrap(err, "failed to reconcile TCPRoute")
	}

	if err := r.reconcileUDPRoute(ctx, server, gwEnabled); err != nil {
		return errors.Wrap(err, "failed to reconcile UDPRoute")
	}

	if err := r.reconcileHTTPRoutes(ctx, server, matchedPlugins, gwEnabled); err != nil {
		return errors.Wrap(err, "failed to reconcile HTTPRoutes")
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
	// Ensure owner reference for retroactive adoption.
	ownerRefsBefore := len(existing.OwnerReferences)
	if err := controllerutil.SetControllerReference(server, &existing, r.Scheme); err != nil {
		return errors.Wrap(err, "failed to set owner reference on TCPRoute")
	}
	ownerRefsChanged := len(existing.OwnerReferences) != ownerRefsBefore
	if !ownerRefsChanged &&
		reflect.DeepEqual(existing.Spec, desired.Spec) && maps.Equal(existing.Labels, desired.Labels) {
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
	// Ensure owner reference for retroactive adoption.
	ownerRefsBefore := len(existing.OwnerReferences)
	if err := controllerutil.SetControllerReference(server, &existing, r.Scheme); err != nil {
		return errors.Wrap(err, "failed to set owner reference on UDPRoute")
	}
	ownerRefsChanged := len(existing.OwnerReferences) != ownerRefsBefore
	if !ownerRefsChanged &&
		reflect.DeepEqual(existing.Spec, desired.Spec) && maps.Equal(existing.Labels, desired.Labels) {
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

// reconcileHTTPRoutes creates, updates, or deletes HTTPRoute resources for plugin HTTP endpoints.
func (r *PaperMCServerReconciler) reconcileHTTPRoutes(
	ctx context.Context,
	server *mcv1beta1.PaperMCServer,
	matchedPlugins []mcv1beta1.Plugin,
	gwEnabled bool,
) error {
	// Build a map of matched plugins by name for quick lookup.
	pluginMap := make(map[string]*mcv1beta1.Plugin, len(matchedPlugins))
	for i := range matchedPlugins {
		pluginMap[matchedPlugins[i].Name] = &matchedPlugins[i]
	}

	// Build set of desired HTTPRoute names.
	desired := make(map[string]gatewayv1.HTTPRoute)

	if gwEnabled && server.Spec.Gateway != nil {
		for _, hr := range server.Spec.Gateway.HTTPRoutes {
			plugin, found := pluginMap[hr.PluginName]
			if !found {
				slog.WarnContext(ctx, "HTTPRoute references plugin not matched to this server",
					"plugin", hr.PluginName, "server", server.Name)
				continue
			}

			endpoint := findHTTPEndpoint(plugin, hr.EndpointName)
			if endpoint == nil {
				slog.WarnContext(ctx, "HTTPRoute references non-existent or non-HTTP endpoint",
					"plugin", hr.PluginName, "endpoint", hr.EndpointName)
				continue
			}

			route := r.buildHTTPRoute(server, hr, endpoint.Port)
			desired[route.Name] = *route
		}
	}

	// List existing HTTPRoutes in the namespace and filter by owner reference.
	var existingList gatewayv1.HTTPRouteList

	err := r.List(ctx, &existingList, client.InNamespace(server.Namespace))
	if err != nil {
		if meta.IsNoMatchError(err) {
			slog.DebugContext(ctx, "Gateway API HTTPRoute CRD not installed, skipping")
			return nil
		}

		return errors.Wrap(err, "failed to list HTTPRoutes")
	}

	existingMap := make(map[string]*gatewayv1.HTTPRoute)
	for i := range existingList.Items {
		route := &existingList.Items[i]
		if route.Labels["mc.k8s.lex.la/route-type"] == "http" && isOwnedBy(route, server) {
			existingMap[route.Name] = route
		}
	}

	// Create or update desired routes.
	for name, desiredRoute := range desired {
		existing, exists := existingMap[name]
		if !exists {
			slog.InfoContext(ctx, "Creating HTTPRoute", "name", name)
			route := desiredRoute
			if err := controllerutil.SetControllerReference(server, &route, r.Scheme); err != nil {
				return errors.Wrap(err, "failed to set owner reference on HTTPRoute")
			}

			if err := r.Create(ctx, &route); err != nil {
				if meta.IsNoMatchError(err) {
					slog.DebugContext(ctx, "Gateway API HTTPRoute CRD not installed, skipping")
					return nil
				}

				return errors.Wrap(err, "failed to create HTTPRoute")
			}

			continue
		}

		// Update if changed.
		ownerRefsBefore := len(existing.OwnerReferences)
		if err := controllerutil.SetControllerReference(server, existing, r.Scheme); err != nil {
			return errors.Wrap(err, "failed to set owner reference on HTTPRoute")
		}

		ownerRefsChanged := len(existing.OwnerReferences) != ownerRefsBefore
		if !ownerRefsChanged &&
			reflect.DeepEqual(existing.Spec, desiredRoute.Spec) &&
			maps.Equal(existing.Labels, desiredRoute.Labels) {
			continue
		}

		slog.InfoContext(ctx, "Updating HTTPRoute", "name", name)
		existing.Spec = desiredRoute.Spec
		existing.Labels = desiredRoute.Labels

		if err := r.Update(ctx, existing); err != nil {
			return errors.Wrap(err, "failed to update HTTPRoute")
		}
	}

	// Delete orphaned routes.
	for name, existing := range existingMap {
		if _, wanted := desired[name]; !wanted {
			slog.InfoContext(ctx, "Deleting orphaned HTTPRoute", "name", name)

			if err := r.Delete(ctx, existing); err != nil && !apierrors.IsNotFound(err) {
				return errors.Wrap(err, "failed to delete orphaned HTTPRoute")
			}
		}
	}

	return nil
}

// isOwnedBy checks if a route is owned by the given server.
func isOwnedBy(route *gatewayv1.HTTPRoute, server *mcv1beta1.PaperMCServer) bool {
	for _, ref := range route.OwnerReferences {
		if ref.UID == server.UID {
			return true
		}
	}

	return false
}

// findHTTPEndpoint finds an HTTP-protocol endpoint by name in a plugin.
func findHTTPEndpoint(plugin *mcv1beta1.Plugin, endpointName string) *mcv1beta1.PluginEndpoint {
	for i := range plugin.Spec.Endpoints {
		ep := &plugin.Spec.Endpoints[i]
		if ep.Name == endpointName && ep.Protocol == "HTTP" {
			return ep
		}
	}

	return nil
}

// buildHTTPRoute constructs the desired HTTPRoute for a plugin endpoint.
func (r *PaperMCServerReconciler) buildHTTPRoute(
	server *mcv1beta1.PaperMCServer,
	hr mcv1beta1.PluginHTTPRoute,
	port int32,
) *gatewayv1.HTTPRoute {
	parentRefs := convertParentRefs(server.Spec.Gateway.ParentRefs)
	hostname := gatewayv1.Hostname(hr.Hostname)
	backendPort := gatewayv1.PortNumber(port)

	rule := gatewayv1.HTTPRouteRule{
		BackendRefs: []gatewayv1.HTTPBackendRef{
			{
				BackendRef: gatewayv1.BackendRef{
					BackendObjectReference: gatewayv1.BackendObjectReference{
						Name: gatewayv1.ObjectName(server.Name),
						Port: &backendPort,
					},
				},
			},
		},
	}

	if hr.PathPrefix != "" {
		pathType := gatewayv1.PathMatchPathPrefix
		rule.Matches = []gatewayv1.HTTPRouteMatch{
			{
				Path: &gatewayv1.HTTPPathMatch{
					Type:  &pathType,
					Value: &hr.PathPrefix,
				},
			},
		}
	}

	labels := standardLabels(server.Name, "networking")
	labels["mc.k8s.lex.la/route-type"] = "http"

	routeName := fmt.Sprintf("%s-http-%s-%s", server.Name, hr.PluginName, hr.EndpointName)

	return &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      routeName,
			Namespace: server.Namespace,
			Labels:    labels,
		},
		Spec: gatewayv1.HTTPRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{
				ParentRefs: parentRefs,
			},
			Hostnames: []gatewayv1.Hostname{hostname},
			Rules:     []gatewayv1.HTTPRouteRule{rule},
		},
	}
}
