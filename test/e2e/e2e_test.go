//go:build e2e
// +build e2e

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

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/lexfrei/minecraft-operator/test/utils"
)

// namespace where the project is deployed in
const namespace = "minecraft-operator-system"

// kindClusterContext returns the kubectl context for the Kind cluster.
// Kind contexts are named "kind-{cluster-name}".
func kindClusterContext() string {
	cluster := "kind"
	if v, ok := os.LookupEnv("KIND_CLUSTER"); ok {
		cluster = v
	}

	return "kind-" + cluster
}

// kubectlCmd creates a kubectl command with the Kind cluster context.
// All kubectl commands in e2e tests must specify --context to prevent
// accidentally targeting the wrong cluster.
func kubectlCmd(args ...string) *exec.Cmd {
	fullArgs := append([]string{"--context", kindClusterContext()}, args...)

	return exec.Command("kubectl", fullArgs...)
}

// serviceAccountName created for the project
const serviceAccountName = "minecraft-operator-controller-manager"

// metricsServiceName is the name of the metrics service of the project
const metricsServiceName = "minecraft-operator-controller-manager-metrics-service"

// metricsRoleBindingName is the name of the RBAC that will be created to allow get the metrics data
const metricsRoleBindingName = "minecraft-operator-metrics-binding"

var _ = Describe("Manager", Ordered, func() {
	var controllerPodName string

	// Before running the tests, set up the environment by creating the namespace,
	// enforce the restricted security policy to the namespace,
	// and deploying the controller (CRDs are applied automatically by the operator at startup).
	BeforeAll(func() {
		By("creating manager namespace")
		cmd := kubectlCmd("create", "ns", namespace)
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create namespace")

		By("labeling the namespace to enforce the restricted security policy")
		cmd = kubectlCmd("label", "--overwrite", "ns", namespace,
			"pod-security.kubernetes.io/enforce=restricted")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to label namespace with restricted policy")

		By("deploying the controller-manager (CRDs are applied automatically at startup)")
		imageRepo, imageTag := parseImage(projectImage)
		cmd = exec.Command("helm", "install", "minecraft-operator",
			"./charts/minecraft-operator",
			"--namespace", namespace,
			"--set", fmt.Sprintf("image.repository=%s", imageRepo),
			"--set", fmt.Sprintf("image.tag=%s", imageTag))
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to deploy the controller-manager")
	})

	// After all tests have been executed, clean up by undeploying the controller
	// and deleting the namespace.
	AfterAll(func() {
		By("cleaning up the curl pod for metrics")
		cmd := kubectlCmd("delete", "pod", "curl-metrics", "--namespace", namespace)
		_, _ = utils.Run(cmd)

		By("undeploying the controller-manager")
		cmd = exec.Command("helm", "uninstall", "minecraft-operator",
			"--namespace", namespace)
		_, _ = utils.Run(cmd)

		By("removing manager namespace")
		cmd = kubectlCmd("delete", "ns", namespace)
		_, _ = utils.Run(cmd)
	})

	// After each test, check for failures and collect logs, events,
	// and pod descriptions for debugging.
	AfterEach(func() {
		specReport := CurrentSpecReport()
		if specReport.Failed() {
			By("Fetching controller manager pod logs")
			cmd := kubectlCmd("logs", controllerPodName, "--namespace", namespace)
			controllerLogs, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Controller logs:\n %s", controllerLogs)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get Controller logs: %s", err)
			}

			By("Fetching Kubernetes events")
			cmd = kubectlCmd("get", "events", "--namespace", namespace, "--sort-by=.lastTimestamp")
			eventsOutput, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Kubernetes events:\n%s", eventsOutput)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get Kubernetes events: %s", err)
			}

			By("Fetching curl-metrics logs")
			cmd = kubectlCmd("logs", "curl-metrics", "--namespace", namespace)
			metricsOutput, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Metrics logs:\n %s", metricsOutput)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get curl-metrics logs: %s", err)
			}

			By("Fetching controller manager pod description")
			cmd = kubectlCmd("describe", "pod", controllerPodName, "--namespace", namespace)
			podDescription, err := utils.Run(cmd)
			if err == nil {
				fmt.Println("Pod description:\n", podDescription)
			} else {
				fmt.Println("Failed to describe controller pod")
			}
		}
	})

	SetDefaultEventuallyTimeout(2 * time.Minute)
	SetDefaultEventuallyPollingInterval(time.Second)

	Context("Manager", func() {
		It("should run successfully", func() {
			By("validating that the controller-manager pod is running as expected")
			verifyControllerUp := func(g Gomega) {
				// Get the name of the controller-manager pod
				cmd := kubectlCmd("get",
					"pods", "--selector", "control-plane=controller-manager",
					"--output", "go-template={{ range .items }}"+
						"{{ if not .metadata.deletionTimestamp }}"+
						"{{ .metadata.name }}"+
						"{{ \"\\n\" }}{{ end }}{{ end }}",
					"--namespace", namespace,
				)

				podOutput, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred(), "Failed to retrieve controller-manager pod information")
				podNames := utils.GetNonEmptyLines(podOutput)
				g.Expect(podNames).To(HaveLen(1), "expected 1 controller pod running")
				controllerPodName = podNames[0]
				g.Expect(controllerPodName).To(ContainSubstring("controller-manager"))

				// Validate the pod's status
				cmd = kubectlCmd("get",
					"pods", controllerPodName, "--output", "jsonpath={.status.phase}",
					"--namespace", namespace,
				)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Running"), "Incorrect controller-manager pod status")
			}
			Eventually(verifyControllerUp).Should(Succeed())
		})

		It("should ensure the metrics endpoint is serving metrics", func() {
			By("creating a ClusterRoleBinding for the service account to allow access to metrics")
			cmd := kubectlCmd("create", "clusterrolebinding", metricsRoleBindingName,
				"--clusterrole=minecraft-operator-metrics-reader",
				fmt.Sprintf("--serviceaccount=%s:%s", namespace, serviceAccountName),
			)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create ClusterRoleBinding")

			By("validating that the metrics service is available")
			cmd = kubectlCmd("get", "service", metricsServiceName, "--namespace", namespace)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Metrics service should exist")

			By("getting the service account token")
			token, err := serviceAccountToken()
			Expect(err).NotTo(HaveOccurred())
			Expect(token).NotTo(BeEmpty())

			By("waiting for the metrics endpoint to be ready")
			verifyMetricsEndpointReady := func(g Gomega) {
				cmd := kubectlCmd("get", "endpoints", metricsServiceName, "--namespace", namespace)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("8443"), "Metrics endpoint is not ready")
			}
			Eventually(verifyMetricsEndpointReady).Should(Succeed())

			By("verifying that the controller manager is serving the metrics server")
			verifyMetricsServerStarted := func(g Gomega) {
				cmd := kubectlCmd("logs", controllerPodName, "--namespace", namespace)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("Serving metrics server"),
					"Metrics server not yet started")
			}
			Eventually(verifyMetricsServerStarted).Should(Succeed())

			By("creating the curl-metrics pod to access the metrics endpoint")
			cmd = kubectlCmd("run", "curl-metrics", "--restart=Never",
				"--namespace", namespace,
				"--image=curlimages/curl:latest",
				"--overrides",
				fmt.Sprintf(`{
					"spec": {
						"containers": [{
							"name": "curl",
							"image": "curlimages/curl:latest",
							"command": ["/bin/sh", "-c"],
							"args": ["curl -v -k -H 'Authorization: Bearer %s' https://%s.%s.svc.cluster.local:8443/metrics"],
							"securityContext": {
								"readOnlyRootFilesystem": true,
								"allowPrivilegeEscalation": false,
								"capabilities": {
									"drop": ["ALL"]
								},
								"runAsNonRoot": true,
								"runAsUser": 1000,
								"seccompProfile": {
									"type": "RuntimeDefault"
								}
							}
						}],
						"serviceAccountName": "%s"
					}
				}`, token, metricsServiceName, namespace, serviceAccountName))
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create curl-metrics pod")

			By("waiting for the curl-metrics pod to complete.")
			verifyCurlUp := func(g Gomega) {
				cmd := kubectlCmd("get", "pods", "curl-metrics",
					"--output", "jsonpath={.status.phase}",
					"--namespace", namespace)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Succeeded"), "curl pod in wrong status")
			}
			Eventually(verifyCurlUp, 5*time.Minute).Should(Succeed())

			By("getting the metrics by checking curl-metrics logs")
			verifyMetricsAvailable := func(g Gomega) {
				metricsOutput, err := getMetricsOutput()
				g.Expect(err).NotTo(HaveOccurred(), "Failed to retrieve logs from curl pod")
				g.Expect(metricsOutput).NotTo(BeEmpty())
				g.Expect(metricsOutput).To(ContainSubstring("< HTTP/1.1 200 OK"))
			}
			Eventually(verifyMetricsAvailable, 2*time.Minute).Should(Succeed())
		})

		// +kubebuilder:scaffold:e2e-webhooks-checks
	})

	Context("PaperMCServer lifecycle", func() {
		const serverName = "e2e-test-server"

		AfterEach(func() {
			// Clean up CR and associated resources
			cmd := kubectlCmd("delete", "papermcserver", serverName,
				"--namespace", namespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)

			cmd = kubectlCmd("delete", "secret", serverName+"-rcon",
				"--namespace", namespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)

			// Wait for cleanup to complete
			Eventually(func(g Gomega) {
				cmd := kubectlCmd("get", "statefulset", serverName,
					"--namespace", namespace)
				_, err := utils.Run(cmd)
				g.Expect(err).To(HaveOccurred(), "StatefulSet should be deleted")
			}, 60*time.Second).Should(Succeed())
		})

		It("should create StatefulSet and Service when PaperMCServer is created", func() {
			By("creating RCON secret")
			cmd := kubectlCmd("create", "secret", "generic",
				serverName+"-rcon",
				"--from-literal=password=test-pass",
				"--namespace", namespace)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("creating a PaperMCServer CR")
			serverYAML := fmt.Sprintf(`apiVersion: mc.k8s.lex.la/v1beta1
kind: PaperMCServer
metadata:
  name: %s
  namespace: %s
spec:
  updateStrategy: "latest"
  updateSchedule:
    checkCron: "0 3 * * *"
    maintenanceWindow:
      enabled: true
      cron: "0 4 * * 0"
  gracefulShutdown:
    timeout: 60s
  rcon:
    enabled: true
    port: 25575
    passwordSecret:
      name: %s-rcon
      key: password
  podTemplate:
    spec:
      containers:
      - name: minecraft
        image: docker.io/lexfrei/papermc:1.21.1-91
        resources:
          requests:
            memory: "512Mi"
            cpu: "250m"
          limits:
            memory: "1Gi"
            cpu: "500m"
        env:
        - name: EULA
          value: "TRUE"`, serverName, namespace, serverName)

			cmd = kubectlCmd("apply", "--filename", "-")
			cmd.Stdin = strings.NewReader(serverYAML)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("verifying StatefulSet is created")
			Eventually(func(g Gomega) {
				cmd := kubectlCmd("get", "statefulset", serverName,
					"--namespace", namespace, "--output", "jsonpath={.metadata.name}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal(serverName))
			}, 60*time.Second).Should(Succeed())

			By("verifying Service is created")
			Eventually(func(g Gomega) {
				cmd := kubectlCmd("get", "service", serverName,
					"--namespace", namespace, "--output", "jsonpath={.metadata.name}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal(serverName))
			}, 30*time.Second).Should(Succeed())

			By("verifying StatefulSet has correct image (not :latest)")
			cmd = kubectlCmd("get", "statefulset", serverName,
				"--namespace", namespace,
				"--output", "jsonpath={.spec.template.spec.containers[0].image}")
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).NotTo(ContainSubstring(":latest"),
				"StatefulSet image must never use :latest tag")
			Expect(output).To(ContainSubstring("lexfrei/papermc:"),
				"StatefulSet image should reference papermc")
		})

		It("should clean up StatefulSet and Service when PaperMCServer is deleted", func() {
			By("creating RCON secret")
			cmd := kubectlCmd("create", "secret", "generic",
				serverName+"-rcon",
				"--from-literal=password=test-pass",
				"--namespace", namespace)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("creating a PaperMCServer CR")
			serverYAML := fmt.Sprintf(`apiVersion: mc.k8s.lex.la/v1beta1
kind: PaperMCServer
metadata:
  name: %s
  namespace: %s
spec:
  updateStrategy: "latest"
  updateSchedule:
    checkCron: "0 3 * * *"
    maintenanceWindow:
      enabled: true
      cron: "0 4 * * 0"
  gracefulShutdown:
    timeout: 60s
  rcon:
    enabled: true
    port: 25575
    passwordSecret:
      name: %s-rcon
      key: password
  podTemplate:
    spec:
      containers:
      - name: minecraft
        image: docker.io/lexfrei/papermc:1.21.1-91
        resources:
          requests:
            memory: "512Mi"
            cpu: "250m"
        env:
        - name: EULA
          value: "TRUE"`, serverName, namespace, serverName)

			cmd = kubectlCmd("apply", "--filename", "-")
			cmd.Stdin = strings.NewReader(serverYAML)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("waiting for StatefulSet to appear")
			Eventually(func(g Gomega) {
				cmd := kubectlCmd("get", "statefulset", serverName,
					"--namespace", namespace)
				_, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
			}, 60*time.Second).Should(Succeed())

			By("deleting the PaperMCServer CR")
			cmd = kubectlCmd("delete", "papermcserver", serverName,
				"--namespace", namespace)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("verifying StatefulSet is removed via owner reference cascade")
			Eventually(func(g Gomega) {
				cmd := kubectlCmd("get", "statefulset", serverName,
					"--namespace", namespace)
				_, err := utils.Run(cmd)
				g.Expect(err).To(HaveOccurred(), "StatefulSet should be deleted")
			}, 60*time.Second).Should(Succeed())

			By("verifying Service is removed")
			Eventually(func(g Gomega) {
				cmd := kubectlCmd("get", "service", serverName,
					"--namespace", namespace)
				_, err := utils.Run(cmd)
				g.Expect(err).To(HaveOccurred(), "Service should be deleted")
			}, 30*time.Second).Should(Succeed())
		})
	})

	Context("Operator handles misconfiguration gracefully", func() {
		const badCronServer = "e2e-bad-cron"

		AfterEach(func() {
			cmd := kubectlCmd("delete", "papermcserver", badCronServer,
				"--namespace", namespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)

			cmd = kubectlCmd("delete", "secret", badCronServer+"-rcon",
				"--namespace", namespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)
		})

		It("should set CronScheduleValid=False for invalid cron and continue operating", func() {
			By("creating RCON secret")
			cmd := kubectlCmd("create", "secret", "generic",
				badCronServer+"-rcon",
				"--from-literal=password=test-pass",
				"--namespace", namespace)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("creating PaperMCServer with invalid cron expression")
			serverYAML := fmt.Sprintf(`apiVersion: mc.k8s.lex.la/v1beta1
kind: PaperMCServer
metadata:
  name: %s
  namespace: %s
spec:
  updateStrategy: "latest"
  updateSchedule:
    checkCron: "not-a-valid-cron"
    maintenanceWindow:
      enabled: true
      cron: "0 4 * * 0"
  gracefulShutdown:
    timeout: 60s
  rcon:
    enabled: true
    port: 25575
    passwordSecret:
      name: %s-rcon
      key: password
  podTemplate:
    spec:
      containers:
      - name: minecraft
        image: docker.io/lexfrei/papermc:1.21.1-91
        resources:
          requests:
            memory: "512Mi"
            cpu: "250m"
        env:
        - name: EULA
          value: "TRUE"`, badCronServer, namespace, badCronServer)

			cmd = kubectlCmd("apply", "--filename", "-")
			cmd.Stdin = strings.NewReader(serverYAML)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("verifying CronScheduleValid condition is set to False")
			Eventually(func(g Gomega) {
				cmd := kubectlCmd("get", "papermcserver", badCronServer,
					"--namespace", namespace,
					"--output", "jsonpath={.status.conditions[?(@.type=='CronScheduleValid')].status}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("False"),
					"CronScheduleValid should be False for invalid cron")
			}, 60*time.Second).Should(Succeed())

			By("verifying operator pod is still running (not crash-looping)")
			cmd = kubectlCmd("get", "pods", "--selector", "control-plane=controller-manager",
				"--namespace", namespace, "--output", "jsonpath={.items[0].status.phase}")
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(Equal("Running"),
				"Operator should continue running despite invalid cron")
		})
	})

	Context("Backup lifecycle", func() {
		const backupServer = "e2e-backup-server"

		AfterEach(func() {
			// Clean up VolumeSnapshots
			cmd := kubectlCmd("delete", "volumesnapshot",
				"--selector=mc.k8s.lex.la/server-name="+backupServer,
				"--namespace", namespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)

			// Clean up CR and associated resources
			cmd = kubectlCmd("delete", "papermcserver", backupServer,
				"--namespace", namespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)

			cmd = kubectlCmd("delete", "secret", backupServer+"-rcon",
				"--namespace", namespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)

			// Wait for cleanup
			Eventually(func(g Gomega) {
				cmd := kubectlCmd("get", "statefulset", backupServer,
					"--namespace", namespace)
				_, err := utils.Run(cmd)
				g.Expect(err).To(HaveOccurred(), "StatefulSet should be deleted")
			}, 60*time.Second).Should(Succeed())
		})

		It("should create VolumeSnapshot when backup-now annotation is set", func() {
			By("creating RCON secret")
			cmd := kubectlCmd("create", "secret", "generic",
				backupServer+"-rcon",
				"--from-literal=password=test-pass",
				"--namespace", namespace)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("creating PaperMCServer with backup enabled (no backup-now annotation yet)")
			serverYAML := fmt.Sprintf(`apiVersion: mc.k8s.lex.la/v1beta1
kind: PaperMCServer
metadata:
  name: %s
  namespace: %s
spec:
  updateStrategy: "latest"
  updateSchedule:
    checkCron: "0 3 * * *"
    maintenanceWindow:
      enabled: true
      cron: "0 4 * * 0"
  gracefulShutdown:
    timeout: 60s
  rcon:
    enabled: true
    port: 25575
    passwordSecret:
      name: %s-rcon
      key: password
  backup:
    enabled: true
    retention:
      maxCount: 5
  podTemplate:
    spec:
      containers:
      - name: minecraft
        image: docker.io/lexfrei/papermc:1.21.1-91
        resources:
          requests:
            memory: "512Mi"
            cpu: "250m"
          limits:
            memory: "1Gi"
            cpu: "500m"
        env:
        - name: EULA
          value: "TRUE"`, backupServer, namespace, backupServer)

			cmd = kubectlCmd("apply", "--filename", "-")
			cmd.Stdin = strings.NewReader(serverYAML)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("waiting for PVC to be created by StatefulSet")
			Eventually(func(g Gomega) {
				cmd := kubectlCmd("get", "pvc",
					fmt.Sprintf("data-%s-0", backupServer),
					"--namespace", namespace)
				_, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
			}, 2*time.Minute).Should(Succeed())

			By("applying backup-now annotation after PVC exists")
			cmd = kubectlCmd("annotate", "papermcserver", backupServer,
				fmt.Sprintf("mc.k8s.lex.la/backup-now=%d", time.Now().Unix()),
				"--namespace", namespace)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("verifying VolumeSnapshot is created with correct labels")
			Eventually(func(g Gomega) {
				cmd := kubectlCmd("get", "volumesnapshot",
					"--selector=mc.k8s.lex.la/server-name="+backupServer,
					"--namespace", namespace,
					"--output", "jsonpath={.items[0].metadata.labels.mc\\.k8s\\.lex\\.la/backup-trigger}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("manual"),
					"Backup trigger label should be 'manual'")
			}, 2*time.Minute).Should(Succeed())

			By("verifying backup-now annotation was removed")
			Eventually(func(g Gomega) {
				cmd := kubectlCmd("get", "papermcserver", backupServer,
					"--namespace", namespace,
					"--output", "jsonpath={.metadata.annotations.mc\\.k8s\\.lex\\.la/backup-now}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(BeEmpty(),
					"backup-now annotation should be removed after backup")
			}, 30*time.Second).Should(Succeed())

			By("verifying backup status is updated")
			Eventually(func(g Gomega) {
				cmd := kubectlCmd("get", "papermcserver", backupServer,
					"--namespace", namespace,
					"--output", "jsonpath={.status.backup.lastBackup.trigger}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("manual"),
					"Backup status trigger should be 'manual'")
			}, 30*time.Second).Should(Succeed())
		})

		It("should set BackupCronValid condition for invalid backup cron", func() {
			By("creating RCON secret")
			cmd := kubectlCmd("create", "secret", "generic",
				backupServer+"-rcon",
				"--from-literal=password=test-pass",
				"--namespace", namespace)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("creating PaperMCServer with invalid backup cron")
			serverYAML := fmt.Sprintf(`apiVersion: mc.k8s.lex.la/v1beta1
kind: PaperMCServer
metadata:
  name: %s
  namespace: %s
spec:
  updateStrategy: "latest"
  updateSchedule:
    checkCron: "0 3 * * *"
    maintenanceWindow:
      enabled: true
      cron: "0 4 * * 0"
  gracefulShutdown:
    timeout: 60s
  rcon:
    enabled: true
    port: 25575
    passwordSecret:
      name: %s-rcon
      key: password
  backup:
    enabled: true
    schedule: "invalid-cron-expression"
  podTemplate:
    spec:
      containers:
      - name: minecraft
        image: docker.io/lexfrei/papermc:1.21.1-91
        resources:
          requests:
            memory: "512Mi"
            cpu: "250m"
        env:
        - name: EULA
          value: "TRUE"`, backupServer, namespace, backupServer)

			cmd = kubectlCmd("apply", "--filename", "-")
			cmd.Stdin = strings.NewReader(serverYAML)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("verifying BackupCronValid condition is set to False")
			Eventually(func(g Gomega) {
				cmd := kubectlCmd("get", "papermcserver", backupServer,
					"--namespace", namespace,
					"--output", "jsonpath={.status.conditions[?(@.type=='BackupCronValid')].status}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("False"),
					"BackupCronValid should be False for invalid cron")
			}, 60*time.Second).Should(Succeed())
		})
	})
})

// serviceAccountToken returns a token for the specified service account in the given namespace.
// It uses the Kubernetes TokenRequest API to generate a token by directly sending a request
// and parsing the resulting token from the API response.
func serviceAccountToken() (string, error) {
	const tokenRequestRawString = `{
		"apiVersion": "authentication.k8s.io/v1",
		"kind": "TokenRequest"
	}`

	// Temporary file to store the token request
	secretName := fmt.Sprintf("%s-token-request", serviceAccountName)
	tokenRequestFile := filepath.Join("/tmp", secretName)
	err := os.WriteFile(tokenRequestFile, []byte(tokenRequestRawString), os.FileMode(0o644))
	if err != nil {
		return "", err
	}

	var out string
	verifyTokenCreation := func(g Gomega) {
		// Execute kubectl command to create the token
		cmd := kubectlCmd("create", "--raw", fmt.Sprintf(
			"/api/v1/namespaces/%s/serviceaccounts/%s/token",
			namespace,
			serviceAccountName,
		), "--filename", tokenRequestFile)

		output, err := cmd.CombinedOutput()
		g.Expect(err).NotTo(HaveOccurred())

		// Parse the JSON output to extract the token
		var token tokenRequest
		err = json.Unmarshal(output, &token)
		g.Expect(err).NotTo(HaveOccurred())

		out = token.Status.Token
	}
	Eventually(verifyTokenCreation).Should(Succeed())

	return out, err
}

// getMetricsOutput retrieves and returns the logs from the curl pod used to access the metrics endpoint.
func getMetricsOutput() (string, error) {
	By("getting the curl-metrics logs")
	cmd := kubectlCmd("logs", "curl-metrics", "--namespace", namespace)
	return utils.Run(cmd)
}

// tokenRequest is a simplified representation of the Kubernetes TokenRequest API response,
// containing only the token field that we need to extract.
type tokenRequest struct {
	Status struct {
		Token string `json:"token"`
	} `json:"status"`
}

// parseImage splits a container image reference into repository and tag.
// For example: "example.com/minecraft-operator:v0.0.1" returns
// ("example.com/minecraft-operator", "v0.0.1")
func parseImage(image string) (string, string) {
	parts := strings.Split(image, ":")
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return image, "latest"
}
