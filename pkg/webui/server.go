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
	mux.HandleFunc("/ui/server/", srv.handleServerDetail)
	mux.HandleFunc("/ui/events", srv.handleSSE)
	mux.HandleFunc("/ui/server/resolve", srv.handleServerResolve)
	mux.HandleFunc("/ui/server/status", srv.handleServerStatus)
	mux.HandleFunc("/ui/plugin/resolve", srv.handlePluginResolve)

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

// handleServerDetail serves the server details page.
func (s *Server) handleServerDetail(w http.ResponseWriter, r *http.Request) {
	// Extract server name from URL path
	serverName := r.URL.Path[len("/ui/server/"):]
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

	// Return progress indicator that starts polling for status
	w.Header().Set("Content-Type", "text/html")
	resolvingHTML := `<button disabled style="background-color: #ccc; color: #666; border: none; ` +
		`padding: 6px 12px; border-radius: 4px; font-size: 13px; cursor: not-allowed; ` +
		`white-space: nowrap;">⏳ Resolving...</button><script>setTimeout(function(){` +
		`htmx.ajax('GET','/ui/server/status?name=%s&namespace=%s',` +
		`{target:'#solver-status-%s',swap:'innerHTML'});},1000);</script>`
	_, _ = fmt.Fprintf(w, resolvingHTML, serverName, namespace, serverName)
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

	if serverName == "" || namespace == "" {
		http.Error(w, "Missing name or namespace parameter", http.StatusBadRequest)
		return
	}

	var server mck8slexlav1alpha1.PaperMCServer
	if err := s.client.Get(ctx, client.ObjectKey{Name: serverName, Namespace: namespace}, &server); err != nil {
		w.Header().Set("Content-Type", "text/html")
		_, _ = fmt.Fprintf(w, `<span style="color: red;">❌ Error</span>`)
		return
	}

	// Check if reconciliation is complete by examining status conditions
	hasError := false

	for _, cond := range server.Status.Conditions {
		if cond.Type == "Ready" {
			if cond.Status == "True" {
				// Reconciliation completed successfully
				w.Header().Set("Content-Type", "text/html")
				successHTML := `<span style="color: green;">✓ Resolved</span><script>setTimeout(function(){` +
					`var btn=document.querySelector('#resolve-button-%s');if(btn){` +
					`btn.innerHTML='<button hx-post="/ui/server/resolve?name=%s&namespace=%s" ` +
					`hx-swap="outerHTML" hx-target="#resolve-button-%s" ` +
					`style="background-color: var(--accent); color: white; border: none; ` +
					`padding: 6px 12px; border-radius: 4px; font-size: 13px; cursor: pointer; ` +
					`white-space: nowrap;">🔄 Resolve Server</button>';}},2000);</script>`
				_, _ = fmt.Fprintf(w, successHTML, serverName, serverName, namespace, serverName)
				return
			} else if cond.Reason == "ReconciliationFailed" {
				hasError = true
			}
		}
	}

	if hasError {
		w.Header().Set("Content-Type", "text/html")
		failedHTML := `<span style="color: red;">❌ Failed</span><script>setTimeout(function(){` +
			`var btn=document.querySelector('#resolve-button-%s');if(btn){` +
			`btn.innerHTML='<button hx-post="/ui/server/resolve?name=%s&namespace=%s" ` +
			`hx-swap="outerHTML" hx-target="#resolve-button-%s" ` +
			`style="background-color: var(--accent); color: white; border: none; ` +
			`padding: 6px 12px; border-radius: 4px; font-size: 13px; cursor: pointer; ` +
			`white-space: nowrap;">🔄 Resolve Server</button>';}},2000);</script>`
		_, _ = fmt.Fprintf(w, failedHTML, serverName, serverName, namespace, serverName)
		return
	}

	// Still reconciling - continue polling
	w.Header().Set("Content-Type", "text/html")
	workingHTML := `<span style="color: orange;" hx-get="/ui/server/status?name=%s&namespace=%s" ` +
		`hx-trigger="load delay:2s" hx-target="this" hx-swap="outerHTML">⏳ Working...</span>`
	_, _ = fmt.Fprintf(w, workingHTML, serverName, namespace)
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
