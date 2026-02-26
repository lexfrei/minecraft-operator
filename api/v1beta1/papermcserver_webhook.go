/*
Copyright 2026, Aleksei Sviridkin.

SPDX-License-Identifier: BSD-3-Clause
*/

package v1beta1

import (
	"context"
	"fmt"

	"github.com/robfig/cron/v3"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
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
	var allErrs field.ErrorList

	specPath := field.NewPath("spec")

	allErrs = append(allErrs, validateServerStrategy(s, specPath)...)
	allErrs = append(allErrs, validateCronExpressions(s, specPath)...)
	allErrs = append(allErrs, validateRCON(s, specPath)...)
	allErrs = append(allErrs, validateBackup(s, specPath)...)
	allErrs = append(allErrs, validateGateway(s, specPath)...)

	return nil, invalidIfNotEmpty(allErrs)
}

func validateServerStrategy(s *PaperMCServer, specPath *field.Path) field.ErrorList {
	var errs field.ErrorList

	switch s.Spec.UpdateStrategy {
	case "pin":
		if s.Spec.Version == "" {
			errs = append(errs, field.Required(
				specPath.Child("version"),
				"version is required for 'pin' strategy",
			))
		}
	case "build-pin":
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

	if _, err := cronParser.Parse(s.Spec.UpdateSchedule.MaintenanceWindow.Cron); err != nil {
		errs = append(errs, field.Invalid(
			schedulePath.Child("maintenanceWindow", "cron"),
			s.Spec.UpdateSchedule.MaintenanceWindow.Cron,
			fmt.Sprintf("invalid cron expression: %v", err),
		))
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

	if s.Spec.Gateway == nil || !s.Spec.Gateway.Enabled {
		return nil
	}

	if len(s.Spec.Gateway.ParentRefs) == 0 {
		errs = append(errs, field.Required(
			specPath.Child("gateway", "parentRefs"),
			"at least one parentRef is required when gateway is enabled",
		))
	}

	return errs
}
