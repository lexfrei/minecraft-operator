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
