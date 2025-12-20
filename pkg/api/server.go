package api

import (
	"net/http"

	"github.com/lexfrei/minecraft-operator/api/openapi/generated"
	"github.com/lexfrei/minecraft-operator/pkg/service"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Server handles REST API requests.
type Server struct {
	serverService    *service.ServerService
	pluginService    *service.PluginService
	namespaceService *service.NamespaceService
	versionInfo      VersionInfo
}

// VersionInfo contains version information about the operator.
type VersionInfo struct {
	Version   string
	GitCommit string
	BuildDate string
	GoVersion string
}

// NewServer creates a new API server instance.
func NewServer(c client.Client, versionInfo VersionInfo) *Server {
	return &Server{
		serverService:    service.NewServerService(c),
		pluginService:    service.NewPluginService(c),
		namespaceService: service.NewNamespaceService(c),
		versionInfo:      versionInfo,
	}
}

// Handler returns an HTTP handler for the API server.
// The handler should be mounted at /api/v1/.
func (s *Server) Handler() http.Handler {
	strictHandler := generated.NewStrictHandler(s, nil)
	return generated.Handler(strictHandler)
}
