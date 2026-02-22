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
