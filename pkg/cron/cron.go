/*
Copyright 2026, Aleksei Sviridkin.

SPDX-License-Identifier: BSD-3-Clause
*/

// Package cron provides a cron scheduling interface and its production implementation.
package cron

import (
	"context"

	"github.com/robfig/cron/v3"
)

// Scheduler is an interface for cron scheduling operations.
type Scheduler interface {
	AddFunc(spec string, cmd func()) (cron.EntryID, error)
	Remove(id cron.EntryID)
	Start()
	Stop()
	Entries() []cron.Entry
}

// RealScheduler wraps robfig/cron for production use.
type RealScheduler struct {
	*cron.Cron
}

// NewRealScheduler creates a production cron scheduler.
func NewRealScheduler() *RealScheduler {
	return &RealScheduler{
		Cron: cron.New(),
	}
}

// Start starts the cron scheduler.
func (r *RealScheduler) Start() {
	r.Cron.Start()
}

// Stop stops the cron scheduler.
func (r *RealScheduler) Stop() {
	ctx := r.Cron.Stop()
	<-ctx.Done()
}

// CronRunnable adapts a Scheduler to the manager.Runnable interface.
// It blocks until the context is cancelled, then stops the scheduler.
type CronRunnable struct {
	scheduler Scheduler
}

// NewCronRunnable creates a Runnable that stops the scheduler when the manager shuts down.
func NewCronRunnable(s Scheduler) *CronRunnable {
	return &CronRunnable{scheduler: s}
}

// Start blocks until ctx is done, then calls Stop on the scheduler.
func (r *CronRunnable) Start(ctx context.Context) error {
	<-ctx.Done()
	r.scheduler.Stop()

	return nil
}
