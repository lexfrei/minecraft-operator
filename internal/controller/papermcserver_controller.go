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
	"regexp"
	"strconv"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/go-logr/logr"
	mcv1alpha1 "github.com/lexfrei/minecraft-operator/api/v1alpha1"
	"github.com/lexfrei/minecraft-operator/pkg/paper"
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
	reasonServerReconcileSuccess  = "ReconcileSuccess"
	reasonServerReconcileError    = "ReconcileError"
	reasonStatefulSetCreated      = "StatefulSetCreated"
	reasonStatefulSetNotReady     = "StatefulSetNotReady"
	reasonStatefulSetReady        = "StatefulSetReady"
	reasonUpdateFound             = "UpdateFound"
	reasonNoUpdate                = "NoUpdate"
	reasonUpdateBlocked           = "UpdateBlocked"
	reasonUpdateUnblocked         = "UpdateUnblocked"
	reasonDowngradeNotAllowed     = "DowngradeNotAllowed"
	reasonPluginIncompatible      = "PluginIncompatible"
	defaultPaperImage             = "lexfrei/papermc:latest"
	defaultStorageSize            = "10Gi"
	defaultTerminationGracePeriod = int64(300)
	finalizerName                 = "mc.k8s.lex.la/papermcserver-finalizer"
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

	// Step 2: Resolve and update desired version
	if err := r.updateDesiredVersion(ctx, log, server, matchedPlugins); err != nil {
		return ctrl.Result{}, err
	}

	// Step 3: Check if updates are blocked
	if server.Status.UpdateBlocked != nil && server.Status.UpdateBlocked.Blocked {
		log.Info("Update blocked, skipping infrastructure update",
			"reason", server.Status.UpdateBlocked.Reason)
		// Don't proceed with StatefulSet update, but continue to update status
	}

	// Step 4: Ensure infrastructure (StatefulSet and Service)
	statefulSet, err := r.ensureInfrastructure(ctx, server)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Step 5: Update server status
	r.updateServerStatus(ctx, log, server, statefulSet, matchedPlugins)

	return ctrl.Result{RequeueAfter: 15 * time.Minute}, nil
}

// updateDesiredVersion resolves and updates the desired Paper version in server status.
func (r *PaperMCServerReconciler) updateDesiredVersion(
	ctx context.Context,
	log logr.Logger,
	server *mcv1alpha1.PaperMCServer,
	matchedPlugins []mcv1alpha1.Plugin,
) error {
	desiredVersion, desiredBuild, err := r.resolveDesiredPaperVersion(ctx, log, server, matchedPlugins)
	if err != nil {
		log.Error(err, "Failed to resolve desired Paper version")
		// Don't fail reconciliation completely - keep existing desired if set
		if server.Status.DesiredPaperVersion != "" {
			desiredVersion = server.Status.DesiredPaperVersion
			desiredBuild = server.Status.DesiredPaperBuild
		} else {
			// No fallback, cannot proceed
			return errors.Wrap(err, "cannot resolve desired version and no fallback available")
		}
	}

	// Store desired in status
	server.Status.DesiredPaperVersion = desiredVersion
	server.Status.DesiredPaperBuild = desiredBuild

	log.Info("Resolved desired Paper version", "version", desiredVersion, "build", desiredBuild)
	return nil
}

// ensureInfrastructure ensures StatefulSet and Service exist.
func (r *PaperMCServerReconciler) ensureInfrastructure(
	ctx context.Context,
	server *mcv1alpha1.PaperMCServer,
) (*appsv1.StatefulSet, error) {
	statefulSet, err := r.ensureStatefulSet(ctx, server)
	if err != nil {
		return nil, errors.Wrap(err, "failed to ensure statefulset")
	}

	if err := r.ensureService(ctx, server); err != nil {
		return nil, errors.Wrap(err, "failed to ensure service")
	}

	return statefulSet, nil
}

// updateServerStatus updates all status fields for the server.
func (r *PaperMCServerReconciler) updateServerStatus(
	ctx context.Context,
	log logr.Logger,
	server *mcv1alpha1.PaperMCServer,
	statefulSet *appsv1.StatefulSet,
	matchedPlugins []mcv1alpha1.Plugin,
) {
	// Detect current Paper version and build from StatefulSet
	currentVersion, currentBuild := r.detectCurrentPaperVersion(statefulSet)
	server.Status.CurrentPaperVersion = currentVersion
	server.Status.CurrentPaperBuild = currentBuild

	log.Info("Detected Paper version", "version", currentVersion, "build", currentBuild)

	// Update plugin status
	server.Status.Plugins = r.buildPluginStatus(matchedPlugins)

	// Update available update status
	r.updateAvailableUpdateStatus(ctx, log, server, matchedPlugins)

	// Update StatefulSet readiness condition
	r.updateStatefulSetReadiness(server, statefulSet)
}

// updateAvailableUpdateStatus checks for and updates available update information.
func (r *PaperMCServerReconciler) updateAvailableUpdateStatus(
	ctx context.Context,
	log logr.Logger,
	server *mcv1alpha1.PaperMCServer,
	matchedPlugins []mcv1alpha1.Plugin,
) {
	availableUpdate, err := r.findAvailableUpdate(ctx, server, matchedPlugins)
	if err != nil {
		log.Error(err, "Failed to find available update, continuing without update")
		server.Status.AvailableUpdate = nil
		r.setCondition(server, conditionTypeUpdateAvailable, metav1.ConditionFalse,
			reasonNoUpdate, err.Error())
		return
	}

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

	// Construct image based on desired version from status
	var image string
	if server.Status.DesiredPaperVersion == versionPolicyLatest || server.Status.DesiredPaperVersion == "" {
		image = "docker.io/lexfrei/papermc:latest"
	} else {
		image = fmt.Sprintf("docker.io/lexfrei/papermc:%s-%d",
			server.Status.DesiredPaperVersion,
			server.Status.DesiredPaperBuild)
	}

	container.Image = image

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
	if paperVersion != "" && paperVersion != versionPolicyLatest {
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

// detectCurrentPaperVersion parses version from StatefulSet container image tag.
// Supported formats:
//   - docker.io/lexfrei/papermc:1.21.10-91 -> version="1.21.10", build=91
//   - docker.io/lexfrei/papermc:latest -> version="latest", build=0
//   - lexfrei/papermc:1.21.10-91 -> version="1.21.10", build=91
func (r *PaperMCServerReconciler) detectCurrentPaperVersion(statefulSet *appsv1.StatefulSet) (string, int) {
	if statefulSet == nil || len(statefulSet.Spec.Template.Spec.Containers) == 0 {
		return "", 0
	}

	image := statefulSet.Spec.Template.Spec.Containers[0].Image

	// Parse image tag from formats:
	// - docker.io/lexfrei/papermc:1.21.10-91
	// - lexfrei/papermc:latest
	// Pattern: (.*/)?(lexfrei/papermc):(.+)
	imageRegex := regexp.MustCompile(`(?:.*/)?(lexfrei/papermc):(.+)`)
	matches := imageRegex.FindStringSubmatch(image)

	if len(matches) < 3 {
		ctrl.Log.WithName("detectCurrentPaperVersion").
			Error(errors.New("image format mismatch"), "Failed to parse image",
				"image", image)
		return "", 0
	}

	tag := matches[2]

	// Handle "latest" tag specially
	if tag == versionPolicyLatest {
		return versionPolicyLatest, 0
	}

	// Parse version-build format: 1.21.10-91
	tagRegex := regexp.MustCompile(`^([0-9.]+)-([0-9]+)$`)
	tagMatches := tagRegex.FindStringSubmatch(tag)

	if len(tagMatches) < 3 {
		ctrl.Log.WithName("detectCurrentPaperVersion").
			Error(errors.New("tag format mismatch"), "Failed to parse tag",
				"tag", tag, "image", image)
		return "", 0
	}

	paperVersion := tagMatches[1]
	build, err := strconv.Atoi(tagMatches[2])
	if err != nil {
		ctrl.Log.WithName("detectCurrentPaperVersion").
			Error(err, "Failed to parse build number", "buildStr", tagMatches[2])
		return paperVersion, 0
	}

	return paperVersion, build
}

// resolveDesiredPaperVersion determines the target Paper version based on spec.updateStrategy.
// Returns version string, build number, and error if resolution fails.
// CRITICAL: Checks downgrade and plugin compatibility BEFORE setting desired version.
func (r *PaperMCServerReconciler) resolveDesiredPaperVersion(
	ctx context.Context,
	log logr.Logger,
	server *mcv1alpha1.PaperMCServer,
	matchedPlugins []mcv1alpha1.Plugin,
) (paperVersion string, build int, err error) {
	switch server.Spec.UpdateStrategy {
	case updateStrategyLatest:
		return r.resolveLatestVersion(ctx, log, server, matchedPlugins)

	case updateStrategyAuto:
		return r.resolveAutoVersion(ctx, log, server, matchedPlugins)

	case updateStrategyPin:
		if server.Spec.PaperVersion == "" {
			return "", 0, errors.New("paperVersion is required for 'pin' strategy")
		}
		return r.resolveVersionOnlyMode(ctx, log, server, server.Spec.PaperVersion, matchedPlugins)

	case updateStrategyBuildPin:
		if server.Spec.PaperVersion == "" {
			return "", 0, errors.New("paperVersion is required for 'build-pin' strategy")
		}
		if server.Spec.PaperBuild == nil {
			return "", 0, errors.New("paperBuild is required for 'build-pin' strategy")
		}
		buildStr := fmt.Sprintf("%d", *server.Spec.PaperBuild)
		return r.resolvePinnedVersionBuild(ctx, log, server, server.Spec.PaperVersion, buildStr,
			fmt.Sprintf("%s-%s", server.Spec.PaperVersion, buildStr), matchedPlugins)

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
	if server.Status.CurrentPaperVersion == "" {
		return nil // First deployment, no downgrade possible
	}

	// Allow any version when current is "latest" (unresolved)
	if server.Status.CurrentPaperVersion == updateStrategyLatest {
		return nil
	}

	isDowngrade, err := version.IsDowngrade(server.Status.CurrentPaperVersion, candidateVersion)
	if err != nil {
		return errors.Wrap(err, "failed to compare versions")
	}

	if isDowngrade {
		reason := fmt.Sprintf("Downgrade not allowed: current=%s, candidate=%s",
			server.Status.CurrentPaperVersion, candidateVersion)
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
				blockedBy = &mcv1alpha1.BlockedByInfo{
					Plugin:  blockingPlugin,
					Version: plugin.Status.ResolvedVersion,
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
	log logr.Logger,
	server *mcv1alpha1.PaperMCServer,
	matchedPlugins []mcv1alpha1.Plugin,
) (string, int, error) {
	log.Info("Using latest Paper version policy - resolving from Docker Hub tags")

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

	log.Info("Resolved latest from Docker Hub", "version", candidateVersion, "build", candidateBuild)

	// Check downgrade
	if err := r.checkDowngrade(server, candidateVersion); err != nil {
		return server.Status.DesiredPaperVersion, server.Status.DesiredPaperBuild, err
	}

	// Check plugin compatibility
	if err := r.checkCompatibility(ctx, server, candidateVersion, matchedPlugins); err != nil {
		return server.Status.DesiredPaperVersion, server.Status.DesiredPaperBuild, err
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
	log logr.Logger,
	server *mcv1alpha1.PaperMCServer,
	matchedPlugins []mcv1alpha1.Plugin,
) (string, int, error) {
	log.Info("Using auto Paper version policy")

	availableUpdate, err := r.findAvailableUpdate(ctx, server, matchedPlugins)
	if err != nil {
		return "", 0, errors.Wrap(err, "failed to find available update in auto mode")
	}

	if availableUpdate == nil {
		return "", 0, errors.New("no available update found in auto mode")
	}

	candidateVersion := availableUpdate.PaperVersion
	build := availableUpdate.PaperBuild

	// Check downgrade
	if err := r.checkDowngrade(server, candidateVersion); err != nil {
		return server.Status.DesiredPaperVersion, server.Status.DesiredPaperBuild, err
	}

	// Check plugin compatibility
	if err := r.checkCompatibility(ctx, server, candidateVersion, matchedPlugins); err != nil {
		return server.Status.DesiredPaperVersion, server.Status.DesiredPaperBuild, err
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

	log.Info("Resolved auto version", "version", candidateVersion, "build", build)
	return candidateVersion, build, nil
}

// resolveVersionOnlyMode finds latest build for a specific version.
func (r *PaperMCServerReconciler) resolveVersionOnlyMode(
	ctx context.Context,
	log logr.Logger,
	server *mcv1alpha1.PaperMCServer,
	candidateVersion string,
	matchedPlugins []mcv1alpha1.Plugin,
) (string, int, error) {
	log.Info("Using specific version policy", "version", candidateVersion)

	// Check downgrade
	if err := r.checkDowngrade(server, candidateVersion); err != nil {
		return server.Status.DesiredPaperVersion, server.Status.DesiredPaperBuild, err
	}

	// Check plugin compatibility
	if err := r.checkCompatibility(ctx, server, candidateVersion, matchedPlugins); err != nil {
		return server.Status.DesiredPaperVersion, server.Status.DesiredPaperBuild, err
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
			log.Error(err, "Failed to check image existence", "tag", tag)
			continue
		}

		if exists {
			// Clear any previous block
			r.clearUpdateBlocked(server)

			log.Info("Found existing image for build", "version", candidateVersion, "build", buildNum)
			return candidateVersion, buildNum, nil
		}
	}

	return "", 0, errors.Newf("no Docker images found for any build of version %s", candidateVersion)
}

// resolvePinnedVersionBuild resolves exact version-build combination.
func (r *PaperMCServerReconciler) resolvePinnedVersionBuild(
	ctx context.Context,
	log logr.Logger,
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

	log.Info("Using pinned version-build policy", "version", candidateVersion, "build", build)

	// Check downgrade
	if err := r.checkDowngrade(server, candidateVersion); err != nil {
		return server.Status.DesiredPaperVersion, server.Status.DesiredPaperBuild, err
	}

	// Check plugin compatibility
	if err := r.checkCompatibility(ctx, server, candidateVersion, matchedPlugins); err != nil {
		return server.Status.DesiredPaperVersion, server.Status.DesiredPaperBuild, err
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

	log.Info("Verified pinned image exists", "version", candidateVersion, "build", build)
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

		// Check plugin.Status.ResolvedVersion compatibility with candidateVersion
		isCompatible := r.isPluginCompatibleWithPaper(ctx, plugin)
		if !isCompatible {
			reason := fmt.Sprintf("Plugin '%s' (version %s) is incompatible with Paper %s",
				plugin.Name,
				plugin.Status.ResolvedVersion,
				candidateVersion)
			return false, plugin.Name, reason
		}
	}

	return true, "", ""
}

// isPluginCompatibleWithPaper checks if a specific plugin version is compatible with a Paper version.
func (r *PaperMCServerReconciler) isPluginCompatibleWithPaper(
	ctx context.Context,
	plugin *mcv1alpha1.Plugin,
) bool {
	// Check if plugin has CompatibilityOverride
	if plugin.Spec.CompatibilityOverride != nil {
		// TODO: Use override when implemented in Plugin CRD
		// For now, log and continue to other checks
		ctrl.LoggerFrom(ctx).Info("Plugin has compatibilityOverride, but checking is not yet implemented",
			"plugin", plugin.Name)
	}

	// Check plugin.Status.AvailableVersions for compatibility metadata
	// This would come from Hangar/Modrinth API
	// For MVP: Use permissive approach if no compatibility info available
	// TODO: Implement actual compatibility check using plugin metadata from status

	// Fallback: assume compatible (permissive mode for MVP)
	// In production, this should use plugin API metadata
	return true
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

// clearUpdateBlocked clears the update blocked status.
func (r *PaperMCServerReconciler) clearUpdateBlocked(server *mcv1alpha1.PaperMCServer) {
	if server.Status.UpdateBlocked != nil && server.Status.UpdateBlocked.Blocked {
		server.Status.UpdateBlocked = &mcv1alpha1.UpdateBlockedStatus{
			Blocked: false,
		}

		r.setCondition(server, conditionTypeUpdateBlocked, metav1.ConditionFalse,
			reasonUpdateUnblocked, "No compatibility issues preventing update")
	}
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
	if a.DesiredPaperVersion != b.DesiredPaperVersion {
		return false
	}
	if a.DesiredPaperBuild != b.DesiredPaperBuild {
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
