// Package version provides version comparison and filtering utilities.
package version

import (
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/cockroachdb/errors"
)

// VersionInfo contains metadata about a specific version.
type VersionInfo struct {
	// Version is the version string.
	Version string
	// ReleaseDate is when this version was released.
	ReleaseDate time.Time
}

// ParseVersion parses a version string into a semver.Version.
// Returns an error if the version string is invalid.
func ParseVersion(version string) (*semver.Version, error) {
	v, err := semver.NewVersion(version)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parse version %q", version)
	}
	return v, nil
}

// CompareVersions compares two version strings.
// Returns:
//   - -1 if v1 < v2
//   - 0 if v1 == v2
//   - 1 if v1 > v2
//   - error if either version is invalid
func CompareVersions(v1, v2 string) (int, error) {
	ver1, err := ParseVersion(v1)
	if err != nil {
		return 0, errors.Wrap(err, "failed to parse first version")
	}

	ver2, err := ParseVersion(v2)
	if err != nil {
		return 0, errors.Wrap(err, "failed to parse second version")
	}

	return ver1.Compare(ver2), nil
}

// FilterByUpdateDelay filters versions based on release date and update delay.
// Only versions released before (now - delay) are returned.
// If delay is zero, all versions are returned.
func FilterByUpdateDelay(versions []VersionInfo, delay time.Duration) []VersionInfo {
	if delay == 0 {
		result := make([]VersionInfo, len(versions))
		copy(result, versions)

		return result
	}

	cutoff := time.Now().Add(-delay)
	filtered := make([]VersionInfo, 0, len(versions))

	for _, v := range versions {
		if v.ReleaseDate.Before(cutoff) || v.ReleaseDate.Equal(cutoff) {
			filtered = append(filtered, v)
		}
	}

	return filtered
}

// FindMaxVersion finds the maximum version from a list of version strings.
// Returns empty string if the list is empty or all versions are invalid.
func FindMaxVersion(versions []string) (string, error) {
	if len(versions) == 0 {
		return "", nil
	}

	var maxVer *semver.Version
	var maxStr string

	for _, v := range versions {
		ver, err := ParseVersion(v)
		if err != nil {
			// Skip invalid versions
			continue
		}

		if maxVer == nil || ver.GreaterThan(maxVer) {
			maxVer = ver
			maxStr = v
		}
	}

	if maxVer == nil {
		return "", errors.New("no valid versions found")
	}

	return maxStr, nil
}
