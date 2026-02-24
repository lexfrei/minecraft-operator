/*
Copyright 2026.

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

import "github.com/robfig/cron/v3"

// cronEntryInfo stores both the cron entry ID and the spec string
// to avoid needing to retrieve the spec from the scheduler.
// Used by both UpdateReconciler and BackupReconciler.
type cronEntryInfo struct {
	ID   cron.EntryID
	Spec string
}
