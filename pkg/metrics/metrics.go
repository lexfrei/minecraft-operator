// Package metrics provides Prometheus metrics for the minecraft-operator.
package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Recorder defines the interface for recording operator metrics.
type Recorder interface {
	// RecordReconcile records a reconciliation loop completion.
	RecordReconcile(controller string, err error, duration time.Duration)

	// RecordPluginAPICall records a plugin repository API call.
	RecordPluginAPICall(source string, err error, duration time.Duration)

	// RecordSolverRun records a constraint solver invocation.
	RecordSolverRun(solverType string, err error, duration time.Duration)

	// RecordUpdate records a server update attempt.
	RecordUpdate(success bool)
}

// Histogram bucket definitions tuned for each domain's latency profile.
var (
	reconcileBuckets = []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30}
	pluginAPIBuckets = []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30}
	solverBuckets    = []float64{0.0001, 0.0005, 0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1}
)

// PrometheusRecorder implements Recorder using Prometheus metrics.
type PrometheusRecorder struct {
	reconcileTotal       *prometheus.CounterVec
	reconcileErrorsTotal *prometheus.CounterVec
	reconcileDuration    *prometheus.HistogramVec

	pluginAPIRequestsTotal *prometheus.CounterVec
	pluginAPIErrorsTotal   *prometheus.CounterVec
	pluginAPIDuration      *prometheus.HistogramVec

	solverRunsTotal   *prometheus.CounterVec
	solverErrorsTotal *prometheus.CounterVec
	solverDuration    *prometheus.HistogramVec

	updatesTotal *prometheus.CounterVec
}

// NewPrometheusRecorder creates a PrometheusRecorder and registers all metrics
// with the provided prometheus.Registerer.
func NewPrometheusRecorder(reg prometheus.Registerer) *PrometheusRecorder {
	r := &PrometheusRecorder{
		reconcileTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "minecraft_operator_reconcile_total",
			Help: "Total number of reconciliation loops completed.",
		}, []string{"controller"}),
		reconcileErrorsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "minecraft_operator_reconcile_errors_total",
			Help: "Total number of failed reconciliation loops.",
		}, []string{"controller"}),
		reconcileDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "minecraft_operator_reconcile_duration_seconds",
			Help:    "Duration of reconciliation loops in seconds.",
			Buckets: reconcileBuckets,
		}, []string{"controller"}),
		pluginAPIRequestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "minecraft_operator_plugin_api_requests_total",
			Help: "Total number of plugin repository API requests.",
		}, []string{"source"}),
		pluginAPIErrorsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "minecraft_operator_plugin_api_errors_total",
			Help: "Total number of failed plugin repository API requests.",
		}, []string{"source"}),
		pluginAPIDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "minecraft_operator_plugin_api_duration_seconds",
			Help:    "Duration of plugin repository API requests in seconds.",
			Buckets: pluginAPIBuckets,
		}, []string{"source"}),
		solverRunsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "minecraft_operator_solver_runs_total",
			Help: "Total number of constraint solver invocations.",
		}, []string{"type"}),
		solverErrorsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "minecraft_operator_solver_errors_total",
			Help: "Total number of failed constraint solver invocations.",
		}, []string{"type"}),
		solverDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "minecraft_operator_solver_duration_seconds",
			Help:    "Duration of constraint solver runs in seconds.",
			Buckets: solverBuckets,
		}, []string{"type"}),
		updatesTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "minecraft_operator_updates_total",
			Help: "Total number of server update attempts.",
		}, []string{"result"}),
	}

	reg.MustRegister(
		r.reconcileTotal,
		r.reconcileErrorsTotal,
		r.reconcileDuration,
		r.pluginAPIRequestsTotal,
		r.pluginAPIErrorsTotal,
		r.pluginAPIDuration,
		r.solverRunsTotal,
		r.solverErrorsTotal,
		r.solverDuration,
		r.updatesTotal,
	)

	return r
}

func (r *PrometheusRecorder) RecordReconcile(controller string, err error, duration time.Duration) {
	r.reconcileTotal.WithLabelValues(controller).Inc()
	r.reconcileDuration.WithLabelValues(controller).Observe(duration.Seconds())

	if err != nil {
		r.reconcileErrorsTotal.WithLabelValues(controller).Inc()
	}
}

func (r *PrometheusRecorder) RecordPluginAPICall(source string, err error, duration time.Duration) {
	r.pluginAPIRequestsTotal.WithLabelValues(source).Inc()
	r.pluginAPIDuration.WithLabelValues(source).Observe(duration.Seconds())

	if err != nil {
		r.pluginAPIErrorsTotal.WithLabelValues(source).Inc()
	}
}

func (r *PrometheusRecorder) RecordSolverRun(solverType string, err error, duration time.Duration) {
	r.solverRunsTotal.WithLabelValues(solverType).Inc()
	r.solverDuration.WithLabelValues(solverType).Observe(duration.Seconds())

	if err != nil {
		r.solverErrorsTotal.WithLabelValues(solverType).Inc()
	}
}

func (r *PrometheusRecorder) RecordUpdate(success bool) {
	result := "success"
	if !success {
		result = "failure"
	}

	r.updatesTotal.WithLabelValues(result).Inc()
}
