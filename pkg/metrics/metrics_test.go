package metrics_test

import (
	"errors"
	"testing"
	"time"

	"github.com/lexfrei/minecraft-operator/pkg/metrics"
	"github.com/prometheus/client_golang/prometheus"
	io_prometheus_client "github.com/prometheus/client_model/go"
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
	r.RecordUpdate(true)
	r.RecordUpdate(false)
}

func TestPrometheusRecorderImplementsRecorder(t *testing.T) {
	reg := prometheus.NewPedanticRegistry()
	var _ metrics.Recorder = metrics.NewPrometheusRecorder(reg)
}

func TestPrometheusRecorderRegistersAllMetrics(t *testing.T) {
	reg := prometheus.NewPedanticRegistry()
	r := metrics.NewPrometheusRecorder(reg)

	// Call each method to ensure all label combinations are populated.
	r.RecordReconcile("test", nil, time.Millisecond)
	r.RecordReconcile("test", errors.New("err"), time.Millisecond)
	r.RecordPluginAPICall("test", nil, time.Millisecond)
	r.RecordPluginAPICall("test", errors.New("err"), time.Millisecond)
	r.RecordSolverRun("test", nil, time.Millisecond)
	r.RecordSolverRun("test", errors.New("err"), time.Millisecond)
	r.RecordUpdate(true)

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}

	expected := map[string]bool{
		"minecraft_operator_reconcile_total":             false,
		"minecraft_operator_reconcile_errors_total":      false,
		"minecraft_operator_reconcile_duration_seconds":  false,
		"minecraft_operator_plugin_api_requests_total":   false,
		"minecraft_operator_plugin_api_errors_total":     false,
		"minecraft_operator_plugin_api_duration_seconds": false,
		"minecraft_operator_solver_runs_total":           false,
		"minecraft_operator_solver_errors_total":         false,
		"minecraft_operator_solver_duration_seconds":     false,
		"minecraft_operator_updates_total":               false,
	}

	for _, f := range families {
		if _, ok := expected[f.GetName()]; ok {
			expected[f.GetName()] = true
		}
	}

	for name, found := range expected {
		if !found {
			t.Errorf("metric %q not registered", name)
		}
	}
}

func TestRecordReconcileIncrementsCounter(t *testing.T) {
	reg := prometheus.NewPedanticRegistry()
	r := metrics.NewPrometheusRecorder(reg)

	r.RecordReconcile("plugin", nil, 100*time.Millisecond)
	r.RecordReconcile("plugin", nil, 200*time.Millisecond)

	labels := map[string]string{"controller": "plugin"}

	val := getCounterValue(t, reg,
		"minecraft_operator_reconcile_total", labels)
	if val != 2 {
		t.Errorf("expected reconcile_total=2, got %v", val)
	}
}

func TestRecordReconcileErrorIncrementsErrorCounter(t *testing.T) {
	reg := prometheus.NewPedanticRegistry()
	r := metrics.NewPrometheusRecorder(reg)

	r.RecordReconcile("plugin", errors.New("fail"), time.Second)
	r.RecordReconcile("plugin", nil, time.Second)

	labels := map[string]string{"controller": "plugin"}

	errVal := getCounterValue(t, reg,
		"minecraft_operator_reconcile_errors_total", labels)
	if errVal != 1 {
		t.Errorf("expected reconcile_errors_total=1, got %v", errVal)
	}

	totalVal := getCounterValue(t, reg,
		"minecraft_operator_reconcile_total", labels)
	if totalVal != 2 {
		t.Errorf("expected reconcile_total=2, got %v", totalVal)
	}
}

func TestRecordReconcileDurationObservesHistogram(t *testing.T) {
	reg := prometheus.NewPedanticRegistry()
	r := metrics.NewPrometheusRecorder(reg)

	r.RecordReconcile("update", nil, 500*time.Millisecond)

	labels := map[string]string{"controller": "update"}

	count := getHistogramCount(t, reg,
		"minecraft_operator_reconcile_duration_seconds", labels)
	if count != 1 {
		t.Errorf("expected histogram count=1, got %v", count)
	}
}

func TestRecordPluginAPICall(t *testing.T) {
	reg := prometheus.NewPedanticRegistry()
	r := metrics.NewPrometheusRecorder(reg)

	r.RecordPluginAPICall("hangar", nil, 50*time.Millisecond)
	r.RecordPluginAPICall("hangar", errors.New("timeout"), 100*time.Millisecond)

	labels := map[string]string{"source": "hangar"}

	totalVal := getCounterValue(t, reg,
		"minecraft_operator_plugin_api_requests_total", labels)
	if totalVal != 2 {
		t.Errorf("expected api_requests_total=2, got %v", totalVal)
	}

	errVal := getCounterValue(t, reg,
		"minecraft_operator_plugin_api_errors_total", labels)
	if errVal != 1 {
		t.Errorf("expected api_errors_total=1, got %v", errVal)
	}

	count := getHistogramCount(t, reg,
		"minecraft_operator_plugin_api_duration_seconds", labels)
	if count != 2 {
		t.Errorf("expected histogram count=2, got %v", count)
	}
}

func TestRecordSolverRun(t *testing.T) {
	reg := prometheus.NewPedanticRegistry()
	r := metrics.NewPrometheusRecorder(reg)

	r.RecordSolverRun("plugin_version", nil, 10*time.Millisecond)
	r.RecordSolverRun("paper_version", errors.New("no solution"), 20*time.Millisecond)

	pluginLabels := map[string]string{"type": "plugin_version"}

	pluginVal := getCounterValue(t, reg,
		"minecraft_operator_solver_runs_total", pluginLabels)
	if pluginVal != 1 {
		t.Errorf("expected solver_runs_total(plugin_version)=1, got %v", pluginVal)
	}

	paperLabels := map[string]string{"type": "paper_version"}

	paperVal := getCounterValue(t, reg,
		"minecraft_operator_solver_runs_total", paperLabels)
	if paperVal != 1 {
		t.Errorf("expected solver_runs_total(paper_version)=1, got %v", paperVal)
	}

	paperErrVal := getCounterValue(t, reg,
		"minecraft_operator_solver_errors_total", paperLabels)
	if paperErrVal != 1 {
		t.Errorf(
			"expected solver_errors_total(paper_version)=1, got %v",
			paperErrVal,
		)
	}
}

func TestRecordUpdate(t *testing.T) {
	reg := prometheus.NewPedanticRegistry()
	r := metrics.NewPrometheusRecorder(reg)

	r.RecordUpdate(true)
	r.RecordUpdate(false)
	r.RecordUpdate(true)

	successVal := getCounterValue(t, reg,
		"minecraft_operator_updates_total",
		map[string]string{"result": "success"})
	if successVal != 2 {
		t.Errorf("expected updates_total(success)=2, got %v", successVal)
	}

	failVal := getCounterValue(t, reg,
		"minecraft_operator_updates_total",
		map[string]string{"result": "failure"})
	if failVal != 1 {
		t.Errorf("expected updates_total(failure)=1, got %v", failVal)
	}
}

// getCounterValue extracts counter value for given labels from the registry.
func getCounterValue(
	t *testing.T,
	reg *prometheus.Registry,
	name string,
	labels map[string]string,
) float64 {
	t.Helper()

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("failed to gather: %v", err)
	}

	for _, f := range families {
		if f.GetName() != name {
			continue
		}

		for _, m := range f.GetMetric() {
			if matchLabels(m, labels) {
				return m.GetCounter().GetValue()
			}
		}
	}

	t.Fatalf("metric %q with labels %v not found", name, labels)

	return 0
}

// getHistogramCount extracts histogram sample count for given labels.
func getHistogramCount(
	t *testing.T,
	reg *prometheus.Registry,
	name string,
	labels map[string]string,
) uint64 {
	t.Helper()

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("failed to gather: %v", err)
	}

	for _, f := range families {
		if f.GetName() != name {
			continue
		}

		for _, m := range f.GetMetric() {
			if matchLabels(m, labels) {
				return m.GetHistogram().GetSampleCount()
			}
		}
	}

	t.Fatalf("metric %q with labels %v not found", name, labels)

	return 0
}

// matchLabels checks if a metric has all expected label pairs.
func matchLabels(
	m *io_prometheus_client.Metric,
	expected map[string]string,
) bool {
	labelMap := make(map[string]string)
	for _, lp := range m.GetLabel() {
		labelMap[lp.GetName()] = lp.GetValue()
	}

	for k, v := range expected {
		if labelMap[k] != v {
			return false
		}
	}

	return true
}
