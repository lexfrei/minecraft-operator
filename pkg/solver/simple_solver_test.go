package solver

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	mcv1beta1 "github.com/lexfrei/minecraft-operator/api/v1beta1"
	"github.com/lexfrei/minecraft-operator/pkg/plugins"
	"github.com/lexfrei/minecraft-operator/pkg/version"
)

// Test fixtures.
func makePlugin(strategy, ver string) *mcv1beta1.Plugin {
	return &mcv1beta1.Plugin{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-plugin",
			Namespace: "default",
		},
		Spec: mcv1beta1.PluginSpec{
			UpdateStrategy: strategy,
			Version:        ver,
		},
	}
}

func makeServer(name, currentVersion, specVersion string) mcv1beta1.PaperMCServer {
	return mcv1beta1.PaperMCServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: mcv1beta1.PaperMCServerSpec{
			Version: specVersion,
		},
		Status: mcv1beta1.PaperMCServerStatus{
			CurrentVersion: currentVersion,
		},
	}
}

func makePluginVersion(ver string, mcVersions []string, releaseDate time.Time) plugins.PluginVersion {
	return plugins.PluginVersion{
		Version:           ver,
		MinecraftVersions: mcVersions,
		ReleaseDate:       releaseDate,
	}
}

// --- Helper function tests ---

func TestContainsVersion_ExactMatch(t *testing.T) {
	t.Parallel()

	versions := []string{"1.20.1", "1.20.4", "1.21.1"}

	assert.True(t, version.ContainsVersion(versions, "1.20.1"))
	assert.True(t, version.ContainsVersion(versions, "1.21.1"))
	assert.False(t, version.ContainsVersion(versions, "1.19.4"))
}

func TestContainsVersion_PatternMatch(t *testing.T) {
	t.Parallel()

	versions := []string{"1.21.x", "1.20.4"}

	assert.True(t, version.ContainsVersion(versions, "1.21.1"))
	assert.True(t, version.ContainsVersion(versions, "1.21.4"))
	assert.True(t, version.ContainsVersion(versions, "1.20.4"))
	assert.False(t, version.ContainsVersion(versions, "1.19.4"))
	assert.False(t, version.ContainsVersion(versions, "1.20.1"))
}

func TestContainsVersion_EmptyList(t *testing.T) {
	t.Parallel()

	assert.False(t, version.ContainsVersion([]string{}, "1.21.1"))
	assert.False(t, version.ContainsVersion(nil, "1.21.1"))
}

func TestMatchesVersionPattern_WithXSuffix(t *testing.T) {
	t.Parallel()

	assert.True(t, version.MatchesVersionPattern("1.21.x", "1.21.1"))
	assert.True(t, version.MatchesVersionPattern("1.21.x", "1.21.4"))
	assert.True(t, version.MatchesVersionPattern("1.20.x", "1.20.6"))
	assert.False(t, version.MatchesVersionPattern("1.21.x", "1.20.1"))
	assert.False(t, version.MatchesVersionPattern("1.21.x", "1.22.1"))
}

func TestMatchesVersionPattern_WithoutXSuffix(t *testing.T) {
	t.Parallel()

	assert.False(t, version.MatchesVersionPattern("1.21.1", "1.21.1"))
	assert.False(t, version.MatchesVersionPattern("1.21", "1.21.1"))
}

func TestSortVersionsDesc(t *testing.T) {
	t.Parallel()

	now := time.Now()
	versions := []plugins.PluginVersion{
		makePluginVersion("1.0.0", nil, now),
		makePluginVersion("2.0.0", nil, now),
		makePluginVersion("1.5.0", nil, now),
		makePluginVersion("1.5.1", nil, now),
	}

	sorted := sortVersionsDesc(versions)

	require.Len(t, sorted, 4)
	assert.Equal(t, "2.0.0", sorted[0].Version)
	assert.Equal(t, "1.5.1", sorted[1].Version)
	assert.Equal(t, "1.5.0", sorted[2].Version)
	assert.Equal(t, "1.0.0", sorted[3].Version)
}

func TestSortVersionsDesc_SkipsInvalidSemver(t *testing.T) {
	t.Parallel()

	now := time.Now()
	versions := []plugins.PluginVersion{
		makePluginVersion("1.0.0", nil, now),
		makePluginVersion("invalid-version", nil, now),
		makePluginVersion("2.0.0", nil, now),
	}

	sorted := sortVersionsDesc(versions)

	// Invalid semver versions are not compared, so they stay in place
	// Valid versions get sorted around them
	require.Len(t, sorted, 3)
	assert.Equal(t, "2.0.0", sorted[0].Version)
	// invalid-version stays at index 1 since it can't be compared
	assert.Equal(t, "invalid-version", sorted[1].Version)
	assert.Equal(t, "1.0.0", sorted[2].Version)
}

func TestSortPaperVersionsDesc(t *testing.T) {
	t.Parallel()

	versions := []string{"1.20.1", "1.21.4", "1.20.6", "1.21.1"}

	sorted := sortPaperVersionsDesc(versions)

	require.Len(t, sorted, 4)
	assert.Equal(t, "1.21.4", sorted[0])
	assert.Equal(t, "1.21.1", sorted[1])
	assert.Equal(t, "1.20.6", sorted[2])
	assert.Equal(t, "1.20.1", sorted[3])
}

func TestFilterByDelay_FiltersNewVersions(t *testing.T) {
	t.Parallel()

	now := time.Now()
	versions := []plugins.PluginVersion{
		makePluginVersion("1.0.0", nil, now.Add(-48*time.Hour)), // 2 days ago
		makePluginVersion("2.0.0", nil, now.Add(-1*time.Hour)),  // 1 hour ago
		makePluginVersion("3.0.0", nil, now),                    // now
	}

	filtered := filterByDelay(versions, 24*time.Hour)

	require.Len(t, filtered, 1)
	assert.Equal(t, "1.0.0", filtered[0].Version)
}

func TestFilterByDelay_ZeroDelay_ReturnsAll(t *testing.T) {
	t.Parallel()

	now := time.Now()
	versions := []plugins.PluginVersion{
		makePluginVersion("1.0.0", nil, now.Add(-48*time.Hour)),
		makePluginVersion("2.0.0", nil, now),
	}

	filtered := filterByDelay(versions, 0)

	require.Len(t, filtered, 2)
}

func TestIsPluginCompatibleWithServer_ExactMatch(t *testing.T) {
	t.Parallel()

	pv := makePluginVersion("1.0.0", []string{"1.21.1", "1.21.4"}, time.Now())
	server := makeServer("test", "1.21.1", "")
	plugin := makePlugin("latest", "")

	assert.True(t, isPluginCompatibleWithServer(pv, &server, plugin))
}

func TestIsPluginCompatibleWithServer_NoMatch(t *testing.T) {
	t.Parallel()

	pv := makePluginVersion("1.0.0", []string{"1.20.1", "1.20.4"}, time.Now())
	server := makeServer("test", "1.21.1", "")
	plugin := makePlugin("latest", "")

	assert.False(t, isPluginCompatibleWithServer(pv, &server, plugin))
}

func TestIsPluginCompatibleWithServer_EmptyVersions_AssumesCompatible(t *testing.T) {
	t.Parallel()

	pv := makePluginVersion("1.0.0", []string{}, time.Now())
	server := makeServer("test", "1.21.1", "")
	plugin := makePlugin("latest", "")

	assert.True(t, isPluginCompatibleWithServer(pv, &server, plugin))
}

func TestIsPluginCompatibleWithServer_UsesSpecVersion_WhenStatusEmpty(t *testing.T) {
	t.Parallel()

	pv := makePluginVersion("1.0.0", []string{"1.21.1"}, time.Now())
	server := makeServer("test", "", "1.21.1")
	plugin := makePlugin("latest", "")

	assert.True(t, isPluginCompatibleWithServer(pv, &server, plugin))
}

func TestIsPluginCompatibleWithServer_CompatibilityOverride(t *testing.T) {
	t.Parallel()

	pv := makePluginVersion("1.0.0", []string{"1.20.1"}, time.Now())
	server := makeServer("test", "1.21.1", "")
	plugin := makePlugin("latest", "")
	plugin.Spec.CompatibilityOverride = &mcv1beta1.CompatibilityOverride{
		Enabled:           true,
		MinecraftVersions: []string{"1.21.1", "1.21.4"},
	}

	assert.True(t, isPluginCompatibleWithServer(pv, &server, plugin))
}

func TestIsPluginCompatibleWithServer_CompatibilityOverride_EmptyVersions(t *testing.T) {
	t.Parallel()

	pv := makePluginVersion("1.0.0", []string{"1.20.1"}, time.Now())
	server := makeServer("test", "1.21.1", "")
	plugin := makePlugin("latest", "")
	plugin.Spec.CompatibilityOverride = &mcv1beta1.CompatibilityOverride{
		Enabled:           true,
		MinecraftVersions: []string{},
	}

	assert.True(t, isPluginCompatibleWithServer(pv, &server, plugin))
}

// --- FindBestPluginVersion tests ---

func TestFindBestPluginVersion_PinStrategy_ReturnsExactVersion(t *testing.T) {
	t.Parallel()

	solver := NewSimpleSolver()
	plugin := makePlugin("pin", "1.5.0")
	servers := []mcv1beta1.PaperMCServer{makeServer("s1", "1.21.1", "")}
	versions := []plugins.PluginVersion{
		makePluginVersion("2.0.0", []string{"1.21.1"}, time.Now()),
		makePluginVersion("1.5.0", []string{"1.21.1"}, time.Now()),
	}

	result, err := solver.FindBestPluginVersion(context.Background(), plugin, servers, versions)

	require.NoError(t, err)
	assert.Equal(t, "1.5.0", result)
}

func TestFindBestPluginVersion_BuildPinStrategy_ReturnsExactVersion(t *testing.T) {
	t.Parallel()

	solver := NewSimpleSolver()
	plugin := makePlugin("build-pin", "1.2.3")
	servers := []mcv1beta1.PaperMCServer{makeServer("s1", "1.21.1", "")}
	versions := []plugins.PluginVersion{
		makePluginVersion("2.0.0", []string{"1.21.1"}, time.Now()),
	}

	result, err := solver.FindBestPluginVersion(context.Background(), plugin, servers, versions)

	require.NoError(t, err)
	assert.Equal(t, "1.2.3", result)
}

func TestFindBestPluginVersion_LatestStrategy_FindsNewest(t *testing.T) {
	t.Parallel()

	solver := NewSimpleSolver()
	plugin := makePlugin("latest", "")
	servers := []mcv1beta1.PaperMCServer{makeServer("s1", "1.21.1", "")}
	now := time.Now()
	versions := []plugins.PluginVersion{
		makePluginVersion("1.0.0", []string{"1.21.1"}, now.Add(-48*time.Hour)),
		makePluginVersion("2.0.0", []string{"1.21.1"}, now.Add(-48*time.Hour)),
		makePluginVersion("1.5.0", []string{"1.21.1"}, now.Add(-48*time.Hour)),
	}

	result, err := solver.FindBestPluginVersion(context.Background(), plugin, servers, versions)

	require.NoError(t, err)
	assert.Equal(t, "2.0.0", result)
}

func TestFindBestPluginVersion_NoVersionsAvailable_ReturnsError(t *testing.T) {
	t.Parallel()

	solver := NewSimpleSolver()
	plugin := makePlugin("latest", "")
	servers := []mcv1beta1.PaperMCServer{makeServer("s1", "1.21.1", "")}

	_, err := solver.FindBestPluginVersion(context.Background(), plugin, servers, []plugins.PluginVersion{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no versions available")
}

func TestFindBestPluginVersion_NoServersMatched_ReturnsError(t *testing.T) {
	t.Parallel()

	solver := NewSimpleSolver()
	plugin := makePlugin("latest", "")
	versions := []plugins.PluginVersion{
		makePluginVersion("1.0.0", []string{"1.21.1"}, time.Now()),
	}

	_, err := solver.FindBestPluginVersion(context.Background(), plugin, []mcv1beta1.PaperMCServer{}, versions)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no servers matched")
}

func TestFindBestPluginVersion_NoCompatibleVersion_ReturnsError(t *testing.T) {
	t.Parallel()

	solver := NewSimpleSolver()
	plugin := makePlugin("latest", "")
	servers := []mcv1beta1.PaperMCServer{makeServer("s1", "1.21.1", "")}
	now := time.Now()
	versions := []plugins.PluginVersion{
		makePluginVersion("1.0.0", []string{"1.20.1"}, now.Add(-48*time.Hour)),
		makePluginVersion("2.0.0", []string{"1.20.4"}, now.Add(-48*time.Hour)),
	}

	_, err := solver.FindBestPluginVersion(context.Background(), plugin, servers, versions)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no plugin version compatible")
}

func TestFindBestPluginVersion_UpdateDelay_FiltersNewVersions(t *testing.T) {
	t.Parallel()

	solver := NewSimpleSolver()
	plugin := makePlugin("latest", "")
	plugin.Spec.UpdateDelay = &metav1.Duration{Duration: 24 * time.Hour}
	servers := []mcv1beta1.PaperMCServer{makeServer("s1", "1.21.1", "")}
	now := time.Now()
	versions := []plugins.PluginVersion{
		makePluginVersion("1.0.0", []string{"1.21.1"}, now.Add(-48*time.Hour)),
		makePluginVersion("2.0.0", []string{"1.21.1"}, now.Add(-1*time.Hour)), // too new
	}

	result, err := solver.FindBestPluginVersion(context.Background(), plugin, servers, versions)

	require.NoError(t, err)
	assert.Equal(t, "1.0.0", result)
}

func TestFindBestPluginVersion_UpdateDelay_AllFiltered_ReturnsError(t *testing.T) {
	t.Parallel()

	solver := NewSimpleSolver()
	plugin := makePlugin("latest", "")
	plugin.Spec.UpdateDelay = &metav1.Duration{Duration: 24 * time.Hour}
	servers := []mcv1beta1.PaperMCServer{makeServer("s1", "1.21.1", "")}
	now := time.Now()
	versions := []plugins.PluginVersion{
		makePluginVersion("1.0.0", []string{"1.21.1"}, now), // too new
	}

	_, err := solver.FindBestPluginVersion(context.Background(), plugin, servers, versions)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "updateDelay filtering")
}

func TestFindBestPaperVersion_UpdateDelay_ShouldNotFilterAllVersions(t *testing.T) {
	t.Parallel()

	solver := NewSimpleSolver()

	// Server with updateStrategy=auto and 1-hour updateDelay
	server := makeServer("test-server", "1.21.0", "")
	server.Spec.UpdateStrategy = "auto"
	server.Spec.UpdateDelay = &metav1.Duration{Duration: 1 * time.Hour}

	// Plugin compatible with both Paper versions
	plugin := mcv1beta1.Plugin{
		ObjectMeta: metav1.ObjectMeta{Name: "test-plugin", Namespace: "default"},
		Spec:       mcv1beta1.PluginSpec{UpdateStrategy: "latest"},
		Status: mcv1beta1.PluginStatus{
			AvailableVersions: []mcv1beta1.PluginVersionInfo{
				{Version: "1.0.0", MinecraftVersions: []string{"1.21.0", "1.21.1"}},
			},
		},
	}

	// Paper versions — these have existed for weeks in reality,
	// but the solver sets ReleaseDate=time.Now() internally
	paperVersions := []string{"1.21.0", "1.21.1"}

	// BUG: Currently fails because Paper versions get ReleaseDate=time.Now(),
	// which means they're always "just released" and updateDelay filters ALL of them out.
	// Expected: solver should skip updateDelay for Paper versions (no release dates available)
	// or track first-seen dates.
	result, err := solver.FindBestPaperVersion(
		context.Background(),
		&server,
		[]mcv1beta1.Plugin{plugin},
		paperVersions,
	)

	require.NoError(t, err, "FindBestPaperVersion should not error when Paper versions exist")
	assert.NotEmpty(t, result, "Should find a compatible Paper version even with updateDelay")
}

func TestFindBestPluginVersion_MultipleServers_MustSatisfyAll(t *testing.T) {
	t.Parallel()

	solver := NewSimpleSolver()
	plugin := makePlugin("latest", "")
	servers := []mcv1beta1.PaperMCServer{
		makeServer("s1", "1.21.1", ""),
		makeServer("s2", "1.20.4", ""),
	}
	now := time.Now()
	versions := []plugins.PluginVersion{
		makePluginVersion("2.0.0", []string{"1.21.1"}, now.Add(-48*time.Hour)),           // only 1.21.1
		makePluginVersion("1.5.0", []string{"1.21.1", "1.20.4"}, now.Add(-48*time.Hour)), // both
		makePluginVersion("1.0.0", []string{"1.20.4"}, now.Add(-48*time.Hour)),           // only 1.20.4
	}

	result, err := solver.FindBestPluginVersion(context.Background(), plugin, servers, versions)

	require.NoError(t, err)
	assert.Equal(t, "1.5.0", result)
}

// --- FindBestPaperVersion tests ---

func TestFindBestPaperVersion_PinStrategy_ReturnsExactVersion(t *testing.T) {
	t.Parallel()

	solver := NewSimpleSolver()
	server := mcv1beta1.PaperMCServer{
		Spec: mcv1beta1.PaperMCServerSpec{
			UpdateStrategy: "pin",
			Version:        "1.20.4",
		},
	}
	paperVersions := []string{"1.21.1", "1.21.4", "1.20.4"}

	result, err := solver.FindBestPaperVersion(context.Background(), &server, nil, paperVersions)

	require.NoError(t, err)
	assert.Equal(t, "1.20.4", result)
}

func TestFindBestPaperVersion_PinStrategy_NoVersion_ReturnsError(t *testing.T) {
	t.Parallel()

	solver := NewSimpleSolver()
	server := mcv1beta1.PaperMCServer{
		Spec: mcv1beta1.PaperMCServerSpec{
			UpdateStrategy: "pin",
			Version:        "",
		},
	}
	paperVersions := []string{"1.21.1"}

	_, err := solver.FindBestPaperVersion(context.Background(), &server, nil, paperVersions)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "version is not set")
}

func TestFindBestPaperVersion_LatestStrategy_ReturnsNewest(t *testing.T) {
	t.Parallel()

	solver := NewSimpleSolver()
	server := mcv1beta1.PaperMCServer{
		Spec: mcv1beta1.PaperMCServerSpec{
			UpdateStrategy: "latest",
		},
	}
	paperVersions := []string{"1.20.1", "1.21.4", "1.20.6", "1.21.1"}

	result, err := solver.FindBestPaperVersion(context.Background(), &server, nil, paperVersions)

	require.NoError(t, err)
	assert.Equal(t, "1.21.4", result)
}

func TestFindBestPaperVersion_AutoStrategy_ChecksPluginCompatibility(t *testing.T) {
	t.Parallel()

	solver := NewSimpleSolver()
	server := mcv1beta1.PaperMCServer{
		Spec: mcv1beta1.PaperMCServerSpec{
			UpdateStrategy: "auto",
		},
	}
	matchedPlugins := []mcv1beta1.Plugin{
		{
			Status: mcv1beta1.PluginStatus{
				AvailableVersions: []mcv1beta1.PluginVersionInfo{
					{Version: "1.0.0", MinecraftVersions: []string{"1.20.4", "1.20.6"}},
				},
			},
		},
	}
	paperVersions := []string{"1.21.1", "1.20.6", "1.20.4"}

	result, err := solver.FindBestPaperVersion(context.Background(), &server, matchedPlugins, paperVersions)

	require.NoError(t, err)
	assert.Equal(t, "1.20.6", result)
}

func TestFindBestPaperVersion_NoVersionsAvailable_ReturnsError(t *testing.T) {
	t.Parallel()

	solver := NewSimpleSolver()
	server := mcv1beta1.PaperMCServer{
		Spec: mcv1beta1.PaperMCServerSpec{
			UpdateStrategy: "latest",
		},
	}

	_, err := solver.FindBestPaperVersion(context.Background(), &server, nil, []string{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no Paper versions available")
}

func TestFindBestPaperVersion_InvalidStrategy_ReturnsError(t *testing.T) {
	t.Parallel()

	solver := NewSimpleSolver()
	server := mcv1beta1.PaperMCServer{
		Spec: mcv1beta1.PaperMCServerSpec{
			UpdateStrategy: "invalid",
		},
	}
	paperVersions := []string{"1.21.1"}

	_, err := solver.FindBestPaperVersion(context.Background(), &server, nil, paperVersions)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid updateStrategy")
}

func TestFindBestPaperVersion_PluginWithoutCachedVersions_AssumesCompatible(t *testing.T) {
	t.Parallel()

	solver := NewSimpleSolver()
	server := mcv1beta1.PaperMCServer{
		Spec: mcv1beta1.PaperMCServerSpec{
			UpdateStrategy: "auto",
		},
	}
	matchedPlugins := []mcv1beta1.Plugin{
		{
			Status: mcv1beta1.PluginStatus{
				AvailableVersions: []mcv1beta1.PluginVersionInfo{}, // empty
			},
		},
	}
	paperVersions := []string{"1.21.4", "1.21.1"}

	result, err := solver.FindBestPaperVersion(context.Background(), &server, matchedPlugins, paperVersions)

	require.NoError(t, err)
	assert.Equal(t, "1.21.4", result)
}

func TestFindBestPaperVersion_MultiplePlugins_MustSatisfyAll(t *testing.T) {
	t.Parallel()

	solver := NewSimpleSolver()
	server := mcv1beta1.PaperMCServer{
		Spec: mcv1beta1.PaperMCServerSpec{
			UpdateStrategy: "auto",
		},
	}
	matchedPlugins := []mcv1beta1.Plugin{
		{
			Status: mcv1beta1.PluginStatus{
				AvailableVersions: []mcv1beta1.PluginVersionInfo{
					{Version: "1.0.0", MinecraftVersions: []string{"1.21.1", "1.20.6"}},
				},
			},
		},
		{
			Status: mcv1beta1.PluginStatus{
				AvailableVersions: []mcv1beta1.PluginVersionInfo{
					{Version: "2.0.0", MinecraftVersions: []string{"1.20.6", "1.20.4"}},
				},
			},
		},
	}
	paperVersions := []string{"1.21.1", "1.20.6", "1.20.4"}

	result, err := solver.FindBestPaperVersion(context.Background(), &server, matchedPlugins, paperVersions)

	require.NoError(t, err)
	assert.Equal(t, "1.20.6", result)
}

func TestFindBestPaperVersion_ConflictingPlugins_ReturnsError(t *testing.T) {
	t.Parallel()

	solver := NewSimpleSolver()
	server := mcv1beta1.PaperMCServer{
		Spec: mcv1beta1.PaperMCServerSpec{
			UpdateStrategy: "auto",
		},
	}
	matchedPlugins := []mcv1beta1.Plugin{
		{
			Status: mcv1beta1.PluginStatus{
				AvailableVersions: []mcv1beta1.PluginVersionInfo{
					{Version: "1.0.0", MinecraftVersions: []string{"1.21.1"}},
				},
			},
		},
		{
			Status: mcv1beta1.PluginStatus{
				AvailableVersions: []mcv1beta1.PluginVersionInfo{
					{Version: "2.0.0", MinecraftVersions: []string{"1.20.4"}},
				},
			},
		},
	}
	paperVersions := []string{"1.21.1", "1.20.4"}

	_, err := solver.FindBestPaperVersion(context.Background(), &server, matchedPlugins, paperVersions)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no Paper version compatible")
}

func TestFindBestPaperVersion_DefaultStrategy_TreatedAsLatest(t *testing.T) {
	t.Parallel()

	solver := NewSimpleSolver()
	server := mcv1beta1.PaperMCServer{
		Spec: mcv1beta1.PaperMCServerSpec{
			UpdateStrategy: "", // empty, should default to latest
		},
	}
	paperVersions := []string{"1.20.1", "1.21.4", "1.21.1"}

	result, err := solver.FindBestPaperVersion(context.Background(), &server, nil, paperVersions)

	require.NoError(t, err)
	assert.Equal(t, "1.21.4", result)
}
