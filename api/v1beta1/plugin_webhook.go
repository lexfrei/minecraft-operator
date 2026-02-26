/*
Copyright 2026, Aleksei Sviridkin.

SPDX-License-Identifier: BSD-3-Clause
*/

package v1beta1

import (
	"context"
	"fmt"
	"net/url"

	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

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
		allErrs = append(allErrs, validateURLSource(p, specPath)...)
		if p.Spec.Source.Checksum == "" {
			warnings = append(warnings, "spec.source.checksum is not set; downloads will not be integrity-verified")
		}
	}

	// Strategy-specific validation.
	allErrs = append(allErrs, validatePluginStrategy(p, specPath)...)

	return warnings, invalidIfNotEmpty(allErrs)
}

func validateURLSource(p *Plugin, specPath *field.Path) field.ErrorList {
	var errs field.ErrorList

	urlPath := specPath.Child("source", "url")

	if p.Spec.Source.URL == "" {
		errs = append(errs, field.Required(urlPath, "url is required for source type 'url'"))

		return errs
	}

	parsed, err := url.Parse(p.Spec.Source.URL)
	if err != nil {
		errs = append(errs, field.Invalid(urlPath, p.Spec.Source.URL, fmt.Sprintf("invalid URL: %v", err)))

		return errs
	}

	if parsed.Scheme != "https" {
		errs = append(errs, field.Invalid(urlPath, p.Spec.Source.URL, "only https URLs are allowed"))
	}

	return errs
}

func validatePluginStrategy(p *Plugin, specPath *field.Path) field.ErrorList {
	var errs field.ErrorList

	switch p.Spec.UpdateStrategy {
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
	}

	return errs
}

func invalidIfNotEmpty(errs field.ErrorList) error {
	if len(errs) == 0 {
		return nil
	}

	return errs.ToAggregate()
}
