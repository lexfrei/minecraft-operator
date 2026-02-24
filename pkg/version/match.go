/*
Copyright 2026, Aleksei Sviridkin.

SPDX-License-Identifier: BSD-3-Clause
*/

package version

// MatchesVersionPattern checks if a target version matches a glob pattern.
// Supports patterns like "1.21.x" which matches any version starting with "1.21.".
func MatchesVersionPattern(pattern, target string) bool {
	if len(pattern) > 2 && pattern[len(pattern)-2:] == ".x" {
		prefix := pattern[:len(pattern)-1]
		return len(target) >= len(prefix) && target[:len(prefix)] == prefix
	}

	return false
}

// ContainsVersion checks if a version string is in the list.
// Supports both exact matches and glob patterns (e.g., "1.21.x").
func ContainsVersion(versions []string, target string) bool {
	for _, v := range versions {
		if v == target {
			return true
		}

		if MatchesVersionPattern(v, target) {
			return true
		}
	}

	return false
}
