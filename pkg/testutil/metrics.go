/*
Copyright 2026, Aleksei Sviridkin.

SPDX-License-Identifier: BSD-3-Clause
*/

package testutil

import (
	"sync"
	"time"
)

// MockMetricsRecorder implements metrics.Recorder with call tracking.
type MockMetricsRecorder struct {
	mu sync.Mutex

	PluginAPICalls   int
	PluginAPISources []string
}

// RecordReconcile is a no-op.
func (m *MockMetricsRecorder) RecordReconcile(_ string, _ error, _ time.Duration) {}

// RecordPluginAPICall records the source type.
func (m *MockMetricsRecorder) RecordPluginAPICall(source string, _ error, _ time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.PluginAPICalls++
	m.PluginAPISources = append(m.PluginAPISources, source)
}

// RecordSolverRun is a no-op.
func (m *MockMetricsRecorder) RecordSolverRun(_ string, _ error, _ time.Duration) {}

// RecordUpdate is a no-op.
func (m *MockMetricsRecorder) RecordUpdate(_ bool) {}
