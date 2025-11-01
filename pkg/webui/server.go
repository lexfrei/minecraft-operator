package webui

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/cockroachdb/errors"
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
