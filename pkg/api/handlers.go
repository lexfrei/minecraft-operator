package api

import (
	"context"
	"log/slog"
	"runtime"
	"time"

	"github.com/lexfrei/minecraft-operator/api/openapi/generated"
	"github.com/lexfrei/minecraft-operator/pkg/service"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Ensure Server implements StrictServerInterface.
var _ generated.StrictServerInterface = (*Server)(nil)

// GetHealth implements the health check endpoint.
func (s *Server) GetHealth(
	ctx context.Context,
	_ generated.GetHealthRequestObject,
) (generated.GetHealthResponseObject, error) {
	// Quick check - try to list namespaces to verify K8s connectivity
	_, err := s.namespaceService.ListNamespaces(ctx)
	if err != nil {
		slog.WarnContext(ctx, "Health check failed", "error", err)
		errMsg := "Kubernetes API unavailable"
		return generated.GetHealth503JSONResponse{
			Status:    generated.Unhealthy,
			Timestamp: time.Now(),
			Error:     &errMsg,
		}, nil
	}

	return generated.GetHealth200JSONResponse{
		Status:    generated.Healthy,
		Timestamp: time.Now(),
	}, nil
}

// GetVersion implements the version endpoint.
func (s *Server) GetVersion(
	_ context.Context,
	_ generated.GetVersionRequestObject,
) (generated.GetVersionResponseObject, error) {
	resp := generated.VersionResponse{
		Version:    s.versionInfo.Version,
		ApiVersion: "v1",
		GoVersion:  ptr(runtime.Version()),
	}

	if s.versionInfo.GitCommit != "" {
		resp.GitCommit = &s.versionInfo.GitCommit
	}

	if s.versionInfo.BuildDate != "" {
		if t, err := time.Parse(time.RFC3339, s.versionInfo.BuildDate); err == nil {
			resp.BuildDate = &t
		}
	}

	return generated.GetVersion200JSONResponse(resp), nil
}

// ListNamespaces implements listing available namespaces.
func (s *Server) ListNamespaces(
	ctx context.Context,
	_ generated.ListNamespacesRequestObject,
) (generated.ListNamespacesResponseObject, error) {
	namespaces, err := s.namespaceService.ListNamespaces(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to list namespaces", "error", err)
		return generated.ListNamespaces500JSONResponse{
			InternalServerErrorJSONResponse: generated.InternalServerErrorJSONResponse{
				Error: "Failed to list namespaces",
				Code:  ptr(generated.INTERNALERROR),
			},
		}, nil
	}

	return generated.ListNamespaces200JSONResponse{
		Namespaces: namespaces,
	}, nil
}

// ListServers implements listing all PaperMC servers.
func (s *Server) ListServers(
	ctx context.Context,
	req generated.ListServersRequestObject,
) (generated.ListServersResponseObject, error) {
	namespace := ""
	if req.Params.Namespace != nil {
		namespace = *req.Params.Namespace
	}

	servers, err := s.serverService.ListServers(ctx, namespace)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to list servers", "error", err, "namespace", namespace)
		return generated.ListServers500JSONResponse{
			InternalServerErrorJSONResponse: generated.InternalServerErrorJSONResponse{
				Error: "Failed to list servers",
				Code:  ptr(generated.INTERNALERROR),
			},
		}, nil
	}

	summaries := make([]generated.ServerSummary, 0, len(servers))
	for _, srv := range servers {
		summaries = append(summaries, serverDataToSummary(srv))
	}

	return generated.ListServers200JSONResponse{
		Servers: summaries,
	}, nil
}

// CreateServer implements creating a new PaperMC server.
func (s *Server) CreateServer(
	ctx context.Context,
	req generated.CreateServerRequestObject,
) (generated.CreateServerResponseObject, error) {
	if req.Body == nil {
		return generated.CreateServer400JSONResponse{
			BadRequestJSONResponse: generated.BadRequestJSONResponse{
				Error: "Request body is required",
				Code:  ptr(generated.INVALIDREQUEST),
			},
		}, nil
	}

	data := serverCreateRequestToData(*req.Body)

	err := s.serverService.CreateServer(ctx, data)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create server", "error", err, "name", req.Body.Name)

		// Check for already exists error
		if isAlreadyExistsError(err) {
			return generated.CreateServer409JSONResponse{
				ConflictJSONResponse: generated.ConflictJSONResponse{
					Error: err.Error(),
					Code:  ptr(generated.ALREADYEXISTS),
				},
			}, nil
		}

		return generated.CreateServer500JSONResponse{
			InternalServerErrorJSONResponse: generated.InternalServerErrorJSONResponse{
				Error: "Failed to create server",
				Code:  ptr(generated.INTERNALERROR),
			},
		}, nil
	}

	// Fetch the created server to return details
	srv, err := s.serverService.GetServer(ctx, req.Body.Namespace, req.Body.Name)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to get created server", "error", err)
		return generated.CreateServer500JSONResponse{
			InternalServerErrorJSONResponse: generated.InternalServerErrorJSONResponse{
				Error: "Server created but failed to retrieve details",
				Code:  ptr(generated.INTERNALERROR),
			},
		}, nil
	}

	return generated.CreateServer201JSONResponse(serverDataToDetail(*srv)), nil
}

// GetServer implements getting a specific server.
func (s *Server) GetServer(
	ctx context.Context,
	req generated.GetServerRequestObject,
) (generated.GetServerResponseObject, error) {
	srv, err := s.serverService.GetServer(ctx, req.Namespace, req.Name)
	if err != nil {
		if isNotFoundError(err) {
			return generated.GetServer404JSONResponse{
				NotFoundJSONResponse: generated.NotFoundJSONResponse{
					Error: err.Error(),
					Code:  ptr(generated.NOTFOUND),
				},
			}, nil
		}

		slog.ErrorContext(ctx, "Failed to get server", "error", err, "namespace", req.Namespace, "name", req.Name)
		return generated.GetServer500JSONResponse{
			InternalServerErrorJSONResponse: generated.InternalServerErrorJSONResponse{
				Error: "Failed to get server",
				Code:  ptr(generated.INTERNALERROR),
			},
		}, nil
	}

	return generated.GetServer200JSONResponse(serverDataToDetail(*srv)), nil
}

// UpdateServer implements updating an existing server.
//
//nolint:dupl // Similar structure to UpdatePlugin is intentional for API consistency
func (s *Server) UpdateServer(
	ctx context.Context,
	req generated.UpdateServerRequestObject,
) (generated.UpdateServerResponseObject, error) {
	if req.Body == nil {
		return generated.UpdateServer400JSONResponse{
			BadRequestJSONResponse: generated.BadRequestJSONResponse{
				Error: "Request body is required",
				Code:  ptr(generated.INVALIDREQUEST),
			},
		}, nil
	}

	data := serverUpdateRequestToData(req.Namespace, req.Name, *req.Body)

	err := s.serverService.UpdateServer(ctx, data)
	if err != nil {
		if isNotFoundError(err) {
			return generated.UpdateServer404JSONResponse{
				NotFoundJSONResponse: generated.NotFoundJSONResponse{
					Error: err.Error(),
					Code:  ptr(generated.NOTFOUND),
				},
			}, nil
		}

		slog.ErrorContext(ctx, "Failed to update server", "error", err, "namespace", req.Namespace, "name", req.Name)
		return generated.UpdateServer500JSONResponse{
			InternalServerErrorJSONResponse: generated.InternalServerErrorJSONResponse{
				Error: "Failed to update server",
				Code:  ptr(generated.INTERNALERROR),
			},
		}, nil
	}

	// Fetch updated server
	srv, err := s.serverService.GetServer(ctx, req.Namespace, req.Name)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to get updated server", "error", err)
		return generated.UpdateServer500JSONResponse{
			InternalServerErrorJSONResponse: generated.InternalServerErrorJSONResponse{
				Error: "Server updated but failed to retrieve details",
				Code:  ptr(generated.INTERNALERROR),
			},
		}, nil
	}

	return generated.UpdateServer200JSONResponse(serverDataToDetail(*srv)), nil
}

// DeleteServer implements deleting a server.
func (s *Server) DeleteServer(
	ctx context.Context,
	req generated.DeleteServerRequestObject,
) (generated.DeleteServerResponseObject, error) {
	err := s.serverService.DeleteServer(ctx, req.Namespace, req.Name)
	if err != nil {
		if isNotFoundError(err) {
			return generated.DeleteServer404JSONResponse{
				NotFoundJSONResponse: generated.NotFoundJSONResponse{
					Error: err.Error(),
					Code:  ptr(generated.NOTFOUND),
				},
			}, nil
		}

		slog.ErrorContext(ctx, "Failed to delete server", "error", err, "namespace", req.Namespace, "name", req.Name)
		return generated.DeleteServer500JSONResponse{
			InternalServerErrorJSONResponse: generated.InternalServerErrorJSONResponse{
				Error: "Failed to delete server",
				Code:  ptr(generated.INTERNALERROR),
			},
		}, nil
	}

	return generated.DeleteServer204Response{}, nil
}

// ResolveServer implements triggering server reconciliation.
func (s *Server) ResolveServer(
	ctx context.Context,
	req generated.ResolveServerRequestObject,
) (generated.ResolveServerResponseObject, error) {
	err := s.serverService.TriggerReconciliation(ctx, req.Namespace, req.Name)
	if err != nil {
		if isNotFoundError(err) {
			return generated.ResolveServer404JSONResponse{
				NotFoundJSONResponse: generated.NotFoundJSONResponse{
					Error: err.Error(),
					Code:  ptr(generated.NOTFOUND),
				},
			}, nil
		}

		slog.ErrorContext(ctx, "Failed to trigger reconciliation", "error", err, "namespace", req.Namespace, "name", req.Name)
		return generated.ResolveServer500JSONResponse{
			InternalServerErrorJSONResponse: generated.InternalServerErrorJSONResponse{
				Error: "Failed to trigger reconciliation",
				Code:  ptr(generated.INTERNALERROR),
			},
		}, nil
	}

	return generated.ResolveServer202JSONResponse{
		Message: "Reconciliation triggered",
	}, nil
}

// ApplyNowServer implements applying pending updates immediately.
func (s *Server) ApplyNowServer(
	ctx context.Context,
	req generated.ApplyNowServerRequestObject,
) (generated.ApplyNowServerResponseObject, error) {
	err := s.serverService.ApplyNow(ctx, req.Namespace, req.Name)
	if err != nil {
		if isNotFoundError(err) {
			return generated.ApplyNowServer404JSONResponse{
				NotFoundJSONResponse: generated.NotFoundJSONResponse{
					Error: err.Error(),
					Code:  ptr(generated.NOTFOUND),
				},
			}, nil
		}

		slog.ErrorContext(ctx, "Failed to apply updates", "error", err, "namespace", req.Namespace, "name", req.Name)
		return generated.ApplyNowServer500JSONResponse{
			InternalServerErrorJSONResponse: generated.InternalServerErrorJSONResponse{
				Error: "Failed to apply updates",
				Code:  ptr(generated.INTERNALERROR),
			},
		}, nil
	}

	return generated.ApplyNowServer202JSONResponse{
		Message: "Update applied",
	}, nil
}

// ListPlugins implements listing all plugins.
func (s *Server) ListPlugins(
	ctx context.Context,
	req generated.ListPluginsRequestObject,
) (generated.ListPluginsResponseObject, error) {
	namespace := ""
	if req.Params.Namespace != nil {
		namespace = *req.Params.Namespace
	}

	plugins, err := s.pluginService.ListPlugins(ctx, namespace)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to list plugins", "error", err, "namespace", namespace)
		return generated.ListPlugins500JSONResponse{
			InternalServerErrorJSONResponse: generated.InternalServerErrorJSONResponse{
				Error: "Failed to list plugins",
				Code:  ptr(generated.INTERNALERROR),
			},
		}, nil
	}

	summaries := make([]generated.PluginSummary, 0, len(plugins))
	for _, p := range plugins {
		summaries = append(summaries, pluginDataToSummary(p))
	}

	return generated.ListPlugins200JSONResponse{
		Plugins: summaries,
	}, nil
}

// CreatePlugin implements creating a new plugin.
func (s *Server) CreatePlugin(
	ctx context.Context,
	req generated.CreatePluginRequestObject,
) (generated.CreatePluginResponseObject, error) {
	if req.Body == nil {
		return generated.CreatePlugin400JSONResponse{
			BadRequestJSONResponse: generated.BadRequestJSONResponse{
				Error: "Request body is required",
				Code:  ptr(generated.INVALIDREQUEST),
			},
		}, nil
	}

	data := pluginCreateRequestToData(*req.Body)

	err := s.pluginService.CreatePlugin(ctx, data)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create plugin", "error", err, "name", req.Body.Name)

		if isAlreadyExistsError(err) {
			return generated.CreatePlugin409JSONResponse{
				ConflictJSONResponse: generated.ConflictJSONResponse{
					Error: err.Error(),
					Code:  ptr(generated.ALREADYEXISTS),
				},
			}, nil
		}

		return generated.CreatePlugin500JSONResponse{
			InternalServerErrorJSONResponse: generated.InternalServerErrorJSONResponse{
				Error: "Failed to create plugin",
				Code:  ptr(generated.INTERNALERROR),
			},
		}, nil
	}

	// Fetch created plugin
	p, err := s.pluginService.GetPlugin(ctx, req.Body.Namespace, req.Body.Name)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to get created plugin", "error", err)
		return generated.CreatePlugin500JSONResponse{
			InternalServerErrorJSONResponse: generated.InternalServerErrorJSONResponse{
				Error: "Plugin created but failed to retrieve details",
				Code:  ptr(generated.INTERNALERROR),
			},
		}, nil
	}

	return generated.CreatePlugin201JSONResponse(pluginDataToDetail(*p)), nil
}

// GetPlugin implements getting a specific plugin.
func (s *Server) GetPlugin(
	ctx context.Context,
	req generated.GetPluginRequestObject,
) (generated.GetPluginResponseObject, error) {
	p, err := s.pluginService.GetPlugin(ctx, req.Namespace, req.Name)
	if err != nil {
		if isNotFoundError(err) {
			return generated.GetPlugin404JSONResponse{
				NotFoundJSONResponse: generated.NotFoundJSONResponse{
					Error: err.Error(),
					Code:  ptr(generated.NOTFOUND),
				},
			}, nil
		}

		slog.ErrorContext(ctx, "Failed to get plugin", "error", err, "namespace", req.Namespace, "name", req.Name)
		return generated.GetPlugin500JSONResponse{
			InternalServerErrorJSONResponse: generated.InternalServerErrorJSONResponse{
				Error: "Failed to get plugin",
				Code:  ptr(generated.INTERNALERROR),
			},
		}, nil
	}

	return generated.GetPlugin200JSONResponse(pluginDataToDetail(*p)), nil
}

// UpdatePlugin implements updating an existing plugin.
//
//nolint:dupl // Similar structure to UpdateServer is intentional for API consistency
func (s *Server) UpdatePlugin(
	ctx context.Context,
	req generated.UpdatePluginRequestObject,
) (generated.UpdatePluginResponseObject, error) {
	if req.Body == nil {
		return generated.UpdatePlugin400JSONResponse{
			BadRequestJSONResponse: generated.BadRequestJSONResponse{
				Error: "Request body is required",
				Code:  ptr(generated.INVALIDREQUEST),
			},
		}, nil
	}

	data := pluginUpdateRequestToData(req.Namespace, req.Name, *req.Body)

	err := s.pluginService.UpdatePlugin(ctx, data)
	if err != nil {
		if isNotFoundError(err) {
			return generated.UpdatePlugin404JSONResponse{
				NotFoundJSONResponse: generated.NotFoundJSONResponse{
					Error: err.Error(),
					Code:  ptr(generated.NOTFOUND),
				},
			}, nil
		}

		slog.ErrorContext(ctx, "Failed to update plugin", "error", err, "namespace", req.Namespace, "name", req.Name)
		return generated.UpdatePlugin500JSONResponse{
			InternalServerErrorJSONResponse: generated.InternalServerErrorJSONResponse{
				Error: "Failed to update plugin",
				Code:  ptr(generated.INTERNALERROR),
			},
		}, nil
	}

	// Fetch updated plugin
	p, err := s.pluginService.GetPlugin(ctx, req.Namespace, req.Name)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to get updated plugin", "error", err)
		return generated.UpdatePlugin500JSONResponse{
			InternalServerErrorJSONResponse: generated.InternalServerErrorJSONResponse{
				Error: "Plugin updated but failed to retrieve details",
				Code:  ptr(generated.INTERNALERROR),
			},
		}, nil
	}

	return generated.UpdatePlugin200JSONResponse(pluginDataToDetail(*p)), nil
}

// DeletePlugin implements deleting a plugin.
func (s *Server) DeletePlugin(
	ctx context.Context,
	req generated.DeletePluginRequestObject,
) (generated.DeletePluginResponseObject, error) {
	err := s.pluginService.DeletePlugin(ctx, req.Namespace, req.Name)
	if err != nil {
		if isNotFoundError(err) {
			return generated.DeletePlugin404JSONResponse{
				NotFoundJSONResponse: generated.NotFoundJSONResponse{
					Error: err.Error(),
					Code:  ptr(generated.NOTFOUND),
				},
			}, nil
		}

		slog.ErrorContext(ctx, "Failed to delete plugin", "error", err, "namespace", req.Namespace, "name", req.Name)
		return generated.DeletePlugin500JSONResponse{
			InternalServerErrorJSONResponse: generated.InternalServerErrorJSONResponse{
				Error: "Failed to delete plugin",
				Code:  ptr(generated.INTERNALERROR),
			},
		}, nil
	}

	return generated.DeletePlugin204Response{}, nil
}

// ResolvePlugin implements triggering plugin reconciliation.
func (s *Server) ResolvePlugin(
	ctx context.Context,
	req generated.ResolvePluginRequestObject,
) (generated.ResolvePluginResponseObject, error) {
	err := s.pluginService.TriggerReconciliation(ctx, req.Namespace, req.Name)
	if err != nil {
		if isNotFoundError(err) {
			return generated.ResolvePlugin404JSONResponse{
				NotFoundJSONResponse: generated.NotFoundJSONResponse{
					Error: err.Error(),
					Code:  ptr(generated.NOTFOUND),
				},
			}, nil
		}

		slog.ErrorContext(ctx, "Failed to trigger reconciliation", "error", err, "namespace", req.Namespace, "name", req.Name)
		return generated.ResolvePlugin500JSONResponse{
			InternalServerErrorJSONResponse: generated.InternalServerErrorJSONResponse{
				Error: "Failed to trigger reconciliation",
				Code:  ptr(generated.INTERNALERROR),
			},
		}, nil
	}

	return generated.ResolvePlugin202JSONResponse{
		Message: "Reconciliation triggered",
	}, nil
}

// ptr is a helper to get a pointer to a value.
func ptr[T any](v T) *T {
	return &v
}

// isNotFoundError checks if an error is a "not found" error.
func isNotFoundError(err error) bool {
	return err != nil && (err.Error() == "not found" ||
		containsString(err.Error(), "not found") ||
		containsString(err.Error(), "NotFound"))
}

// isAlreadyExistsError checks if an error is an "already exists" error.
func isAlreadyExistsError(err error) bool {
	return err != nil && containsString(err.Error(), "already exists")
}

// containsString is a simple string contains helper.
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && findSubstring(s, substr) >= 0
}

// findSubstring finds the index of substr in s.
func findSubstring(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// serverDataToSummary converts service.ServerData to generated.ServerSummary.
func serverDataToSummary(data service.ServerData) generated.ServerSummary {
	summary := generated.ServerSummary{
		Name:           data.Name,
		Namespace:      data.Namespace,
		Status:         generated.ServerStatus(data.Status),
		UpdateStrategy: generated.UpdateStrategy(data.UpdateStrategy),
	}

	if data.CurrentVersion != "" {
		summary.CurrentVersion = &data.CurrentVersion
	}
	if data.DesiredVersion != "" {
		summary.DesiredVersion = &data.DesiredVersion
	}
	if len(data.Labels) > 0 {
		summary.Labels = &data.Labels
	}
	if data.PluginCount > 0 {
		summary.PluginCount = &data.PluginCount
	}
	if data.NextMaintenance != "" {
		summary.NextMaintenance = &data.NextMaintenance
	}

	if data.AvailableUpdate != nil {
		update := generated.AvailableUpdate{
			Version: &data.AvailableUpdate.Version,
			Build:   &data.AvailableUpdate.Build,
		}
		if !data.AvailableUpdate.ReleasedAt.IsZero() {
			update.ReleasedAt = &data.AvailableUpdate.ReleasedAt
		}
		summary.AvailableUpdate = &update
	}

	return summary
}

// serverDataToDetail converts service.ServerData to generated.ServerDetail.
//
//nolint:funlen // Complex conversion requires many field assignments
func serverDataToDetail(data service.ServerData) generated.ServerDetail {
	detail := generated.ServerDetail{
		Name:           data.Name,
		Namespace:      data.Namespace,
		Status:         generated.ServerStatus(data.Status),
		UpdateStrategy: generated.UpdateStrategy(data.UpdateStrategy),
	}

	if data.CurrentVersion != "" {
		detail.CurrentVersion = &data.CurrentVersion
	}
	if data.DesiredVersion != "" {
		detail.DesiredVersion = &data.DesiredVersion
	}
	if len(data.Labels) > 0 {
		detail.Labels = &data.Labels
	}
	if data.PluginCount > 0 {
		detail.PluginCount = &data.PluginCount
	}
	if data.NextMaintenance != "" {
		detail.NextMaintenance = &data.NextMaintenance
	}

	if data.AvailableUpdate != nil {
		update := generated.AvailableUpdate{
			Version: &data.AvailableUpdate.Version,
			Build:   &data.AvailableUpdate.Build,
		}
		if !data.AvailableUpdate.ReleasedAt.IsZero() {
			update.ReleasedAt = &data.AvailableUpdate.ReleasedAt
		}
		detail.AvailableUpdate = &update
	}

	// Convert plugins
	if len(data.Plugins) > 0 {
		plugins := make([]generated.ServerPlugin, 0, len(data.Plugins))
		for _, p := range data.Plugins {
			sp := generated.ServerPlugin{
				Name:       p.Name,
				Compatible: p.Compatible,
			}
			if p.Namespace != "" {
				sp.Namespace = &p.Namespace
			}
			if p.CurrentVersion != "" {
				sp.CurrentVersion = &p.CurrentVersion
			}
			if p.DesiredVersion != "" {
				sp.DesiredVersion = &p.DesiredVersion
			}
			if p.SourceType != "" {
				sp.Source = &p.SourceType
			}
			plugins = append(plugins, sp)
		}
		detail.Plugins = &plugins
	}

	// Convert maintenance schedule
	if data.MaintenanceSchedule != nil {
		ms := generated.MaintenanceSchedule{
			Enabled: &data.MaintenanceSchedule.Enabled,
		}
		if data.MaintenanceSchedule.CheckCron != "" {
			ms.CheckCron = &data.MaintenanceSchedule.CheckCron
		}
		if data.MaintenanceSchedule.WindowCron != "" {
			ms.WindowCron = &data.MaintenanceSchedule.WindowCron
		}
		if data.MaintenanceSchedule.NextWindow != "" {
			ms.NextWindow = &data.MaintenanceSchedule.NextWindow
		}
		detail.MaintenanceSchedule = &ms
	}

	// Convert update history
	if data.UpdateHistory != nil {
		uh := generated.UpdateHistory{
			Successful: &data.UpdateHistory.Successful,
		}
		if data.UpdateHistory.PreviousVersion != "" {
			uh.PreviousVersion = &data.UpdateHistory.PreviousVersion
		}
		if !data.UpdateHistory.AppliedAt.IsZero() {
			uh.AppliedAt = &data.UpdateHistory.AppliedAt
		}
		detail.LastUpdate = &uh
	}

	return detail
}

// pluginDataToSummary converts service.PluginData to generated.PluginSummary.
func pluginDataToSummary(data service.PluginData) generated.PluginSummary {
	summary := generated.PluginSummary{
		Name:           data.Name,
		Namespace:      data.Namespace,
		SourceType:     generated.PluginSourceType(data.SourceType),
		UpdateStrategy: generated.UpdateStrategy(data.UpdateStrategy),
	}

	if data.Project != "" {
		summary.Project = &data.Project
	}
	if data.Version != "" {
		summary.Version = &data.Version
	}
	if data.ResolvedVersion != "" {
		summary.ResolvedVersion = &data.ResolvedVersion
	}
	if data.MatchedServers > 0 {
		summary.MatchedServers = &data.MatchedServers
	}

	return summary
}

// pluginDataToDetail converts service.PluginData to generated.PluginDetail.
func pluginDataToDetail(data service.PluginData) generated.PluginDetail {
	detail := generated.PluginDetail{
		Name:           data.Name,
		Namespace:      data.Namespace,
		SourceType:     generated.PluginSourceType(data.SourceType),
		UpdateStrategy: generated.UpdateStrategy(data.UpdateStrategy),
	}

	if data.Project != "" {
		detail.Project = &data.Project
	}
	if data.Version != "" {
		detail.Version = &data.Version
	}
	if data.ResolvedVersion != "" {
		detail.ResolvedVersion = &data.ResolvedVersion
	}
	if data.MatchedServers > 0 {
		detail.MatchedServers = &data.MatchedServers
	}
	if data.RepositoryStatus != "" {
		detail.RepositoryStatus = ptr(generated.RepositoryStatus(data.RepositoryStatus))
	}

	// Convert matched instances
	if len(data.MatchedInstances) > 0 {
		instances := make([]generated.MatchedInstance, 0, len(data.MatchedInstances))
		for _, mi := range data.MatchedInstances {
			inst := generated.MatchedInstance{
				Name:       mi.Name,
				Namespace:  mi.Namespace,
				Compatible: mi.Compatible,
			}
			if mi.Version != "" {
				inst.Version = &mi.Version
			}
			instances = append(instances, inst)
		}
		detail.MatchedInstances = &instances
	}

	// Convert available versions
	if len(data.AvailableVersions) > 0 {
		versions := make([]generated.PluginVersion, 0, len(data.AvailableVersions))
		for _, v := range data.AvailableVersions {
			pv := generated.PluginVersion{
				Version:           v.Version,
				DownloadUrl:       v.DownloadURL,
				MinecraftVersions: v.SupportedVersions,
			}
			if !v.ReleasedAt.IsZero() {
				pv.ReleasedAt = &v.ReleasedAt
			}
			versions = append(versions, pv)
		}
		detail.AvailableVersions = &versions
	}

	return detail
}

// serverCreateRequestToData converts generated.ServerCreateRequest to service.ServerCreateData.
func serverCreateRequestToData(req generated.ServerCreateRequest) service.ServerCreateData {
	data := service.ServerCreateData{
		Name:           req.Name,
		Namespace:      req.Namespace,
		UpdateStrategy: string(req.UpdateStrategy),
	}

	if req.Version != nil {
		data.Version = *req.Version
	}
	if req.Build != nil {
		data.Build = *req.Build
	}
	if req.UpdateDelay != nil {
		data.UpdateDelay = *req.UpdateDelay
	}
	if req.CheckCron != nil {
		data.CheckCron = *req.CheckCron
	}
	if req.MaintenanceWindow != nil {
		if req.MaintenanceWindow.Enabled != nil {
			data.MaintenanceEnabled = *req.MaintenanceWindow.Enabled
		}
		if req.MaintenanceWindow.Cron != nil {
			data.MaintenanceCron = *req.MaintenanceWindow.Cron
		}
	}
	if req.Labels != nil {
		data.Labels = *req.Labels
	}

	return data
}

// serverUpdateRequestToData converts generated.ServerUpdateRequest to service.ServerUpdateData.
func serverUpdateRequestToData(namespace, name string, req generated.ServerUpdateRequest) service.ServerUpdateData {
	data := service.ServerUpdateData{
		Namespace: namespace,
		Name:      name,
	}

	if req.UpdateStrategy != nil {
		data.UpdateStrategy = ptr(string(*req.UpdateStrategy))
	}
	if req.Version != nil {
		data.Version = req.Version
	}
	if req.Build != nil {
		data.Build = req.Build
	}
	if req.UpdateDelay != nil {
		data.UpdateDelay = req.UpdateDelay
	}
	if req.CheckCron != nil {
		data.CheckCron = req.CheckCron
	}
	if req.MaintenanceWindow != nil {
		data.MaintenanceEnabled = req.MaintenanceWindow.Enabled
		data.MaintenanceCron = req.MaintenanceWindow.Cron
	}
	if req.Labels != nil {
		data.Labels = req.Labels
	}

	return data
}

// pluginCreateRequestToData converts generated.PluginCreateRequest to service.PluginCreateData.
func pluginCreateRequestToData(req generated.PluginCreateRequest) service.PluginCreateData {
	source := service.PluginSourceData{
		Type: string(req.Source.Type),
	}
	if req.Source.Project != nil {
		source.Project = *req.Source.Project
	}
	if req.Source.Url != nil {
		source.URL = *req.Source.Url
	}

	data := service.PluginCreateData{
		Name:             req.Name,
		Namespace:        req.Namespace,
		Source:           source,
		InstanceSelector: labelSelectorToK8s(req.InstanceSelector),
	}

	if req.UpdateStrategy != nil {
		data.UpdateStrategy = string(*req.UpdateStrategy)
	}
	if req.Version != nil {
		data.Version = *req.Version
	}
	if req.Build != nil {
		data.Build = *req.Build
	}
	if req.UpdateDelay != nil {
		data.UpdateDelay = *req.UpdateDelay
	}
	if req.Port != nil {
		data.Port = int32(*req.Port)
	}

	return data
}

// pluginUpdateRequestToData converts generated.PluginUpdateRequest to service.PluginUpdateData.
func pluginUpdateRequestToData(namespace, name string, req generated.PluginUpdateRequest) service.PluginUpdateData {
	data := service.PluginUpdateData{
		Namespace: namespace,
		Name:      name,
	}

	if req.UpdateStrategy != nil {
		data.UpdateStrategy = ptr(string(*req.UpdateStrategy))
	}
	if req.Version != nil {
		data.Version = req.Version
	}
	if req.Build != nil {
		data.Build = req.Build
	}
	if req.UpdateDelay != nil {
		data.UpdateDelay = req.UpdateDelay
	}
	if req.Port != nil {
		port := int32(*req.Port)
		data.Port = &port
	}
	if req.InstanceSelector != nil {
		selector := labelSelectorToK8s(*req.InstanceSelector)
		data.InstanceSelector = &selector
	}

	return data
}

// labelSelectorToK8s converts generated.LabelSelector to metav1.LabelSelector.
func labelSelectorToK8s(sel generated.LabelSelector) metav1.LabelSelector {
	result := metav1.LabelSelector{}

	if sel.MatchLabels != nil {
		result.MatchLabels = *sel.MatchLabels
	}

	if sel.MatchExpressions != nil {
		result.MatchExpressions = make([]metav1.LabelSelectorRequirement, 0, len(*sel.MatchExpressions))
		for _, expr := range *sel.MatchExpressions {
			req := metav1.LabelSelectorRequirement{
				Key:      expr.Key,
				Operator: metav1.LabelSelectorOperator(expr.Operator),
			}
			if expr.Values != nil {
				req.Values = *expr.Values
			}
			result.MatchExpressions = append(result.MatchExpressions, req)
		}
	}

	return result
}
