/*
Copyright 2026, Aleksei Sviridkin.

SPDX-License-Identifier: BSD-3-Clause
*/

// Package architecture holds guard tests that keep the repository's
// architecture documentation (.architecture.yaml) in sync with the
// dependency versions actually pinned in go.mod, so the two cannot
// silently diverge on a dependency bump.
package architecture
