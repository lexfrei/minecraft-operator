package metrics

import "time"

// NoopRecorder is a no-op implementation of Recorder for use in tests.
type NoopRecorder struct{}

func (n *NoopRecorder) RecordReconcile(_ string, _ error, _ time.Duration) {}

func (n *NoopRecorder) RecordPluginAPICall(_ string, _ error, _ time.Duration) {}

func (n *NoopRecorder) RecordSolverRun(_ string, _ error, _ time.Duration) {}

func (n *NoopRecorder) RecordUpdate(_ bool) {}
