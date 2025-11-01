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
			v1:      "1.21.10",
			v2:      "1.21.10",
			want:    0,
			wantErr: false,
		},
		{
			name:    "v1 greater than v2",
			v1:      "1.21.11",
			v2:      "1.21.10",
			want:    1,
			wantErr: false,
		},
		{
			name:    "v1 less than v2",
			v1:      "1.21.9",
			v2:      "1.21.10",
			want:    -1,
			wantErr: false,
		},
		{
			name:    "latest equals latest",
			v1:      "latest",
			v2:      "latest",
			want:    0,
			wantErr: false,
		},
		{
			name:    "latest greater than version",
			v1:      "latest",
			v2:      "1.21.10",
			want:    1,
			wantErr: false,
		},
		{
			name:    "version less than latest",
			v1:      "1.21.10",
			v2:      "latest",
			want:    -1,
			wantErr: false,
		},
		{
			name:    "major version difference",
			v1:      "2.0.0",
			v2:      "1.21.10",
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
			v1:      "invalid",
			v2:      "1.21.10",
			want:    0,
			wantErr: true,
		},
		{
			name:    "invalid v2",
			v1:      "1.21.10",
			v2:      "invalid",
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
			currentVersion:   "1.21.10",
			candidateVersion: "1.21.10",
			want:             false,
			wantErr:          false,
		},
		{
			name:             "upgrade - not downgrade",
			currentVersion:   "1.21.10",
			candidateVersion: "1.21.11",
			want:             false,
			wantErr:          false,
		},
		{
			name:             "downgrade detected",
			currentVersion:   "1.21.10",
			candidateVersion: "1.21.9",
			want:             true,
			wantErr:          false,
		},
		{
			name:             "major version downgrade",
			currentVersion:   "2.0.0",
			candidateVersion: "1.21.10",
			want:             true,
			wantErr:          false,
		},
		{
			name:             "major version upgrade",
			currentVersion:   "1.21.10",
			candidateVersion: "2.0.0",
			want:             false,
			wantErr:          false,
		},
		{
			name:             "latest to version - downgrade",
			currentVersion:   "latest",
			candidateVersion: "1.21.10",
			want:             true,
			wantErr:          false,
		},
		{
			name:             "version to latest - upgrade",
			currentVersion:   "1.21.10",
			candidateVersion: "latest",
			want:             false,
			wantErr:          false,
		},
		{
			name:             "invalid current version",
			currentVersion:   "invalid",
			candidateVersion: "1.21.10",
			want:             false,
			wantErr:          true,
		},
		{
			name:             "invalid candidate version",
			currentVersion:   "1.21.10",
			candidateVersion: "invalid",
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
