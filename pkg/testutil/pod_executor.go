/*
Copyright 2026, Aleksei Sviridkin.

SPDX-License-Identifier: BSD-3-Clause
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
