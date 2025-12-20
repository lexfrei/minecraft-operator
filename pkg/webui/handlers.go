package webui

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
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

// fetchPluginListData retrieves all Plugins, optionally filtered by namespace.
func (s *Server) fetchPluginListData(ctx context.Context, filterNamespace string) (templates.PluginListData, error) {
	var pluginList mck8slexlav1alpha1.PluginList

	if err := s.client.List(ctx, &pluginList); err != nil {
		return templates.PluginListData{}, errors.Wrap(err, "failed to list Plugins")
	}

	// Collect unique namespaces
	namespaceSet := make(map[string]bool)
	plugins := make([]templates.PluginListItem, 0, len(pluginList.Items))

	for _, plugin := range pluginList.Items {
		namespaceSet[plugin.Namespace] = true

		// Skip if filtering by namespace and this plugin is not in the filtered namespace
		if filterNamespace != "" && plugin.Namespace != filterNamespace {
			continue
		}

		item := templates.PluginListItem{
			Name:           plugin.Name,
			Namespace:      plugin.Namespace,
			SourceType:     plugin.Spec.Source.Type,
			Project:        plugin.Spec.Source.Project,
			UpdateStrategy: plugin.Spec.UpdateStrategy,
			Version:        plugin.Spec.Version,
		}

		// For pinned strategy, show the pinned version as resolved
		if plugin.Spec.UpdateStrategy == "pinned" && plugin.Spec.Version != "" {
			item.ResolvedVersion = plugin.Spec.Version
		}

		// Count matched servers
		item.MatchedServers = len(plugin.Status.MatchedInstances)

		plugins = append(plugins, item)
	}

	// Convert namespaceSet to slice
	namespaces := make([]string, 0, len(namespaceSet))
	for ns := range namespaceSet {
		namespaces = append(namespaces, ns)
	}

	return templates.PluginListData{
		Plugins:           plugins,
		Namespaces:        namespaces,
		SelectedNamespace: filterNamespace,
	}, nil
}

// handlePluginList serves the plugin list page.
func (s *Server) handlePluginList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	filterNamespace := r.URL.Query().Get("namespace")

	data, err := s.fetchPluginListData(ctx, filterNamespace)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to fetch plugin list: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	component := templates.PluginList(data)
	if err := component.Render(ctx, w); err != nil {
		http.Error(w, fmt.Sprintf("Failed to render plugin list: %v", err), http.StatusInternalServerError)
	}
}

// handlePluginCreate handles plugin creation (GET for form, POST for creation).
func (s *Server) handlePluginCreate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if r.Method == http.MethodGet {
		s.showPluginCreateForm(w, ctx)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.processPluginCreateForm(w, r)
}

// showPluginCreateForm renders the plugin creation form.
func (s *Server) showPluginCreateForm(w http.ResponseWriter, ctx context.Context) {
	data := templates.PluginFormData{
		IsEdit:     false,
		Namespaces: s.getAvailableNamespaces(ctx),
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	component := templates.PluginForm(data)
	if err := component.Render(ctx, w); err != nil {
		http.Error(w, fmt.Sprintf("Failed to render form: %v", err), http.StatusInternalServerError)
	}
}

// processPluginCreateForm handles plugin creation form submission.
func (s *Server) processPluginCreateForm(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Failed to parse form", http.StatusBadRequest)
		return
	}

	plugin, err := s.parsePluginForm(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := s.client.Create(ctx, plugin); err != nil {
		http.Error(w, fmt.Sprintf("Failed to create plugin: %v", err), http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/ui/plugins", http.StatusSeeOther)
}

// parsePluginForm parses and validates the plugin form data.
func (s *Server) parsePluginForm(r *http.Request) (*mck8slexlav1alpha1.Plugin, error) {
	name := r.FormValue("name")
	namespace := r.FormValue("namespace")
	sourceType := r.FormValue("sourceType")
	project := r.FormValue("project")
	updateStrategy := r.FormValue("updateStrategy")

	if name == "" || namespace == "" || sourceType == "" || project == "" || updateStrategy == "" {
		return nil, errors.New("missing required fields")
	}

	if !isValidKubernetesName(name) {
		return nil, errors.New("invalid plugin name format")
	}

	plugin := &mck8slexlav1alpha1.Plugin{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: mck8slexlav1alpha1.PluginSpec{
			Source: mck8slexlav1alpha1.PluginSource{
				Type:    sourceType,
				Project: project,
			},
			UpdateStrategy: updateStrategy,
		},
	}

	if updateStrategy == "pinned" {
		if version := r.FormValue("version"); version != "" {
			plugin.Spec.Version = version
		}
	}

	if updateDelay := r.FormValue("updateDelay"); updateDelay != "" {
		if duration, err := time.ParseDuration(updateDelay); err == nil {
			plugin.Spec.UpdateDelay = &metav1.Duration{Duration: duration}
		}
	}

	return plugin, nil
}

// handlePluginDelete handles plugin deletion.
func (s *Server) handlePluginDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()

	// Extract plugin name from URL path: /ui/plugin/{name}/delete
	path := r.URL.Path
	parts := strings.Split(strings.TrimPrefix(path, "/ui/plugin/"), "/")
	if len(parts) < 2 || parts[1] != "delete" {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	pluginName := parts[0]
	namespace := r.URL.Query().Get("namespace")

	if pluginName == "" || namespace == "" {
		http.Error(w, "Missing plugin name or namespace", http.StatusBadRequest)
		return
	}

	// Get plugin
	var plugin mck8slexlav1alpha1.Plugin
	if err := s.client.Get(ctx, client.ObjectKey{Name: pluginName, Namespace: namespace}, &plugin); err != nil {
		http.Error(w, fmt.Sprintf("Failed to get plugin: %v", err), http.StatusNotFound)
		return
	}

	// Delete plugin
	if err := s.client.Delete(ctx, &plugin); err != nil {
		http.Error(w, fmt.Sprintf("Failed to delete plugin: %v", err), http.StatusInternalServerError)
		return
	}

	// Redirect to plugin list
	http.Redirect(w, r, "/ui/plugins", http.StatusSeeOther)
}

// handleApplyNow triggers immediate update application by setting annotation.
func (s *Server) handleApplyNow(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()

	// Extract server name from URL path: /ui/server/{name}/apply-now
	path := r.URL.Path
	parts := strings.Split(strings.TrimPrefix(path, "/ui/server/"), "/")
	if len(parts) < 2 || parts[1] != "apply-now" {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	serverName := parts[0]
	namespace := r.URL.Query().Get("namespace")

	if serverName == "" || namespace == "" {
		http.Error(w, "Missing server name or namespace", http.StatusBadRequest)
		return
	}

	// Get server
	var server mck8slexlav1alpha1.PaperMCServer
	if err := s.client.Get(ctx, client.ObjectKey{Name: serverName, Namespace: namespace}, &server); err != nil {
		http.Error(w, fmt.Sprintf("Failed to get server: %v", err), http.StatusNotFound)
		return
	}

	// Set apply-now annotation with current Unix timestamp
	if server.Annotations == nil {
		server.Annotations = make(map[string]string)
	}
	server.Annotations[annotationApplyNow] = fmt.Sprintf("%d", time.Now().Unix())

	// Update server
	if err := s.client.Update(ctx, &server); err != nil {
		http.Error(w, fmt.Sprintf("Failed to update server: %v", err), http.StatusInternalServerError)
		return
	}

	// Return HTMX response
	w.Header().Set("Content-Type", "text/html")
	_, _ = fmt.Fprintf(w, `<span style="color: green;">✓ Apply Now triggered</span>`)
}

// getAvailableNamespaces returns list of namespaces that have plugins or servers.
func (s *Server) getAvailableNamespaces(ctx context.Context) []string {
	namespaceSet := make(map[string]bool)

	// Get namespaces from plugins
	var pluginList mck8slexlav1alpha1.PluginList
	if err := s.client.List(ctx, &pluginList); err == nil {
		for _, p := range pluginList.Items {
			namespaceSet[p.Namespace] = true
		}
	}

	// Get namespaces from servers
	var serverList mck8slexlav1alpha1.PaperMCServerList
	if err := s.client.List(ctx, &serverList); err == nil {
		for _, s := range serverList.Items {
			namespaceSet[s.Namespace] = true
		}
	}

	// Always include default namespace
	namespaceSet["default"] = true

	namespaces := make([]string, 0, len(namespaceSet))
	for ns := range namespaceSet {
		namespaces = append(namespaces, ns)
	}

	return namespaces
}

// isValidKubernetesName checks if a name is valid for Kubernetes resources.
func isValidKubernetesName(name string) bool {
	if name == "" || len(name) > 253 {
		return false
	}

	// Must start and end with alphanumeric
	if !isAlphanumeric(rune(name[0])) || !isAlphanumeric(rune(name[len(name)-1])) {
		return false
	}

	// Can only contain alphanumeric, '-', and '.'
	for _, ch := range name {
		if !isAlphanumeric(ch) && ch != '-' && ch != '.' {
			return false
		}
	}

	return true
}

// isAlphanumeric checks if a rune is alphanumeric.
func isAlphanumeric(ch rune) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9')
}

// annotationApplyNow is the annotation key for immediate update trigger.
const annotationApplyNow = "mc.k8s.lex.la/apply-now"

// handlePluginRoutes routes plugin-specific requests based on URL path.
func (s *Server) handlePluginRoutes(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/ui/plugin/")
	parts := strings.Split(path, "/")

	if len(parts) < 1 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}

	// Route based on action
	if len(parts) >= 2 {
		switch parts[1] {
		case "delete":
			s.handlePluginDelete(w, r)
			return
		case "apply-now":
			s.handleApplyNow(w, r)
			return
		}
	}

	// Default: show plugin detail or 404
	http.NotFound(w, r)
}
