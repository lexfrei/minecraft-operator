// Package service provides business logic for API and UI layers.
package service

import (
	"context"
	"fmt"
	"time"

	"github.com/cockroachdb/errors"
	mck8slexlav1beta1 "github.com/lexfrei/minecraft-operator/api/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// StatusRunning indicates the server is running normally.
	StatusRunning = "running"
	// StatusUpdating indicates the server is being updated.
	StatusUpdating = "updating"
	// StatusUnknown indicates the server status cannot be determined.
	StatusUnknown = "unknown"

	// AnnotationReconcile triggers reconciliation.
	AnnotationReconcile = "mc.k8s.lex.la/reconcile"
	// AnnotationApplyNow triggers immediate update application.
	AnnotationApplyNow = "mc.k8s.lex.la/apply-now"
)

// ServerService provides operations for PaperMCServer resources.
type ServerService struct {
	client client.Client
}

// NewServerService creates a new ServerService.
func NewServerService(c client.Client) *ServerService {
	return &ServerService{client: c}
}

// ServerData represents server information for API/UI consumption.
type ServerData struct {
	Name                string
	Namespace           string
	CurrentVersion      string
	CurrentBuild        int
	DesiredVersion      string
	DesiredBuild        int
	UpdateStrategy      string
	Status              string
	PluginCount         int
	AvailableUpdate     *AvailableUpdateData
	NextMaintenance     string
	Labels              map[string]string
	Plugins             []ServerPluginData
	MaintenanceSchedule *MaintenanceScheduleData
	UpdateHistory       *UpdateHistoryData
}

// AvailableUpdateData represents an available update.
type AvailableUpdateData struct {
	Version    string
	Build      int
	ReleasedAt time.Time
}

// ServerPluginData represents plugin status on a server.
type ServerPluginData struct {
	Name            string
	Namespace       string
	ResolvedVersion string
	CurrentVersion  string
	DesiredVersion  string
	Compatible      bool
	SourceType      string
	Project         string
}

// MaintenanceScheduleData represents maintenance schedule information.
type MaintenanceScheduleData struct {
	CheckCron  string
	WindowCron string
	NextWindow string
	Enabled    bool
}

// UpdateHistoryData represents the last update history.
type UpdateHistoryData struct {
	AppliedAt       time.Time
	PreviousVersion string
	Successful      bool
}

// ServerCreateData contains data for creating a new server.
type ServerCreateData struct {
	Name               string
	Namespace          string
	UpdateStrategy     string
	Version            string
	Build              int
	UpdateDelay        string
	CheckCron          string
	MaintenanceEnabled bool
	MaintenanceCron    string
	Labels             map[string]string
}

// ServerUpdateData contains data for updating an existing server.
type ServerUpdateData struct {
	Namespace          string
	Name               string
	UpdateStrategy     *string
	Version            *string
	Build              *int
	UpdateDelay        *string
	CheckCron          *string
	MaintenanceEnabled *bool
	MaintenanceCron    *string
	Labels             *map[string]string
}

// ListServers returns all servers, optionally filtered by namespace.
func (s *ServerService) ListServers(ctx context.Context, namespace string) ([]ServerData, error) {
	var serverList mck8slexlav1beta1.PaperMCServerList

	if err := s.client.List(ctx, &serverList); err != nil {
		return nil, errors.Wrap(err, "failed to list PaperMCServers")
	}

	servers := make([]ServerData, 0, len(serverList.Items))

	for i := range serverList.Items {
		server := &serverList.Items[i]

		// Skip if filtering by namespace
		if namespace != "" && server.Namespace != namespace {
			continue
		}

		data := s.serverToData(ctx, server)
		servers = append(servers, data)
	}

	return servers, nil
}

// GetServer returns a specific server by namespace and name.
func (s *ServerService) GetServer(ctx context.Context, namespace, name string) (*ServerData, error) {
	var server mck8slexlav1beta1.PaperMCServer

	if err := s.client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &server); err != nil {
		return nil, errors.Wrap(err, "failed to get PaperMCServer")
	}

	data := s.serverToData(ctx, &server)

	// Fetch full plugin details
	data.Plugins = s.fetchPluginDetails(ctx, server.Status.Plugins)

	return &data, nil
}

// GetServerByName finds a server by name across all namespaces.
func (s *ServerService) GetServerByName(ctx context.Context, name string) (*ServerData, error) {
	var serverList mck8slexlav1beta1.PaperMCServerList

	if err := s.client.List(ctx, &serverList); err != nil {
		return nil, errors.Wrap(err, "failed to list PaperMCServers")
	}

	for i := range serverList.Items {
		if serverList.Items[i].Name == name {
			data := s.serverToData(ctx, &serverList.Items[i])
			data.Plugins = s.fetchPluginDetails(ctx, serverList.Items[i].Status.Plugins)
			return &data, nil
		}
	}

	return nil, errors.Newf("PaperMCServer %q not found", name)
}

// TriggerReconciliation sets the reconcile annotation to trigger reconciliation.
func (s *ServerService) TriggerReconciliation(ctx context.Context, namespace, name string) error {
	var server mck8slexlav1beta1.PaperMCServer

	if err := s.client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &server); err != nil {
		return errors.Wrap(err, "failed to get server")
	}

	if server.Annotations == nil {
		server.Annotations = make(map[string]string)
	}
	server.Annotations[AnnotationReconcile] = fmt.Sprintf("%d", time.Now().Unix())

	if err := s.client.Update(ctx, &server); err != nil {
		return errors.Wrap(err, "failed to update server")
	}

	return nil
}

// ApplyNow sets the apply-now annotation to trigger immediate update.
func (s *ServerService) ApplyNow(ctx context.Context, namespace, name string) error {
	var server mck8slexlav1beta1.PaperMCServer

	if err := s.client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &server); err != nil {
		return errors.Wrap(err, "failed to get server")
	}

	if server.Annotations == nil {
		server.Annotations = make(map[string]string)
	}
	server.Annotations[AnnotationApplyNow] = fmt.Sprintf("%d", time.Now().Unix())

	if err := s.client.Update(ctx, &server); err != nil {
		return errors.Wrap(err, "failed to update server")
	}

	return nil
}

// DeleteServer deletes a server.
func (s *ServerService) DeleteServer(ctx context.Context, namespace, name string) error {
	var server mck8slexlav1beta1.PaperMCServer

	if err := s.client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &server); err != nil {
		return errors.Wrap(err, "failed to get server")
	}

	if err := s.client.Delete(ctx, &server); err != nil {
		return errors.Wrap(err, "failed to delete server")
	}

	return nil
}

// CreateServer creates a new PaperMCServer from the provided data.
func (s *ServerService) CreateServer(ctx context.Context, data ServerCreateData) error {
	server := &mck8slexlav1beta1.PaperMCServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      data.Name,
			Namespace: data.Namespace,
			Labels:    data.Labels,
		},
		Spec: mck8slexlav1beta1.PaperMCServerSpec{
			UpdateStrategy: data.UpdateStrategy,
			Version:        data.Version,
		},
	}

	if data.Build != 0 {
		server.Spec.Build = &data.Build
	}

	if data.UpdateDelay != "" {
		d, err := time.ParseDuration(data.UpdateDelay)
		if err != nil {
			return errors.Wrapf(err, "invalid update delay %q", data.UpdateDelay)
		}

		server.Spec.UpdateDelay = &metav1.Duration{Duration: d}
	}

	if data.CheckCron != "" {
		server.Spec.UpdateSchedule.CheckCron = data.CheckCron
	}

	if data.MaintenanceEnabled {
		server.Spec.UpdateSchedule.MaintenanceWindow.Enabled = true
		server.Spec.UpdateSchedule.MaintenanceWindow.Cron = data.MaintenanceCron
	}

	if err := s.client.Create(ctx, server); err != nil {
		return errors.Wrap(err, "failed to create PaperMCServer")
	}

	return nil
}

// UpdateServer updates an existing PaperMCServer with the provided data.
func (s *ServerService) UpdateServer(ctx context.Context, data ServerUpdateData) error {
	var server mck8slexlav1beta1.PaperMCServer

	if err := s.client.Get(ctx, client.ObjectKey{Namespace: data.Namespace, Name: data.Name}, &server); err != nil {
		return errors.Wrap(err, "failed to get server")
	}

	if data.UpdateStrategy != nil {
		server.Spec.UpdateStrategy = *data.UpdateStrategy
	}

	if data.Version != nil {
		server.Spec.Version = *data.Version
	}

	if data.Build != nil {
		server.Spec.Build = data.Build
	}

	if data.UpdateDelay != nil {
		d, err := time.ParseDuration(*data.UpdateDelay)
		if err != nil {
			return errors.Wrapf(err, "invalid update delay %q", *data.UpdateDelay)
		}

		server.Spec.UpdateDelay = &metav1.Duration{Duration: d}
	}

	if data.CheckCron != nil {
		server.Spec.UpdateSchedule.CheckCron = *data.CheckCron
	}

	if data.MaintenanceEnabled != nil {
		server.Spec.UpdateSchedule.MaintenanceWindow.Enabled = *data.MaintenanceEnabled
	}

	if data.MaintenanceCron != nil {
		server.Spec.UpdateSchedule.MaintenanceWindow.Cron = *data.MaintenanceCron
	}

	if data.Labels != nil {
		server.Labels = *data.Labels
	}

	if err := s.client.Update(ctx, &server); err != nil {
		return errors.Wrap(err, "failed to update server")
	}

	return nil
}

// DetermineStatus determines the display status of a server.
func (s *ServerService) DetermineStatus(server *mck8slexlav1beta1.PaperMCServer) string {
	// Check StatefulSet readiness via conditions
	for _, condition := range server.Status.Conditions {
		if condition.Type == "StatefulSetReady" {
			if condition.Status == metav1.ConditionTrue {
				return StatusRunning
			}
			if condition.Status == metav1.ConditionFalse && condition.Reason == "Updating" {
				return StatusUpdating
			}
		}
	}

	// Fallback: check if CurrentVersion is populated
	if server.Status.CurrentVersion != "" {
		return StatusRunning
	}

	return StatusUnknown
}

// CheckStatefulSetStatus checks the actual StatefulSet status in real-time.
func (s *ServerService) CheckStatefulSetStatus(ctx context.Context, name, namespace string) string {
	var sts appsv1.StatefulSet

	if err := s.client.Get(ctx, client.ObjectKey{
		Name:      name,
		Namespace: namespace,
	}, &sts); err != nil {
		return StatusUnknown
	}

	// Check if StatefulSet is ready
	if sts.Status.ReadyReplicas > 0 && sts.Status.ReadyReplicas == sts.Status.Replicas {
		return StatusRunning
	}

	// Check if updating
	if sts.Status.CurrentRevision != sts.Status.UpdateRevision {
		return StatusUpdating
	}

	return StatusUnknown
}

// serverToData converts a PaperMCServer to ServerData.
func (s *ServerService) serverToData(ctx context.Context, server *mck8slexlav1beta1.PaperMCServer) ServerData {
	data := ServerData{
		Name:           server.Name,
		Namespace:      server.Namespace,
		CurrentVersion: server.Status.CurrentVersion,
		CurrentBuild:   server.Status.CurrentBuild,
		DesiredVersion: server.Status.DesiredVersion,
		DesiredBuild:   server.Status.DesiredBuild,
		UpdateStrategy: server.Spec.UpdateStrategy,
		Status:         s.DetermineStatus(server),
		PluginCount:    len(server.Status.Plugins),
		Labels:         server.Labels,
	}

	// Check actual StatefulSet status if condition is stale
	if data.Status == StatusUnknown {
		data.Status = s.CheckStatefulSetStatus(ctx, server.Name, server.Namespace)
	}

	if server.Status.AvailableUpdate != nil {
		data.AvailableUpdate = &AvailableUpdateData{
			Version:    server.Status.AvailableUpdate.Version,
			Build:      server.Status.AvailableUpdate.Build,
			ReleasedAt: server.Status.AvailableUpdate.ReleasedAt.Time,
		}
	}

	if server.Spec.UpdateSchedule.MaintenanceWindow.Enabled {
		data.NextMaintenance = FormatCronSchedule(server.Spec.UpdateSchedule.MaintenanceWindow.Cron)
		data.MaintenanceSchedule = &MaintenanceScheduleData{
			CheckCron:  server.Spec.UpdateSchedule.CheckCron,
			WindowCron: server.Spec.UpdateSchedule.MaintenanceWindow.Cron,
			NextWindow: FormatCronSchedule(server.Spec.UpdateSchedule.MaintenanceWindow.Cron),
			Enabled:    true,
		}
	}

	if server.Status.LastUpdate != nil {
		data.UpdateHistory = &UpdateHistoryData{
			AppliedAt:       server.Status.LastUpdate.AppliedAt.Time,
			PreviousVersion: server.Status.LastUpdate.PreviousVersion,
			Successful:      server.Status.LastUpdate.Successful,
		}
	}

	// Build basic plugin data
	data.Plugins = make([]ServerPluginData, 0, len(server.Status.Plugins))
	for _, ps := range server.Status.Plugins {
		data.Plugins = append(data.Plugins, ServerPluginData{
			Name:            ps.PluginRef.Name,
			Namespace:       ps.PluginRef.Namespace,
			ResolvedVersion: ps.ResolvedVersion,
			CurrentVersion:  ps.CurrentVersion,
			DesiredVersion:  ps.DesiredVersion,
			Compatible:      ps.Compatible,
			SourceType:      ps.Source,
		})
	}

	return data
}

// fetchPluginDetails fetches full plugin details from Plugin CRDs.
func (s *ServerService) fetchPluginDetails(
	ctx context.Context,
	pluginStatuses []mck8slexlav1beta1.ServerPluginStatus,
) []ServerPluginData {
	plugins := make([]ServerPluginData, 0, len(pluginStatuses))

	for _, pluginStatus := range pluginStatuses {
		var plugin mck8slexlav1beta1.Plugin
		if err := s.client.Get(ctx, client.ObjectKey{
			Name:      pluginStatus.PluginRef.Name,
			Namespace: pluginStatus.PluginRef.Namespace,
		}, &plugin); err != nil {
			// Still include plugin with partial data
			plugins = append(plugins, ServerPluginData{
				Name:            pluginStatus.PluginRef.Name,
				Namespace:       pluginStatus.PluginRef.Namespace,
				ResolvedVersion: pluginStatus.ResolvedVersion,
				Compatible:      pluginStatus.Compatible,
			})
			continue
		}

		plugins = append(plugins, ServerPluginData{
			Name:            plugin.Name,
			Namespace:       plugin.Namespace,
			ResolvedVersion: pluginStatus.ResolvedVersion,
			CurrentVersion:  pluginStatus.CurrentVersion,
			DesiredVersion:  pluginStatus.DesiredVersion,
			Compatible:      pluginStatus.Compatible,
			SourceType:      plugin.Spec.Source.Type,
			Project:         plugin.Spec.Source.Project,
		})
	}

	return plugins
}

// GetServerNamespaces returns unique namespaces that have servers.
func (s *ServerService) GetServerNamespaces(ctx context.Context) ([]string, error) {
	var serverList mck8slexlav1beta1.PaperMCServerList

	if err := s.client.List(ctx, &serverList); err != nil {
		return nil, errors.Wrap(err, "failed to list PaperMCServers")
	}

	namespaceSet := make(map[string]bool)
	for _, server := range serverList.Items {
		namespaceSet[server.Namespace] = true
	}

	namespaces := make([]string, 0, len(namespaceSet))
	for ns := range namespaceSet {
		namespaces = append(namespaces, ns)
	}

	return namespaces, nil
}
