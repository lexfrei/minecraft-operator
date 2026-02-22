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

package testutil

import (
	"sync"

	"github.com/robfig/cron/v3"
)

// MockCronScheduler is a mock cron scheduler for testing.
type MockCronScheduler struct {
	mu sync.Mutex

	// Track added jobs
	Jobs map[cron.EntryID]*MockCronJob

	// Auto-increment ID
	nextID cron.EntryID

	// Control behavior
	AddFuncError error
	Started      bool
}

// MockCronJob represents a cron job in the mock.
type MockCronJob struct {
	ID      cron.EntryID
	Spec    string
	Func    func()
	Removed bool
}

// NewMockCronScheduler creates a new mock cron scheduler.
func NewMockCronScheduler() *MockCronScheduler {
	return &MockCronScheduler{
		Jobs:   make(map[cron.EntryID]*MockCronJob),
		nextID: 1,
	}
}

// AddFunc adds a cron job to the mock.
func (m *MockCronScheduler) AddFunc(spec string, cmd func()) (cron.EntryID, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.AddFuncError != nil {
		return 0, m.AddFuncError
	}

	// Validate cron expression
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	if _, err := parser.Parse(spec); err != nil {
		return 0, err
	}

	id := m.nextID
	m.nextID++

	m.Jobs[id] = &MockCronJob{
		ID:   id,
		Spec: spec,
		Func: cmd,
	}

	return id, nil
}

// Remove removes a cron job from the mock.
func (m *MockCronScheduler) Remove(id cron.EntryID) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if job, ok := m.Jobs[id]; ok {
		job.Removed = true
	}
}

// Start marks the scheduler as started.
func (m *MockCronScheduler) Start() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Started = true
}

// Stop marks the scheduler as stopped.
func (m *MockCronScheduler) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Started = false
}

// Entries returns all cron entries (mock implementation).
func (m *MockCronScheduler) Entries() []cron.Entry {
	m.mu.Lock()
	defer m.mu.Unlock()

	entries := make([]cron.Entry, 0, len(m.Jobs))
	for _, job := range m.Jobs {
		if !job.Removed {
			entries = append(entries, cron.Entry{
				ID: job.ID,
			})
		}
	}
	return entries
}

// TriggerJob manually triggers a cron job by ID (for testing).
func (m *MockCronScheduler) TriggerJob(id cron.EntryID) {
	m.mu.Lock()
	job, ok := m.Jobs[id]
	m.mu.Unlock()

	if ok && !job.Removed && job.Func != nil {
		job.Func()
	}
}

// GetJob returns a cron job by ID (for testing).
func (m *MockCronScheduler) GetJob(id cron.EntryID) *MockCronJob {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.Jobs[id]
}

// GetJobBySpec returns the first cron job matching the spec (for testing).
func (m *MockCronScheduler) GetJobBySpec(spec string) *MockCronJob {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, job := range m.Jobs {
		if job.Spec == spec && !job.Removed {
			return job
		}
	}
	return nil
}

// Reset clears all jobs.
func (m *MockCronScheduler) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Jobs = make(map[cron.EntryID]*MockCronJob)
	m.nextID = 1
	m.AddFuncError = nil
	m.Started = false
}
