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

// +kubebuilder:rbac:groups=authentication.k8s.io,resources=tokenreviews,verbs=create
// +kubebuilder:rbac:groups=authorization.k8s.io,resources=subjectaccessreviews,verbs=create

package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"log/slog"
	"os"
	goruntime "runtime"
	"time"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	mck8slexlav1alpha1 "github.com/lexfrei/minecraft-operator/api/v1alpha1"
	"github.com/lexfrei/minecraft-operator/internal/controller"
	"github.com/lexfrei/minecraft-operator/pkg/api"
	mccron "github.com/lexfrei/minecraft-operator/pkg/cron"
	"github.com/lexfrei/minecraft-operator/pkg/paper"
	"github.com/lexfrei/minecraft-operator/pkg/plugins"
	"github.com/lexfrei/minecraft-operator/pkg/registry"
	"github.com/lexfrei/minecraft-operator/pkg/solver"
	"github.com/lexfrei/minecraft-operator/pkg/webui"
	// +kubebuilder:scaffold:imports
)

var (
	// Version information set via ldflags.
	version   = "dev"
	gitCommit = "unknown"
	buildDate = ""

	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(mck8slexlav1alpha1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

// nolint:gocyclo,funlen // Kubebuilder-generated scaffolding code
func main() {
	var metricsAddr string
	var metricsCertPath, metricsCertName, metricsCertKey string
	var webhookCertPath, webhookCertName, webhookCertKey string
	var enableLeaderElection bool
	var probeAddr string
	var secureMetrics bool
	var enableHTTP2 bool
	var webuiAddr string
	var webuiNamespace string
	var webuiEnabled bool
	var logLevel string
	var logFormat string
	var tlsOpts []func(*tls.Config)
	flag.StringVar(&metricsAddr, "metrics-bind-address", "0", "The address the metrics endpoint binds to. "+
		"Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable the metrics service.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&secureMetrics, "metrics-secure", true,
		"If set, the metrics endpoint is served securely via HTTPS. Use --metrics-secure=false to use HTTP instead.")
	flag.StringVar(&webhookCertPath, "webhook-cert-path", "", "The directory that contains the webhook certificate.")
	flag.StringVar(&webhookCertName, "webhook-cert-name", "tls.crt", "The name of the webhook certificate file.")
	flag.StringVar(&webhookCertKey, "webhook-cert-key", "tls.key", "The name of the webhook key file.")
	flag.StringVar(&metricsCertPath, "metrics-cert-path", "",
		"The directory that contains the metrics server certificate.")
	flag.StringVar(&metricsCertName, "metrics-cert-name", "tls.crt", "The name of the metrics server certificate file.")
	flag.StringVar(&metricsCertKey, "metrics-cert-key", "tls.key", "The name of the metrics server key file.")
	flag.BoolVar(&enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")
	flag.StringVar(&webuiAddr, "webui-bind-address", ":8082", "The address the Web UI endpoint binds to.")
	flag.StringVar(&webuiNamespace, "webui-namespace", "",
		"The namespace to display in Web UI. If empty, uses the operator's namespace.")
	flag.BoolVar(&webuiEnabled, "webui-enabled", true, "Enable the Web UI server.")
	flag.StringVar(&logLevel, "log-level", "info",
		"Log level (debug, info, warn, error). Default: info")
	flag.StringVar(&logFormat, "log-format", "json",
		"Log format (json, text). Default: json")
	flag.Parse()

	// Configure slog handler based on flags
	var slogLevel slog.Level
	switch logLevel {
	case "debug":
		slogLevel = slog.LevelDebug
	case "info":
		slogLevel = slog.LevelInfo
	case "warn":
		slogLevel = slog.LevelWarn
	case "error":
		slogLevel = slog.LevelError
	default:
		slogLevel = slog.LevelInfo
		fmt.Fprintf(os.Stderr, "WARNING: unknown log level %q, defaulting to \"info\"\n", logLevel)
	}

	handlerOpts := &slog.HandlerOptions{
		Level: slogLevel,
	}

	var slogHandler slog.Handler

	switch logFormat {
	case "text":
		slogHandler = slog.NewTextHandler(os.Stderr, handlerOpts)
	case "json":
		slogHandler = slog.NewJSONHandler(os.Stderr, handlerOpts)
	default:
		fmt.Fprintf(os.Stderr, "WARNING: unknown log format %q, defaulting to \"json\"\n", logFormat)

		slogHandler = slog.NewJSONHandler(os.Stderr, handlerOpts)
	}

	// Set default slog logger so all direct slog calls use configured handler
	slog.SetDefault(slog.New(slogHandler))

	// Set controller-runtime logger using slog bridge
	ctrl.SetLogger(logr.FromSlogHandler(slogHandler))

	// if the enable-http2 flag is false (the default), http/2 should be disabled
	// due to its vulnerabilities. More specifically, disabling http/2 will
	// prevent from being vulnerable to the HTTP/2 Stream Cancellation and
	// Rapid Reset CVEs. For more information see:
	// - https://github.com/advisories/GHSA-qppj-fm5r-hxr3
	// - https://github.com/advisories/GHSA-4374-p667-p6c8
	disableHTTP2 := func(c *tls.Config) {
		setupLog.Info("disabling http/2")
		c.NextProtos = []string{"http/1.1"}
	}

	if !enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	// Initial webhook TLS options
	webhookTLSOpts := tlsOpts
	webhookServerOptions := webhook.Options{
		TLSOpts: webhookTLSOpts,
	}

	if len(webhookCertPath) > 0 {
		setupLog.Info("Initializing webhook certificate watcher using provided certificates",
			"webhook-cert-path", webhookCertPath, "webhook-cert-name", webhookCertName, "webhook-cert-key", webhookCertKey)

		webhookServerOptions.CertDir = webhookCertPath
		webhookServerOptions.CertName = webhookCertName
		webhookServerOptions.KeyName = webhookCertKey
	}

	webhookServer := webhook.NewServer(webhookServerOptions)

	// Metrics endpoint can be configured via Helm chart values. The Metrics options configure the server.
	// More info:
	// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.22.1/pkg/metrics/server
	// - https://book.kubebuilder.io/reference/metrics.html
	metricsServerOptions := metricsserver.Options{
		BindAddress:   metricsAddr,
		SecureServing: secureMetrics,
		TLSOpts:       tlsOpts,
	}

	if secureMetrics {
		// FilterProvider is used to protect the metrics endpoint with authn/authz.
		// These configurations ensure that only authorized users and service accounts
		// can access the metrics endpoint. The RBAC are configured in Helm chart templates. More info:
		// https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.22.1/pkg/metrics/filters#WithAuthenticationAndAuthorization
		metricsServerOptions.FilterProvider = filters.WithAuthenticationAndAuthorization
	}

	// If the certificate is not specified, controller-runtime will automatically
	// generate self-signed certificates for the metrics server. While convenient for development and testing,
	// this setup is not recommended for production.
	//
	// TODO(user): For production deployments, configure TLS certificates via Helm chart values:
	// - Set metrics.certPath, metrics.certName, and metrics.certKey in values.yaml
	// - Use cert-manager to generate certificates for the metrics server
	// - Configure Prometheus ServiceMonitor with TLS settings in the Helm chart
	if len(metricsCertPath) > 0 {
		setupLog.Info("Initializing metrics certificate watcher using provided certificates",
			"metrics-cert-path", metricsCertPath, "metrics-cert-name", metricsCertName, "metrics-cert-key", metricsCertKey)

		metricsServerOptions.CertDir = metricsCertPath
		metricsServerOptions.CertName = metricsCertName
		metricsServerOptions.KeyName = metricsCertKey
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsServerOptions,
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "5fdad980.mc.k8s.lex.la",
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	// Initialize plugin client with caching
	pluginClient := plugins.NewCachedPluginClient(
		plugins.NewHangarClient(),
		time.Hour, // 1 hour cache TTL
	)

	// Initialize Paper API client
	paperClient := paper.NewClient()

	// Initialize Docker Hub registry client
	registryClient := registry.NewClient()

	// Initialize constraint solver
	constraintSolver := solver.NewSimpleSolver()

	// Initialize cron scheduler for update controller
	cronScheduler := mccron.NewRealScheduler()

	// Setup Plugin controller
	if err := (&controller.PluginReconciler{
		Client:       mgr.GetClient(),
		Scheme:       mgr.GetScheme(),
		PluginClient: pluginClient,
		Solver:       constraintSolver,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Plugin")
		os.Exit(1)
	}

	// Setup PaperMCServer controller
	if err := (&controller.PaperMCServerReconciler{
		Client:         mgr.GetClient(),
		Scheme:         mgr.GetScheme(),
		Config:         mgr.GetConfig(),
		PaperClient:    paperClient,
		Solver:         constraintSolver,
		RegistryClient: registryClient,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "PaperMCServer")
		os.Exit(1)
	}

	// Create PodExecutor for executing commands inside pods
	podExecutor, err := controller.NewK8sPodExecutor(mgr.GetConfig())
	if err != nil {
		setupLog.Error(err, "unable to create pod executor")
		os.Exit(1)
	}

	// Setup Update controller with real cron scheduler
	updateReconciler := &controller.UpdateReconciler{
		Client:       mgr.GetClient(),
		Scheme:       mgr.GetScheme(),
		PaperClient:  paperClient,
		PluginClient: pluginClient,
		PodExecutor:  podExecutor,
	}
	updateReconciler.SetCron(cronScheduler)
	if err := updateReconciler.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Update")
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	// Initialize and start Web UI server with REST API
	if webuiEnabled {
		// Use operator namespace if webui-namespace not specified
		uiNamespace := webuiNamespace
		if uiNamespace == "" {
			uiNamespace = os.Getenv("POD_NAMESPACE")
			if uiNamespace == "" {
				uiNamespace = "default"
			}
		}

		// Create API server with version info
		apiServer := api.NewServer(mgr.GetClient(), api.VersionInfo{
			Version:   version,
			GitCommit: gitCommit,
			BuildDate: buildDate,
			GoVersion: goruntime.Version(),
		})

		// Create webui server with API handler
		webuiServer := webui.NewServer(mgr.GetClient(), uiNamespace, webuiAddr, apiServer.Handler())
		if err := mgr.Add(webuiServer); err != nil {
			setupLog.Error(err, "unable to add webui server to manager")
			os.Exit(1)
		}
		setupLog.Info("web ui and api server enabled", "address", webuiAddr, "namespace", uiNamespace)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
