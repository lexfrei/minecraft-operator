/*
Copyright 2026, Aleksei Sviridkin.

SPDX-License-Identifier: BSD-3-Clause
*/

package architecture_test

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// repoRoot walks up from the test working directory until it finds the
// directory containing go.mod, returning that path.
func repoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}

	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found walking up from test directory")
		}

		dir = parent
	}
}

// readFile reads a repo-relative file or fails the test.
func readFile(t *testing.T, root, name string) string {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(root, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}

	return string(data)
}

// goModVersion extracts the pinned version of a module from go.mod text.
func goModVersion(t *testing.T, gomod, module string) string {
	t.Helper()

	re := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(module) + `\s+(v\S+)`)

	m := re.FindStringSubmatch(gomod)
	if m == nil {
		t.Fatalf("module %q not found in go.mod", module)
	}

	return m[1]
}

// archVersion extracts a `key: "vX.Y.Z"` semver value from
// .architecture.yaml text. The value pattern is restricted to a
// MAJOR.MINOR.PATCH semver so an ambiguous key (e.g. api_version, which
// also names the CRD apiVersion "v1beta1") resolves to the dependency
// version rather than the unrelated first match.
func archVersion(t *testing.T, arch, key string) string {
	t.Helper()

	re := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(key) + `:\s*"(v\d+\.\d+\.\d+)"`)

	m := re.FindStringSubmatch(arch)
	if m == nil {
		t.Fatalf("key %q not found in .architecture.yaml", key)
	}

	return m[1]
}

// TestArchitectureDocMatchesGoMod pins the exact dependency versions
// recorded in .architecture.yaml to the versions in go.mod. A
// dependency bump that forgets to update the architecture doc fails
// here instead of shipping a contradictory source of truth.
func TestArchitectureDocMatchesGoMod(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	gomod := readFile(t, root, "go.mod")
	arch := readFile(t, root, ".architecture.yaml")

	cases := []struct {
		archKey string
		module  string
	}{
		{"operator_version", "sigs.k8s.io/controller-runtime"},
		{"client_go_version", "k8s.io/client-go"},
		{"apimachinery_version", "k8s.io/apimachinery"},
		{"api_version", "k8s.io/api"},
	}

	for _, tc := range cases {
		t.Run(tc.archKey, func(t *testing.T) {
			t.Parallel()

			want := goModVersion(t, gomod, tc.module)

			got := archVersion(t, arch, tc.archKey)
			if got != want {
				t.Errorf(".architecture.yaml %s = %q, but go.mod %s = %q — update the architecture doc",
					tc.archKey, got, tc.module, want)
			}
		})
	}
}
