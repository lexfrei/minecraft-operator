/*
Copyright 2026, Aleksei Sviridkin.

SPDX-License-Identifier: BSD-3-Clause
*/

package testutil

import (
	"context"
	"sync"

	"github.com/lexfrei/minecraft-operator/pkg/plugins"
)

// MockPluginClient implements plugins.PluginClient for controller testing.
type MockPluginClient struct {
	mu sync.Mutex

	// Responses
	Versions   []plugins.PluginVersion
	VersionErr error

	CompatInfo plugins.CompatibilityInfo
	CompatErr  error

	// Call tracking
	GetVersionsCalls    int
	GetVersionsProjects []string

	GetCompatCalls    int
	GetCompatProjects []string
	GetCompatVersions []string
}

// GetVersions returns configured versions or error.
func (m *MockPluginClient) GetVersions(ctx context.Context, project string) ([]plugins.PluginVersion, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.GetVersionsCalls++
	m.GetVersionsProjects = append(m.GetVersionsProjects, project)

	if m.VersionErr != nil {
		return nil, m.VersionErr
	}

	return m.Versions, nil
}

// GetCompatibility returns configured compatibility info or error.
func (m *MockPluginClient) GetCompatibility(
	ctx context.Context,
	project, version string,
) (plugins.CompatibilityInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.GetCompatCalls++
	m.GetCompatProjects = append(m.GetCompatProjects, project)
	m.GetCompatVersions = append(m.GetCompatVersions, version)

	if m.CompatErr != nil {
		return plugins.CompatibilityInfo{}, m.CompatErr
	}

	return m.CompatInfo, nil
}
