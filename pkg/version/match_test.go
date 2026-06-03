/*
Copyright 2026, Aleksei Sviridkin.

SPDX-License-Identifier: BSD-3-Clause
*/

package version

import (
	"testing"
)

func TestMatchesVersionPattern(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		target  string
		want    bool
	}{
		{
			name:    "wildcard matches",
			pattern: testPat121x,
			target:  testVer1211,
			want:    true,
		},
		{
			name:    "wildcard matches another minor",
			pattern: testPat121x,
			target:  "1.21.4",
			want:    true,
		},
		{
			name:    "wildcard does not match different minor",
			pattern: "1.20.x",
			target:  testVer1211,
			want:    false,
		},
		{
			name:    "exact match is not a pattern",
			pattern: testVer1211,
			target:  testVer1211,
			want:    false,
		},
		{
			name:    "short pattern",
			pattern: ".x",
			target:  testVer1211,
			want:    false,
		},
		{
			name:    "empty pattern",
			pattern: "",
			target:  testVer1211,
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MatchesVersionPattern(tt.pattern, tt.target)
			if got != tt.want {
				t.Errorf("MatchesVersionPattern(%q, %q) = %v, want %v",
					tt.pattern, tt.target, got, tt.want)
			}
		})
	}
}

func TestContainsVersion(t *testing.T) {
	tests := []struct {
		name     string
		versions []string
		target   string
		want     bool
	}{
		{
			name:     "exact match found",
			versions: []string{"1.20.4", "1.21.0", testVer1211},
			target:   testVer1211,
			want:     true,
		},
		{
			name:     "no match",
			versions: []string{"1.19.4", "1.20.0"},
			target:   testVer1211,
			want:     false,
		},
		{
			name:     "wildcard match",
			versions: []string{testPat121x},
			target:   "1.21.4",
			want:     true,
		},
		{
			name:     "empty list",
			versions: []string{},
			target:   testVer1211,
			want:     false,
		},
		{
			name:     "nil list",
			versions: nil,
			target:   testVer1211,
			want:     false,
		},
		{
			name:     "mixed exact and wildcard",
			versions: []string{"1.20.4", testPat121x},
			target:   "1.21.3",
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ContainsVersion(tt.versions, tt.target)
			if got != tt.want {
				t.Errorf("ContainsVersion(%v, %q) = %v, want %v",
					tt.versions, tt.target, got, tt.want)
			}
		})
	}
}
