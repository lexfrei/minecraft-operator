/*
Copyright 2026, Aleksei Sviridkin.

SPDX-License-Identifier: BSD-3-Clause
*/

package testutil

import (
	"context"
	"sync"

	"github.com/lexfrei/minecraft-operator/pkg/paper"
	"github.com/lexfrei/minecraft-operator/pkg/solver"
)

// MockPaperAPI is a mock implementation of controller.PaperAPI for testing.
type MockPaperAPI struct {
	mu sync.Mutex

	// Candidates configures the response for GetPaperCandidates.
	// If nil, it is synthesised from Versions by tagging each as STABLE channel.
	Candidates    []solver.PaperCandidate
	CandidatesErr error

	// Versions is a legacy field used only to synthesise Candidates when the
	// latter is nil. Keeps existing tests that only set Versions working.
	Versions    []string
	VersionsErr error

	BuildInfo    *paper.BuildInfo
	BuildInfoErr error
	BuildNumbers []int
	BuildsErr    error

	// Track calls
	GetCandidatesCalls int
	GetBuildCalls      []string
	GetBuildsCalls     []string
}

// GetPaperCandidates returns the configured candidates or error.
// If Candidates is nil, synthesises one STABLE candidate per Versions entry.
func (m *MockPaperAPI) GetPaperCandidates(_ context.Context) ([]solver.PaperCandidate, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.GetCandidatesCalls++

	if m.CandidatesErr != nil {
		return nil, m.CandidatesErr
	}
	if m.VersionsErr != nil {
		return nil, m.VersionsErr
	}
	if m.Candidates != nil {
		return m.Candidates, nil
	}

	out := make([]solver.PaperCandidate, 0, len(m.Versions))
	for _, v := range m.Versions {
		out = append(out, solver.PaperCandidate{Version: v, Channels: []string{"STABLE"}})
	}
	return out, nil
}

// GetPaperBuild returns the configured build info or error. The allowedChannels
// argument is accepted to match the interface but is not used to filter
// BuildInfo — tests configure BuildInfo explicitly.
func (m *MockPaperAPI) GetPaperBuild(_ context.Context, version string, _ []string) (*paper.BuildInfo, error) {
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
