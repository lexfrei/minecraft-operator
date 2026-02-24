/*
Copyright 2026, Aleksei Sviridkin.

SPDX-License-Identifier: BSD-3-Clause
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
