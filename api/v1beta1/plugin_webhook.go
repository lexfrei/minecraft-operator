/*
Copyright 2026, Aleksei Sviridkin.

SPDX-License-Identifier: BSD-3-Clause
*/

package v1beta1

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// sha256Pattern matches exactly 64 lowercase hexadecimal characters.
var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// PluginValidator validates Plugin resources.
type PluginValidator struct{}

// ValidateCreate validates a Plugin on creation.
func (v *PluginValidator) ValidateCreate(_ context.Context, obj *Plugin) (admission.Warnings, error) {
	return v.validate(obj)
}

// ValidateUpdate validates a Plugin on update.
func (v *PluginValidator) ValidateUpdate(_ context.Context, _, newObj *Plugin) (admission.Warnings, error) {
	return v.validate(newObj)
}

// ValidateDelete validates a Plugin on deletion.
func (v *PluginValidator) ValidateDelete(_ context.Context, _ *Plugin) (admission.Warnings, error) {
	return nil, nil
}

func (v *PluginValidator) validate(p *Plugin) (admission.Warnings, error) {
	var allErrs field.ErrorList

	var warnings admission.Warnings

	specPath := field.NewPath("spec")

	// Validate source.project for shell-unsafe characters regardless of source type.
	// The project name is used as a fallback for pluginDirName in config injection scripts.
	if p.Spec.Source.Project != "" {
		allErrs = append(allErrs, validateSafeName(p.Spec.Source.Project, specPath.Child("source", "project"))...)
	}

	// Source-specific validation.
	switch p.Spec.Source.Type {
	case "hangar":
		if p.Spec.Source.Project == "" {
			allErrs = append(allErrs, field.Required(
				specPath.Child("source", "project"),
				"project is required for source type 'hangar'",
			))
		}
	case "url":
		urlWarnings, urlErrs := validateURLSource(p, specPath)
		allErrs = append(allErrs, urlErrs...)
		warnings = append(warnings, urlWarnings...)
		if p.Spec.Source.Checksum == "" {
			warnings = append(warnings, "spec.source.checksum is not set; downloads will not be integrity-verified")
		} else if !sha256Pattern.MatchString(strings.ToLower(p.Spec.Source.Checksum)) {
			allErrs = append(allErrs, field.Invalid(
				specPath.Child("source", "checksum"),
				p.Spec.Source.Checksum,
				"checksum must be a valid SHA256 hex string (64 hex characters)",
			))
		}
	default:
		allErrs = append(allErrs, field.NotSupported(
			specPath.Child("source", "type"),
			p.Spec.Source.Type,
			[]string{"hangar", "url"},
		))
	}

	// Strategy-specific validation.
	allErrs = append(allErrs, validatePluginStrategy(p, specPath)...)

	// Endpoint validation.
	allErrs = append(allErrs, validateEndpoints(p, specPath)...)

	// Config validation.
	allErrs = append(allErrs, validatePluginConfigs(p, specPath)...)

	return warnings, invalidIfNotEmpty(allErrs)
}

func validateURLSource(p *Plugin, specPath *field.Path) (admission.Warnings, field.ErrorList) {
	var errs field.ErrorList

	var warnings admission.Warnings

	urlPath := specPath.Child("source", "url")

	if p.Spec.Source.URL == "" {
		errs = append(errs, field.Required(urlPath, "url is required for source type 'url'"))

		return warnings, errs
	}

	parsed, err := url.Parse(p.Spec.Source.URL)
	if err != nil {
		errs = append(errs, field.Invalid(urlPath, p.Spec.Source.URL, fmt.Sprintf("invalid URL: %v", err)))

		return warnings, errs
	}

	if parsed.Scheme != "https" {
		errs = append(errs, field.Invalid(urlPath, p.Spec.Source.URL, "only https URLs are allowed"))
	}

	if parsed.Host == "" {
		errs = append(errs, field.Invalid(urlPath, p.Spec.Source.URL, "URL must include a host"))
	}

	if !strings.HasSuffix(strings.ToLower(parsed.Path), ".jar") {
		warnings = append(warnings, "spec.source.url path does not end in .jar; ensure the URL points to a valid plugin JAR")
	}

	return warnings, errs
}

func validatePluginStrategy(p *Plugin, specPath *field.Path) field.ErrorList {
	var errs field.ErrorList

	switch p.Spec.UpdateStrategy {
	case "latest", "auto":
		// No additional fields required.
	case "pin":
		if p.Spec.Version == "" {
			errs = append(errs, field.Required(
				specPath.Child("version"),
				"version is required for 'pin' strategy",
			))
		}
	case "build-pin":
		if p.Spec.Version == "" {
			errs = append(errs, field.Required(
				specPath.Child("version"),
				"version is required for 'build-pin' strategy",
			))
		}

		if p.Spec.Build == nil {
			errs = append(errs, field.Required(
				specPath.Child("build"),
				"build is required for 'build-pin' strategy",
			))
		}
	default:
		errs = append(errs, field.NotSupported(
			specPath.Child("updateStrategy"),
			p.Spec.UpdateStrategy,
			[]string{"latest", "auto", "pin", "build-pin"},
		))
	}

	return errs
}

func validateEndpoints(p *Plugin, specPath *field.Path) field.ErrorList {
	var errs field.ErrorList

	epsPath := specPath.Child("endpoints")
	seenNames := make(map[string]bool)

	type portProto struct {
		port     int32
		protocol string
	}

	seenPortProto := make(map[portProto]bool)

	for i, ep := range p.Spec.Endpoints {
		epPath := epsPath.Index(i)

		if ep.Name == "" {
			errs = append(errs, field.Required(epPath.Child("name"), "endpoint name is required"))
		} else if seenNames[ep.Name] {
			errs = append(errs, field.Duplicate(epPath.Child("name"), ep.Name))
		} else {
			seenNames[ep.Name] = true
		}

		if ep.Port < 1 || ep.Port > 65535 {
			errs = append(errs, field.Invalid(
				epPath.Child("port"), ep.Port,
				"port must be between 1 and 65535",
			))
		}

		proto := ep.Protocol
		if proto == "" {
			proto = "TCP"
		}

		switch proto {
		case "TCP", "UDP", "HTTP":
			// valid
		default:
			errs = append(errs, field.NotSupported(
				epPath.Child("protocol"), ep.Protocol,
				[]string{"TCP", "UDP", "HTTP"},
			))
		}

		key := portProto{ep.Port, proto}
		if seenPortProto[key] {
			errs = append(errs, field.Duplicate(
				epPath.Child("port"),
				fmt.Sprintf("%d/%s", ep.Port, proto),
			))
		} else {
			seenPortProto[key] = true
		}
	}

	return errs
}

func validatePluginConfigs(p *Plugin, specPath *field.Path) field.ErrorList {
	var errs field.ErrorList

	if len(p.Spec.Configs) == 0 {
		return nil
	}

	// pluginDirName is required when configs are specified.
	if p.Spec.PluginDirName == "" {
		errs = append(errs, field.Required(
			specPath.Child("pluginDirName"),
			"pluginDirName is required when configs are specified",
		))
	} else {
		errs = append(errs, validateSafeName(p.Spec.PluginDirName, specPath.Child("pluginDirName"))...)
	}

	configsPath := specPath.Child("configs")
	seenPaths := make(map[string]bool)

	for i, cfg := range p.Spec.Configs {
		cfgPath := configsPath.Index(i)
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

// shellUnsafeChars are characters that can break shell quoting or enable injection.
// These are rejected in pluginDirName, source.project, config paths, and ConfigMap keys at admission time.
var shellUnsafeChars = []string{`"`, "'", "`", "$", `\`, "\n", "\r"}

// containsShellUnsafeChars returns true if s contains any shell-unsafe characters.
func containsShellUnsafeChars(s string) bool {
	for _, ch := range shellUnsafeChars {
		if strings.Contains(s, ch) {
			return true
		}
	}

	return false
}

// validateSafeName checks a name doesn't contain path traversal or shell-unsafe characters.
func validateSafeName(name string, fldPath *field.Path) field.ErrorList {
	var errs field.ErrorList

	if strings.Contains(name, "/") || strings.Contains(name, "..") {
		errs = append(errs, field.Invalid(fldPath, name,
			"must not contain '/' or '..'"))
	}

	if containsShellUnsafeChars(name) {
		errs = append(errs, field.Invalid(fldPath, name,
			`must not contain shell-unsafe characters ('"', "'", '`+"`"+`', '$', '\', newline, carriage return)`))
	}

	return errs
}

// validateConfigFile validates a config file entry (path + configMapRef).
func validateConfigFile(ref ConfigMapKeyRef, path string, fldPath *field.Path) field.ErrorList {
	var errs field.ErrorList

	if path == "" {
		errs = append(errs, field.Required(fldPath.Child("path"), "path is required"))
	} else if strings.HasPrefix(path, "/") {
		errs = append(errs, field.Invalid(fldPath.Child("path"), path,
			"must be a relative path, not starting with '/'"))
	} else if strings.Contains(path, "..") {
		errs = append(errs, field.Invalid(fldPath.Child("path"), path,
			"must not contain '..' (path traversal)"))
	}

	if ref.Name == "" {
		errs = append(errs, field.Required(fldPath.Child("configMapRef", "name"),
			"ConfigMap name is required"))
	}

	if ref.Key == "" {
		errs = append(errs, field.Required(fldPath.Child("configMapRef", "key"),
			"ConfigMap key is required"))
	}

	return errs
}

func invalidIfNotEmpty(errs field.ErrorList) error {
	if len(errs) == 0 {
		return nil
	}

	return errs.ToAggregate()
}
