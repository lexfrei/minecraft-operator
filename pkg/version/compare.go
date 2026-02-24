/*
Copyright 2026, Aleksei Sviridkin.

SPDX-License-Identifier: BSD-3-Clause
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
