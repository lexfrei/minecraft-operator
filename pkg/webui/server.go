package webui

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/cockroachdb/errors"
	mck8slexlav1alpha1 "github.com/lexfrei/minecraft-operator/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// Server represents the Web UI HTTP server.
type Server struct {
	client    client.Client
	namespace string
	server    *http.Server
	sse       *SSEBroker
}

// NewServer creates a new Web UI server instance.
func NewServer(k8sClient client.Client, namespace string, bindAddress string) *Server {
	sse := NewSSEBroker()
	srv := &Server{
		client:    k8sClient,
		namespace: namespace,
		sse:       sse,
	}

	mux := http.NewServeMux()

	// Register routes
	mux.HandleFunc("/ui", srv.handleDashboard)
	mux.HandleFunc("/ui/server/", srv.handleServerRoutes)
	mux.HandleFunc("/ui/events", srv.handleSSE)
	mux.HandleFunc("/ui/server/resolve", srv.handleServerResolve)
	mux.HandleFunc("/ui/server/status", srv.handleServerStatus)
	mux.HandleFunc("/ui/plugin/resolve", srv.handlePluginResolve)

	// Plugin CRUD routes
	mux.HandleFunc("/ui/plugins", srv.handlePluginList)
	mux.HandleFunc("/ui/plugin/new", srv.handlePluginCreate)
	mux.HandleFunc("/ui/plugin/", srv.handlePluginRoutes)

	srv.server = &http.Server{
		Addr:              bindAddress,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	return srv
}

// Start starts the Web UI HTTP server in a goroutine.
func (s *Server) Start(ctx context.Context) error {
	logger := log.FromContext(ctx).WithName("webui")

	// Start SSE broker
	go s.sse.Start(ctx)

	// Start watching Kubernetes resources
	go s.startWatching(ctx)

	// Start HTTP server in goroutine
	go func() {
		logger.Info("starting web ui server", "address", s.server.Addr)
		if err := s.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error(err, "web ui server error")
		}
	}()

	// Handle shutdown
	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.server.Shutdown(shutdownCtx); err != nil {
		return errors.Wrap(err, "failed to shutdown web ui server")
	}

	return nil
}

// GetSSEBroker returns the SSE broker for testing.
func (s *Server) GetSSEBroker() *SSEBroker {
	return s.sse
}

// handleDashboard serves the dashboard page with list of all PaperMCServers.
func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/ui" {
		http.NotFound(w, r)
		return
	}

	ctx := r.Context()

	// Get namespace filter from query parameter
	filterNamespace := r.URL.Query().Get("namespace")

	data, err := s.fetchDashboardData(ctx, filterNamespace)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to fetch dashboard data: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := renderDashboard(w, data); err != nil {
		http.Error(w, fmt.Sprintf("Failed to render dashboard: %v", err), http.StatusInternalServerError)
	}
}

// handleServerDetailPage serves the server details page.
func (s *Server) handleServerDetailPage(w http.ResponseWriter, r *http.Request, serverName string) {
	if serverName == "" {
		http.NotFound(w, r)
		return
	}

	ctx := r.Context()
	data, err := s.fetchServerDetailData(ctx, serverName)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to fetch server details: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := renderServerDetail(w, data); err != nil {
		http.Error(w, fmt.Sprintf("Failed to render server details: %v", err), http.StatusInternalServerError)
	}
}

// handleSSE serves Server-Sent Events endpoint for real-time updates.
func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	s.sse.ServeHTTP(w, r)
}

// handleServerResolve triggers server reconciliation by adding an annotation.
func (s *Server) handleServerResolve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()
	serverName := r.URL.Query().Get("name")
	namespace := r.URL.Query().Get("namespace")

	if serverName == "" || namespace == "" {
		http.Error(w, "Missing name or namespace parameter", http.StatusBadRequest)
		return
	}

	if err := s.triggerServerReconciliation(ctx, serverName, namespace); err != nil {
		http.Error(w, fmt.Sprintf("Failed to trigger reconciliation: %v", err), http.StatusInternalServerError)
		return
	}

	// Return disabled button and trigger status polling
	w.Header().Set("Content-Type", "text/html")
	buttonHTML := `<button disabled style="background-color: #ccc; color: #666; border: none; ` +
		`padding: 6px 12px; border-radius: 4px; font-size: 13px; cursor: not-allowed; ` +
		`white-space: nowrap;">⏳ Resolving...</button>` +
		`<div hx-get="/ui/server/status?name=%s&namespace=%s&attempt=1" ` +
		`hx-trigger="load delay:1s" hx-target="#solver-status-%s" hx-swap="innerHTML"></div>`
	_, _ = fmt.Fprintf(w, buttonHTML, serverName, namespace, serverName)
}

// solverStatusInfo holds parsed solver status from conditions.
type solverStatusInfo struct {
	solverRunning   bool
	solverCompleted bool
	solverFailed    bool
	readySuccess    bool
	readyFailed     bool
}

// parseSolverStatus extracts solver status from server conditions.
func parseSolverStatus(server *mck8slexlav1alpha1.PaperMCServer) solverStatusInfo {
	var info solverStatusInfo
	for _, cond := range server.Status.Conditions {
		if cond.Type == "SolverRunning" {
			info.solverRunning = (cond.Status == "True")
			if cond.Reason == "SolverCompleted" {
				info.solverCompleted = true
			}
			if cond.Reason == "SolverFailed" {
				info.solverFailed = true
			}
		}
		if cond.Type == "Ready" {
			info.readySuccess = (cond.Status == "True")
			info.readyFailed = (cond.Reason == "ReconciliationFailed")
		}
	}
	return info
}

// handleServerStatus returns current solver status for a server.
func (s *Server) handleServerStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()
	serverName := r.URL.Query().Get("name")
	namespace := r.URL.Query().Get("namespace")
	attemptStr := r.URL.Query().Get("attempt")

	if serverName == "" || namespace == "" {
		http.Error(w, "Missing name or namespace parameter", http.StatusBadRequest)
		return
	}

	// Parse attempt number for timeout
	attempt := 1
	if attemptStr != "" {
		_, _ = fmt.Sscanf(attemptStr, "%d", &attempt)
	}

	// Timeout after 30 attempts (60 seconds with 2s delay)
	if attempt > 30 {
		w.Header().Set("Content-Type", "text/html")
		_, _ = fmt.Fprintf(w, `<span style="color: orange;">⏱ Timeout - check logs</span>`)
		return
	}

	var server mck8slexlav1alpha1.PaperMCServer
	if err := s.client.Get(ctx, client.ObjectKey{Name: serverName, Namespace: namespace}, &server); err != nil {
		w.Header().Set("Content-Type", "text/html")
		_, _ = fmt.Fprintf(w, `<span style="color: red;">❌ Error</span>`)
		return
	}

	status := parseSolverStatus(&server)
	w.Header().Set("Content-Type", "text/html")

	s.renderSolverStatus(w, status, serverName, namespace, attempt)
}

// renderSolverStatus renders the appropriate HTML based on solver status.
func (s *Server) renderSolverStatus(
	w http.ResponseWriter,
	status solverStatusInfo,
	serverName, namespace string,
	attempt int,
) {
	nextAttempt := attempt + 1

	// Solver is actively running
	if status.solverRunning {
		statusHTML := `<span style="color: orange;" hx-get="/ui/server/status?name=%s&namespace=%s&attempt=%d" ` +
			`hx-trigger="load delay:2s" hx-target="this" hx-swap="outerHTML">⏳ Running solver...</span>`
		_, _ = fmt.Fprintf(w, statusHTML, serverName, namespace, nextAttempt)
		return
	}

	// Solver failed
	if status.solverFailed {
		_, _ = fmt.Fprintf(w, `<span style="color: red;">❌ Solver failed</span>`)
		return
	}

	// Solver completed and reconciliation succeeded
	if status.solverCompleted && status.readySuccess {
		_, _ = fmt.Fprintf(w, `<span style="color: green;">✓ Resolved</span>`)
		return
	}

	// Reconciliation failed
	if status.readyFailed {
		_, _ = fmt.Fprintf(w, `<span style="color: red;">❌ Failed</span>`)
		return
	}

	// Still working - continue polling
	statusHTML := `<span style="color: orange;" hx-get="/ui/server/status?name=%s&namespace=%s&attempt=%d" ` +
		`hx-trigger="load delay:2s" hx-target="this" hx-swap="outerHTML">⏳ Working...</span>`
	_, _ = fmt.Fprintf(w, statusHTML, serverName, namespace, nextAttempt)
}

// handlePluginResolve triggers plugin reconciliation by adding an annotation.
func (s *Server) handlePluginResolve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()
	pluginName := r.URL.Query().Get("name")
	namespace := r.URL.Query().Get("namespace")

	if pluginName == "" || namespace == "" {
		http.Error(w, "Missing name or namespace parameter", http.StatusBadRequest)
		return
	}

	if err := s.triggerPluginReconciliation(ctx, pluginName, namespace); err != nil {
		http.Error(w, fmt.Sprintf("Failed to trigger reconciliation: %v", err), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintf(w, "Plugin %s reconciliation triggered", pluginName)
}

// triggerPluginReconciliation triggers plugin reconciliation by adding an annotation.
func (s *Server) triggerPluginReconciliation(ctx context.Context, name, namespace string) error {
	var plugin mck8slexlav1alpha1.Plugin

	// Get plugin from cluster
	if err := s.client.Get(ctx, client.ObjectKey{
		Name:      name,
		Namespace: namespace,
	}, &plugin); err != nil {
		return errors.Wrap(err, "failed to get plugin")
	}

	// Add reconciliation trigger annotation
	if plugin.Annotations == nil {
		plugin.Annotations = make(map[string]string)
	}
	plugin.Annotations["mc.k8s.lex.la/reconcile"] = fmt.Sprintf("%d", time.Now().Unix())

	if err := s.client.Update(ctx, &plugin); err != nil {
		return errors.Wrap(err, "failed to update plugin")
	}

	return nil
}

// triggerServerReconciliation triggers server reconciliation by adding an annotation.
func (s *Server) triggerServerReconciliation(ctx context.Context, name, namespace string) error {
	var server mck8slexlav1alpha1.PaperMCServer

	// Get server from cluster
	if err := s.client.Get(ctx, client.ObjectKey{
		Name:      name,
		Namespace: namespace,
	}, &server); err != nil {
		return errors.Wrap(err, "failed to get server")
	}

	// Add reconciliation trigger annotation
	if server.Annotations == nil {
		server.Annotations = make(map[string]string)
	}
	server.Annotations["mc.k8s.lex.la/reconcile"] = fmt.Sprintf("%d", time.Now().Unix())

	if err := s.client.Update(ctx, &server); err != nil {
		return errors.Wrap(err, "failed to update server")
	}

	return nil
}
