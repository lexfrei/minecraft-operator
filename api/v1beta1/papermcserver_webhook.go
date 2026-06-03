/*
Copyright 2026, Aleksei Sviridkin.

SPDX-License-Identifier: BSD-3-Clause
*/

package v1beta1

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/robfig/cron/v3"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// Update strategy values shared by PaperMCServer and Plugin validation.
const (
	strategyLatest   = "latest"
	strategyAuto     = "auto"
	strategyPin      = "pin"
	strategyBuildPin = "build-pin"
)

// Plugin source type values.
const (
	sourceTypeHangar = "hangar"
	sourceTypeURL    = "url"
)

// Endpoint protocol values.
const (
	protocolTCP  = "TCP"
	protocolUDP  = "UDP"
	protocolHTTP = "HTTP"
)

// hostnamePattern matches valid RFC 1123 hostnames.
var hostnamePattern = regexp.MustCompile(
	`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*$`,
)

// cronParser is the standard 5-field cron parser (minute hour dom month dow).
var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

// PaperMCServerValidator validates PaperMCServer resources.
type PaperMCServerValidator struct{}

// ValidateCreate validates a PaperMCServer on creation.
func (v *PaperMCServerValidator) ValidateCreate(_ context.Context, obj *PaperMCServer) (admission.Warnings, error) {
	return v.validate(obj)
}

// ValidateUpdate validates a PaperMCServer on update.
func (v *PaperMCServerValidator) ValidateUpdate(
	_ context.Context, _, newObj *PaperMCServer,
) (admission.Warnings, error) {
	return v.validate(newObj)
}

// ValidateDelete validates a PaperMCServer on deletion.
func (v *PaperMCServerValidator) ValidateDelete(_ context.Context, _ *PaperMCServer) (admission.Warnings, error) {
	return nil, nil
}

func (v *PaperMCServerValidator) validate(s *PaperMCServer) (admission.Warnings, error) {
	allErrs := make(field.ErrorList, 0, 5)

	specPath := field.NewPath("spec")

	allErrs = append(allErrs, validateServerStrategy(s, specPath)...)
	allErrs = append(allErrs, validateCronExpressions(s, specPath)...)
	allErrs = append(allErrs, validateRCON(s, specPath)...)
	allErrs = append(allErrs, validateBackup(s, specPath)...)
	allErrs = append(allErrs, validateGateway(s, specPath)...)
	allErrs = append(allErrs, validateServerPluginConfigs(s, specPath)...)
	allErrs = append(allErrs, validateServerConfigFiles(s, specPath)...)

	return nil, invalidIfNotEmpty(allErrs)
}

func validateServerStrategy(s *PaperMCServer, specPath *field.Path) field.ErrorList {
	var errs field.ErrorList

	switch s.Spec.UpdateStrategy {
	case strategyLatest, strategyAuto:
		// No additional fields required.
	case strategyPin:
		if s.Spec.Version == "" {
			errs = append(errs, field.Required(
				specPath.Child("version"),
				"version is required for 'pin' strategy",
			))
		}
	case strategyBuildPin:
		if s.Spec.Version == "" {
			errs = append(errs, field.Required(
				specPath.Child("version"),
				"version is required for 'build-pin' strategy",
			))
		}

		if s.Spec.Build == nil {
			errs = append(errs, field.Required(
				specPath.Child("build"),
				"build is required for 'build-pin' strategy",
			))
		}
	default:
		errs = append(errs, field.NotSupported(
			specPath.Child("updateStrategy"),
			s.Spec.UpdateStrategy,
			[]string{strategyLatest, strategyAuto, strategyPin, strategyBuildPin},
		))
	}

	return errs
}

func validateCronExpressions(s *PaperMCServer, specPath *field.Path) field.ErrorList {
	var errs field.ErrorList

	schedulePath := specPath.Child("updateSchedule")

	if _, err := cronParser.Parse(s.Spec.UpdateSchedule.CheckCron); err != nil {
		errs = append(errs, field.Invalid(
			schedulePath.Child("checkCron"),
			s.Spec.UpdateSchedule.CheckCron,
			fmt.Sprintf("invalid cron expression: %v", err),
		))
	}

	if s.Spec.UpdateSchedule.MaintenanceWindow.Enabled {
		if _, err := cronParser.Parse(s.Spec.UpdateSchedule.MaintenanceWindow.Cron); err != nil {
			errs = append(errs, field.Invalid(
				schedulePath.Child("maintenanceWindow", "cron"),
				s.Spec.UpdateSchedule.MaintenanceWindow.Cron,
				fmt.Sprintf("invalid cron expression: %v", err),
			))
		}
	}

	return errs
}

func validateRCON(s *PaperMCServer, specPath *field.Path) field.ErrorList {
	var errs field.ErrorList

	if !s.Spec.RCON.Enabled {
		return nil
	}

	rconPath := specPath.Child("rcon", "passwordSecret")

	if s.Spec.RCON.PasswordSecret.Name == "" {
		errs = append(errs, field.Required(rconPath.Child("name"), "secret name is required when RCON is enabled"))
	}

	if s.Spec.RCON.PasswordSecret.Key == "" {
		errs = append(errs, field.Required(rconPath.Child("key"), "secret key is required when RCON is enabled"))
	}

	return errs
}

func validateBackup(s *PaperMCServer, specPath *field.Path) field.ErrorList {
	var errs field.ErrorList

	if s.Spec.Backup == nil || !s.Spec.Backup.Enabled {
		return nil
	}

	if s.Spec.Backup.Schedule != "" {
		if _, err := cronParser.Parse(s.Spec.Backup.Schedule); err != nil {
			errs = append(errs, field.Invalid(
				specPath.Child("backup", "schedule"),
				s.Spec.Backup.Schedule,
				fmt.Sprintf("invalid cron expression: %v", err),
			))
		}
	}

	return errs
}

func validateGateway(s *PaperMCServer, specPath *field.Path) field.ErrorList {
	var errs field.ErrorList

	if s.Spec.Gateway == nil {
		return nil
	}

	gwPath := specPath.Child("gateway")

	// HTTPRoutes require gateway to be enabled.
	if !s.Spec.Gateway.Enabled && len(s.Spec.Gateway.HTTPRoutes) > 0 {
		errs = append(errs, field.Forbidden(
			gwPath.Child("httpRoutes"),
			"httpRoutes require gateway.enabled=true",
		))
	}

	if !s.Spec.Gateway.Enabled {
		return errs
	}

	if len(s.Spec.Gateway.ParentRefs) == 0 {
		errs = append(errs, field.Required(
			gwPath.Child("parentRefs"),
			"at least one parentRef is required when gateway is enabled",
		))
	}

	errs = append(errs, validateHTTPRoutes(s.Spec.Gateway.HTTPRoutes, gwPath)...)

	return errs
}

func validateServerPluginConfigs(s *PaperMCServer, specPath *field.Path) field.ErrorList {
	var errs field.ErrorList

	if len(s.Spec.PluginConfigs) == 0 {
		return nil
	}

	pluginConfigsPath := specPath.Child("pluginConfigs")
	seenPluginNames := make(map[string]bool)

	for i, pc := range s.Spec.PluginConfigs {
		pcPath := pluginConfigsPath.Index(i)

		if pc.PluginName == "" {
			errs = append(errs, field.Required(pcPath.Child("pluginName"), "pluginName is required"))
		} else {
			// Validate for path traversal and shell injection.
			errs = append(errs, validateSafeName(pc.PluginName, pcPath.Child("pluginName"))...)

			// Reject duplicate pluginName entries.
			if seenPluginNames[pc.PluginName] {
				errs = append(errs, field.Duplicate(pcPath.Child("pluginName"), pc.PluginName))
			} else {
				seenPluginNames[pc.PluginName] = true
			}
		}

		seenPaths := make(map[string]bool)

		for j, cfg := range pc.Configs {
			cfgPath := pcPath.Child("configs").Index(j)
			errs = append(errs, validateConfigFile(cfg.ConfigMapRef, cfg.Path, cfgPath)...)

			if cfg.Path != "" {
				if seenPaths[cfg.Path] {
					errs = append(errs, field.Duplicate(cfgPath.Child("path"), cfg.Path))
				} else {
					seenPaths[cfg.Path] = true
				}
			}
		}
	}

	return errs
}

func validateServerConfigFiles(s *PaperMCServer, specPath *field.Path) field.ErrorList {
	var errs field.ErrorList

	if len(s.Spec.ServerConfigs) == 0 {
		return nil
	}

	serverConfigsPath := specPath.Child("serverConfigs")
	seenPaths := make(map[string]bool)

	for i, cfg := range s.Spec.ServerConfigs {
		cfgPath := serverConfigsPath.Index(i)
		errs = append(errs, validateConfigFile(cfg.ConfigMapRef, cfg.Path, cfgPath)...)

		if cfg.Path != "" {
			if seenPaths[cfg.Path] {
				errs = append(errs, field.Duplicate(cfgPath.Child("path"), cfg.Path))
			} else {
				seenPaths[cfg.Path] = true
			}
		}
	}

	return errs
}

func validateHTTPRoutes(routes []PluginHTTPRoute, gwPath *field.Path) field.ErrorList {
	var errs field.ErrorList

	routesPath := gwPath.Child("httpRoutes")
	seenPluginEndpoint := make(map[string]bool)
	seenHostPath := make(map[string]bool)

	for i, route := range routes {
		routePath := routesPath.Index(i)

		if route.PluginName == "" {
			errs = append(errs, field.Required(routePath.Child("pluginName"), "pluginName is required"))
		}

		if route.EndpointName == "" {
			errs = append(errs, field.Required(routePath.Child("endpointName"), "endpointName is required"))
		}

		if route.Hostname == "" {
			errs = append(errs, field.Required(routePath.Child("hostname"), "hostname is required"))
		} else if !hostnamePattern.MatchString(strings.ToLower(route.Hostname)) {
			errs = append(errs, field.Invalid(
				routePath.Child("hostname"), route.Hostname,
				"must be a valid RFC 1123 hostname",
			))
		}

		if route.PathPrefix != "" && !strings.HasPrefix(route.PathPrefix, "/") {
			errs = append(errs, field.Invalid(
				routePath.Child("pathPrefix"), route.PathPrefix,
				"pathPrefix must start with '/'",
			))
		}

		if route.PluginName != "" && route.EndpointName != "" {
			key := route.PluginName + "/" + route.EndpointName
			if seenPluginEndpoint[key] {
				errs = append(errs, field.Duplicate(
					routePath,
					fmt.Sprintf("%s/%s", route.PluginName, route.EndpointName),
				))
			} else {
				seenPluginEndpoint[key] = true
			}
		}

		if route.Hostname != "" {
			hostPathKey := route.Hostname + "|" + route.PathPrefix
			if seenHostPath[hostPathKey] {
				errs = append(errs, field.Duplicate(
					routePath,
					fmt.Sprintf("hostname=%s pathPrefix=%s", route.Hostname, route.PathPrefix),
				))
			} else {
				seenHostPath[hostPathKey] = true
			}
		}
	}

	return errs
}
