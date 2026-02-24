/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

// Update strategy constants (unified for both Plugin and PaperMCServer).
const (
	updateStrategyLatest   = "latest"
	updateStrategyAuto     = "auto"
	updateStrategyPin      = "pin"
	updateStrategyBuildPin = "build-pin"
	// Legacy constants for backward compatibility.
	versionPolicyLatest = "latest"
	versionPolicyPinned = "pinned"
)

// Finalizer and annotation constants.
const (
	// PluginFinalizer is the finalizer added to Plugin resources to ensure JAR cleanup.
	PluginFinalizer = "mc.k8s.lex.la/plugin-cleanup"

	// AnnotationApplyNow triggers immediate update application, bypassing maintenance window.
	// Value should be a Unix timestamp (seconds since epoch) of when the annotation was set.
	// Annotations older than 5 minutes are ignored to prevent stale triggers.
	AnnotationApplyNow = "mc.k8s.lex.la/apply-now"

	// AnnotationBackupNow triggers immediate VolumeSnapshot backup.
	// Value should be a Unix timestamp (seconds since epoch) of when the annotation was set.
	// Annotations older than 5 minutes are ignored to prevent stale triggers.
	AnnotationBackupNow = "mc.k8s.lex.la/backup-now"
)
