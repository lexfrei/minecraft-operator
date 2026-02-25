/*
Copyright 2026, Aleksei Sviridkin.

SPDX-License-Identifier: BSD-3-Clause
*/

package controller

// standardLabels returns the Kubernetes recommended labels for a resource
// managed by the minecraft-operator. The component parameter identifies the
// role of the resource (e.g., "server", "service", "networking").
func standardLabels(instanceName, component string) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":       "papermc",
		"app.kubernetes.io/instance":   instanceName,
		"app.kubernetes.io/managed-by": "minecraft-operator",
		"app.kubernetes.io/component":  component,
		"app.kubernetes.io/part-of":    "minecraft-operator",
	}
}
