/*
Copyright 2026, Aleksei Sviridkin.

SPDX-License-Identifier: BSD-3-Clause
*/

package testutil

import (
	"bytes"
	"testing"
)

func TestBuildTestJARMultiDeterministic(t *testing.T) {
	t.Parallel()

	files := map[string]string{
		"plugin.yml":       "name: TestPlugin\nversion: 1.0",
		"paper-plugin.yml": "name: TestPlugin\nversion: 2.0",
		"config.yml":       "key: value",
	}

	// Build the JAR multiple times and verify output is identical.
	// With non-deterministic map iteration, at least one of these
	// will differ (statistically near-certain with 20 iterations).
	first := BuildTestJARMulti(files)

	for i := range 20 {
		result := BuildTestJARMulti(files)
		if !bytes.Equal(first, result) {
			t.Fatalf("BuildTestJARMulti produced different output on iteration %d; "+
				"map iteration order is non-deterministic", i)
		}
	}
}

func TestBuildTestJARSingleFile(t *testing.T) {
	t.Parallel()

	jar := BuildTestJAR("plugin.yml", "name: Test\nversion: 1.0")
	if len(jar) == 0 {
		t.Fatal("BuildTestJAR returned empty bytes")
	}
}
