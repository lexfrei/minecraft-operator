/*
Copyright 2026, Aleksei Sviridkin.

SPDX-License-Identifier: BSD-3-Clause
*/

package paper

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakePaperServer is a minimal httptest-backed Paper v3 API for unit tests.
type fakePaperServer struct {
	projectResponse string
	// buildsByVersion maps version string to JSON array body for /builds endpoint.
	buildsByVersion map[string]string
	// requestCount counts per-path hits (key: request path).
	requestCount map[string]*int64
}

func newFakePaperServer() *fakePaperServer {
	return &fakePaperServer{
		buildsByVersion: make(map[string]string),
		requestCount:    make(map[string]*int64),
	}
}

func (f *fakePaperServer) start(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/v3/projects/paper", func(w http.ResponseWriter, r *http.Request) {
		f.countHit(r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, f.projectResponse)
	})

	mux.HandleFunc("/v3/projects/paper/versions/", func(w http.ResponseWriter, r *http.Request) {
		// Expect path like /v3/projects/paper/versions/{version}/builds
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) < 6 || parts[5] != "builds" {
			http.NotFound(w, r)
			return
		}
		version := parts[4]
		body, ok := f.buildsByVersion[version]
		if !ok {
			http.NotFound(w, r)
			return
		}
		f.countHit(r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, body)
	})

	return httptest.NewServer(mux)
}

func (f *fakePaperServer) countHit(path string) {
	if _, ok := f.requestCount[path]; !ok {
		var c int64
		f.requestCount[path] = &c
	}
	atomic.AddInt64(f.requestCount[path], 1)
}

func (f *fakePaperServer) hitCount(path string) int64 {
	if c, ok := f.requestCount[path]; ok {
		return atomic.LoadInt64(c)
	}
	return 0
}

// setProject sets the /v3/projects/paper response with the given versions grouped into a single key.
func (f *fakePaperServer) setProject(versions []string) {
	quoted := make([]string, 0, len(versions))
	for _, v := range versions {
		quoted = append(quoted, fmt.Sprintf("%q", v))
	}
	body := map[string]any{
		"project":  map[string]string{"id": "paper", "name": "Paper"},
		"versions": map[string][]string{"all": versions},
	}
	raw, _ := json.Marshal(body)
	// goPaperMC's FlattenVersions traverses all keys; group name doesn't matter.
	f.projectResponse = string(raw)
	_ = quoted
}

// buildSpec describes a single fake build in deterministic order.
type buildSpec struct {
	ID      int
	Channel string
}

// setBuilds sets the /builds response for a specific version. Builds are written
// in the slice order provided — preserving deterministic "latest build" semantics.
func (f *fakePaperServer) setBuilds(version string, specs []buildSpec) {
	builds := make([]map[string]any, 0, len(specs))
	for _, s := range specs {
		builds = append(builds, map[string]any{
			"id":      s.ID,
			"time":    "2026-04-15T00:00:00Z",
			"channel": s.Channel,
			"commits": []any{},
			"downloads": map[string]any{
				"server:default": map[string]any{
					"name":      fmt.Sprintf("paper-%s-%d.jar", version, s.ID),
					"checksums": map[string]string{"sha256": "abc"},
					"size":      1,
					"url":       fmt.Sprintf("https://example.test/paper-%s-%d.jar", version, s.ID),
				},
			},
		})
	}
	raw, _ := json.Marshal(builds)
	f.buildsByVersion[version] = string(raw)
}

func TestGetPaperCandidates_IncludesChannelsForEachVersion(t *testing.T) {
	t.Parallel()

	fake := newFakePaperServer()
	fake.setProject([]string{"1.21.11", "26.1.2"})
	fake.setBuilds("1.21.11", []buildSpec{{ID: 1, Channel: "STABLE"}})
	fake.setBuilds("26.1.2", []buildSpec{{ID: 2, Channel: "ALPHA"}})

	srv := fake.start(t)
	defer srv.Close()

	client := NewClient().WithBaseURL(srv.URL)

	candidates, err := client.GetPaperCandidates(context.Background())
	require.NoError(t, err)
	require.Len(t, candidates, 2)

	byVersion := make(map[string][]string, len(candidates))
	for _, c := range candidates {
		byVersion[c.Version] = c.Channels
	}

	assert.ElementsMatch(t, []string{"STABLE"}, byVersion["1.21.11"])
	assert.ElementsMatch(t, []string{"ALPHA"}, byVersion["26.1.2"])
}

func TestGetPaperCandidates_CacheHit_NoSecondHTTP(t *testing.T) {
	t.Parallel()

	fake := newFakePaperServer()
	fake.setProject([]string{"1.21.11"})
	fake.setBuilds("1.21.11", []buildSpec{{ID: 1, Channel: "STABLE"}})

	srv := fake.start(t)
	defer srv.Close()

	client := NewClient().WithBaseURL(srv.URL)

	_, err := client.GetPaperCandidates(context.Background())
	require.NoError(t, err)
	firstHits := fake.hitCount("/v3/projects/paper")

	_, err = client.GetPaperCandidates(context.Background())
	require.NoError(t, err)
	secondHits := fake.hitCount("/v3/projects/paper")

	assert.Equal(t, firstHits, secondHits, "second call within TTL must hit cache, not upstream")
}

func TestGetPaperBuild_ChannelFilter_SkipsNonMatching(t *testing.T) {
	t.Parallel()

	fake := newFakePaperServer()
	fake.setProject([]string{"1.21.11"})
	// Builds in ascending ID order: stable b1, then ALPHA b2 (newer).
	// Without channel filter, latest=b2 ALPHA. With STABLE filter, latest=b1.
	fake.setBuilds("1.21.11", []buildSpec{{ID: 1, Channel: "STABLE"}, {ID: 2, Channel: "ALPHA"}})

	srv := fake.start(t)
	defer srv.Close()

	client := NewClient().WithBaseURL(srv.URL)

	info, err := client.GetPaperBuild(context.Background(), "1.21.11", []string{"STABLE"})
	require.NoError(t, err)
	assert.Equal(t, 1, info.Build, "STABLE filter must pick b1 even though b2 is newer")
}

func TestGetPaperBuild_ChannelFilter_AllowsMatching(t *testing.T) {
	t.Parallel()

	fake := newFakePaperServer()
	fake.setProject([]string{"26.1.2"})
	fake.setBuilds("26.1.2", []buildSpec{{ID: 7, Channel: "ALPHA"}})

	srv := fake.start(t)
	defer srv.Close()

	client := NewClient().WithBaseURL(srv.URL)

	info, err := client.GetPaperBuild(context.Background(), "26.1.2", []string{"STABLE", "BETA", "ALPHA"})
	require.NoError(t, err)
	assert.Equal(t, 7, info.Build)
}

func TestGetPaperBuild_ChannelFilter_NoMatch_ReturnsError(t *testing.T) {
	t.Parallel()

	fake := newFakePaperServer()
	fake.setProject([]string{"26.1.2"})
	fake.setBuilds("26.1.2", []buildSpec{{ID: 7, Channel: "ALPHA"}})

	srv := fake.start(t)
	defer srv.Close()

	client := NewClient().WithBaseURL(srv.URL)

	_, err := client.GetPaperBuild(context.Background(), "26.1.2", []string{"STABLE"})
	require.Error(t, err, "stable filter with only ALPHA builds must fail")
}

func TestGetPaperBuild_EmptyChannels_NoFilter(t *testing.T) {
	t.Parallel()

	fake := newFakePaperServer()
	fake.setProject([]string{"1.21.11"})
	fake.setBuilds("1.21.11", []buildSpec{{ID: 1, Channel: "STABLE"}, {ID: 2, Channel: "ALPHA"}})

	srv := fake.start(t)
	defer srv.Close()

	client := NewClient().WithBaseURL(srv.URL)

	info, err := client.GetPaperBuild(context.Background(), "1.21.11", nil)
	require.NoError(t, err)
	// No filter → returns the last build in the original order (B2 ALPHA here).
	assert.Equal(t, 2, info.Build)
}
