/*
Copyright 2026, Aleksei Sviridkin.

SPDX-License-Identifier: BSD-3-Clause
*/

package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"maps"
	"reflect"
	"strings"

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
	desired, issues := r.buildDesiredHTTPRoutes(ctx, server, matchedPlugins, gwEnabled)
	r.setHTTPRouteCondition(server, issues)

	existingMap, err := r.listOwnedHTTPRoutes(ctx, server)
	if err != nil {
		return err
	}

	if existingMap == nil {
		// HTTPRoute CRD not installed, skip.
		return nil
	}

	if err := r.applyHTTPRouteChanges(ctx, server, desired, existingMap); err != nil {
		return err
	}

	return r.deleteOrphanedHTTPRoutes(ctx, desired, existingMap)
}

const conditionTypeHTTPRouteConfigValid = "HTTPRouteConfigValid"

// setHTTPRouteCondition sets the HTTPRouteConfigValid condition based on build issues.
func (r *PaperMCServerReconciler) setHTTPRouteCondition(
	server *mcv1beta1.PaperMCServer,
	issues []string,
) {
	if server.Spec.Gateway == nil || len(server.Spec.Gateway.HTTPRoutes) == 0 {
		return
	}

	if len(issues) == 0 {
		r.setCondition(server, conditionTypeHTTPRouteConfigValid,
			metav1.ConditionTrue, "AllRoutesValid",
			"All httpRoutes reference valid plugins and HTTP endpoints")
		return
	}

	r.setCondition(server, conditionTypeHTTPRouteConfigValid,
		metav1.ConditionFalse, "InvalidRouteConfig",
		fmt.Sprintf("Invalid httpRoutes: %s", strings.Join(issues, "; ")))
}

// buildDesiredHTTPRoutes computes the set of HTTPRoutes that should exist for this server.
// Returns the desired routes and a list of issues for invalid httpRoutes entries.
func (r *PaperMCServerReconciler) buildDesiredHTTPRoutes(
	ctx context.Context,
	server *mcv1beta1.PaperMCServer,
	matchedPlugins []mcv1beta1.Plugin,
	gwEnabled bool,
) (map[string]gatewayv1.HTTPRoute, []string) {
	desired := make(map[string]gatewayv1.HTTPRoute)
	var issues []string

	if !gwEnabled || server.Spec.Gateway == nil {
		return desired, nil
	}

	pluginMap := make(map[string]*mcv1beta1.Plugin, len(matchedPlugins))
	for i := range matchedPlugins {
		pluginMap[matchedPlugins[i].Name] = &matchedPlugins[i]
	}

	for _, hr := range server.Spec.Gateway.HTTPRoutes {
		plugin, found := pluginMap[hr.PluginName]
		if !found {
			msg := fmt.Sprintf("plugin %q not matched to this server", hr.PluginName)
			slog.WarnContext(ctx, "HTTPRoute references unmatched plugin",
				"plugin", hr.PluginName, "server", server.Name)
			issues = append(issues, msg)

			continue
		}

		endpoint := findHTTPEndpoint(plugin, hr.EndpointName)
		if endpoint == nil {
			msg := fmt.Sprintf("plugin %q has no HTTP endpoint %q", hr.PluginName, hr.EndpointName)
			slog.WarnContext(ctx, "HTTPRoute references non-existent or non-HTTP endpoint",
				"plugin", hr.PluginName, "endpoint", hr.EndpointName)
			issues = append(issues, msg)

			continue
		}

		route := r.buildHTTPRoute(server, hr, endpoint.Port)
		desired[route.Name] = *route
	}

	return desired, issues
}

// listOwnedHTTPRoutes lists HTTPRoutes owned by this server.
// Returns nil map if HTTPRoute CRD is not installed.
func (r *PaperMCServerReconciler) listOwnedHTTPRoutes(
	ctx context.Context,
	server *mcv1beta1.PaperMCServer,
) (map[string]*gatewayv1.HTTPRoute, error) {
	var existingList gatewayv1.HTTPRouteList

	if err := r.List(ctx, &existingList,
		client.InNamespace(server.Namespace),
		client.MatchingLabels{"mc.k8s.lex.la/route-type": "http"},
	); err != nil {
		if meta.IsNoMatchError(err) {
			slog.DebugContext(ctx, "Gateway API HTTPRoute CRD not installed, skipping")
			return nil, nil //nolint:nilnil // nil map signals CRD not installed
		}

		return nil, errors.Wrap(err, "failed to list HTTPRoutes")
	}

	result := make(map[string]*gatewayv1.HTTPRoute)
	for i := range existingList.Items {
		route := &existingList.Items[i]
		if route.Labels["mc.k8s.lex.la/route-type"] == "http" && isOwnedBy(route, server) {
			result[route.Name] = route
		}
	}

	return result, nil
}

// applyHTTPRouteChanges creates or updates desired HTTPRoutes.
func (r *PaperMCServerReconciler) applyHTTPRouteChanges(
	ctx context.Context,
	server *mcv1beta1.PaperMCServer,
	desired map[string]gatewayv1.HTTPRoute,
	existing map[string]*gatewayv1.HTTPRoute,
) error {
	for name, desiredRoute := range desired {
		current, exists := existing[name]
		if !exists {
			slog.InfoContext(ctx, "Creating HTTPRoute", "name", name)

			route := desiredRoute
			if err := controllerutil.SetControllerReference(server, &route, r.Scheme); err != nil {
				return errors.Wrap(err, "failed to set owner reference on HTTPRoute")
			}

			if err := r.Create(ctx, &route); err != nil {
				return errors.Wrap(err, "failed to create HTTPRoute")
			}

			continue
		}

		if err := r.updateHTTPRouteIfChanged(ctx, server, current, desiredRoute); err != nil {
			return err
		}
	}

	return nil
}

// updateHTTPRouteIfChanged updates an existing HTTPRoute if its spec or labels differ.
func (r *PaperMCServerReconciler) updateHTTPRouteIfChanged(
	ctx context.Context,
	server *mcv1beta1.PaperMCServer,
	existing *gatewayv1.HTTPRoute,
	desired gatewayv1.HTTPRoute,
) error {
	ownerRefsBefore := len(existing.OwnerReferences)
	if err := controllerutil.SetControllerReference(server, existing, r.Scheme); err != nil {
		return errors.Wrap(err, "failed to set owner reference on HTTPRoute")
	}

	ownerRefsChanged := len(existing.OwnerReferences) != ownerRefsBefore
	if !ownerRefsChanged &&
		reflect.DeepEqual(existing.Spec, desired.Spec) &&
		maps.Equal(existing.Labels, desired.Labels) {
		return nil
	}

	slog.InfoContext(ctx, "Updating HTTPRoute", "name", existing.Name)
	existing.Spec = desired.Spec
	existing.Labels = desired.Labels

	return errors.Wrap(r.Update(ctx, existing), "failed to update HTTPRoute")
}

// deleteOrphanedHTTPRoutes removes HTTPRoutes that are no longer desired.
func (r *PaperMCServerReconciler) deleteOrphanedHTTPRoutes(
	ctx context.Context,
	desired map[string]gatewayv1.HTTPRoute,
	existing map[string]*gatewayv1.HTTPRoute,
) error {
	for name, route := range existing {
		if _, wanted := desired[name]; !wanted {
			slog.InfoContext(ctx, "Deleting orphaned HTTPRoute", "name", name)

			if err := r.Delete(ctx, route); err != nil && !apierrors.IsNotFound(err) {
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
	backendPort := port

	coreGroup := gatewayv1.Group("")
	serviceKind := gatewayv1.Kind("Service")

	rule := gatewayv1.HTTPRouteRule{
		BackendRefs: []gatewayv1.HTTPBackendRef{
			{
				BackendRef: gatewayv1.BackendRef{
					BackendObjectReference: gatewayv1.BackendObjectReference{
						Group: &coreGroup,
						Kind:  &serviceKind,
						Name:  gatewayv1.ObjectName(server.Name),
						Port:  &backendPort,
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

	routeName := truncateK8sName(
		fmt.Sprintf("%s-http-%s-%s", server.Name, hr.PluginName, hr.EndpointName),
	)

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

// maxK8sNameLength is the maximum length for Kubernetes resource names (RFC 1123 DNS subdomain).
const maxK8sNameLength = 253

// hashSuffixLength is the length of the hash suffix used when truncating names (8 hex chars + dash).
const hashSuffixLength = 9

// truncateK8sName truncates a Kubernetes resource name to maxK8sNameLength characters.
// If truncation is needed, a SHA-256 hash suffix is appended to preserve uniqueness.
func truncateK8sName(name string) string {
	if len(name) <= maxK8sNameLength {
		return name
	}

	hash := sha256.Sum256([]byte(name))
	suffix := hex.EncodeToString(hash[:4])

	return name[:maxK8sNameLength-hashSuffixLength] + "-" + suffix
}
