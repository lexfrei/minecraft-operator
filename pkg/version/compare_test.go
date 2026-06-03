/*
Copyright 2026, Aleksei Sviridkin.

SPDX-License-Identifier: BSD-3-Clause
*/

package version

import (
	"testing"
)

// Test fixture constants.
const (
	testVer100   = "1.0.0"
	testVer200   = "2.0.0"
	testVer1211  = "1.21.1"
	testVer12110 = "1.21.10"
	testPat121x  = "1.21.x"
	testInvalid  = "invalid"
)

//nolint:funlen // Table-driven tests are expected to be long
func TestCompare(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		v1      string
		v2      string
		want    int
		wantErr bool
	}{
		{
			name:    "equal versions",
			v1:      testVer12110,
			v2:      testVer12110,
			want:    0,
			wantErr: false,
		},
		{
			name:    "v1 greater than v2",
			v1:      "1.21.11",
			v2:      testVer12110,
			want:    1,
			wantErr: false,
		},
		{
			name:    "v1 less than v2",
			v1:      "1.21.9",
			v2:      testVer12110,
			want:    -1,
			wantErr: false,
		},
		{
			name:    "latest equals latest",
			v1:      Latest,
			v2:      Latest,
			want:    0,
			wantErr: false,
		},
		{
			name:    "latest greater than version",
			v1:      Latest,
			v2:      testVer12110,
			want:    1,
			wantErr: false,
		},
		{
			name:    "version less than latest",
			v1:      testVer12110,
			v2:      Latest,
			want:    -1,
			wantErr: false,
		},
		{
			name:    "major version difference",
			v1:      testVer200,
			v2:      testVer12110,
			want:    1,
			wantErr: false,
		},
		{
			name:    "minor version difference",
			v1:      "1.20.0",
			v2:      "1.21.0",
			want:    -1,
			wantErr: false,
		},
		{
			name:    "invalid v1",
			v1:      testInvalid,
			v2:      testVer12110,
			want:    0,
			wantErr: true,
		},
		{
			name:    "invalid v2",
			v1:      testVer12110,
			v2:      testInvalid,
			want:    0,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := Compare(tt.v1, tt.v2)
			if (err != nil) != tt.wantErr {
				t.Errorf("Compare() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("Compare() = %v, want %v", got, tt.want)
			}
		})
	}
}

//nolint:funlen // Table-driven tests are expected to be long
func TestIsDowngrade(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		currentVersion   string
		candidateVersion string
		want             bool
		wantErr          bool
	}{
		{
			name:             "same version - not downgrade",
			currentVersion:   testVer12110,
			candidateVersion: testVer12110,
			want:             false,
			wantErr:          false,
		},
		{
			name:             "upgrade - not downgrade",
			currentVersion:   testVer12110,
			candidateVersion: "1.21.11",
			want:             false,
			wantErr:          false,
		},
		{
			name:             "downgrade detected",
			currentVersion:   testVer12110,
			candidateVersion: "1.21.9",
			want:             true,
			wantErr:          false,
		},
		{
			name:             "major version downgrade",
			currentVersion:   testVer200,
			candidateVersion: testVer12110,
			want:             true,
			wantErr:          false,
		},
		{
			name:             "major version upgrade",
			currentVersion:   testVer12110,
			candidateVersion: testVer200,
			want:             false,
			wantErr:          false,
		},
		{
			name:             "latest to version - downgrade",
			currentVersion:   Latest,
			candidateVersion: testVer12110,
			want:             true,
			wantErr:          false,
		},
		{
			name:             "version to latest - upgrade",
			currentVersion:   testVer12110,
			candidateVersion: Latest,
			want:             false,
			wantErr:          false,
		},
		{
			name:             "invalid current version",
			currentVersion:   testInvalid,
			candidateVersion: testVer12110,
			want:             false,
			wantErr:          true,
		},
		{
			name:             "invalid candidate version",
			currentVersion:   testVer12110,
			candidateVersion: testInvalid,
			want:             false,
			wantErr:          true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := IsDowngrade(tt.currentVersion, tt.candidateVersion)
			if (err != nil) != tt.wantErr {
				t.Errorf("IsDowngrade() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("IsDowngrade() = %v, want %v", got, tt.want)
			}
		})
	}
}
