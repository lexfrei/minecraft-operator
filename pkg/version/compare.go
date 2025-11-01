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
	"github.com/Masterminds/semver/v3"
	"github.com/cockroachdb/errors"
)

const (
	// Latest represents the special "latest" version tag.
	Latest = "latest"
)

// Compare compares two version strings using semantic versioning.
// Returns:
//
//	-1 if v1 < v2
//	 0 if v1 == v2
//	 1 if v1 > v2
func Compare(v1, v2 string) (int, error) {
	// Handle "latest" tag specially
	if v1 == Latest || v2 == Latest {
		if v1 == v2 {
			return 0, nil
		}
		// "latest" is always considered newer
		if v1 == Latest {
			return 1, nil
		}
		return -1, nil
	}

	// Parse semantic versions
	ver1, err := semver.NewVersion(v1)
	if err != nil {
		return 0, errors.Wrapf(err, "invalid version: %s", v1)
	}

	ver2, err := semver.NewVersion(v2)
	if err != nil {
		return 0, errors.Wrapf(err, "invalid version: %s", v2)
	}

	return ver1.Compare(ver2), nil
}

// IsDowngrade checks if changing from currentVersion to candidateVersion would be a downgrade.
func IsDowngrade(currentVersion, candidateVersion string) (bool, error) {
	cmp, err := Compare(candidateVersion, currentVersion)
	if err != nil {
		return false, err
	}
	return cmp < 0, nil
}
