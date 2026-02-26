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

func invalidIfNotEmpty(errs field.ErrorList) error {
	if len(errs) == 0 {
		return nil
	}

	return errs.ToAggregate()
}
