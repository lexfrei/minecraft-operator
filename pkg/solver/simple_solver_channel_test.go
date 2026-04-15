package solver

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mcv1beta1 "github.com/lexfrei/minecraft-operator/api/v1beta1"
)

// stableCandidates wraps a slice of version strings as PaperCandidate values
// tagged with STABLE channel. Used by legacy tests that predate channel awareness.
func stableCandidates(versions []string) []PaperCandidate {
	out := make([]PaperCandidate, 0, len(versions))
	for _, v := range versions {
		out = append(out, PaperCandidate{Version: v, Channels: []string{channelStable}})
	}
	return out
}

func TestFindBestPaperVersion_StableChannel_SkipsAlphaCandidate(t *testing.T) {
	t.Parallel()

	solver := NewSimpleSolver()
	server := mcv1beta1.PaperMCServer{
		Spec: mcv1beta1.PaperMCServerSpec{
			UpdateStrategy: "latest",
			Channel:        "stable",
		},
	}
	candidates := []PaperCandidate{
		{Version: "26.1.2", Channels: []string{"ALPHA"}},
		{Version: "1.21.11", Channels: []string{"STABLE"}},
	}

	result, err := solver.FindBestPaperVersion(context.Background(), &server, nil, candidates)

	require.NoError(t, err)
	assert.Equal(t, "1.21.11", result, "stable channel must not select ALPHA candidate even if semver is higher")
}

func TestFindBestPaperVersion_ExperimentalChannel_AllowsAlphaCandidate(t *testing.T) {
	t.Parallel()

	solver := NewSimpleSolver()
	server := mcv1beta1.PaperMCServer{
		Spec: mcv1beta1.PaperMCServerSpec{
			UpdateStrategy: "latest",
			Channel:        "experimental",
		},
	}
	candidates := []PaperCandidate{
		{Version: "26.1.2", Channels: []string{"ALPHA"}},
		{Version: "1.21.11", Channels: []string{"STABLE"}},
	}

	result, err := solver.FindBestPaperVersion(context.Background(), &server, nil, candidates)

	require.NoError(t, err)
	assert.Equal(t, "26.1.2", result, "experimental channel must allow ALPHA candidate")
}

func TestFindBestPaperVersion_UnsetChannel_DefaultsToStable(t *testing.T) {
	t.Parallel()

	solver := NewSimpleSolver()
	server := mcv1beta1.PaperMCServer{
		Spec: mcv1beta1.PaperMCServerSpec{
			UpdateStrategy: "latest",
			Channel:        "",
		},
	}
	candidates := []PaperCandidate{
		{Version: "26.1.2", Channels: []string{"ALPHA"}},
		{Version: "1.21.11", Channels: []string{"STABLE"}},
	}

	result, err := solver.FindBestPaperVersion(context.Background(), &server, nil, candidates)

	require.NoError(t, err)
	assert.Equal(t, "1.21.11", result, "empty channel must default to stable behavior")
}

func TestFindBestPaperVersion_MixedChannels_StableSelectsIfAny(t *testing.T) {
	t.Parallel()

	solver := NewSimpleSolver()
	server := mcv1beta1.PaperMCServer{
		Spec: mcv1beta1.PaperMCServerSpec{
			UpdateStrategy: "latest",
			Channel:        "stable",
		},
	}
	candidates := []PaperCandidate{
		{Version: "1.21.11", Channels: []string{"STABLE", "BETA"}},
	}

	result, err := solver.FindBestPaperVersion(context.Background(), &server, nil, candidates)

	require.NoError(t, err)
	assert.Equal(t, "1.21.11", result, "version with at least one STABLE build must be selected under stable channel")
}

func TestFindBestPaperVersion_StableChannel_OnlyAlphaCandidates_ReturnsError(t *testing.T) {
	t.Parallel()

	solver := NewSimpleSolver()
	server := mcv1beta1.PaperMCServer{
		Spec: mcv1beta1.PaperMCServerSpec{
			UpdateStrategy: "latest",
			Channel:        "stable",
		},
	}
	candidates := []PaperCandidate{
		{Version: "26.1.2", Channels: []string{"ALPHA"}},
	}

	_, err := solver.FindBestPaperVersion(context.Background(), &server, nil, candidates)

	require.Error(t, err, "no eligible stable candidates must be an error")
}

func TestAllowedChannels_StableSpec(t *testing.T) {
	t.Parallel()

	got := AllowedChannels("stable")
	assert.ElementsMatch(t, []string{"STABLE"}, got)
}

func TestAllowedChannels_ExperimentalSpec(t *testing.T) {
	t.Parallel()

	got := AllowedChannels("experimental")
	assert.ElementsMatch(t, []string{"STABLE", "BETA", "ALPHA"}, got)
}

func TestAllowedChannels_EmptySpec_DefaultsToStable(t *testing.T) {
	t.Parallel()

	got := AllowedChannels("")
	assert.ElementsMatch(t, []string{"STABLE"}, got)
}
