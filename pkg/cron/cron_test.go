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
