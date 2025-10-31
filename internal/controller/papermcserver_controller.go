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

package controller

import (
	"context"
	"fmt"
	"time"

	"github.com/cockroachdb/errors"
	mcv1alpha1 "github.com/lexfrei/minecraft-operator/api/v1alpha1"
	"github.com/lexfrei/minecraft-operator/pkg/paper"
	"github.com/lexfrei/minecraft-operator/pkg/selector"
	"github.com/lexfrei/minecraft-operator/pkg/solver"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	conditionTypeServerReady      = "Ready"
	conditionTypeStatefulSetReady = "StatefulSetReady"
	conditionTypeUpdateAvailable  = "UpdateAvailable"
	reasonServerReconcileSuccess  = "ReconcileSuccess"
	reasonServerReconcileError    = "ReconcileError"
	reasonStatefulSetCreated      = "StatefulSetCreated"
	reasonStatefulSetNotReady     = "StatefulSetNotReady"
	reasonStatefulSetReady        = "StatefulSetReady"
	reasonUpdateFound             = "UpdateFound"
	reasonNoUpdate                = "NoUpdate"
	defaultPaperImage             = "lexfrei/papermc:latest"
	defaultStorageSize            = "10Gi"
	defaultTerminationGracePeriod = int64(300)
	finalizerName                 = "mc.k8s.lex.la/papermcserver-finalizer"
)

// PaperMCServerReconciler reconciles a PaperMCServer object.
type PaperMCServerReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	PaperClient *paper.Client
	Solver      solver.Solver
}

//+kubebuilder:rbac:groups=mc.k8s.lex.la,resources=papermcservers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=mc.k8s.lex.la,resources=papermcservers/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=mc.k8s.lex.la,resources=papermcservers/finalizers,verbs=update
//+kubebuilder:rbac:groups=mc.k8s.lex.la,resources=plugins,verbs=get;list;watch
//+kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
//nolint:revive // kubebuilder markers require no space after //

// Reconcile implements the reconciliation loop for PaperMCServer resources.
func (r *PaperMCServerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)

	// Fetch the PaperMCServer resource
	var server mcv1alpha1.PaperMCServer
	if err := r.Get(ctx, req.NamespacedName, &server); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("PaperMCServer resource not found, ignoring")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get PaperMCServer resource")
		return ctrl.Result{}, errors.Wrap(err, "failed to get server")
	}

	// Store original status for comparison
	originalStatus := server.Status.DeepCopy()

	// Run reconciliation logic
	result, err := r.doReconcile(ctx, &server)

	// Update status if changed
	if err != nil || !serverStatusEqual(&server.Status, originalStatus) {
		if updateErr := r.Status().Update(ctx, &server); updateErr != nil {
			log.Error(updateErr, "Failed to update PaperMCServer status")
			if err == nil {
				err = updateErr
			}
		}
	}

	// Set conditions based on result
	if err != nil {
		log.Error(err, "Reconciliation failed")
		r.setCondition(&server, conditionTypeServerReady, metav1.ConditionFalse,
			reasonServerReconcileError, err.Error())
	} else {
		r.setCondition(&server, conditionTypeServerReady, metav1.ConditionTrue,
			reasonServerReconcileSuccess, "Server reconciled successfully")
	}

	return result, err
}

// doReconcile performs the actual reconciliation logic.
func (r *PaperMCServerReconciler) doReconcile(ctx context.Context, server *mcv1alpha1.PaperMCServer) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)

	// Step 1: Find matched plugins
	matchedPlugins, err := r.findMatchedPlugins(ctx, server)
	if err != nil {
		return ctrl.Result{}, errors.Wrap(err, "failed to find matched plugins")
	}

	log.Info("Found matching plugins", "count", len(matchedPlugins))

	// Step 2: Ensure StatefulSet exists
	statefulSet, err := r.ensureStatefulSet(ctx, server)
	if err != nil {
		return ctrl.Result{}, errors.Wrap(err, "failed to ensure statefulset")
	}

	// Step 2.5: Ensure Service exists
	if err := r.ensureService(ctx, server); err != nil {
		return ctrl.Result{}, errors.Wrap(err, "failed to ensure service")
	}

	// Step 3: Detect current Paper version and build from StatefulSet
	currentVersion, currentBuild := r.detectCurrentPaperVersion(statefulSet)
	server.Status.CurrentPaperVersion = currentVersion
	server.Status.CurrentPaperBuild = currentBuild

	log.Info("Detected Paper version", "version", currentVersion, "build", currentBuild)

	// Step 4: Update plugin status
	server.Status.Plugins = r.buildPluginStatus(matchedPlugins)

	// Step 5: Run solver to find available updates
	availableUpdate, err := r.findAvailableUpdate(ctx, server, matchedPlugins)
	if err != nil {
		log.Error(err, "Failed to find available update, continuing without update")
		server.Status.AvailableUpdate = nil
		r.setCondition(server, conditionTypeUpdateAvailable, metav1.ConditionFalse,
			reasonNoUpdate, err.Error())
	} else {
		server.Status.AvailableUpdate = availableUpdate
		if availableUpdate != nil {
			log.Info("Available update found", "version", availableUpdate.PaperVersion)
			r.setCondition(server, conditionTypeUpdateAvailable, metav1.ConditionTrue,
				reasonUpdateFound, fmt.Sprintf("Update to %s available", availableUpdate.PaperVersion))
		} else {
			r.setCondition(server, conditionTypeUpdateAvailable, metav1.ConditionFalse,
				reasonNoUpdate, "Server is up to date")
		}
	}

	// Step 6: Check StatefulSet readiness
	if r.isStatefulSetReady(statefulSet) {
		r.setCondition(server, conditionTypeStatefulSetReady, metav1.ConditionTrue,
			reasonStatefulSetReady, "StatefulSet is ready")
	} else {
		r.setCondition(server, conditionTypeStatefulSetReady, metav1.ConditionFalse,
			reasonStatefulSetNotReady, "StatefulSet is not ready")
	}

	return ctrl.Result{RequeueAfter: 15 * time.Minute}, nil
}

// findMatchedPlugins finds all Plugin resources matching this server's labels.
func (r *PaperMCServerReconciler) findMatchedPlugins(
	ctx context.Context,
	server *mcv1alpha1.PaperMCServer,
) ([]mcv1alpha1.Plugin, error) {
	plugins, err := selector.FindMatchingPlugins(ctx, r.Client, server.Namespace, server.Labels)
	if err != nil {
		return nil, errors.Wrap(err, "failed to find matching plugins")
	}
	return plugins, nil
}

// ensureStatefulSet creates or verifies the StatefulSet for this server.
func (r *PaperMCServerReconciler) ensureStatefulSet(
	ctx context.Context,
	server *mcv1alpha1.PaperMCServer,
) (*appsv1.StatefulSet, error) {
	log := ctrl.LoggerFrom(ctx)

	statefulSetName := server.Name
	var statefulSet appsv1.StatefulSet

	err := r.Get(ctx, client.ObjectKey{
		Name:      statefulSetName,
		Namespace: server.Namespace,
	}, &statefulSet)

	if err == nil {
		// StatefulSet exists
		log.Info("StatefulSet already exists", "name", statefulSetName)
		return &statefulSet, nil
	}

	if !apierrors.IsNotFound(err) {
		return nil, errors.Wrap(err, "failed to get statefulset")
	}

	// StatefulSet doesn't exist, create it
	log.Info("Creating new StatefulSet", "name", statefulSetName)

	newStatefulSet := r.buildStatefulSet(server)

	// Set owner reference
	if err := controllerutil.SetControllerReference(server, newStatefulSet, r.Scheme); err != nil {
		return nil, errors.Wrap(err, "failed to set owner reference")
	}

	if err := r.Create(ctx, newStatefulSet); err != nil {
		return nil, errors.Wrap(err, "failed to create statefulset")
	}

	r.setCondition(server, conditionTypeStatefulSetReady, metav1.ConditionFalse,
		reasonStatefulSetCreated, "StatefulSet created, waiting for ready")

	return newStatefulSet, nil
}

// buildStatefulSet constructs a StatefulSet for the PaperMCServer.
func (r *PaperMCServerReconciler) buildStatefulSet(server *mcv1alpha1.PaperMCServer) *appsv1.StatefulSet {
	replicas := int32(1)
	serviceName := server.Name

	podSpec := r.buildPodSpec(server)

	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      server.Name,
			Namespace: server.Namespace,
			Labels:    server.Labels,
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas:             &replicas,
			ServiceName:          serviceName,
			Selector:             r.buildSelector(server),
			Template:             r.buildPodTemplate(server, podSpec),
			VolumeClaimTemplates: r.buildVolumeClaimTemplates(),
		},
	}

	return statefulSet
}

// buildPodSpec constructs the pod spec with configured container.
func (r *PaperMCServerReconciler) buildPodSpec(server *mcv1alpha1.PaperMCServer) *corev1.PodSpec {
	podSpec := server.Spec.PodTemplate.Spec.DeepCopy()

	if len(podSpec.Containers) == 0 {
		podSpec.Containers = []corev1.Container{{
			Name:  "papermc",
			Image: defaultPaperImage,
		}}
	}

	container := &podSpec.Containers[0]
	if container.Image == "" {
		container.Image = defaultPaperImage
	}

	container.Env = r.buildEnvironmentVariables(server, container.Env)
	container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
		Name:      "data",
		MountPath: "/data",
	})

	terminationGracePeriod := defaultTerminationGracePeriod
	if server.Spec.GracefulShutdown.Timeout.Duration > 0 {
		terminationGracePeriod = int64(server.Spec.GracefulShutdown.Timeout.Seconds())
	}
	podSpec.TerminationGracePeriodSeconds = &terminationGracePeriod

	return podSpec
}

// buildSelector creates the label selector for the StatefulSet.
func (r *PaperMCServerReconciler) buildSelector(server *mcv1alpha1.PaperMCServer) *metav1.LabelSelector {
	return &metav1.LabelSelector{
		MatchLabels: map[string]string{
			"app":                       "papermc",
			"mc.k8s.lex.la/server-name": server.Name,
		},
	}
}

// buildPodTemplate creates the pod template for the StatefulSet.
func (r *PaperMCServerReconciler) buildPodTemplate(
	server *mcv1alpha1.PaperMCServer,
	podSpec *corev1.PodSpec,
) corev1.PodTemplateSpec {
	return corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"app":                       "papermc",
				"mc.k8s.lex.la/server-name": server.Name,
			},
		},
		Spec: *podSpec,
	}
}

// buildVolumeClaimTemplates creates the volume claim templates for the StatefulSet.
func (r *PaperMCServerReconciler) buildVolumeClaimTemplates() []corev1.PersistentVolumeClaim {
	return []corev1.PersistentVolumeClaim{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "data",
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{
					corev1.ReadWriteOnce,
				},
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse(defaultStorageSize),
					},
				},
			},
		},
	}
}

// buildEnvironmentVariables constructs environment variables for the Paper container.
func (r *PaperMCServerReconciler) buildEnvironmentVariables(
	server *mcv1alpha1.PaperMCServer,
	existingEnv []corev1.EnvVar,
) []corev1.EnvVar {
	env := existingEnv

	// Add EULA acceptance
	env = r.addOrUpdateEnv(env, "EULA", "TRUE")

	// Add Paper version
	paperVersion := server.Spec.PaperVersion
	if paperVersion != "" && paperVersion != "latest" {
		env = r.addOrUpdateEnv(env, "PAPER_VERSION", paperVersion)
	}

	// Add Paper build if specified
	if server.Spec.PaperBuild != nil {
		env = r.addOrUpdateEnv(env, "PAPER_BUILD", fmt.Sprintf("%d", *server.Spec.PaperBuild))
	}

	// Add RCON configuration if enabled
	if server.Spec.RCON.Enabled {
		env = r.addOrUpdateEnv(env, "RCON_PORT", fmt.Sprintf("%d", server.Spec.RCON.Port))

		// Add RCON password from secret
		env = append(env, corev1.EnvVar{
			Name: "RCON_PASSWORD",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: server.Spec.RCON.PasswordSecret.Name,
					},
					Key: server.Spec.RCON.PasswordSecret.Key,
				},
			},
		})
	}

	return env
}

// addOrUpdateEnv adds or updates an environment variable.
func (r *PaperMCServerReconciler) addOrUpdateEnv(env []corev1.EnvVar, name, value string) []corev1.EnvVar {
	for i := range env {
		if env[i].Name == name {
			env[i].Value = value
			return env
		}
	}
	return append(env, corev1.EnvVar{Name: name, Value: value})
}

// ensureService creates the Service if it doesn't exist.
func (r *PaperMCServerReconciler) ensureService(
	ctx context.Context,
	server *mcv1alpha1.PaperMCServer,
) error {
	log := ctrl.LoggerFrom(ctx)

	serviceName := server.Name
	var service corev1.Service

	err := r.Get(ctx, client.ObjectKey{
		Name:      serviceName,
		Namespace: server.Namespace,
	}, &service)

	if err == nil {
		// Service exists
		log.Info("Service already exists", "name", serviceName)
		return nil
	}

	if !apierrors.IsNotFound(err) {
		return errors.Wrap(err, "failed to get service")
	}

	// Service doesn't exist, create it
	log.Info("Creating new Service", "name", serviceName)

	newService := r.buildService(server)

	// Set owner reference
	if err := controllerutil.SetControllerReference(server, newService, r.Scheme); err != nil {
		return errors.Wrap(err, "failed to set owner reference")
	}

	if err := r.Create(ctx, newService); err != nil {
		return errors.Wrap(err, "failed to create service")
	}

	return nil
}

// buildService constructs a Service for the PaperMCServer.
func (r *PaperMCServerReconciler) buildService(server *mcv1alpha1.PaperMCServer) *corev1.Service {
	ports := []corev1.ServicePort{
		{
			Name:       "minecraft",
			Port:       25565,
			TargetPort: intstr.FromInt(25565),
			Protocol:   corev1.ProtocolTCP,
		},
	}

	// Add RCON port if enabled
	if server.Spec.RCON.Enabled {
		ports = append(ports, corev1.ServicePort{
			Name:       "rcon",
			Port:       25575,
			TargetPort: intstr.FromInt(25575),
			Protocol:   corev1.ProtocolTCP,
		})
	}

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      server.Name,
			Namespace: server.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "papermc",
				"app.kubernetes.io/instance":   server.Name,
				"app.kubernetes.io/managed-by": "minecraft-operator",
			},
		},
		Spec: corev1.ServiceSpec{
			Type:  corev1.ServiceTypeLoadBalancer,
			Ports: ports,
			Selector: map[string]string{
				"app":                       "papermc",
				"mc.k8s.lex.la/server-name": server.Name,
			},
		},
	}
}

// detectCurrentPaperVersion extracts the current Paper version and build from StatefulSet.
func (r *PaperMCServerReconciler) detectCurrentPaperVersion(statefulSet *appsv1.StatefulSet) (string, int) {
	version := ""
	build := 0

	// Try to get version and build from env vars
	if len(statefulSet.Spec.Template.Spec.Containers) > 0 {
		for _, env := range statefulSet.Spec.Template.Spec.Containers[0].Env {
			if env.Name == "PAPER_VERSION" {
				version = env.Value
			}
			if env.Name == "PAPER_BUILD" {
				// Parse build number
				fmt.Sscanf(env.Value, "%d", &build)
			}
		}
	}
	return version, build
}

// isStatefulSetReady checks if the StatefulSet is ready.
func (r *PaperMCServerReconciler) isStatefulSetReady(statefulSet *appsv1.StatefulSet) bool {
	if statefulSet == nil {
		return false
	}

	return statefulSet.Status.ReadyReplicas > 0 &&
		statefulSet.Status.ReadyReplicas == statefulSet.Status.Replicas
}

// buildPluginStatus constructs plugin status list for the server.
func (r *PaperMCServerReconciler) buildPluginStatus(plugins []mcv1alpha1.Plugin) []mcv1alpha1.ServerPluginStatus {
	status := make([]mcv1alpha1.ServerPluginStatus, 0, len(plugins))

	for i := range plugins {
		plugin := &plugins[i]

		pluginStatus := mcv1alpha1.ServerPluginStatus{
			PluginRef: mcv1alpha1.PluginRef{
				Name:      plugin.Name,
				Namespace: plugin.Namespace,
			},
			ResolvedVersion: plugin.Status.ResolvedVersion,
			Compatible:      true,
			Source:          plugin.Spec.Source.Type,
		}

		status = append(status, pluginStatus)
	}

	return status
}

// findAvailableUpdate runs the solver to find available Paper version or build updates.
func (r *PaperMCServerReconciler) findAvailableUpdate(
	ctx context.Context,
	server *mcv1alpha1.PaperMCServer,
	matchedPlugins []mcv1alpha1.Plugin,
) (*mcv1alpha1.AvailableUpdate, error) {
	// Case 1: paperVersion == "latest" - find best version through solver
	if server.Spec.PaperVersion == "latest" {
		return r.findVersionUpdate(ctx, server, matchedPlugins)
	}

	// Case 2: specific version but no build specified - check for build updates
	if server.Spec.PaperBuild == nil {
		return r.findBuildUpdate(ctx, server, matchedPlugins)
	}

	// Case 3: both version and build pinned - no updates
	return nil, nil
}

// findVersionUpdate finds the best Paper version using the solver.
func (r *PaperMCServerReconciler) findVersionUpdate(
	ctx context.Context,
	server *mcv1alpha1.PaperMCServer,
	matchedPlugins []mcv1alpha1.Plugin,
) (*mcv1alpha1.AvailableUpdate, error) {
	log := ctrl.LoggerFrom(ctx)

	// Fetch available Paper versions
	paperVersions, err := r.PaperClient.GetPaperVersions(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch paper versions")
	}

	log.Info("Fetched Paper versions", "count", len(paperVersions))

	// Run solver
	bestVersion, err := r.Solver.FindBestPaperVersion(ctx, server, matchedPlugins, paperVersions)
	if err != nil {
		return nil, errors.Wrap(err, "solver failed to find best version")
	}

	// Check if update is needed
	if bestVersion == server.Status.CurrentPaperVersion || bestVersion == "" {
		return nil, nil
	}

	// Get build info for the best version
	buildInfo, err := r.PaperClient.GetPaperBuild(ctx, bestVersion)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get build info")
	}

	// Build plugin version pairs
	pluginPairs := r.buildPluginVersionPairs(matchedPlugins)

	return &mcv1alpha1.AvailableUpdate{
		PaperVersion: bestVersion,
		PaperBuild:   buildInfo.Build,
		ReleasedAt:   metav1.Now(),
		Plugins:      pluginPairs,
		FoundAt:      metav1.Now(),
	}, nil
}

// findBuildUpdate checks for newer builds of the current version.
func (r *PaperMCServerReconciler) findBuildUpdate(
	ctx context.Context,
	server *mcv1alpha1.PaperMCServer,
	matchedPlugins []mcv1alpha1.Plugin,
) (*mcv1alpha1.AvailableUpdate, error) {
	log := ctrl.LoggerFrom(ctx)

	// Get latest build for the specified version
	buildInfo, err := r.PaperClient.GetPaperBuild(ctx, server.Spec.PaperVersion)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get latest build")
	}

	log.Info("Found latest build", "version", buildInfo.Version, "build", buildInfo.Build)

	// Check if update is needed
	if buildInfo.Build <= server.Status.CurrentPaperBuild {
		log.Info("Already on latest build", "current", server.Status.CurrentPaperBuild, "latest", buildInfo.Build)
		return nil, nil
	}

	// Check update delay if configured
	if server.Spec.UpdateDelay != nil {
		// TODO: Get actual build release time from API and check delay
		// For now, we assume delay has passed
		log.Info("Update delay check skipped (not implemented yet)")
	}

	// Build plugin version pairs
	pluginPairs := r.buildPluginVersionPairs(matchedPlugins)

	log.Info("Build update available", "current", server.Status.CurrentPaperBuild, "available", buildInfo.Build)

	return &mcv1alpha1.AvailableUpdate{
		PaperVersion: server.Spec.PaperVersion,
		PaperBuild:   buildInfo.Build,
		ReleasedAt:   metav1.Now(),
		Plugins:      pluginPairs,
		FoundAt:      metav1.Now(),
	}, nil
}

// buildPluginVersionPairs constructs plugin version pairs from matched plugins.
func (r *PaperMCServerReconciler) buildPluginVersionPairs(
	matchedPlugins []mcv1alpha1.Plugin,
) []mcv1alpha1.PluginVersionPair {
	pluginPairs := make([]mcv1alpha1.PluginVersionPair, 0, len(matchedPlugins))
	for i := range matchedPlugins {
		plugin := &matchedPlugins[i]
		if plugin.Status.ResolvedVersion != "" {
			pluginPairs = append(pluginPairs, mcv1alpha1.PluginVersionPair{
				PluginRef: mcv1alpha1.PluginRef{
					Name:      plugin.Name,
					Namespace: plugin.Namespace,
				},
				Version: plugin.Status.ResolvedVersion,
			})
		}
	}
	return pluginPairs
}

// setCondition sets or updates a condition in the server status.
func (r *PaperMCServerReconciler) setCondition(
	server *mcv1alpha1.PaperMCServer,
	conditionType string,
	status metav1.ConditionStatus,
	reason,
	message string,
) {
	condition := metav1.Condition{
		Type:               conditionType,
		Status:             status,
		ObservedGeneration: server.Generation,
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	}

	meta.SetStatusCondition(&server.Status.Conditions, condition)
}

// serverStatusEqual compares two server statuses for equality.
func serverStatusEqual(a, b *mcv1alpha1.PaperMCServerStatus) bool {
	if a.CurrentPaperVersion != b.CurrentPaperVersion {
		return false
	}
	if a.CurrentPaperBuild != b.CurrentPaperBuild {
		return false
	}
	if len(a.Plugins) != len(b.Plugins) {
		return false
	}
	if (a.AvailableUpdate == nil) != (b.AvailableUpdate == nil) {
		return false
	}
	return true
}

// SetupWithManager sets up the controller with the Manager.
func (r *PaperMCServerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&mcv1alpha1.PaperMCServer{}).
		Owns(&appsv1.StatefulSet{}).
		Owns(&corev1.Service{}).
		Watches(
			&mcv1alpha1.Plugin{},
			handler.EnqueueRequestsFromMapFunc(r.findServersForPlugin),
		).
		Named("papermcserver").
		Complete(r)
}

// findServersForPlugin maps Plugin changes to PaperMCServer reconciliation requests.
// This ensures servers are reconciled when plugin status changes.
func (r *PaperMCServerReconciler) findServersForPlugin(ctx context.Context, obj client.Object) []reconcile.Request {
	plugin, ok := obj.(*mcv1alpha1.Plugin)
	if !ok {
		return nil
	}

	log := ctrl.LoggerFrom(ctx)

	// Find all servers in the same namespace that match this plugin's selector
	matchedServers, err := selector.FindMatchingServers(
		ctx,
		r.Client,
		plugin.Namespace,
		plugin.Spec.InstanceSelector,
	)
	if err != nil {
		log.Error(err, "Failed to find servers for plugin watch")
		return nil
	}

	requests := make([]reconcile.Request, 0, len(matchedServers))
	for i := range matchedServers {
		server := &matchedServers[i]
		requests = append(requests, reconcile.Request{
			NamespacedName: client.ObjectKey{
				Name:      server.Name,
				Namespace: server.Namespace,
			},
		})
	}

	log.Info("Plugin change triggered server reconciliations",
		"plugin", plugin.Name,
		"servers", len(requests))

	return requests
}
