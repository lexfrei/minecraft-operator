/*
Copyright 2026, Aleksei Sviridkin.

SPDX-License-Identifier: BSD-3-Clause
*/

package cron

import (
	"context"
	"testing"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stopTracker implements Scheduler and tracks Stop() calls.
type stopTracker struct {
	stopped bool
	stopCh  chan struct{}
}

func (s *stopTracker) AddFunc(_ string, _ func()) (cron.EntryID, error) { return 0, nil }
func (s *stopTracker) Remove(_ cron.EntryID)                            {}
func (s *stopTracker) Entries() []cron.Entry                            { return nil }
func (s *stopTracker) Start()                                           {}

func (s *stopTracker) Stop() {
	s.stopped = true
	close(s.stopCh)
}

func TestCronRunnable_StopsSchedulerOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	stopCalled := make(chan struct{})
	scheduler := &stopTracker{stopCh: stopCalled}

	runnable := NewCronRunnable(scheduler)

	// Run the runnable in a goroutine.
	errCh := make(chan error, 1)
	go func() {
		errCh <- runnable.Start(ctx)
	}()

	// Cancel context — should trigger Stop().
	cancel()

	// Wait for Stop() to be called.
	select {
	case <-stopCalled:
		// Success.
	case <-time.After(5 * time.Second):
		t.Fatal("Stop() was not called within timeout")
	}

	// Verify Start returned nil error.
	err := <-errCh
	require.NoError(t, err)
	assert.True(t, scheduler.stopped, "scheduler.Stop() should have been called")
}
