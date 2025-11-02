package webui

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/cockroachdb/errors"
	mck8slexlav1alpha1 "github.com/lexfrei/minecraft-operator/api/v1alpha1"
	"github.com/lexfrei/minecraft-operator/pkg/webui/templates"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	statusRunning  = "running"
	statusUpdating = "updating"
	statusUnknown  = "unknown"
)

// fetchDashboardData retrieves all PaperMCServer instances, optionally filtered by namespace.
func (s *Server) fetchDashboardData(ctx context.Context, filterNamespace string) (templates.DashboardData, error) {
	var serverList mck8slexlav1alpha1.PaperMCServerList

	// List from all namespaces to collect available namespaces
	if err := s.client.List(ctx, &serverList); err != nil {
		return templates.DashboardData{}, errors.Wrap(err, "failed to list PaperMCServers")
	}

	// Collect unique namespaces
	namespaceSet := make(map[string]bool)
	servers := make([]templates.ServerSummary, 0, len(serverList.Items))

	for _, server := range serverList.Items {
		namespaceSet[server.Namespace] = true

		// Skip if filtering by namespace and this server is not in the filtered namespace
		if filterNamespace != "" && server.Namespace != filterNamespace {
			continue
		}

		summary := templates.ServerSummary{
			Name:                server.Name,
			Namespace:           server.Namespace,
			CurrentPaperVersion: formatVersionWithBuild(server.Status.CurrentVersion, server.Status.CurrentBuild),
			Strategy:            server.Spec.UpdateStrategy,
			PluginCount:         len(server.Status.Plugins),
			Status:              determineServerStatus(&server),
			Plugins:             buildPluginSummaries(server.Status.Plugins),
			Labels:              server.Labels,
		}

		// Check actual StatefulSet status if condition is stale
		if summary.Status == statusUnknown {
			summary.Status = s.checkStatefulSetStatus(ctx, server.Name, server.Namespace)
		}

		if server.Status.AvailableUpdate != nil {
			summary.AvailableUpdate = formatVersionWithBuild(
				server.Status.AvailableUpdate.Version,
				server.Status.AvailableUpdate.Build,
			)

			// Build update plan
			summary.UpdatePlan = buildUpdatePlan(&server)
		}

		if server.Spec.UpdateSchedule.MaintenanceWindow.Enabled {
			summary.NextMaintenance = formatCronSchedule(server.Spec.UpdateSchedule.MaintenanceWindow.Cron)
		}

		servers = append(servers, summary)
	}

	// Convert namespaceSet to sorted slice
	namespaces := make([]string, 0, len(namespaceSet))
	for ns := range namespaceSet {
		namespaces = append(namespaces, ns)
	}

	return templates.DashboardData{
		Servers:           servers,
		Namespaces:        namespaces,
		SelectedNamespace: filterNamespace,
	}, nil
}

// fetchServerDetailData retrieves detailed information for a specific PaperMCServer.
func (s *Server) fetchServerDetailData(ctx context.Context, serverName string) (templates.ServerDetailData, error) {
	// List all servers to find the one with matching name
	var serverList mck8slexlav1alpha1.PaperMCServerList
	if err := s.client.List(ctx, &serverList); err != nil {
		return templates.ServerDetailData{}, errors.Wrap(err, "failed to list PaperMCServers")
	}

	// Find server by name across all namespaces
	var server *mck8slexlav1alpha1.PaperMCServer
	for i := range serverList.Items {
		if serverList.Items[i].Name == serverName {
			server = &serverList.Items[i]
			break
		}
	}

	if server == nil {
		return templates.ServerDetailData{}, errors.Newf("PaperMCServer %q not found", serverName)
	}

	data := templates.ServerDetailData{
		Name:                server.Name,
		Namespace:           server.Namespace,
		CurrentPaperVersion: formatVersionWithBuild(server.Status.CurrentVersion, server.Status.CurrentBuild),
		DesiredPaperVersion: formatVersionWithBuild(server.Status.DesiredVersion, server.Status.DesiredBuild),
		Plugins:             make([]templates.PluginInfo, 0, len(server.Status.Plugins)),
		Labels:              server.Labels,
	}

	if server.Status.AvailableUpdate != nil {
		data.AvailableUpdate = formatVersionWithBuild(
			server.Status.AvailableUpdate.Version,
			server.Status.AvailableUpdate.Build,
		)
	}

	// Fetch plugin information
	data.Plugins = s.fetchPluginInfo(ctx, server.Status.Plugins, server.Namespace)

	// Maintenance schedule
	if server.Spec.UpdateSchedule.CheckCron != "" {
		data.MaintenanceSchedule.CheckCron = server.Spec.UpdateSchedule.CheckCron
	}
	if server.Spec.UpdateSchedule.MaintenanceWindow.Enabled {
		data.MaintenanceSchedule.WindowCron = server.Spec.UpdateSchedule.MaintenanceWindow.Cron
		data.MaintenanceSchedule.NextWindow = formatCronSchedule(server.Spec.UpdateSchedule.MaintenanceWindow.Cron)
	}

	// Last update information
	if server.Status.LastUpdate != nil {
		status := "failed"
		if server.Status.LastUpdate.Successful {
			status = "success"
		}
		data.LastUpdate = templates.UpdateInfo{
			Timestamp: formatTimestamp(server.Status.LastUpdate.AppliedAt),
			Status:    status,
			Message:   fmt.Sprintf("Updated from %s", server.Status.LastUpdate.PreviousVersion),
		}
	}

	return data, nil
}

// renderDashboard renders the dashboard template.
func renderDashboard(w io.Writer, data templates.DashboardData) error {
	component := templates.Dashboard(data)
	if err := component.Render(context.Background(), w); err != nil {
		return errors.Wrap(err, "failed to render dashboard template")
	}
	return nil
}

// renderServerDetail renders the server detail template.
func renderServerDetail(w io.Writer, data templates.ServerDetailData) error {
	component := templates.ServerDetail(data)
	if err := component.Render(context.Background(), w); err != nil {
		return errors.Wrap(err, "failed to render server detail template")
	}
	return nil
}

// determineServerStatus determines the display status of a server.
func determineServerStatus(server *mck8slexlav1alpha1.PaperMCServer) string {
	// Check StatefulSet readiness via conditions
	for _, condition := range server.Status.Conditions {
		if condition.Type == "StatefulSetReady" {
			if condition.Status == metav1.ConditionTrue {
				return statusRunning
			}
			if condition.Status == metav1.ConditionFalse && condition.Reason == "Updating" {
				return statusUpdating
			}
		}
	}

	// Fallback: check if CurrentVersion is populated
	if server.Status.CurrentVersion != "" {
		return statusRunning
	}

	return statusUnknown
}

// checkStatefulSetStatus checks the actual StatefulSet status in real-time.
func (s *Server) checkStatefulSetStatus(ctx context.Context, name, namespace string) string {
	var sts appsv1.StatefulSet
	if err := s.client.Get(ctx, client.ObjectKey{
		Name:      name,
		Namespace: namespace,
	}, &sts); err != nil {
		return statusUnknown
	}

	// Check if StatefulSet is ready
	if sts.Status.ReadyReplicas > 0 && sts.Status.ReadyReplicas == sts.Status.Replicas {
		return statusRunning
	}

	// Check if updating
	if sts.Status.CurrentRevision != sts.Status.UpdateRevision {
		return statusUpdating
	}

	return statusUnknown
}

// formatPluginCompatibility formats plugin compatibility status.
func formatPluginCompatibility(compatible bool) string {
	if compatible {
		return "compatible"
	}
	return "incompatible"
}

// formatCronSchedule formats a cron schedule for display.
func formatCronSchedule(cronExpr string) string {
	if cronExpr == "" {
		return "Not scheduled"
	}

	// Parse common cron patterns into human-readable format
	// Format: minute hour day month weekday
	parts := splitCronExpression(cronExpr)
	if len(parts) != 5 {
		return fmt.Sprintf("Cron: %s", cronExpr)
	}

	minute, hour, day, month, weekday := parts[0], parts[1], parts[2], parts[3], parts[4]

	// Daily patterns: "* * * * *" where day and month are wildcards
	if day == "*" && month == "*" && weekday == "*" {
		if minute == "0" && hour != "*" {
			return fmt.Sprintf("Daily at %s:00", hour)
		}
		return fmt.Sprintf("Daily at %s:%s", hour, minute)
	}

	// Weekly patterns: specific weekday
	if day == "*" && month == "*" && weekday != "*" {
		weekdayName := getWeekdayName(weekday)
		if weekdayName != "" {
			if minute == "0" {
				return fmt.Sprintf("Every %s at %s:00", weekdayName, hour)
			}
			return fmt.Sprintf("Every %s at %s:%s", weekdayName, hour, minute)
		}
	}

	// Fallback for complex expressions
	return fmt.Sprintf("Cron: %s", cronExpr)
}

// splitCronExpression splits a cron expression into its components.
func splitCronExpression(expr string) []string {
	parts := make([]string, 0, 5)
	current := ""
	for _, ch := range expr {
		if ch == ' ' || ch == '\t' {
			if current != "" {
				parts = append(parts, current)
				current = ""
			}
		} else {
			current += string(ch)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}
	return parts
}

// getWeekdayName returns the human-readable weekday name for a cron weekday value.
func getWeekdayName(weekday string) string {
	weekdays := map[string]string{
		"0": "Sunday",
		"1": "Monday",
		"2": "Tuesday",
		"3": "Wednesday",
		"4": "Thursday",
		"5": "Friday",
		"6": "Saturday",
		"7": "Sunday", // Sunday can be 0 or 7 in cron
	}
	return weekdays[weekday]
}

// formatVersionWithBuild formats a version with build number.
func formatVersionWithBuild(version string, build int) string {
	if version == "" {
		return "Unknown"
	}
	if build > 0 {
		return fmt.Sprintf("%s-%d", version, build)
	}
	return version
}

// formatTimestamp formats a metav1.Time for display.
func formatTimestamp(t metav1.Time) string {
	if t.IsZero() {
		return "Never"
	}
	return t.Format(time.RFC1123)
}

// fetchPluginInfo fetches plugin details from Plugin CRDs.
func (s *Server) fetchPluginInfo(
	ctx context.Context,
	pluginStatuses []mck8slexlav1alpha1.ServerPluginStatus,
	namespace string,
) []templates.PluginInfo {
	plugins := make([]templates.PluginInfo, 0, len(pluginStatuses))

	for _, pluginStatus := range pluginStatuses {
		var plugin mck8slexlav1alpha1.Plugin
		if err := s.client.Get(ctx, client.ObjectKey{
			Name:      pluginStatus.PluginRef.Name,
			Namespace: namespace,
		}, &plugin); err != nil {
			continue
		}

		pluginInfo := templates.PluginInfo{
			Name:            plugin.Name,
			Project:         plugin.Spec.Source.Project,
			ResolvedVersion: pluginStatus.ResolvedVersion,
			Status:          formatPluginCompatibility(pluginStatus.Compatible),
		}

		plugins = append(plugins, pluginInfo)
	}

	return plugins
}

// buildPluginSummaries builds a list of plugin summaries from server plugin status.
func buildPluginSummaries(pluginStatuses []mck8slexlav1alpha1.ServerPluginStatus) []templates.PluginSummary {
	plugins := make([]templates.PluginSummary, 0, len(pluginStatuses))

	for _, ps := range pluginStatuses {
		plugins = append(plugins, templates.PluginSummary{
			Name:           ps.PluginRef.Name,
			CurrentVersion: "", // Not tracked yet
			DesiredVersion: ps.ResolvedVersion,
		})
	}

	return plugins
}

// buildUpdatePlan builds an update plan from server status.
func buildUpdatePlan(server *mck8slexlav1alpha1.PaperMCServer) *templates.UpdatePlan {
	plan := &templates.UpdatePlan{}

	// Check if Paper version is changing
	if server.Status.AvailableUpdate != nil {
		currentPaper := formatVersionWithBuild(server.Status.CurrentVersion, server.Status.CurrentBuild)
		availablePaper := formatVersionWithBuild(
			server.Status.AvailableUpdate.Version,
			server.Status.AvailableUpdate.Build,
		)

		if currentPaper != availablePaper && availablePaper != "Unknown" {
			plan.PaperUpdate = fmt.Sprintf("%s → %s", currentPaper, availablePaper)
		}
	}

	// Build plugin updates - check for plugins needing installation or update
	pluginUpdates := make([]templates.PluginUpdate, 0)

	for _, pluginStatus := range server.Status.Plugins {
		// Use resolvedVersion as target
		targetVer := pluginStatus.ResolvedVersion

		// Skip if no target version
		if targetVer == "" {
			continue
		}

		// For now, always show as "Not installed → version" since we don't track current version yet
		// TODO: Track actual installed plugin versions
		pluginUpdates = append(pluginUpdates, templates.PluginUpdate{
			Name:        pluginStatus.PluginRef.Name,
			FromVersion: "Not installed",
			ToVersion:   targetVer,
		})
	}

	plan.PluginUpdates = pluginUpdates

	// Add maintenance window information
	if server.Spec.UpdateSchedule.MaintenanceWindow.Enabled {
		plan.AppliesAt = formatCronSchedule(server.Spec.UpdateSchedule.MaintenanceWindow.Cron)
	}

	// Only return plan if there are actual changes
	if plan.PaperUpdate == "" && len(plan.PluginUpdates) == 0 {
		return nil
	}

	return plan
}

// startWatching starts watching Kubernetes resources and sends SSE updates.
func (s *Server) startWatching(ctx context.Context) {
	// This is a simplified implementation.
	// In production, use controller-runtime informers or watch API.
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Fetch current state and compare with previous
			// Send SSE event if changed
			s.sse.Broadcast([]byte("data: update\n\n"))
		}
	}
}
