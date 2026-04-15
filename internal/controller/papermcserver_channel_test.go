/*
Copyright 2026, Aleksei Sviridkin.

SPDX-License-Identifier: BSD-3-Clause
*/

package controller

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mcv1beta1 "github.com/lexfrei/minecraft-operator/api/v1beta1"
	"github.com/lexfrei/minecraft-operator/pkg/paper"
	"github.com/lexfrei/minecraft-operator/pkg/solver"
	"github.com/lexfrei/minecraft-operator/pkg/testutil"
)

// Direct unit test of findVersionUpdate focused on channel behaviour.
// Does not depend on envtest — uses mock PaperAPI + real solver.

func newChannelReconciler(mockPaper *testutil.MockPaperAPI) *PaperMCServerReconciler {
	return &PaperMCServerReconciler{
		PaperClient: mockPaper,
		Solver:      solver.NewSimpleSolver(),
	}
}

func TestFindVersionUpdate_StableChannel_IgnoresAlphaVersion(t *testing.T) {
	t.Parallel()

	mockPaper := &testutil.MockPaperAPI{
		Candidates: []solver.PaperCandidate{
			{Version: "26.1.2", Channels: []string{"ALPHA"}},
			{Version: "1.21.11", Channels: []string{"STABLE"}},
		},
		BuildInfo: &paper.BuildInfo{Version: "1.21.11", Build: 130},
	}

	r := newChannelReconciler(mockPaper)
	server := &mcv1beta1.PaperMCServer{
		Spec: mcv1beta1.PaperMCServerSpec{
			UpdateStrategy: "latest",
			Channel:        "stable",
		},
		Status: mcv1beta1.PaperMCServerStatus{
			CurrentVersion: "1.21.11",
		},
	}

	update, err := r.findVersionUpdate(context.Background(), server, nil)

	require.NoError(t, err)
	assert.Nil(t, update, "stable channel with current=1.21.11 must report no available update")
}

func TestFindVersionUpdate_ExperimentalChannel_SelectsAlphaVersion(t *testing.T) {
	t.Parallel()

	mockPaper := &testutil.MockPaperAPI{
		Candidates: []solver.PaperCandidate{
			{Version: "26.1.2", Channels: []string{"ALPHA"}},
			{Version: "1.21.11", Channels: []string{"STABLE"}},
		},
		BuildInfo: &paper.BuildInfo{Version: "26.1.2", Build: 2},
	}

	r := newChannelReconciler(mockPaper)
	server := &mcv1beta1.PaperMCServer{
		Spec: mcv1beta1.PaperMCServerSpec{
			UpdateStrategy: "latest",
			Channel:        "experimental",
		},
		Status: mcv1beta1.PaperMCServerStatus{
			CurrentVersion: "1.21.11",
		},
	}

	update, err := r.findVersionUpdate(context.Background(), server, nil)

	require.NoError(t, err)
	require.NotNil(t, update, "experimental channel must surface ALPHA as available update")
	assert.Equal(t, "26.1.2", update.Version)
}

func TestFindVersionUpdate_DefaultChannel_BehavesAsStable(t *testing.T) {
	t.Parallel()

	mockPaper := &testutil.MockPaperAPI{
		Candidates: []solver.PaperCandidate{
			{Version: "26.1.2", Channels: []string{"ALPHA"}},
			{Version: "1.21.11", Channels: []string{"STABLE"}},
		},
		BuildInfo: &paper.BuildInfo{Version: "1.21.11", Build: 130},
	}

	r := newChannelReconciler(mockPaper)
	server := &mcv1beta1.PaperMCServer{
		Spec: mcv1beta1.PaperMCServerSpec{
			UpdateStrategy: "latest",
			Channel:        "",
		},
		Status: mcv1beta1.PaperMCServerStatus{
			CurrentVersion: "1.21.11",
		},
	}

	update, err := r.findVersionUpdate(context.Background(), server, nil)

	require.NoError(t, err)
	assert.Nil(t, update, "empty channel must behave like stable")
}
