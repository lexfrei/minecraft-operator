/*
Copyright 2026, Aleksei Sviridkin.

SPDX-License-Identifier: BSD-3-Clause
*/

package testutil

import (
	"context"
	"sync"
)

// MockRegistryAPI is a mock implementation of controller.RegistryAPI for testing.
type MockRegistryAPI struct {
	mu sync.Mutex

	// Configurable responses
	Tags       []string
	TagsErr    error
	ImageExist bool
	ImageErr   error

	// Per-tag existence for fine-grained control
	TagExists map[string]bool

	// Track calls
	ListTagsCalls   int
	ImageExistCalls []string
}

// ListTags returns the configured tags or error.
func (m *MockRegistryAPI) ListTags(_ context.Context, _ string, _ int) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.ListTagsCalls++

	return m.Tags, m.TagsErr
}

// ImageExists returns the configured existence or error.
func (m *MockRegistryAPI) ImageExists(_ context.Context, _ string, tag string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.ImageExistCalls = append(m.ImageExistCalls, tag)

	// Check per-tag existence first
	if m.TagExists != nil {
		if exists, ok := m.TagExists[tag]; ok {
			return exists, m.ImageErr
		}
	}

	return m.ImageExist, m.ImageErr
}
