// Package metrics provides Prometheus metrics for the minecraft-operator.
package metrics

import "time"

// Recorder defines the interface for recording operator metrics.
type Recorder interface {
	// RecordReconcile records a reconciliation loop completion.
	RecordReconcile(controller string, err error, duration time.Duration)

	// RecordPluginAPICall records a plugin repository API call.
	RecordPluginAPICall(source string, err error, duration time.Duration)

	// RecordSolverRun records a constraint solver invocation.
	RecordSolverRun(solverType string, err error, duration time.Duration)

	// RecordUpdate records a server update attempt.
	RecordUpdate(serverName, namespace string, success bool)
}
