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
	"context"
	"sync"
)

// ExecCall records a single call to ExecInPod.
type ExecCall struct {
	Namespace string
	PodName   string
	Container string
	Command   []string
}

// MockPodExecutor records calls and returns configurable responses.
type MockPodExecutor struct {
	mu     sync.Mutex
	Calls  []ExecCall
	Output []byte
	Err    error
}

// ExecInPod records the call and returns the configured output/error.
func (m *MockPodExecutor) ExecInPod(
	_ context.Context,
	namespace, podName, container string,
	command []string,
) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Calls = append(m.Calls, ExecCall{
		Namespace: namespace,
		PodName:   podName,
		Container: container,
		Command:   command,
	})

	return m.Output, m.Err
}
