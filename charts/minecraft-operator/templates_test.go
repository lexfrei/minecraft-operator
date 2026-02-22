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

package main

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHelmDeploymentMetricsPort verifies that the metrics port in deployment.yaml
// uses the value from values.yaml (metrics.port) instead of being hardcoded.
//
// Bug 17: values.yaml declares metrics.port: 8080, but deployment.yaml hardcodes
// --metrics-bind-address=:8443 and containerPort: 8443.
// The metrics.port configuration is completely ignored.
func TestHelmDeploymentMetricsPort(t *testing.T) {
	// Render the chart with default values
	cmd := exec.Command("helm", "template", "test-release", ".")
	cmd.Dir = "." // charts/minecraft-operator/
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "helm template failed: %s", string(output))

	rendered := string(output)

	// The default metrics.port in values.yaml is 8080.
	// The deployment template should use this value.
	// Bug: deployment.yaml hardcodes :8443 instead of using {{ .Values.metrics.port }}.

	t.Run("should use metrics.port from values for bind address", func(t *testing.T) {
		// With default values (metrics.port: 8080), the rendered template should
		// contain --metrics-bind-address=:8080, NOT :8443.
		assert.Contains(t, rendered, "--metrics-bind-address=:8080",
			"Bug 17: deployment.yaml should use {{ .Values.metrics.port }} (8080) "+
				"instead of hardcoded :8443 for metrics-bind-address")
		assert.NotContains(t, rendered, "--metrics-bind-address=:8443",
			"Bug 17: metrics port 8443 is hardcoded in deployment.yaml, "+
				"ignoring metrics.port from values.yaml (8080)")
	})

	t.Run("should use metrics.port from values for containerPort", func(t *testing.T) {
		// The containerPort for metrics should match metrics.port from values.
		// Currently hardcoded to 8443.
		assert.Contains(t, rendered, "containerPort: 8080",
			"Bug 17: containerPort for metrics should use {{ .Values.metrics.port }} "+
				"(8080) instead of hardcoded 8443")
	})

	t.Run("should respect custom metrics port", func(t *testing.T) {
		// Render with custom metrics port
		cmd := exec.Command("helm", "template", "test-release", ".",
			"--set", "metrics.port=9090")
		cmd.Dir = "."
		customOutput, err := cmd.CombinedOutput()
		require.NoError(t, err, "helm template with custom port failed: %s", string(customOutput))

		customRendered := string(customOutput)

		assert.Contains(t, customRendered, "--metrics-bind-address=:9090",
			"Custom metrics.port=9090 should be reflected in --metrics-bind-address")
		assert.True(t, strings.Contains(customRendered, "containerPort: 9090"),
			"Custom metrics.port=9090 should be reflected in containerPort")
	})
}
