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
	"log/slog"
	"regexp"
	"strconv"
	"time"

	"github.com/cockroachdb/errors"
	mcv1alpha1 "github.com/lexfrei/minecraft-operator/api/v1alpha1"
	"github.com/lexfrei/minecraft-operator/pkg/paper"
	"github.com/lexfrei/minecraft-operator/pkg/plugins"
	"github.com/lexfrei/minecraft-operator/pkg/registry"
	"github.com/lexfrei/minecraft-operator/pkg/selector"
	"github.com/lexfrei/minecraft-operator/pkg/solver"
	"github.com/lexfrei/minecraft-operator/pkg/version"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/rest"
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
	conditionTypeUpdateBlocked    = "UpdateBlocked"
	conditionTypeSolverRunning    = "SolverRunning"
	reasonServerReconcileSuccess  = "ReconcileSuccess"
	reasonServerReconcileError    = "ReconcileError"
	reasonStatefulSetCreated      = "StatefulSetCreated"
	reasonStatefulSetNotReady     = "StatefulSetNotReady"
	reasonStatefulSetReady        = "StatefulSetReady"
	reasonUpdateFound             = "UpdateFound"
	reasonNoUpdate                = "NoUpdate"
	reasonUpdateBlocked           = "UpdateBlocked"
	reasonUpdateUnblocked         = "UpdateUnblocked"
	reasonSolverStarted           = "SolverStarted"
	reasonSolverCompleted         = "SolverCompleted"
	reasonSolverFailed            = "SolverFailed"
	defaultStorageSize            = "10Gi"
	defaultTerminationGracePeriod = int64(300)
)

// PaperMCServerReconciler reconciles a PaperMCServer object.
type PaperMCServerReconciler struct {
	client.Client
	Scheme         *runtime.Scheme
	Config         *rest.Config
	PaperClient    *paper.Client
	Solver         solver.Solver
	RegistryClient *registry.Client
}

//+kubebuilder:rbac:groups=mc.k8s.lex.la,resources=papermcservers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=mc.k8s.lex.la,resources=papermcservers/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=mc.k8s.lex.la,resources=papermcservers/finalizers,verbs=update
//+kubebuilder:rbac:groups=mc.k8s.lex.la,resources=plugins,verbs=get;list;watch
//+kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch
//+kubebuilder:rbac:groups=core,resources=pods/exec,verbs=create
//nolint:revive // kubebuilder markers require no space after //

// Reconcile implements the reconciliation loop for PaperMCServer resources.
func (r *PaperMCServerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// Fetch the PaperMCServer resource
	var server mcv1alpha1.PaperMCServer
	if err := r.Get(ctx, req.NamespacedName, &server); err != nil {
		if apierrors.IsNotFound(err) {
			slog.InfoContext(ctx, "PaperMCServer resource not found, ignoring")
			return ctrl.Result{}, nil
		}
		slog.ErrorContext(ctx, "Failed to get PaperMCServer resource", "error", err)
		return ctrl.Result{}, errors.Wrap(err, "failed to get server")
	}

	// Store original status for comparison
	originalStatus := server.Status.DeepCopy()

	// Run reconciliation logic
	result, err := r.doReconcile(ctx, &server)

	// Set conditions based on result BEFORE status update so they are persisted
	if err != nil {
		slog.ErrorContext(ctx, "Reconciliation failed", "error", err)
		r.setCondition(&server, conditionTypeServerReady, metav1.ConditionFalse,
			reasonServerReconcileError, err.Error())
	} else {
		r.setCondition(&server, conditionTypeServerReady, metav1.ConditionTrue,
			reasonServerReconcileSuccess, "Server reconciled successfully")
	}

	// Update status if changed (includes conditions set above)
	if err != nil || !serverStatusEqual(&server.Status, originalStatus) {
		if updateErr := r.Status().Update(ctx, &server); updateErr != nil {
			slog.ErrorContext(ctx, "Failed to update PaperMCServer status", "error", updateErr)
			if err == nil {
				err = updateErr
			}
		}
	}

	return result, err
}

// doReconcile performs the actual reconciliation logic.
func (r *PaperMCServerReconciler) doReconcile(ctx context.Context, server *mcv1alpha1.PaperMCServer) (ctrl.Result, error) {
	// Step 1: Find matched plugins
	matchedPlugins, err := r.findMatchedPlugins(ctx, server)
	if err != nil {
		return ctrl.Result{}, errors.Wrap(err, "failed to find matched plugins")
	}

	slog.InfoContext(ctx, "Found matching plugins", "count", len(matchedPlugins))

	// Step 2: Resolve and update desired version
	if err := r.updateDesiredVersion(ctx, server, matchedPlugins); err != nil {
		return ctrl.Result{}, err
	}

	// Step 3: Validate that DesiredVersion and DesiredBuild are set before creating infrastructure
	if server.Status.DesiredVersion == "" || server.Status.DesiredBuild == 0 {
		return ctrl.Result{}, errors.New("desired version not resolved - cannot create infrastructure")
	}

	// Step 4: Check if updates are blocked
	if server.Status.UpdateBlocked != nil && server.Status.UpdateBlocked.Blocked {
		slog.InfoContext(ctx, "Update blocked, skipping infrastructure update",
			"reason", server.Status.UpdateBlocked.Reason)
		// Don't proceed with StatefulSet update, but continue to update status
	}

	// Step 5: Ensure infrastructure (StatefulSet and Service)
	statefulSet, err := r.ensureInfrastructure(ctx, server, matchedPlugins)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Step 6: Update server status
	r.updateServerStatus(ctx, server, statefulSet, matchedPlugins)

	return ctrl.Result{RequeueAfter: 15 * time.Minute}, nil
}

// updateDesiredVersion resolves and updates the desired Paper version in server status.
func (r *PaperMCServerReconciler) updateDesiredVersion(
	ctx context.Context,
	server *mcv1alpha1.PaperMCServer,
	matchedPlugins []mcv1alpha1.Plugin,
) error {
	desiredVersion, desiredBuild, err := r.resolveDesiredPaperVersion(ctx, server, matchedPlugins)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to resolve desired Paper version", "error", err)
		// Don't fail reconciliation completely - keep existing desired if set
		if server.Status.DesiredVersion != "" {
			desiredVersion = server.Status.DesiredVersion
			desiredBuild = server.Status.DesiredBuild
		} else {
			// No fallback, cannot proceed
			return errors.Wrap(err, "cannot resolve desired version and no fallback available")
		}
	}

	// Store desired in status
	server.Status.DesiredVersion = desiredVersion
	server.Status.DesiredBuild = desiredBuild

	slog.InfoContext(ctx, "Resolved desired Paper version", "version", desiredVersion, "build", desiredBuild)
	return nil
}

// ensureInfrastructure ensures StatefulSet and Service exist.
func (r *PaperMCServerReconciler) ensureInfrastructure(
	ctx context.Context,
	server *mcv1alpha1.PaperMCServer,
	matchedPlugins []mcv1alpha1.Plugin,
) (*appsv1.StatefulSet, error) {
	statefulSet, err := r.ensureStatefulSet(ctx, server)
	if err != nil {
		return nil, errors.Wrap(err, "failed to ensure statefulset")
	}

	if err := r.ensureService(ctx, server, matchedPlugins); err != nil {
		return nil, errors.Wrap(err, "failed to ensure service")
	}

	return statefulSet, nil
}

// updateServerStatus updates all status fields for the server.
func (r *PaperMCServerReconciler) updateServerStatus(
	ctx context.Context,
	server *mcv1alpha1.PaperMCServer,
	statefulSet *appsv1.StatefulSet,
	matchedPlugins []mcv1alpha1.Plugin,
) {
	// Detect current Paper version and build from StatefulSet
	currentVersion, currentBuild := r.detectCurrentPaperVersion(ctx, statefulSet)
	server.Status.CurrentVersion = currentVersion
	server.Status.CurrentBuild = currentBuild

	slog.InfoContext(ctx, "Detected Paper version", "version", currentVersion, "build", currentBuild)

	// Update plugin status (resolves versions individually for this server)
	server.Status.Plugins = r.buildPluginStatus(ctx, server, matchedPlugins)

	// Update available update status
	r.updateAvailableUpdateStatus(ctx, server, matchedPlugins)

	// Update StatefulSet readiness condition
	r.updateStatefulSetReadiness(server, statefulSet)
}

// updateAvailableUpdateStatus checks for and updates available update information.
func (r *PaperMCServerReconciler) updateAvailableUpdateStatus(
	ctx context.Context,
	server *mcv1alpha1.PaperMCServer,
	matchedPlugins []mcv1alpha1.Plugin,
) {
	availableUpdate, err := r.findAvailableUpdate(ctx, server, matchedPlugins)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to find available update, continuing without update", "error", err)
		server.Status.AvailableUpdate = nil
		r.setCondition(server, conditionTypeUpdateAvailable, metav1.ConditionFalse,
			reasonNoUpdate, err.Error())
		return
	}

	server.Status.AvailableUpdate = availableUpdate
	if availableUpdate != nil {
		slog.InfoContext(ctx, "Update available", "version", availableUpdate.Version)
		r.setCondition(server, conditionTypeUpdateAvailable, metav1.ConditionTrue,
			reasonUpdateFound, fmt.Sprintf("Update to %s available", availableUpdate.Version))
	} else {
		r.setCondition(server, conditionTypeUpdateAvailable, metav1.ConditionFalse,
			reasonNoUpdate, "Server is up to date")
	}
}

// updateStatefulSetReadiness updates the StatefulSet readiness condition.
func (r *PaperMCServerReconciler) updateStatefulSetReadiness(
	server *mcv1alpha1.PaperMCServer,
	statefulSet *appsv1.StatefulSet,
) {
	if r.isStatefulSetReady(statefulSet) {
		r.setCondition(server, conditionTypeStatefulSetReady, metav1.ConditionTrue,
			reasonStatefulSetReady, "StatefulSet is ready")
	} else {
		r.setCondition(server, conditionTypeStatefulSetReady, metav1.ConditionFalse,
			reasonStatefulSetNotReady, "StatefulSet is not ready")
	}
}

// findMatchedPlugins finds all Plugin resources matching this server's labels.
func (r *PaperMCServerReconciler) findMatchedPlugins(
	ctx context.Context,
	server *mcv1alpha1.PaperMCServer,
) ([]mcv1alpha1.Plugin, error) {
	matchedPlugins, err := selector.FindMatchingPlugins(ctx, r.Client, server.Namespace, server.Labels)
	if err != nil {
		return nil, errors.Wrap(err, "failed to find matching plugins")
	}
	return matchedPlugins, nil
}

// ensureStatefulSet creates or verifies the StatefulSet for this server.
func (r *PaperMCServerReconciler) ensureStatefulSet(
	ctx context.Context,
	server *mcv1alpha1.PaperMCServer,
) (*appsv1.StatefulSet, error) {
	statefulSetName := server.Name
	var statefulSet appsv1.StatefulSet

	err := r.Get(ctx, client.ObjectKey{
		Name:      statefulSetName,
		Namespace: server.Namespace,
	}, &statefulSet)

	if err == nil {
		// StatefulSet exists
		slog.InfoContext(ctx, "StatefulSet already exists", "name", statefulSetName)
		return &statefulSet, nil
	}

	if !apierrors.IsNotFound(err) {
		return nil, errors.Wrap(err, "failed to get statefulset")
	}

	// StatefulSet doesn't exist, create it
	slog.InfoContext(ctx, "Creating new StatefulSet", "name", statefulSetName)

	newStatefulSet, err := r.buildStatefulSet(server)
	if err != nil {
		return nil, errors.Wrap(err, "failed to build statefulset")
	}

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
func (r *PaperMCServerReconciler) buildStatefulSet(server *mcv1alpha1.PaperMCServer) (*appsv1.StatefulSet, error) {
	replicas := int32(1)
	serviceName := server.Name

	podSpec, err := r.buildPodSpec(server)
	if err != nil {
		return nil, errors.Wrap(err, "failed to build pod spec")
	}

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

	return statefulSet, nil
}

// buildPodSpec constructs the pod spec with configured container.
// CRITICAL: This function MUST NEVER generate an image tag with :latest.
// All images MUST use concrete version-build tags (e.g., docker.io/lexfrei/papermc:1.21.1-91).
func (r *PaperMCServerReconciler) buildPodSpec(server *mcv1alpha1.PaperMCServer) (*corev1.PodSpec, error) {
	podSpec := server.Spec.PodTemplate.Spec.DeepCopy()

	if len(podSpec.Containers) == 0 {
		podSpec.Containers = []corev1.Container{{
			Name: "papermc",
		}}
	}

	container := &podSpec.Containers[0]

	// Construct image based on desired version from status
	// NEVER use :latest tag - always use concrete version-build
	if server.Status.DesiredVersion == "" || server.Status.DesiredBuild == 0 {
		// This should never happen - doReconcile() validates DesiredVersion/DesiredBuild are set.
		// If we reach here, it's a programming error.
		return nil, errors.Newf("BUG: buildPodSpec called without DesiredVersion/DesiredBuild set "+
			"(version=%s, build=%d)", server.Status.DesiredVersion, server.Status.DesiredBuild)
	}

	container.Image = fmt.Sprintf("docker.io/lexfrei/papermc:%s-%d",
		server.Status.DesiredVersion,
		server.Status.DesiredBuild)

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

	return podSpec, nil
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
	paperVersion := server.Spec.Version
	if paperVersion != "" && paperVersion != versionPolicyLatest {
		env = r.addOrUpdateEnv(env, "PAPER_VERSION", paperVersion)
	}

	// Add Paper build if specified
	if server.Spec.Build != nil {
		env = r.addOrUpdateEnv(env, "PAPER_BUILD", fmt.Sprintf("%d", *server.Spec.Build))
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

// ensureService creates or updates the Service for the PaperMCServer.
func (r *PaperMCServerReconciler) ensureService(
	ctx context.Context,
	server *mcv1alpha1.PaperMCServer,
	matchedPlugins []mcv1alpha1.Plugin,
) error {
	serviceName := server.Name
	var existingService corev1.Service

	err := r.Get(ctx, client.ObjectKey{
		Name:      serviceName,
		Namespace: server.Namespace,
	}, &existingService)

	// Build desired service
	desiredService := r.buildService(server, matchedPlugins)

	if err != nil {
		if !apierrors.IsNotFound(err) {
			return errors.Wrap(err, "failed to get service")
		}

		// Service doesn't exist, create it
		slog.InfoContext(ctx, "Creating new Service", "name", serviceName)

		// Set owner reference
		if err := controllerutil.SetControllerReference(server, desiredService, r.Scheme); err != nil {
			return errors.Wrap(err, "failed to set owner reference")
		}

		if err := r.Create(ctx, desiredService); err != nil {
			return errors.Wrap(err, "failed to create service")
		}

		return nil
	}

	// Service exists, update it
	slog.InfoContext(ctx, "Updating existing Service", "name", serviceName)

	// Preserve immutable fields
	desiredService.ResourceVersion = existingService.ResourceVersion
	desiredService.Spec.ClusterIP = existingService.Spec.ClusterIP
	desiredService.Spec.ClusterIPs = existingService.Spec.ClusterIPs

	// Set owner reference if not already set
	if err := controllerutil.SetControllerReference(server, desiredService, r.Scheme); err != nil {
		return errors.Wrap(err, "failed to set owner reference")
	}

	if err := r.Update(ctx, desiredService); err != nil {
		return errors.Wrap(err, "failed to update service")
	}

	return nil
}

// buildServicePorts constructs the list of ServicePorts for the PaperMCServer.
func buildServicePorts(server *mcv1alpha1.PaperMCServer, matchedPlugins []mcv1alpha1.Plugin) []corev1.ServicePort {
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

	// Add plugin ports (TCP+UDP for each)
	seenPorts := make(map[int32]bool)
	seenPorts[25565] = true // minecraft
	if server.Spec.RCON.Enabled {
		seenPorts[25575] = true // rcon
	}

	for _, plugin := range matchedPlugins {
		if plugin.Spec.Port != nil && !seenPorts[*plugin.Spec.Port] {
			seenPorts[*plugin.Spec.Port] = true
			portName := fmt.Sprintf("plugin-%d", *plugin.Spec.Port)
			// Add TCP port
			ports = append(ports, corev1.ServicePort{
				Name:       portName + "-tcp",
				Port:       *plugin.Spec.Port,
				TargetPort: intstr.FromInt(int(*plugin.Spec.Port)),
				Protocol:   corev1.ProtocolTCP,
			})
			// Add UDP port
			ports = append(ports, corev1.ServicePort{
				Name:       portName + "-udp",
				Port:       *plugin.Spec.Port,
				TargetPort: intstr.FromInt(int(*plugin.Spec.Port)),
				Protocol:   corev1.ProtocolUDP,
			})
		}
	}

	return ports
}

// buildService constructs a Service for the PaperMCServer.
func (r *PaperMCServerReconciler) buildService(
	server *mcv1alpha1.PaperMCServer,
	matchedPlugins []mcv1alpha1.Plugin,
) *corev1.Service {
	ports := buildServicePorts(server, matchedPlugins)

	// Determine service type (default to LoadBalancer)
	serviceType := corev1.ServiceTypeLoadBalancer
	if server.Spec.Service.Type != "" {
		serviceType = server.Spec.Service.Type
	}

	// Build service spec
	serviceSpec := corev1.ServiceSpec{
		Type:  serviceType,
		Ports: ports,
		Selector: map[string]string{
			"app":                       "papermc",
			"mc.k8s.lex.la/server-name": server.Name,
		},
	}

	// Add LoadBalancerIP if specified
	if server.Spec.Service.LoadBalancerIP != "" {
		serviceSpec.LoadBalancerIP = server.Spec.Service.LoadBalancerIP
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
			Annotations: server.Spec.Service.Annotations,
		},
		Spec: serviceSpec,
	}
}

// detectCurrentPaperVersion parses version from StatefulSet container image tag.
// Supported formats:
//   - docker.io/lexfrei/papermc:1.21.10-91 -> version="1.21.10", build=91
//   - docker.io/lexfrei/papermc:latest -> version="latest", build=0 (DEPRECATED, for backward compatibility only)
//   - lexfrei/papermc:1.21.10-91 -> version="1.21.10", build=91
//
// NOTE: The :latest tag is deprecated and should not be used in new deployments.
// This function still handles it for backward compatibility with existing deployments.
func (r *PaperMCServerReconciler) detectCurrentPaperVersion(ctx context.Context, statefulSet *appsv1.StatefulSet) (string, int) {
	if statefulSet == nil || len(statefulSet.Spec.Template.Spec.Containers) == 0 {
		return "", 0
	}

	image := statefulSet.Spec.Template.Spec.Containers[0].Image

	// Parse image tag from formats:
	// - docker.io/lexfrei/papermc:1.21.10-91
	// - lexfrei/papermc:latest (deprecated)
	// Pattern: (.*/)?(lexfrei/papermc):(.+)
	imageRegex := regexp.MustCompile(`(?:.*/)?(lexfrei/papermc):(.+)`)
	matches := imageRegex.FindStringSubmatch(image)

	if len(matches) < 3 {
		slog.ErrorContext(ctx, "Failed to parse image",
			"error", errors.New("image format mismatch"),
			"image", image)
		return "", 0
	}

	tag := matches[2]

	// Handle "latest" tag specially (DEPRECATED)
	// This is only for backward compatibility with existing deployments
	if tag == versionPolicyLatest {
		slog.WarnContext(ctx, "DEPRECATED: Detected :latest tag in existing deployment",
			"image", image,
			"recommendation", "The :latest tag is deprecated. New deployments use concrete version-build tags.")
		return versionPolicyLatest, 0
	}

	// Parse version-build format: 1.21.10-91
	tagRegex := regexp.MustCompile(`^([0-9.]+)-([0-9]+)$`)
	tagMatches := tagRegex.FindStringSubmatch(tag)

	if len(tagMatches) < 3 {
		slog.ErrorContext(ctx, "Failed to parse tag",
			"error", errors.New("tag format mismatch"),
			"tag", tag,
			"image", image)
		return "", 0
	}

	paperVersion := tagMatches[1]
	build, err := strconv.Atoi(tagMatches[2])
	if err != nil {
		slog.ErrorContext(ctx, "Failed to parse build number",
			"error", err,
			"buildStr", tagMatches[2])
		return paperVersion, 0
	}

	return paperVersion, build
}

// resolveDesiredPaperVersion determines the target Paper version based on spec.updateStrategy.
// Returns version string, build number, and error if resolution fails.
// CRITICAL: Checks downgrade and plugin compatibility BEFORE setting desired version.
func (r *PaperMCServerReconciler) resolveDesiredPaperVersion(
	ctx context.Context,
	server *mcv1alpha1.PaperMCServer,
	matchedPlugins []mcv1alpha1.Plugin,
) (paperVersion string, build int, err error) {
	switch server.Spec.UpdateStrategy {
	case updateStrategyLatest:
		return r.resolveLatestVersion(ctx, server, matchedPlugins)

	case updateStrategyAuto:
		return r.resolveAutoVersion(ctx, server, matchedPlugins)

	case updateStrategyPin:
		if server.Spec.Version == "" {
			return "", 0, errors.New("version is required for 'pin' strategy")
		}
		return r.resolveVersionOnlyMode(ctx, server, server.Spec.Version, matchedPlugins)

	case updateStrategyBuildPin:
		if server.Spec.Version == "" {
			return "", 0, errors.New("version is required for 'build-pin' strategy")
		}
		if server.Spec.Build == nil {
			return "", 0, errors.New("build is required for 'build-pin' strategy")
		}
		buildStr := fmt.Sprintf("%d", *server.Spec.Build)
		return r.resolvePinnedVersionBuild(ctx, server, server.Spec.Version, buildStr,
			fmt.Sprintf("%s-%s", server.Spec.Version, buildStr), matchedPlugins)

	default:
		return "", 0, errors.Newf("invalid updateStrategy: %s (expected 'latest', 'auto', 'pin', or 'build-pin')",
			server.Spec.UpdateStrategy)
	}
}

// checkDowngrade checks if candidate version would be a downgrade from current.
func (r *PaperMCServerReconciler) checkDowngrade(
	server *mcv1alpha1.PaperMCServer,
	candidateVersion string,
) error {
	if server.Status.CurrentVersion == "" {
		return nil // First deployment, no downgrade possible
	}

	// Allow any version when current is "latest" (unresolved)
	if server.Status.CurrentVersion == updateStrategyLatest {
		return nil
	}

	isDowngrade, err := version.IsDowngrade(server.Status.CurrentVersion, candidateVersion)
	if err != nil {
		return errors.Wrap(err, "failed to compare versions")
	}

	if isDowngrade {
		reason := fmt.Sprintf("Downgrade not allowed: current=%s, candidate=%s",
			server.Status.CurrentVersion, candidateVersion)
		r.setUpdateBlocked(server, reason, nil)
		return errors.New("downgrade prevented")
	}

	return nil
}

// checkCompatibility checks plugin compatibility with candidate version.
func (r *PaperMCServerReconciler) checkCompatibility(
	ctx context.Context,
	server *mcv1alpha1.PaperMCServer,
	candidateVersion string,
	matchedPlugins []mcv1alpha1.Plugin,
) error {
	compatible, blockingPlugin, blockReason := r.checkPluginCompatibility(ctx, candidateVersion, matchedPlugins)
	if !compatible {
		// Find blocking plugin details
		var blockedBy *mcv1alpha1.BlockedByInfo
		for i := range matchedPlugins {
			plugin := &matchedPlugins[i]
			if plugin.Name == blockingPlugin {
				// Use first available version as placeholder since resolvedVersion is per-server now
				pluginVersion := "unknown"
				if len(plugin.Status.AvailableVersions) > 0 {
					pluginVersion = plugin.Status.AvailableVersions[0].Version
				}
				blockedBy = &mcv1alpha1.BlockedByInfo{
					Plugin:  blockingPlugin,
					Version: pluginVersion,
					// TODO: Get SupportedPaperVersions from plugin metadata
				}
				break
			}
		}

		r.setUpdateBlocked(server, blockReason, blockedBy)
		return errors.New("update blocked by plugin incompatibility")
	}

	return nil
}

// resolveLatestVersion resolves "latest" to actual latest Paper version from Docker Hub.
func (r *PaperMCServerReconciler) resolveLatestVersion(
	ctx context.Context,
	server *mcv1alpha1.PaperMCServer,
	matchedPlugins []mcv1alpha1.Plugin,
) (string, int, error) {
	slog.InfoContext(ctx, "Using latest Paper version policy - resolving from Docker Hub tags")

	// Fetch available tags from Docker Hub
	tags, err := r.RegistryClient.ListTags(ctx, "lexfrei/papermc", 0)
	if err != nil {
		return "", 0, errors.Wrap(err, "failed to list Docker Hub tags")
	}

	// Parse tags and find latest version-build
	candidateVersion, candidateBuild, err := r.findLatestVersionFromTags(tags)
	if err != nil {
		return "", 0, errors.Wrap(err, "failed to find latest version from tags")
	}

	slog.InfoContext(ctx, "Resolved latest from Docker Hub", "version", candidateVersion, "build", candidateBuild)

	// Check downgrade
	if err := r.checkDowngrade(server, candidateVersion); err != nil {
		return server.Status.DesiredVersion, server.Status.DesiredBuild, err
	}

	// Check plugin compatibility
	if err := r.checkCompatibility(ctx, server, candidateVersion, matchedPlugins); err != nil {
		return server.Status.DesiredVersion, server.Status.DesiredBuild, err
	}

	// Clear any previous block
	r.clearUpdateBlocked(server)
	return candidateVersion, candidateBuild, nil
}

// findLatestVersionFromTags parses Docker Hub tags and finds the latest version-build.
func (r *PaperMCServerReconciler) findLatestVersionFromTags(tags []string) (string, int, error) {
	versionBuildRegex := regexp.MustCompile(`^(\d+\.\d+\.\d+)-(\d+)$`)

	var latestVersion string
	var latestBuild int

	for _, tag := range tags {
		matches := versionBuildRegex.FindStringSubmatch(tag)
		if len(matches) != 3 {
			continue // Skip tags that don't match version-build pattern
		}

		candidateVersion := matches[1]
		candidateBuild, err := strconv.Atoi(matches[2])
		if err != nil {
			continue
		}

		// First valid version found
		if latestVersion == "" {
			latestVersion = candidateVersion
			latestBuild = candidateBuild
			continue
		}

		// Compare versions
		cmp, err := version.Compare(candidateVersion, latestVersion)
		if err != nil {
			continue
		}

		if cmp > 0 {
			// candidateVersion is newer
			latestVersion = candidateVersion
			latestBuild = candidateBuild
		} else if cmp == 0 && candidateBuild > latestBuild {
			// Same version, higher build
			latestBuild = candidateBuild
		}
	}

	if latestVersion == "" {
		return "", 0, errors.New("no valid version-build tags found in Docker Hub")
	}

	return latestVersion, latestBuild, nil
}

// resolveAutoVersion resolves version using auto mode with solver.
func (r *PaperMCServerReconciler) resolveAutoVersion(
	ctx context.Context,
	server *mcv1alpha1.PaperMCServer,
	matchedPlugins []mcv1alpha1.Plugin,
) (string, int, error) {
	slog.InfoContext(ctx, "Using auto Paper version policy")

	availableUpdate, err := r.findAvailableUpdate(ctx, server, matchedPlugins)
	if err != nil {
		return "", 0, errors.Wrap(err, "failed to find available update in auto mode")
	}

	if availableUpdate == nil {
		return "", 0, errors.New("no available update found in auto mode")
	}

	candidateVersion := availableUpdate.Version
	build := availableUpdate.Build

	// Check downgrade
	if err := r.checkDowngrade(server, candidateVersion); err != nil {
		return server.Status.DesiredVersion, server.Status.DesiredBuild, err
	}

	// Check plugin compatibility
	if err := r.checkCompatibility(ctx, server, candidateVersion, matchedPlugins); err != nil {
		return server.Status.DesiredVersion, server.Status.DesiredBuild, err
	}

	// Verify image exists in Docker Hub
	tag := fmt.Sprintf("%s-%d", candidateVersion, build)
	exists, err := r.RegistryClient.ImageExists(ctx, "lexfrei/papermc", tag)
	if err != nil {
		return "", 0, errors.Wrapf(err, "failed to verify image existence for %s", tag)
	}

	if !exists {
		return "", 0, errors.Newf("image docker.io/lexfrei/papermc:%s does not exist", tag)
	}

	// Clear any previous block
	r.clearUpdateBlocked(server)

	slog.InfoContext(ctx, "Resolved auto version", "version", candidateVersion, "build", build)
	return candidateVersion, build, nil
}

// resolveVersionOnlyMode finds latest build for a specific version.
func (r *PaperMCServerReconciler) resolveVersionOnlyMode(
	ctx context.Context,
	server *mcv1alpha1.PaperMCServer,
	candidateVersion string,
	matchedPlugins []mcv1alpha1.Plugin,
) (string, int, error) {
	slog.InfoContext(ctx, "Using specific version policy", "version", candidateVersion)

	// Check downgrade
	if err := r.checkDowngrade(server, candidateVersion); err != nil {
		return server.Status.DesiredVersion, server.Status.DesiredBuild, err
	}

	// Check plugin compatibility
	if err := r.checkCompatibility(ctx, server, candidateVersion, matchedPlugins); err != nil {
		return server.Status.DesiredVersion, server.Status.DesiredBuild, err
	}

	// Get all builds for this version
	buildNumbers, err := r.PaperClient.GetBuilds(ctx, candidateVersion)
	if err != nil {
		return "", 0, errors.Wrapf(err, "failed to get builds for version %s", candidateVersion)
	}

	// Builds are in ascending order, iterate from latest to oldest
	for i := len(buildNumbers) - 1; i >= 0; i-- {
		buildNum := buildNumbers[i]
		tag := fmt.Sprintf("%s-%d", candidateVersion, buildNum)

		exists, err := r.RegistryClient.ImageExists(ctx, "lexfrei/papermc", tag)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to check image existence", "error", err, "tag", tag)
			continue
		}

		if exists {
			// Clear any previous block
			r.clearUpdateBlocked(server)

			slog.InfoContext(ctx, "Found existing image for build", "version", candidateVersion, "build", buildNum)
			return candidateVersion, buildNum, nil
		}
	}

	return "", 0, errors.Newf("no Docker images found for any build of version %s", candidateVersion)
}

// resolvePinnedVersionBuild resolves exact version-build combination.
func (r *PaperMCServerReconciler) resolvePinnedVersionBuild(
	ctx context.Context,
	server *mcv1alpha1.PaperMCServer,
	candidateVersion string,
	buildStr string,
	fullSpec string,
	matchedPlugins []mcv1alpha1.Plugin,
) (string, int, error) {
	build, err := strconv.Atoi(buildStr)
	if err != nil {
		return "", 0, errors.Wrapf(err, "failed to parse build number from %s", fullSpec)
	}

	slog.InfoContext(ctx, "Using pinned version-build policy", "version", candidateVersion, "build", build)

	// Check downgrade
	if err := r.checkDowngrade(server, candidateVersion); err != nil {
		return server.Status.DesiredVersion, server.Status.DesiredBuild, err
	}

	// Check plugin compatibility
	if err := r.checkCompatibility(ctx, server, candidateVersion, matchedPlugins); err != nil {
		return server.Status.DesiredVersion, server.Status.DesiredBuild, err
	}

	// Verify image exists
	tag := fmt.Sprintf("%s-%d", candidateVersion, build)
	exists, err := r.RegistryClient.ImageExists(ctx, "lexfrei/papermc", tag)
	if err != nil {
		return "", 0, errors.Wrapf(err, "failed to verify image existence for %s", tag)
	}

	if !exists {
		reason := fmt.Sprintf("Image docker.io/lexfrei/papermc:%s does not exist", tag)
		r.setUpdateBlocked(server, reason, nil)
		return "", 0, errors.Newf("image docker.io/lexfrei/papermc:%s does not exist", tag)
	}

	// Clear any previous block
	r.clearUpdateBlocked(server)

	slog.InfoContext(ctx, "Verified pinned image exists", "version", candidateVersion, "build", build)
	return candidateVersion, build, nil
}

// isStatefulSetReady checks if the StatefulSet is ready.
func (r *PaperMCServerReconciler) isStatefulSetReady(statefulSet *appsv1.StatefulSet) bool {
	if statefulSet == nil {
		return false
	}

	return statefulSet.Status.ReadyReplicas > 0 &&
		statefulSet.Status.ReadyReplicas == statefulSet.Status.Replicas
}

// resolvePluginVersionForServer resolves the best plugin version for this specific server.
// This function was moved from Plugin controller to PaperMCServer controller to enable
// per-server version resolution based on server's Paper version.
//
//nolint:funlen // Complex version resolution logic, hard to simplify further
func (r *PaperMCServerReconciler) resolvePluginVersionForServer(
	ctx context.Context,
	server *mcv1alpha1.PaperMCServer,
	plugin *mcv1alpha1.Plugin,
) (string, error) {
	// Get available versions from plugin status
	if len(plugin.Status.AvailableVersions) == 0 {
		slog.InfoContext(ctx, "Plugin has no available versions yet, will retry",
			"plugin", plugin.Name)

		return "", nil
	}

	// Convert to PluginVersion slice for solver
	allVersions := make([]plugins.PluginVersion, 0, len(plugin.Status.AvailableVersions))
	for _, v := range plugin.Status.AvailableVersions {
		allVersions = append(allVersions, plugins.PluginVersion{
			Version:           v.Version,
			MinecraftVersions: v.MinecraftVersions,
			DownloadURL:       v.DownloadURL,
			Hash:              v.Hash,
			ReleaseDate:       v.ReleasedAt.Time,
		})
	}

	// Handle update strategy
	strategy := plugin.Spec.UpdateStrategy
	if strategy == "" {
		// Default to latest if not specified
		strategy = "latest"
	}

	// Handle pin and build-pin strategies
	if strategy == "pin" || strategy == "build-pin" || strategy == "pinned" {
		pinnedVersion := plugin.Spec.Version
		if pinnedVersion == "" {
			return "", errors.Newf("updateStrategy is '%s' but version is not set", strategy)
		}

		// Verify pinned version exists
		found := false
		for _, v := range allVersions {
			if v.Version == pinnedVersion {
				found = true
				break
			}
		}

		if !found {
			return "", errors.Newf("pinned version %s not found", pinnedVersion)
		}

		slog.InfoContext(ctx, "Using pinned plugin version",
			"plugin", plugin.Name,
			"version", pinnedVersion,
			"strategy", strategy)
		return pinnedVersion, nil
	}

	// Apply update delay filter for latest policy
	filteredVersions := allVersions
	if plugin.Spec.UpdateDelay != nil {
		versionInfos := make([]version.VersionInfo, len(allVersions))
		for i, v := range allVersions {
			versionInfos[i] = version.VersionInfo{
				Version:     v.Version,
				ReleaseDate: v.ReleaseDate,
			}
		}

		filtered := version.FilterByUpdateDelay(versionInfos, plugin.Spec.UpdateDelay.Duration)

		// Convert back
		filteredVersions = make([]plugins.PluginVersion, 0, len(filtered))
		for _, f := range filtered {
			for _, v := range allVersions {
				if v.Version == f.Version {
					filteredVersions = append(filteredVersions, v)
					break
				}
			}
		}

		slog.InfoContext(ctx, "Applied update delay filter",
			"plugin", plugin.Name,
			"original", len(allVersions),
			"filtered", len(filteredVersions))
	}

	if len(filteredVersions) == 0 {
		return "", errors.New("no versions available after applying update delay")
	}

	// Use solver to find best version for THIS SERVER
	// Create a single-server slice for solver
	serverList := []mcv1alpha1.PaperMCServer{*server}

	resolvedVersion, err := r.Solver.FindBestPluginVersion(ctx, plugin, serverList, filteredVersions)
	if err != nil {
		return "", errors.Wrap(err, "solver failed to find compatible version")
	}

	if resolvedVersion == "" {
		return "", errors.Newf("no compatible version found for server %s/%s with Paper %s",
			server.Namespace, server.Name, server.Status.CurrentVersion)
	}

	// Check if the resolved version has metadata (warn if missing)
	for _, v := range filteredVersions {
		if v.Version == resolvedVersion {
			if len(v.MinecraftVersions) == 0 && len(v.PaperVersions) == 0 {
				slog.InfoContext(ctx, "Plugin version has no compatibility metadata, assuming compatible",
					"plugin", plugin.Name,
					"version", resolvedVersion,
					"server", server.Name)
			}
			break
		}
	}

	slog.InfoContext(ctx, "Resolved plugin version for server",
		"plugin", plugin.Name,
		"server", server.Name,
		"paperVersion", server.Status.CurrentVersion,
		"resolvedVersion", resolvedVersion)

	return resolvedVersion, nil
}

// buildPluginStatus constructs plugin status list for the server.
// Version resolution is now done per-server to ensure correct compatibility.
// Fields managed by other controllers (InstalledJARName, CurrentVersion, PendingDeletion)
// are preserved from previous status.
func (r *PaperMCServerReconciler) buildPluginStatus(
	ctx context.Context,
	server *mcv1alpha1.PaperMCServer,
	matchedPlugins []mcv1alpha1.Plugin,
) []mcv1alpha1.ServerPluginStatus {
	// Build lookup from existing status for field preservation
	existing := make(map[string]mcv1alpha1.ServerPluginStatus, len(server.Status.Plugins))
	for _, p := range server.Status.Plugins {
		key := p.PluginRef.Namespace + "/" + p.PluginRef.Name
		existing[key] = p
	}

	status := make([]mcv1alpha1.ServerPluginStatus, 0, len(matchedPlugins))

	for i := range matchedPlugins {
		plugin := &matchedPlugins[i]

		// Resolve version specifically for THIS server
		resolvedVersion, err := r.resolvePluginVersionForServer(ctx, server, plugin)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to resolve plugin version",
				"error", err,
				"plugin", plugin.Name,
				"server", server.Name)
			// Continue with empty resolved version - will be retried on next reconciliation
		}

		pluginStatus := mcv1alpha1.ServerPluginStatus{
			PluginRef: mcv1alpha1.PluginRef{
				Name:      plugin.Name,
				Namespace: plugin.Namespace,
			},
			ResolvedVersion: resolvedVersion,
			Compatible:      resolvedVersion != "",
			Source:          plugin.Spec.Source.Type,
		}

		// Preserve fields managed by other controllers
		key := plugin.Namespace + "/" + plugin.Name
		if prev, ok := existing[key]; ok {
			pluginStatus.InstalledJARName = prev.InstalledJARName
			pluginStatus.CurrentVersion = prev.CurrentVersion
			pluginStatus.PendingDeletion = prev.PendingDeletion
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
	switch server.Spec.UpdateStrategy {
	case updateStrategyLatest, updateStrategyAuto:
		// For latest/auto: find best version through solver
		return r.findVersionUpdate(ctx, server, matchedPlugins)

	case updateStrategyPin:
		// For pin: check for build updates of the pinned version
		return r.findBuildUpdate(ctx, server, matchedPlugins)

	case updateStrategyBuildPin:
		// For build-pin: no updates available
		return nil, nil

	default:
		return nil, errors.Newf("unknown updateStrategy: %s", server.Spec.UpdateStrategy)
	}
}

// findVersionUpdate finds the best Paper version using the solver.
func (r *PaperMCServerReconciler) findVersionUpdate(
	ctx context.Context,
	server *mcv1alpha1.PaperMCServer,
	matchedPlugins []mcv1alpha1.Plugin,
) (*mcv1alpha1.AvailableUpdate, error) {
	// Fetch available Paper versions
	paperVersions, err := r.PaperClient.GetPaperVersions(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch paper versions")
	}

	slog.InfoContext(ctx, "Fetched Paper versions", "count", len(paperVersions))

	// Set solver running condition
	r.setCondition(server, conditionTypeSolverRunning, metav1.ConditionTrue,
		reasonSolverStarted, "Resolving optimal Paper version")

	// Run solver
	bestVersion, err := r.Solver.FindBestPaperVersion(ctx, server, matchedPlugins, paperVersions)
	if err != nil {
		// Mark solver as failed
		r.setCondition(server, conditionTypeSolverRunning, metav1.ConditionFalse,
			reasonSolverFailed, fmt.Sprintf("Solver failed: %v", err))
		return nil, errors.Wrap(err, "solver failed to find best version")
	}

	// Mark solver as completed
	r.setCondition(server, conditionTypeSolverRunning, metav1.ConditionFalse,
		reasonSolverCompleted, "Version resolved successfully")

	// Check if update is needed
	if bestVersion == server.Status.CurrentVersion || bestVersion == "" {
		return nil, nil
	}

	// Get build info for the best version
	buildInfo, err := r.PaperClient.GetPaperBuild(ctx, bestVersion)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get build info")
	}

	// Build plugin version pairs using solver to find compatible versions
	pluginPairs := r.buildPluginVersionPairs(ctx, bestVersion, matchedPlugins)

	return &mcv1alpha1.AvailableUpdate{
		Version:    bestVersion,
		Build:      buildInfo.Build,
		ReleasedAt: metav1.Now(),
		Plugins:    pluginPairs,
		FoundAt:    metav1.Now(),
	}, nil
}

// findBuildUpdate checks for newer builds of the current version.
func (r *PaperMCServerReconciler) findBuildUpdate(
	ctx context.Context,
	server *mcv1alpha1.PaperMCServer,
	matchedPlugins []mcv1alpha1.Plugin,
) (*mcv1alpha1.AvailableUpdate, error) {
	// Get latest build for the specified version
	buildInfo, err := r.PaperClient.GetPaperBuild(ctx, server.Spec.Version)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get latest build")
	}

	slog.InfoContext(ctx, "Found latest build", "version", buildInfo.Version, "build", buildInfo.Build)

	// Check if update is needed
	if buildInfo.Build <= server.Status.CurrentBuild {
		slog.InfoContext(ctx, "Already on latest build", "current", server.Status.CurrentBuild, "latest", buildInfo.Build)
		return nil, nil
	}

	// Check update delay if configured
	if server.Spec.UpdateDelay != nil {
		// TODO: Get actual build release time from API and check delay
		// For now, we assume delay has passed
		slog.InfoContext(ctx, "Update delay check skipped (not implemented yet)")
	}

	// Build plugin version pairs using solver to find compatible versions
	pluginPairs := r.buildPluginVersionPairs(ctx, server.Spec.Version, matchedPlugins)

	slog.InfoContext(ctx, "Build update available", "current", server.Status.CurrentBuild, "available", buildInfo.Build)

	return &mcv1alpha1.AvailableUpdate{
		Version:    server.Spec.Version,
		Build:      buildInfo.Build,
		ReleasedAt: metav1.Now(),
		Plugins:    pluginPairs,
		FoundAt:    metav1.Now(),
	}, nil
}

// buildPluginVersionPairs constructs plugin version pairs from matched plugins.
// It uses the solver to find the best plugin version compatible with the candidate Paper version.
func (r *PaperMCServerReconciler) buildPluginVersionPairs(
	ctx context.Context,
	candidatePaperVersion string,
	matchedPlugins []mcv1alpha1.Plugin,
) []mcv1alpha1.PluginVersionPair {
	pluginPairs := make([]mcv1alpha1.PluginVersionPair, 0, len(matchedPlugins))

	for i := range matchedPlugins {
		plugin := &matchedPlugins[i]

		if len(plugin.Status.AvailableVersions) == 0 {
			continue
		}

		// Convert to plugins.PluginVersion for solver
		allVersions := make([]plugins.PluginVersion, 0, len(plugin.Status.AvailableVersions))
		for _, v := range plugin.Status.AvailableVersions {
			allVersions = append(allVersions, plugins.PluginVersion{
				Version:           v.Version,
				MinecraftVersions: v.MinecraftVersions,
				DownloadURL:       v.DownloadURL,
				Hash:              v.Hash,
				ReleaseDate:       v.ReleasedAt.Time,
			})
		}

		// Create temporary server with candidate version for solver compatibility check
		tempServer := mcv1alpha1.PaperMCServer{
			Status: mcv1alpha1.PaperMCServerStatus{
				CurrentVersion: candidatePaperVersion,
			},
		}

		// Use solver to find best compatible version
		resolvedVersion, err := r.Solver.FindBestPluginVersion(
			ctx, plugin, []mcv1alpha1.PaperMCServer{tempServer}, allVersions)
		if err != nil {
			slog.WarnContext(ctx, "Failed to resolve plugin version for candidate Paper",
				"plugin", plugin.Name,
				"candidatePaperVersion", candidatePaperVersion,
				"error", err)
			// Fall back to first available (best effort)
			resolvedVersion = plugin.Status.AvailableVersions[0].Version
		}

		pluginPairs = append(pluginPairs, mcv1alpha1.PluginVersionPair{
			PluginRef: mcv1alpha1.PluginRef{
				Name:      plugin.Name,
				Namespace: plugin.Namespace,
			},
			Version: resolvedVersion,
		})
	}

	return pluginPairs
}

// checkPluginCompatibility checks if a candidate Paper version is compatible with ALL matched plugins.
// Returns:
//   - compatible: true if compatible with all plugins
//   - blockingPlugin: name of first incompatible plugin (if any)
//   - blockReason: human-readable reason for blocking
func (r *PaperMCServerReconciler) checkPluginCompatibility(
	ctx context.Context,
	candidateVersion string,
	matchedPlugins []mcv1alpha1.Plugin,
) (compatible bool, blockingPlugin string, blockReason string) {
	if len(matchedPlugins) == 0 {
		return true, "", "" // No plugins, no compatibility issues
	}

	for i := range matchedPlugins {
		plugin := &matchedPlugins[i]

		// Plugin without any version metadata and no override — assume compatible per DESIGN.md
		if len(plugin.Status.AvailableVersions) == 0 &&
			(plugin.Spec.CompatibilityOverride == nil || !plugin.Spec.CompatibilityOverride.Enabled) {
			slog.WarnContext(ctx, "Plugin has no version metadata, assuming compatible",
				"plugin", plugin.Name,
				"paperVersion", candidateVersion)

			continue
		}

		// Check compatibility via override or available versions
		if !r.isPluginCompatibleWithPaper(ctx, plugin, candidateVersion) {
			reason := fmt.Sprintf("Plugin '%s' is incompatible with Paper %s",
				plugin.Name,
				candidateVersion)

			return false, plugin.Name, reason
		}
	}

	return true, "", ""
}

// isPluginCompatibleWithPaper checks if any plugin version is compatible with the candidate Paper version.
// Returns true if at least one available plugin version supports the given Paper/Minecraft version.
func (r *PaperMCServerReconciler) isPluginCompatibleWithPaper(
	ctx context.Context,
	plugin *mcv1alpha1.Plugin,
	candidateVersion string,
) bool {
	// Check if plugin has CompatibilityOverride — takes precedence over API metadata
	if plugin.Spec.CompatibilityOverride != nil && plugin.Spec.CompatibilityOverride.Enabled {
		if len(plugin.Spec.CompatibilityOverride.MinecraftVersions) > 0 {
			result := version.ContainsVersion(
				plugin.Spec.CompatibilityOverride.MinecraftVersions, candidateVersion)
			slog.DebugContext(ctx, "Checked plugin compatibilityOverride",
				"plugin", plugin.Name,
				"paperVersion", candidateVersion,
				"overrideVersions", plugin.Spec.CompatibilityOverride.MinecraftVersions,
				"compatible", result)

			return result
		}
		// Override enabled but no versions specified — assume compatible
		slog.DebugContext(ctx, "Plugin has compatibilityOverride enabled with no versions, assuming compatible",
			"plugin", plugin.Name)

		return true
	}

	// Check each available plugin version for compatibility with candidate Paper version
	for _, ver := range plugin.Status.AvailableVersions {
		if version.ContainsVersion(ver.MinecraftVersions, candidateVersion) {
			slog.DebugContext(ctx, "Found compatible plugin version",
				"plugin", plugin.Name,
				"pluginVersion", ver.Version,
				"paperVersion", candidateVersion)

			return true
		}
	}

	// No compatible version found
	slog.InfoContext(ctx, "No compatible plugin version found",
		"plugin", plugin.Name,
		"paperVersion", candidateVersion,
		"availableVersions", len(plugin.Status.AvailableVersions))

	return false
}

// setUpdateBlocked marks the server's updates as blocked.
func (r *PaperMCServerReconciler) setUpdateBlocked(
	server *mcv1alpha1.PaperMCServer,
	reason string,
	blockedBy *mcv1alpha1.BlockedByInfo,
) {
	server.Status.UpdateBlocked = &mcv1alpha1.UpdateBlockedStatus{
		Blocked:   true,
		Reason:    reason,
		BlockedBy: blockedBy,
	}

	r.setCondition(server, conditionTypeUpdateBlocked, metav1.ConditionTrue,
		reasonUpdateBlocked, reason)
}

// clearUpdateBlocked clears the update blocked status unconditionally.
func (r *PaperMCServerReconciler) clearUpdateBlocked(server *mcv1alpha1.PaperMCServer) {
	server.Status.UpdateBlocked = nil

	r.setCondition(server, conditionTypeUpdateBlocked, metav1.ConditionFalse,
		reasonUpdateUnblocked, "No compatibility issues preventing update")
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
	if a.CurrentVersion != b.CurrentVersion {
		return false
	}
	if a.CurrentBuild != b.CurrentBuild {
		return false
	}
	if a.DesiredVersion != b.DesiredVersion {
		return false
	}
	if a.DesiredBuild != b.DesiredBuild {
		return false
	}
	if (a.UpdateBlocked == nil) != (b.UpdateBlocked == nil) {
		return false
	}
	if a.UpdateBlocked != nil && b.UpdateBlocked != nil {
		if a.UpdateBlocked.Blocked != b.UpdateBlocked.Blocked {
			return false
		}
	}
	if len(a.Plugins) != len(b.Plugins) {
		return false
	}
	// Compare plugin status content
	for i := range a.Plugins {
		if a.Plugins[i].ResolvedVersion != b.Plugins[i].ResolvedVersion {
			return false
		}
		if a.Plugins[i].Compatible != b.Plugins[i].Compatible {
			return false
		}
	}

	// Compare AvailableUpdate
	if !availableUpdateEqual(a.AvailableUpdate, b.AvailableUpdate) {
		return false
	}

	// Compare LastUpdate
	if !updateHistoryEqual(a.LastUpdate, b.LastUpdate) {
		return false
	}

	// Compare Conditions
	if len(a.Conditions) != len(b.Conditions) {
		return false
	}

	for i := range a.Conditions {
		if a.Conditions[i].Type != b.Conditions[i].Type ||
			a.Conditions[i].Status != b.Conditions[i].Status ||
			a.Conditions[i].Reason != b.Conditions[i].Reason ||
			a.Conditions[i].Message != b.Conditions[i].Message {
			return false
		}
	}

	return true
}

// availableUpdateEqual compares two AvailableUpdate pointers for equality.
func availableUpdateEqual(a, b *mcv1alpha1.AvailableUpdate) bool {
	if (a == nil) != (b == nil) {
		return false
	}
	if a == nil {
		return true
	}
	return a.Version == b.Version && a.Build == b.Build
}

// updateHistoryEqual compares two UpdateHistory pointers for equality.
func updateHistoryEqual(a, b *mcv1alpha1.UpdateHistory) bool {
	if (a == nil) != (b == nil) {
		return false
	}
	if a == nil {
		return true
	}
	return a.Successful == b.Successful && a.PreviousVersion == b.PreviousVersion
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

	// Find all servers in the same namespace that match this plugin's selector
	matchedServers, err := selector.FindMatchingServers(
		ctx,
		r.Client,
		plugin.Namespace,
		plugin.Spec.InstanceSelector,
	)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to find servers for plugin watch", "error", err)
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

	slog.InfoContext(ctx, "Plugin change triggered server reconciliations",
		"plugin", plugin.Name,
		"servers", len(requests))

	return requests
}
