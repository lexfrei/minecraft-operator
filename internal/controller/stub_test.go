/*
Copyright 2026, Aleksei Sviridkin.

SPDX-License-Identifier: BSD-3-Clause
*/

package controller

import (
	"testing"
)

// RED TEST: BlockedByInfo.SupportedVersions is never populated.
// When a plugin blocks an update, the operator should report which Paper
// versions the plugin supports so users understand why the update is blocked.
// Currently the field is always nil (checkCompatibility at papermcserver_controller.go:896).
func TestBlockedByInfo_ShouldPopulateSupportedVersions(t *testing.T) {
	t.Skip("NOT IMPLEMENTED: BlockedByInfo.SupportedVersions is never populated from plugin metadata")
}

// RED TEST: PaperMCServer update delay is not enforced.
// When spec.updateDelay is set, the operator should wait the configured
// duration after a new build is released before offering it as an available
// update. Currently findBuildUpdate (papermcserver_controller.go:1450) skips
// the delay check entirely.
func TestServerUpdateDelay_ShouldBeEnforced(t *testing.T) {
	t.Skip("NOT IMPLEMENTED: Update delay check is skipped — always assumes delay has passed")
}
