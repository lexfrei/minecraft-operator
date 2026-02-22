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

package version

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFilterByUpdateDelay_ZeroDelay_ShouldReturnCopy(t *testing.T) {
	t.Parallel()

	// BUG: FilterByUpdateDelay returns the original slice when delay is 0
	// instead of a defensive copy. Modifying the returned slice modifies
	// the caller's input, which can cause subtle data corruption.
	input := []VersionInfo{
		{Version: "1.0.0", ReleaseDate: time.Now().Add(-48 * time.Hour)},
		{Version: "2.0.0", ReleaseDate: time.Now().Add(-24 * time.Hour)},
	}

	result := FilterByUpdateDelay(input, 0)

	require.Len(t, result, len(input), "Should return all versions when delay is 0")

	// Verify the returned slice is NOT the same backing array as the input.
	// If they share the same backing array, modifying one affects the other.
	assert.NotSame(t, &input[0], &result[0],
		"FilterByUpdateDelay with delay=0 should return a copy, not the original slice. "+
			"Currently returns the same backing array — modifying the result mutates the input")
}

func TestFilterByUpdateDelay_FiltersOldVersions(t *testing.T) {
	t.Parallel()

	input := []VersionInfo{
		{Version: "1.0.0", ReleaseDate: time.Now().Add(-72 * time.Hour)},
		{Version: "2.0.0", ReleaseDate: time.Now().Add(-24 * time.Hour)},
		{Version: "3.0.0", ReleaseDate: time.Now().Add(-1 * time.Hour)},
	}

	// Delay of 48 hours — only versions older than 48h should pass
	result := FilterByUpdateDelay(input, 48*time.Hour)

	require.Len(t, result, 1)
	assert.Equal(t, "1.0.0", result[0].Version)
}

func TestFilterByUpdateDelay_EmptyInput(t *testing.T) {
	t.Parallel()

	result := FilterByUpdateDelay([]VersionInfo{}, 24*time.Hour)

	assert.Empty(t, result)
}

func TestFilterByUpdateDelay_NilInput(t *testing.T) {
	t.Parallel()

	result := FilterByUpdateDelay(nil, 24*time.Hour)

	assert.Empty(t, result)
}

func TestFilterByUpdateDelay_AllVersionsTooNew(t *testing.T) {
	t.Parallel()

	input := []VersionInfo{
		{Version: "1.0.0", ReleaseDate: time.Now().Add(-1 * time.Hour)},
		{Version: "2.0.0", ReleaseDate: time.Now()},
	}

	result := FilterByUpdateDelay(input, 24*time.Hour)

	assert.Empty(t, result)
}
