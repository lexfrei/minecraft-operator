package webui

import (
	"context"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	mck8slexlav1beta1 "github.com/lexfrei/minecraft-operator/api/v1beta1"
	"github.com/lexfrei/minecraft-operator/pkg/service"
	"github.com/lexfrei/minecraft-operator/pkg/webui/schema"
	"github.com/lexfrei/minecraft-operator/pkg/webui/templates"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// URL path actions.
	actionDelete   = "delete"
	actionApplyNow = "apply-now"
	actionEdit     = "edit"
)

// parseResourcePathAction extracts resource name, action, and namespace from a URL path.
// Returns (resourceName, namespace, ok).
func parseResourcePathAction(r *http.Request, prefix, expectedAction string) (string, string, bool) {
	path := r.URL.Path
	parts := strings.Split(strings.TrimPrefix(path, prefix), "/")
	if len(parts) < 2 || parts[1] != expectedAction {
		return "", "", false
	}
	resourceName := parts[0]
	namespace := r.URL.Query().Get("namespace")
	if resourceName == "" || namespace == "" {
		return "", "", false
	}
	return resourceName, namespace, true
}

// handleResourceDelete is a generic delete handler for Kubernetes resources.
func handleResourceDelete[T client.Object](
	s *Server,
	w http.ResponseWriter,
	r *http.Request,
	pathPrefix string,
	resourceType string,
	newResource func() T,
	redirectURL string,
) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	name, namespace, ok := parseResourcePathAction(r, pathPrefix, actionDelete)
	if !ok {
		http.Error(w, fmt.Sprintf("Missing %s name or namespace", resourceType), http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	resource := newResource()

	if err := s.client.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, resource); err != nil {
		http.Error(w, fmt.Sprintf("Failed to get %s: %v", resourceType, err), http.StatusNotFound)
		return
	}
	if err := s.client.Delete(ctx, resource); err != nil {
		http.Error(w, fmt.Sprintf("Failed to delete %s: %v", resourceType, err), http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, redirectURL, http.StatusSeeOther)
}

// fetchDashboardData retrieves all PaperMCServer instances, optionally filtered by namespace.
//
//nolint:dupl // Similar structure to fetchPluginListData is intentional for consistency
func (s *Server) fetchDashboardData(ctx context.Context, filterNamespace string) (templates.DashboardData, error) {
	// Get all servers (unfiltered) to collect namespaces
	allServers, err := s.serverService.ListServers(ctx, "")
	if err != nil {
		return templates.DashboardData{}, err
	}

	// Collect unique namespaces from all servers
	namespaceSet := make(map[string]bool)
	for _, server := range allServers {
		namespaceSet[server.Namespace] = true
	}

	// Get filtered servers if namespace filter is set
	var serversData []service.ServerData
	if filterNamespace != "" {
		serversData, err = s.serverService.ListServers(ctx, filterNamespace)
		if err != nil {
			return templates.DashboardData{}, err
		}
	} else {
		serversData = allServers
	}

	// Convert to template summaries
	servers := make([]templates.ServerSummary, 0, len(serversData))
	for _, data := range serversData {
		servers = append(servers, serverDataToSummary(data))
	}

	// Convert namespaceSet to slice
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
	serverData, err := s.serverService.GetServerByName(ctx, serverName)
	if err != nil {
		return templates.ServerDetailData{}, err
	}

	return serverDataToDetail(serverData), nil
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
//
//nolint:dupl // Similar structure to fetchDashboardData is intentional for consistency
func (s *Server) fetchPluginListData(ctx context.Context, filterNamespace string) (templates.PluginListData, error) {
	// Get all plugins (unfiltered) to collect namespaces
	allPlugins, err := s.pluginService.ListPlugins(ctx, "")
	if err != nil {
		return templates.PluginListData{}, err
	}

	// Collect unique namespaces from all plugins
	namespaceSet := make(map[string]bool)
	for _, plugin := range allPlugins {
		namespaceSet[plugin.Namespace] = true
	}

	// Get filtered plugins if namespace filter is set
	var pluginsData []service.PluginData
	if filterNamespace != "" {
		pluginsData, err = s.pluginService.ListPlugins(ctx, filterNamespace)
		if err != nil {
			return templates.PluginListData{}, err
		}
	} else {
		pluginsData = allPlugins
	}

	// Convert to template items
	pluginItems := make([]templates.PluginListItem, 0, len(pluginsData))
	for _, data := range pluginsData {
		pluginItems = append(pluginItems, pluginDataToListItem(data))
	}

	// Convert namespaceSet to slice
	namespaces := make([]string, 0, len(namespaceSet))
	for ns := range namespaceSet {
		namespaces = append(namespaces, ns)
	}

	return templates.PluginListData{
		Plugins:           pluginItems,
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

// handlePluginCreate renders the schema-driven plugin creation form.
func (s *Server) handlePluginCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.renderSchemaForm(w, r, "PluginCreateRequest", schema.ModeCreate,
		"/api/v1/plugins", "/ui/plugins", "Create Plugin", nil)
}

// handlePluginDelete handles plugin deletion.
func (s *Server) handlePluginDelete(w http.ResponseWriter, r *http.Request) {
	handleResourceDelete(s, w, r, "/ui/plugin/", "plugin",
		func() *mck8slexlav1beta1.Plugin { return &mck8slexlav1beta1.Plugin{} },
		"/ui/plugins")
}

// handleServerCreate renders the schema-driven server creation form.
func (s *Server) handleServerCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.renderSchemaForm(w, r, "ServerCreateRequest", schema.ModeCreate,
		"/api/v1/servers", "/ui", "Create Server", nil)
}

// renderSchemaForm renders a schema-driven form page.
func (s *Server) renderSchemaForm(
	w http.ResponseWriter,
	r *http.Request,
	schemaName string,
	mode schema.FormMode,
	submitURL, backURL, title string,
	values map[string]any,
) {
	if s.schemaParser == nil {
		http.Error(w, "Schema parser not initialized", http.StatusInternalServerError)
		return
	}

	formSchema, err := s.schemaParser.ParseSchema(schemaName)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to parse schema: %v", err), http.StatusInternalServerError)
		return
	}

	formHTML := schema.RenderForm(formSchema, values, schema.RenderOptions{
		Mode:            mode,
		SubmitURL:       submitURL,
		SuccessRedirect: backURL,
	})

	data := templates.SchemaFormData{
		Title:    title,
		BackURL:  backURL,
		FormHTML: template.HTML(formHTML), //nolint:gosec // HTML is generated by our renderer, not user input
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	component := templates.SchemaFormPage(data)
	if err := component.Render(r.Context(), w); err != nil {
		http.Error(w, fmt.Sprintf("Failed to render form: %v", err), http.StatusInternalServerError)
	}
}

// handleApplyNow triggers immediate update application by setting annotation.
func (s *Server) handleApplyNow(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	serverName, namespace, ok := parseResourcePathAction(r, "/ui/server/", actionApplyNow)
	if !ok {
		http.Error(w, "Missing server name or namespace", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	if err := s.serverService.ApplyNow(ctx, namespace, serverName); err != nil {
		http.Error(w, fmt.Sprintf("Failed to apply now: %v", err), http.StatusInternalServerError)
		return
	}

	// Return HTMX response
	w.Header().Set("Content-Type", "text/html")
	_, _ = fmt.Fprintf(w, `<span style="color: green;">✓ Apply Now triggered</span>`)
}

// handleServerDelete handles server deletion.
func (s *Server) handleServerDelete(w http.ResponseWriter, r *http.Request) {
	handleResourceDelete(s, w, r, "/ui/server/", "server",
		func() *mck8slexlav1beta1.PaperMCServer { return &mck8slexlav1beta1.PaperMCServer{} },
		"/ui")
}

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
		case actionDelete:
			s.handlePluginDelete(w, r)
		case actionEdit:
			s.handlePluginEdit(w, r, parts[0])
		default:
			http.NotFound(w, r)
		}
		return
	}

	// No action — show plugin detail
	s.handlePluginDetailPage(w, r, parts[0])
}

// handleServerRoutes routes server-specific requests based on URL path.
func (s *Server) handleServerRoutes(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/ui/server/")
	parts := strings.Split(path, "/")

	if len(parts) < 1 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}

	// Route based on action
	if len(parts) >= 2 {
		switch parts[1] {
		case actionDelete:
			s.handleServerDelete(w, r)
			return
		case actionApplyNow:
			s.handleApplyNow(w, r)
			return
		case actionEdit:
			s.handleServerEdit(w, r, parts[0])
			return
		}
	}

	// Default: show server detail
	s.handleServerDetailPage(w, r, parts[0])
}

// handleServerEdit renders the schema-driven server edit form.
func (s *Server) handleServerEdit(w http.ResponseWriter, r *http.Request, serverName string) {
	namespace := r.URL.Query().Get("namespace")
	if namespace == "" {
		http.Error(w, "Missing namespace parameter", http.StatusBadRequest)
		return
	}

	serverData, err := s.serverService.GetServer(r.Context(), namespace, serverName)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get server: %v", err), http.StatusInternalServerError)
		return
	}

	values := serverDataToValues(serverData)
	submitURL := fmt.Sprintf("/api/v1/servers/%s/%s", namespace, serverName)
	s.renderSchemaForm(w, r, "ServerUpdateRequest", schema.ModeEdit,
		submitURL, "/ui", fmt.Sprintf("Edit Server: %s", serverName), values)
}

// handlePluginEdit renders the schema-driven plugin edit form.
func (s *Server) handlePluginEdit(w http.ResponseWriter, r *http.Request, pluginName string) {
	namespace := r.URL.Query().Get("namespace")
	if namespace == "" {
		http.Error(w, "Missing namespace parameter", http.StatusBadRequest)
		return
	}

	pluginData, err := s.pluginService.GetPlugin(r.Context(), namespace, pluginName)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get plugin: %v", err), http.StatusInternalServerError)
		return
	}

	values := pluginDataToValues(pluginData)
	submitURL := fmt.Sprintf("/api/v1/plugins/%s/%s", namespace, pluginName)
	s.renderSchemaForm(w, r, "PluginUpdateRequest", schema.ModeEdit,
		submitURL, "/ui/plugins", fmt.Sprintf("Edit Plugin: %s", pluginName), values)
}

// handlePluginDetailPage serves the plugin details page.
func (s *Server) handlePluginDetailPage(w http.ResponseWriter, r *http.Request, pluginName string) {
	if pluginName == "" {
		http.NotFound(w, r)
		return
	}

	ctx := r.Context()
	namespace := r.URL.Query().Get("namespace")

	if namespace == "" {
		http.Error(w, "Missing namespace parameter", http.StatusBadRequest)
		return
	}

	data, err := s.pluginService.GetPlugin(ctx, namespace, pluginName)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to fetch plugin: %v", err),
			http.StatusInternalServerError)
		return
	}

	detail := pluginDataToDetail(data)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	component := templates.PluginDetail(detail)
	if err := component.Render(ctx, w); err != nil {
		http.Error(w, fmt.Sprintf("Failed to render plugin detail: %v", err),
			http.StatusInternalServerError)
	}
}
