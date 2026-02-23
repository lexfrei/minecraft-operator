package metrics_test

import (
	"errors"
	"testing"
	"time"

	"github.com/lexfrei/minecraft-operator/pkg/metrics"
)

func TestNoopRecorderImplementsRecorder(t *testing.T) {
	var _ metrics.Recorder = &metrics.NoopRecorder{}
}

func TestNoopRecorderDoesNotPanic(t *testing.T) {
	r := &metrics.NoopRecorder{}

	r.RecordReconcile("plugin", nil, time.Second)
	r.RecordReconcile("plugin", errors.New("test"), time.Second)
	r.RecordPluginAPICall("hangar", nil, time.Millisecond)
	r.RecordPluginAPICall("hangar", errors.New("test"), time.Millisecond)
	r.RecordSolverRun("plugin_version", nil, time.Millisecond)
	r.RecordSolverRun("paper_version", errors.New("test"), time.Millisecond)
	r.RecordUpdate("test-server", "default", true)
	r.RecordUpdate("test-server", "default", false)
}
