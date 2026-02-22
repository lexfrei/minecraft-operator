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
