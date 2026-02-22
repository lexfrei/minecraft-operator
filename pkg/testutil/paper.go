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

	"github.com/lexfrei/minecraft-operator/pkg/paper"
)

// MockPaperAPI is a mock implementation of controller.PaperAPI for testing.
type MockPaperAPI struct {
	mu sync.Mutex

	// Configurable responses
	Versions     []string
	VersionsErr  error
	BuildInfo    *paper.BuildInfo
	BuildInfoErr error
	BuildNumbers []int
	BuildsErr    error

	// Track calls
	GetVersionsCalls int
	GetBuildCalls    []string
	GetBuildsCalls   []string
}

// GetPaperVersions returns the configured versions or error.
func (m *MockPaperAPI) GetPaperVersions(_ context.Context) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.GetVersionsCalls++

	return m.Versions, m.VersionsErr
}

// GetPaperBuild returns the configured build info or error.
func (m *MockPaperAPI) GetPaperBuild(_ context.Context, version string) (*paper.BuildInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.GetBuildCalls = append(m.GetBuildCalls, version)

	return m.BuildInfo, m.BuildInfoErr
}

// GetBuilds returns the configured build numbers or error.
func (m *MockPaperAPI) GetBuilds(_ context.Context, version string) ([]int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.GetBuildsCalls = append(m.GetBuildsCalls, version)

	return m.BuildNumbers, m.BuildsErr
}
