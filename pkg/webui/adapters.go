package webui

import (
	"fmt"
	"time"

	"github.com/lexfrei/minecraft-operator/pkg/service"
	"github.com/lexfrei/minecraft-operator/pkg/webui/templates"
)

// serverDataToSummary converts service.ServerData to templates.ServerSummary.
func serverDataToSummary(data service.ServerData) templates.ServerSummary {
	summary := templates.ServerSummary{
		Name:                data.Name,
		Namespace:           data.Namespace,
		CurrentPaperVersion: formatVersionWithBuild(data.CurrentVersion, data.CurrentBuild),
		Strategy:            data.UpdateStrategy,
		PluginCount:         data.PluginCount,
		NextMaintenance:     data.NextMaintenance,
		Status:              data.Status,
		Labels:              data.Labels,
	}

	// Convert available update
	if data.AvailableUpdate != nil {
		summary.AvailableUpdate = fmt.Sprintf("%s-%d", data.AvailableUpdate.Version, data.AvailableUpdate.Build)
	}

	// Convert plugins
	summary.Plugins = make([]templates.PluginSummary, 0, len(data.Plugins))
	for _, p := range data.Plugins {
		summary.Plugins = append(summary.Plugins, templates.PluginSummary{
			Name:           p.Name,
			CurrentVersion: p.CurrentVersion,
			DesiredVersion: p.DesiredVersion,
		})
	}

	// Build update plan if there's an available update
	if data.AvailableUpdate != nil {
		summary.UpdatePlan = &templates.UpdatePlan{
			PaperUpdate: fmt.Sprintf("%s-%d", data.AvailableUpdate.Version, data.AvailableUpdate.Build),
		}
		// Add plugin updates
		for _, p := range data.Plugins {
			if p.DesiredVersion != "" && p.DesiredVersion != p.CurrentVersion {
				summary.UpdatePlan.PluginUpdates = append(summary.UpdatePlan.PluginUpdates, templates.PluginUpdate{
					Name:        p.Name,
					FromVersion: p.CurrentVersion,
					ToVersion:   p.DesiredVersion,
				})
			}
		}
		if data.MaintenanceSchedule != nil {
			summary.UpdatePlan.AppliesAt = data.MaintenanceSchedule.NextWindow
		}
	}

	return summary
}

// serverDataToDetail converts service.ServerData to templates.ServerDetailData.
func serverDataToDetail(data *service.ServerData) templates.ServerDetailData {
	detail := templates.ServerDetailData{
		Name:                data.Name,
		Namespace:           data.Namespace,
		CurrentPaperVersion: formatVersionWithBuild(data.CurrentVersion, data.CurrentBuild),
		DesiredPaperVersion: formatVersionWithBuild(data.DesiredVersion, data.DesiredBuild),
		Labels:              data.Labels,
	}

	// Convert available update
	if data.AvailableUpdate != nil {
		detail.AvailableUpdate = fmt.Sprintf("%s-%d", data.AvailableUpdate.Version, data.AvailableUpdate.Build)
	}

	// Convert plugins
	detail.Plugins = make([]templates.PluginInfo, 0, len(data.Plugins))
	for _, p := range data.Plugins {
		status := "compatible"
		if !p.Compatible {
			status = "incompatible"
		}
		detail.Plugins = append(detail.Plugins, templates.PluginInfo{
			Name:            p.Name,
			Project:         p.Project,
			CurrentVersion:  p.CurrentVersion,
			ResolvedVersion: p.ResolvedVersion,
			Status:          status,
		})
	}

	// Convert maintenance schedule
	if data.MaintenanceSchedule != nil {
		detail.MaintenanceSchedule = templates.MaintenanceInfo{
			CheckCron:  data.MaintenanceSchedule.CheckCron,
			WindowCron: data.MaintenanceSchedule.WindowCron,
			NextWindow: data.MaintenanceSchedule.NextWindow,
		}
	}

	// Convert update history
	if data.UpdateHistory != nil {
		status := "success"
		if !data.UpdateHistory.Successful {
			status = "failed"
		}
		detail.LastUpdate = templates.UpdateInfo{
			Timestamp: data.UpdateHistory.AppliedAt.Format(time.RFC3339),
			Status:    status,
			Message:   fmt.Sprintf("Updated from %s", data.UpdateHistory.PreviousVersion),
		}
	}

	return detail
}

// pluginDataToListItem converts service.PluginData to templates.PluginListItem.
func pluginDataToListItem(data service.PluginData) templates.PluginListItem {
	return templates.PluginListItem{
		Name:            data.Name,
		Namespace:       data.Namespace,
		SourceType:      data.SourceType,
		Project:         data.Project,
		UpdateStrategy:  data.UpdateStrategy,
		Version:         data.Version,
		ResolvedVersion: data.ResolvedVersion,
		MatchedServers:  data.MatchedServers,
	}
}

// pluginDataToDetail converts service.PluginData to templates.PluginDetailData.
func pluginDataToDetail(data *service.PluginData) templates.PluginDetailData {
	return templates.PluginDetailData{
		Name:             data.Name,
		Namespace:        data.Namespace,
		SourceType:       data.SourceType,
		Project:          data.Project,
		URL:              data.URL,
		UpdateStrategy:   data.UpdateStrategy,
		Version:          data.Version,
		ResolvedVersion:  data.ResolvedVersion,
		MatchedServers:   data.MatchedServers,
		RepositoryStatus: data.RepositoryStatus,
	}
}

// serverDataToValues converts ServerData to a flat map for form pre-filling.
func serverDataToValues(data *service.ServerData) map[string]any {
	version := data.CurrentVersion
	if version == "" {
		version = data.DesiredVersion
	}
	values := map[string]any{
		keyName:          data.Name,
		keyNamespace:     data.Namespace,
		"updateStrategy": data.UpdateStrategy,
		"version":        version,
		"checkCron":      "",
	}
	if data.CurrentBuild > 0 {
		values["build"] = data.CurrentBuild
	}
	if data.MaintenanceSchedule != nil {
		values["maintenanceWindow.enabled"] = data.MaintenanceSchedule.Enabled
		values["maintenanceWindow.cron"] = data.MaintenanceSchedule.WindowCron
		values["checkCron"] = data.MaintenanceSchedule.CheckCron
	}
	if len(data.Labels) > 0 {
		values["labels"] = data.Labels
	}
	return values
}

// pluginDataToValues converts PluginData to a flat map for form pre-filling.
func pluginDataToValues(data *service.PluginData) map[string]any {
	values := map[string]any{
		"name":           data.Name,
		"namespace":      data.Namespace,
		"source.type":    data.SourceType,
		"source.project": data.Project,
		"source.url":     data.URL,
		"updateStrategy": data.UpdateStrategy,
		"version":        data.Version,
	}
	if data.Build != nil {
		values["build"] = *data.Build
	}
	if data.UpdateDelay != nil {
		values["updateDelay"] = data.UpdateDelay.String()
	}
	return values
}

// formatVersionWithBuild formats version and build into "version-build" format.
func formatVersionWithBuild(version string, build int) string {
	if version == "" {
		return "Unknown"
	}
	if build > 0 {
		return fmt.Sprintf("%s-%d", version, build)
	}
	return version
}
