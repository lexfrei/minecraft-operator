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
