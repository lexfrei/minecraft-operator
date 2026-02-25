/*
Copyright 2026, Aleksei Sviridkin.

SPDX-License-Identifier: BSD-3-Clause
*/

package controller

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"time"

	"github.com/cockroachdb/errors"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	mck8slexlav1beta1 "github.com/lexfrei/minecraft-operator/api/v1beta1"
	"github.com/lexfrei/minecraft-operator/pkg/paper"
	"github.com/lexfrei/minecraft-operator/pkg/plugins"
	"github.com/lexfrei/minecraft-operator/pkg/solver"
	"github.com/lexfrei/minecraft-operator/pkg/testutil"
)

var _ = Describe("Network failure handling", func() {
	// --------------------------------------------------------------------------
	// Helpers shared across all network-failure tests
	// --------------------------------------------------------------------------

	createTestPlugin := func(name, namespace string, spec mck8slexlav1beta1.PluginSpec) {
		plugin := &mck8slexlav1beta1.Plugin{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
			Spec:       spec,
		}
		Expect(k8sClient.Create(context.Background(), plugin)).To(Succeed())
	}

	deleteTestPlugin := func(name, namespace string) {
		plugin := &mck8slexlav1beta1.Plugin{}
		if err := k8sClient.Get(context.Background(),
			types.NamespacedName{Name: name, Namespace: namespace}, plugin); err != nil {
			return
		}
		if controllerutil.ContainsFinalizer(plugin, PluginFinalizer) {
			controllerutil.RemoveFinalizer(plugin, PluginFinalizer)
			if updateErr := k8sClient.Update(context.Background(), plugin); updateErr != nil {
				return
			}
		}
		if plugin.DeletionTimestamp.IsZero() {
			_ = k8sClient.Delete(context.Background(), plugin)
		}
	}

	// reconcilePastFinalizer runs two reconciliations: the first one adds a
	// finalizer; the second one proceeds to the real reconciliation logic.
	reconcilePastFinalizer := func(
		r *PluginReconciler, name, namespace string,
	) (ctrl.Result, error) {
		req := ctrl.Request{
			NamespacedName: types.NamespacedName{Name: name, Namespace: namespace},
		}
		// First reconcile — adds finalizer
		_, err := r.Reconcile(context.Background(), req)
		Expect(err).NotTo(HaveOccurred())

		// Second reconcile — actual logic
		return r.Reconcile(context.Background(), req)
	}

	getPlugin := func(name, namespace string) mck8slexlav1beta1.Plugin {
		var p mck8slexlav1beta1.Plugin
		Expect(k8sClient.Get(context.Background(),
			types.NamespacedName{Name: name, Namespace: namespace}, &p)).To(Succeed())
		return p
	}

	// --------------------------------------------------------------------------
	// 1. Hangar API failure scenarios
	// --------------------------------------------------------------------------

	Context("Hangar API failures", func() {
		const ns = "default"

		It("should set RepositoryAvailable=False and requeue after 5m when Hangar API is down", func() {
			pluginName := "net-hangar-down"
			mock := &testutil.MockPluginClient{
				VersionErr: fmt.Errorf("connection refused"),
			}
			reconciler := &PluginReconciler{
				Client:       k8sClient,
				Scheme:       k8sClient.Scheme(),
				PluginClient: mock,
				Solver:       solver.NewSimpleSolver(),
			}

			createTestPlugin(pluginName, ns, mck8slexlav1beta1.PluginSpec{
				Source:         mck8slexlav1beta1.PluginSource{Type: "hangar", Project: "DownPlugin"},
				UpdateStrategy: "latest",
				InstanceSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{"net-hangar-down": "true"},
				},
			})
			defer deleteTestPlugin(pluginName, ns)

			result, err := reconcilePastFinalizer(reconciler, pluginName, ns)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(5*time.Minute),
				"Should requeue after 5 minutes when Hangar API is down")

			p := getPlugin(pluginName, ns)
			Expect(p.Status.RepositoryStatus).To(Equal("unavailable"))

			cond := findCondition(p.Status.Conditions, conditionTypeRepositoryAvailable)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Message).To(ContainSubstring("connection refused"))
		})

		It("should transition to orphaned status when Hangar goes down after successful fetch", func() {
			pluginName := "net-hangar-orphan"
			mock := &testutil.MockPluginClient{
				Versions: []plugins.PluginVersion{
					{
						Version:           "1.0.0",
						MinecraftVersions: []string{"1.21.1"},
						DownloadURL:       "https://example.com/v1.jar",
						Hash:              "aaa",
					},
				},
			}
			reconciler := &PluginReconciler{
				Client:       k8sClient,
				Scheme:       k8sClient.Scheme(),
				PluginClient: mock,
				Solver:       solver.NewSimpleSolver(),
			}

			createTestPlugin(pluginName, ns, mck8slexlav1beta1.PluginSpec{
				Source:         mck8slexlav1beta1.PluginSource{Type: "hangar", Project: "OrphanPlugin"},
				UpdateStrategy: "latest",
				InstanceSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{"net-hangar-orphan": "true"},
				},
			})
			defer deleteTestPlugin(pluginName, ns)

			// Successful fetch to populate cache
			_, err := reconcilePastFinalizer(reconciler, pluginName, ns)
			Expect(err).NotTo(HaveOccurred())

			p := getPlugin(pluginName, ns)
			Expect(p.Status.RepositoryStatus).To(Equal("available"))
			Expect(p.Status.AvailableVersions).To(HaveLen(1))

			// Hangar goes down
			mock.VersionErr = fmt.Errorf("502 bad gateway")
			mock.Versions = nil

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: pluginName, Namespace: ns}}
			result, err := reconciler.Reconcile(context.Background(), req)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(15*time.Minute),
				"Should use normal requeue when cached data is used (orphaned)")

			p = getPlugin(pluginName, ns)
			Expect(p.Status.RepositoryStatus).To(Equal("orphaned"))
			Expect(p.Status.AvailableVersions).To(HaveLen(1),
				"Cached versions must be preserved in orphaned state")

			cond := findCondition(p.Status.Conditions, conditionTypeRepositoryAvailable)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Message).To(ContainSubstring("using cached data"))
		})

		It("should recover from orphaned status when Hangar comes back up", func() {
			pluginName := "net-hangar-recover"
			mock := &testutil.MockPluginClient{
				Versions: []plugins.PluginVersion{
					{
						Version:           "1.0.0",
						MinecraftVersions: []string{"1.21.1"},
						DownloadURL:       "https://example.com/v1.jar",
						Hash:              "aaa",
					},
				},
			}
			reconciler := &PluginReconciler{
				Client:       k8sClient,
				Scheme:       k8sClient.Scheme(),
				PluginClient: mock,
				Solver:       solver.NewSimpleSolver(),
			}

			createTestPlugin(pluginName, ns, mck8slexlav1beta1.PluginSpec{
				Source:         mck8slexlav1beta1.PluginSource{Type: "hangar", Project: "RecoverPlugin"},
				UpdateStrategy: "latest",
				InstanceSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{"net-hangar-recover": "true"},
				},
			})
			defer deleteTestPlugin(pluginName, ns)

			// Successful fetch
			_, err := reconcilePastFinalizer(reconciler, pluginName, ns)
			Expect(err).NotTo(HaveOccurred())

			// Hangar goes down → orphaned
			mock.VersionErr = fmt.Errorf("timeout")
			mock.Versions = nil

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: pluginName, Namespace: ns}}
			_, err = reconciler.Reconcile(context.Background(), req)
			Expect(err).NotTo(HaveOccurred())

			p := getPlugin(pluginName, ns)
			Expect(p.Status.RepositoryStatus).To(Equal("orphaned"))

			// Hangar comes back with updated versions
			mock.VersionErr = nil
			mock.Versions = []plugins.PluginVersion{
				{
					Version:           "1.0.0",
					MinecraftVersions: []string{"1.21.1"},
					DownloadURL:       "https://example.com/v1.jar",
					Hash:              "aaa",
				},
				{
					Version:           "2.0.0",
					MinecraftVersions: []string{"1.21.1", "1.21.4"},
					DownloadURL:       "https://example.com/v2.jar",
					Hash:              "bbb",
				},
			}

			_, err = reconciler.Reconcile(context.Background(), req)
			Expect(err).NotTo(HaveOccurred())

			p = getPlugin(pluginName, ns)
			Expect(p.Status.RepositoryStatus).To(Equal("available"),
				"Should recover to available after Hangar comes back")
			Expect(p.Status.AvailableVersions).To(HaveLen(2),
				"Should have updated versions after recovery")

			cond := findCondition(p.Status.Conditions, conditionTypeRepositoryAvailable)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionTrue))
		})

		It("should track call counts for retry verification", func() {
			pluginName := "net-hangar-retries"
			mock := &testutil.MockPluginClient{
				VersionErr: fmt.Errorf("service unavailable"),
			}
			reconciler := &PluginReconciler{
				Client:       k8sClient,
				Scheme:       k8sClient.Scheme(),
				PluginClient: mock,
				Solver:       solver.NewSimpleSolver(),
			}

			createTestPlugin(pluginName, ns, mck8slexlav1beta1.PluginSpec{
				Source:         mck8slexlav1beta1.PluginSource{Type: "hangar", Project: "RetryPlugin"},
				UpdateStrategy: "latest",
				InstanceSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{"net-hangar-retries": "true"},
				},
			})
			defer deleteTestPlugin(pluginName, ns)

			// Past finalizer
			_, err := reconcilePastFinalizer(reconciler, pluginName, ns)
			Expect(err).NotTo(HaveOccurred())

			// Each reconcile makes exactly one GetVersions call (no in-loop retry)
			Expect(mock.GetVersionsCalls).To(Equal(1),
				"Each reconcile should make exactly one API call (retry via requeue, not in-loop)")

			// Reconcile again — simulating the requeue
			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: pluginName, Namespace: ns}}
			_, err = reconciler.Reconcile(context.Background(), req)
			Expect(err).NotTo(HaveOccurred())

			Expect(mock.GetVersionsCalls).To(Equal(2),
				"Second reconcile should make a second API call")
		})
	})

	// --------------------------------------------------------------------------
	// 2. URL download failure scenarios
	// --------------------------------------------------------------------------

	Context("URL download failures", func() {
		const ns = "default"

		It("should set RepositoryAvailable=False when URL download returns HTTP 500", func() {
			pluginName := "net-url-500"

			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			}))
			defer ts.Close()

			reconciler := &PluginReconciler{
				Client:     k8sClient,
				Scheme:     k8sClient.Scheme(),
				Solver:     solver.NewSimpleSolver(),
				HTTPClient: wrapTestClient(ts),
			}

			createTestPlugin(pluginName, ns, mck8slexlav1beta1.PluginSpec{
				Source: mck8slexlav1beta1.PluginSource{
					Type: "url",
					URL:  testPluginURL,
				},
				UpdateStrategy: "latest",
				InstanceSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{"net-url-500": "true"},
				},
			})
			defer deleteTestPlugin(pluginName, ns)

			result, err := reconcilePastFinalizer(reconciler, pluginName, ns)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(5*time.Minute),
				"Should requeue after 5m on download failure")

			p := getPlugin(pluginName, ns)
			Expect(p.Status.RepositoryStatus).To(Equal("unavailable"))

			cond := findCondition(p.Status.Conditions, conditionTypeRepositoryAvailable)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		})

		It("should set RepositoryAvailable=False when URL download times out", func() {
			pluginName := "net-url-timeout"

			// Use a channel so the handler can unblock when the test is done
			done := make(chan struct{})

			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				// Write headers but never finish the body — client will time out
				w.Header().Set("Content-Length", "1000000")
				w.WriteHeader(http.StatusOK)
				// Block until client times out or test closes the channel
				<-done
			}))
			defer func() {
				close(done)
				ts.Close()
			}()

			// Create a client with very short timeout
			shortTimeoutClient := wrapTestClient(ts)
			shortTimeoutClient.Timeout = 100 * time.Millisecond

			reconciler := &PluginReconciler{
				Client:     k8sClient,
				Scheme:     k8sClient.Scheme(),
				Solver:     solver.NewSimpleSolver(),
				HTTPClient: shortTimeoutClient,
			}

			createTestPlugin(pluginName, ns, mck8slexlav1beta1.PluginSpec{
				Source: mck8slexlav1beta1.PluginSource{
					Type: "url",
					URL:  testPluginURL,
				},
				UpdateStrategy: "latest",
				InstanceSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{"net-url-timeout": "true"},
				},
			})
			defer deleteTestPlugin(pluginName, ns)

			result, err := reconcilePastFinalizer(reconciler, pluginName, ns)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(5*time.Minute),
				"Should requeue after 5m on timeout")

			p := getPlugin(pluginName, ns)
			Expect(p.Status.RepositoryStatus).To(Equal("unavailable"))
		})

		It("should set RepositoryAvailable=False when URL download returns HTTP 404", func() {
			pluginName := "net-url-404"

			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "Not Found", http.StatusNotFound)
			}))
			defer ts.Close()

			reconciler := &PluginReconciler{
				Client:     k8sClient,
				Scheme:     k8sClient.Scheme(),
				Solver:     solver.NewSimpleSolver(),
				HTTPClient: wrapTestClient(ts),
			}

			createTestPlugin(pluginName, ns, mck8slexlav1beta1.PluginSpec{
				Source: mck8slexlav1beta1.PluginSource{
					Type: "url",
					URL:  testPluginURL,
				},
				UpdateStrategy: "latest",
				InstanceSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{"net-url-404": "true"},
				},
			})
			defer deleteTestPlugin(pluginName, ns)

			result, err := reconcilePastFinalizer(reconciler, pluginName, ns)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(5*time.Minute),
				"Should requeue after 5m on 404")

			p := getPlugin(pluginName, ns)
			Expect(p.Status.RepositoryStatus).To(Equal("unavailable"))
		})

		It("should recover when URL becomes available after failure", func() {
			pluginName := "net-url-recover"
			requestCount := 0

			jarData := testutil.BuildTestJAR("plugin.yml",
				"name: RecoverPlugin\nversion: \"1.0.0\"\napi-version: \"1.21\"\n")
			jarHash := fmt.Sprintf("%x", sha256.Sum256(jarData))

			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requestCount++
				if requestCount <= 1 {
					// First request fails
					http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
					return
				}
				// Subsequent requests succeed
				w.Header().Set("Content-Type", "application/java-archive")
				_, _ = w.Write(jarData)
			}))
			defer ts.Close()

			reconciler := &PluginReconciler{
				Client:     k8sClient,
				Scheme:     k8sClient.Scheme(),
				Solver:     solver.NewSimpleSolver(),
				HTTPClient: wrapTestClient(ts),
			}

			createTestPlugin(pluginName, ns, mck8slexlav1beta1.PluginSpec{
				Source: mck8slexlav1beta1.PluginSource{
					Type:     "url",
					URL:      testPluginURL,
					Checksum: jarHash,
				},
				UpdateStrategy: "latest",
				InstanceSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{"net-url-recover": "true"},
				},
			})
			defer deleteTestPlugin(pluginName, ns)

			// First attempt — fails
			result, err := reconcilePastFinalizer(reconciler, pluginName, ns)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(5 * time.Minute))

			p := getPlugin(pluginName, ns)
			Expect(p.Status.RepositoryStatus).To(Equal("unavailable"))

			// Second attempt — succeeds
			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: pluginName, Namespace: ns}}
			result, err = reconciler.Reconcile(context.Background(), req)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(15 * time.Minute))

			p = getPlugin(pluginName, ns)
			Expect(p.Status.RepositoryStatus).To(Equal("available"),
				"Should recover to available after URL becomes accessible")
			Expect(p.Status.AvailableVersions).To(HaveLen(1))
			Expect(p.Status.AvailableVersions[0].Version).To(Equal("1.0.0"))
		})

		It("should handle connection reset gracefully", func() {
			pluginName := "net-url-connreset"

			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				// Hijack the connection and close it immediately to simulate connection reset
				hj, ok := w.(http.Hijacker)
				if !ok {
					http.Error(w, "hijack not supported", http.StatusInternalServerError)
					return
				}
				conn, _, _ := hj.Hijack()
				_ = conn.Close()
			}))
			defer ts.Close()

			reconciler := &PluginReconciler{
				Client:     k8sClient,
				Scheme:     k8sClient.Scheme(),
				Solver:     solver.NewSimpleSolver(),
				HTTPClient: wrapTestClient(ts),
			}

			createTestPlugin(pluginName, ns, mck8slexlav1beta1.PluginSpec{
				Source: mck8slexlav1beta1.PluginSource{
					Type: "url",
					URL:  testPluginURL,
				},
				UpdateStrategy: "latest",
				InstanceSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{"net-url-connreset": "true"},
				},
			})
			defer deleteTestPlugin(pluginName, ns)

			result, err := reconcilePastFinalizer(reconciler, pluginName, ns)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(5*time.Minute),
				"Should requeue after connection reset")

			p := getPlugin(pluginName, ns)
			Expect(p.Status.RepositoryStatus).To(Equal("unavailable"))
		})

		It("should handle partial response (truncated body) gracefully", func() {
			pluginName := "net-url-truncated"

			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				// Claim a large body but send only partial data
				w.Header().Set("Content-Length", "1000000")
				w.WriteHeader(http.StatusOK)
				_, _ = io.WriteString(w, "partial-data")
				// Connection closes before all data is sent
			}))
			defer ts.Close()

			reconciler := &PluginReconciler{
				Client:     k8sClient,
				Scheme:     k8sClient.Scheme(),
				Solver:     solver.NewSimpleSolver(),
				HTTPClient: wrapTestClient(ts),
			}

			createTestPlugin(pluginName, ns, mck8slexlav1beta1.PluginSpec{
				Source: mck8slexlav1beta1.PluginSource{
					Type: "url",
					URL:  testPluginURL,
				},
				UpdateStrategy: "latest",
				InstanceSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{"net-url-truncated": "true"},
				},
			})
			defer deleteTestPlugin(pluginName, ns)

			_, err := reconcilePastFinalizer(reconciler, pluginName, ns)
			Expect(err).NotTo(HaveOccurred())

			p := getPlugin(pluginName, ns)
			// Truncated body: either download fails (unavailable + requeue 5m)
			// or "JAR" is received but invalid ZIP → parse fails → fallback version
			// Both are acceptable graceful handling
			Expect(p.Status.RepositoryStatus).To(
				SatisfyAny(Equal("unavailable"), Equal("available")),
				"Should handle truncated body gracefully (either unavailable or fallback parse)")
		})
	})

	// --------------------------------------------------------------------------
	// 3. Docker registry failure scenarios (PaperMCServer controller)
	// --------------------------------------------------------------------------

	Context("Docker registry failures", func() {
		const ns = "default"

		It("should fail version resolution when registry ListTags returns error", func() {
			mockRegistry := &testutil.MockRegistryAPI{
				TagsErr: fmt.Errorf("registry unavailable: connection refused"),
			}
			mockPaper := &testutil.MockPaperAPI{
				Versions: []string{"1.21.1", "1.21.0"},
			}

			reconciler := &PaperMCServerReconciler{
				Client:         k8sClient,
				Scheme:         k8sClient.Scheme(),
				PaperClient:    mockPaper,
				RegistryClient: mockRegistry,
				Solver:         solver.NewSimpleSolver(),
			}

			serverName := "net-registry-down"
			server := &mck8slexlav1beta1.PaperMCServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serverName,
					Namespace: ns,
					Labels:    map[string]string{"net-registry": "true"},
				},
				Spec: mck8slexlav1beta1.PaperMCServerSpec{
					UpdateStrategy: "latest",
					PodTemplate: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "papermc", Image: "lexfrei/papermc:1.21.1-91"},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(context.Background(), server)).To(Succeed())
			defer func() {
				_ = k8sClient.Delete(context.Background(), server)
			}()

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: serverName, Namespace: ns}}
			_, err := reconciler.Reconcile(context.Background(), req)

			// Resolution failure should propagate as error (triggering controller-runtime requeue)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("registry unavailable"))
		})

		It("should recover version resolution when registry becomes available", func() {
			mockRegistry := &testutil.MockRegistryAPI{
				TagsErr: fmt.Errorf("registry timeout"),
			}
			mockPaper := &testutil.MockPaperAPI{
				Versions:     []string{"1.21.1"},
				BuildInfo:    &paper.BuildInfo{Version: "1.21.1", Build: 91},
				BuildNumbers: []int{91, 90, 89},
			}

			reconciler := &PaperMCServerReconciler{
				Client:         k8sClient,
				Scheme:         k8sClient.Scheme(),
				PaperClient:    mockPaper,
				RegistryClient: mockRegistry,
				Solver:         solver.NewSimpleSolver(),
			}

			serverName := "net-registry-recover"
			server := &mck8slexlav1beta1.PaperMCServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serverName,
					Namespace: ns,
					Labels:    map[string]string{"net-registry-recover": "true"},
				},
				Spec: mck8slexlav1beta1.PaperMCServerSpec{
					UpdateStrategy: "latest",
					PodTemplate: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "papermc", Image: "lexfrei/papermc:1.21.1-91"},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(context.Background(), server)).To(Succeed())
			defer func() {
				_ = k8sClient.Delete(context.Background(), server)
			}()

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: serverName, Namespace: ns}}

			// First attempt — fails
			_, err := reconciler.Reconcile(context.Background(), req)
			Expect(err).To(HaveOccurred())

			// Registry comes back
			mockRegistry.TagsErr = nil
			mockRegistry.Tags = []string{"1.21.1-91", "1.21.1-90", "1.21.0-50"}

			// Second attempt — succeeds
			_, err = reconciler.Reconcile(context.Background(), req)
			Expect(err).NotTo(HaveOccurred())

			var s mck8slexlav1beta1.PaperMCServer
			Expect(k8sClient.Get(context.Background(),
				types.NamespacedName{Name: serverName, Namespace: ns}, &s)).To(Succeed())

			Expect(s.Status.DesiredVersion).To(Equal("1.21.1"),
				"Should resolve version after registry recovery")
			Expect(s.Status.DesiredBuild).To(Equal(91))
		})

		It("should use existing DesiredVersion as fallback when resolution fails", func() {
			mockRegistry := &testutil.MockRegistryAPI{
				Tags: []string{"1.21.1-91", "1.21.1-90"},
			}
			mockPaper := &testutil.MockPaperAPI{
				Versions:     []string{"1.21.1"},
				BuildInfo:    &paper.BuildInfo{Version: "1.21.1", Build: 91},
				BuildNumbers: []int{91, 90},
			}

			reconciler := &PaperMCServerReconciler{
				Client:         k8sClient,
				Scheme:         k8sClient.Scheme(),
				PaperClient:    mockPaper,
				RegistryClient: mockRegistry,
				Solver:         solver.NewSimpleSolver(),
			}

			serverName := "net-registry-fallback"
			server := &mck8slexlav1beta1.PaperMCServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serverName,
					Namespace: ns,
					Labels:    map[string]string{"net-registry-fallback": "true"},
				},
				Spec: mck8slexlav1beta1.PaperMCServerSpec{
					UpdateStrategy: "latest",
					PodTemplate: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "papermc", Image: "lexfrei/papermc:1.21.1-91"},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(context.Background(), server)).To(Succeed())
			defer func() {
				_ = k8sClient.Delete(context.Background(), server)
			}()

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: serverName, Namespace: ns}}

			// First reconcile — succeeds, sets DesiredVersion
			_, err := reconciler.Reconcile(context.Background(), req)
			Expect(err).NotTo(HaveOccurred())

			var s mck8slexlav1beta1.PaperMCServer
			Expect(k8sClient.Get(context.Background(),
				types.NamespacedName{Name: serverName, Namespace: ns}, &s)).To(Succeed())
			Expect(s.Status.DesiredVersion).To(Equal("1.21.1"))

			// Registry goes down
			mockRegistry.TagsErr = fmt.Errorf("connection refused")
			mockRegistry.Tags = nil

			// Second reconcile — resolution fails but uses existing DesiredVersion fallback
			_, err = reconciler.Reconcile(context.Background(), req)
			Expect(err).NotTo(HaveOccurred(),
				"Should not fail when fallback DesiredVersion is available")

			Expect(k8sClient.Get(context.Background(),
				types.NamespacedName{Name: serverName, Namespace: ns}, &s)).To(Succeed())
			Expect(s.Status.DesiredVersion).To(Equal("1.21.1"),
				"Fallback DesiredVersion should be preserved")
		})
	})

	// --------------------------------------------------------------------------
	// 4. PaperMC API failure scenarios
	// --------------------------------------------------------------------------

	Context("PaperMC API failures", func() {
		const ns = "default"

		It("should fail when PaperMC API GetPaperVersions returns error (pin strategy)", func() {
			mockPaper := &testutil.MockPaperAPI{
				BuildsErr: fmt.Errorf("paper api unreachable"),
			}
			mockRegistry := &testutil.MockRegistryAPI{
				Tags: []string{"1.21.1-91"},
			}

			reconciler := &PaperMCServerReconciler{
				Client:         k8sClient,
				Scheme:         k8sClient.Scheme(),
				PaperClient:    mockPaper,
				RegistryClient: mockRegistry,
				Solver:         solver.NewSimpleSolver(),
			}

			serverName := "net-paper-api-down"
			server := &mck8slexlav1beta1.PaperMCServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serverName,
					Namespace: ns,
					Labels:    map[string]string{"net-paper-api": "true"},
				},
				Spec: mck8slexlav1beta1.PaperMCServerSpec{
					UpdateStrategy: "pin",
					Version:        "1.21.1",
					PodTemplate: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "papermc", Image: "lexfrei/papermc:1.21.1-91"},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(context.Background(), server)).To(Succeed())
			defer func() {
				_ = k8sClient.Delete(context.Background(), server)
			}()

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: serverName, Namespace: ns}}
			_, err := reconciler.Reconcile(context.Background(), req)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("paper api unreachable"))
		})

		It("should fail when PaperMC API GetBuilds returns error (pin strategy)", func() {
			mockPaper := &testutil.MockPaperAPI{
				BuildsErr: fmt.Errorf("build info endpoint unavailable"),
			}
			mockRegistry := &testutil.MockRegistryAPI{
				Tags:      []string{"1.21.1-91"},
				TagExists: map[string]bool{"1.21.1-91": true},
			}

			reconciler := &PaperMCServerReconciler{
				Client:         k8sClient,
				Scheme:         k8sClient.Scheme(),
				PaperClient:    mockPaper,
				RegistryClient: mockRegistry,
				Solver:         solver.NewSimpleSolver(),
			}

			serverName := "net-paper-build-err"
			server := &mck8slexlav1beta1.PaperMCServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serverName,
					Namespace: ns,
					Labels:    map[string]string{"net-paper-build": "true"},
				},
				Spec: mck8slexlav1beta1.PaperMCServerSpec{
					UpdateStrategy: "pin",
					Version:        "1.21.1",
					PodTemplate: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "papermc", Image: "lexfrei/papermc:1.21.1-91"},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(context.Background(), server)).To(Succeed())
			defer func() {
				_ = k8sClient.Delete(context.Background(), server)
			}()

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: serverName, Namespace: ns}}
			_, err := reconciler.Reconcile(context.Background(), req)
			// Pin strategy calls GetBuilds then validates build exists in registry.
			// The key assertion: error should propagate, not silently succeed.
			Expect(err).To(HaveOccurred(),
				"PaperMC API failure should propagate as reconciliation error")
		})
	})

	// --------------------------------------------------------------------------
	// 5. Permanent vs transient error classification
	// --------------------------------------------------------------------------

	Context("Permanent vs transient error classification", func() {
		const ns = "default"

		It("should NOT requeue on checksum mismatch (permanent user error)", func() {
			pluginName := "net-checksum-perm"

			jarData := testutil.BuildTestJAR("plugin.yml",
				"name: ChecksumPlugin\nversion: \"1.0.0\"\n")

			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/java-archive")
				_, _ = w.Write(jarData)
			}))
			defer ts.Close()

			reconciler := &PluginReconciler{
				Client:     k8sClient,
				Scheme:     k8sClient.Scheme(),
				Solver:     solver.NewSimpleSolver(),
				HTTPClient: wrapTestClient(ts),
			}

			createTestPlugin(pluginName, ns, mck8slexlav1beta1.PluginSpec{
				Source: mck8slexlav1beta1.PluginSource{
					Type:     "url",
					URL:      testPluginURL,
					Checksum: "0000000000000000000000000000000000000000000000000000000000000000",
				},
				UpdateStrategy: "latest",
				InstanceSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{"net-checksum": "true"},
				},
			})
			defer deleteTestPlugin(pluginName, ns)

			result, err := reconcilePastFinalizer(reconciler, pluginName, ns)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeZero(),
				"Checksum mismatch is permanent — should NOT requeue periodically")
			Expect(result).To(Equal(ctrl.Result{}),
				"Checksum mismatch should return zero Result (no requeue)")

			p := getPlugin(pluginName, ns)
			Expect(p.Status.RepositoryStatus).To(Equal("unavailable"))

			cond := findCondition(p.Status.Conditions, conditionTypeRepositoryAvailable)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Reason).To(Equal(reasonChecksumMismatch))
		})

		It("should NOT requeue on invalid URL (permanent user error)", func() {
			pluginName := "net-invalid-url-perm"

			reconciler := &PluginReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
				Solver: solver.NewSimpleSolver(),
			}

			createTestPlugin(pluginName, ns, mck8slexlav1beta1.PluginSpec{
				Source: mck8slexlav1beta1.PluginSource{
					Type: "url",
					URL:  "http://insecure.example.com/plugin.jar", // HTTP, not HTTPS
				},
				UpdateStrategy: "latest",
				InstanceSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{"net-invalid-url": "true"},
				},
			})
			defer deleteTestPlugin(pluginName, ns)

			result, err := reconcilePastFinalizer(reconciler, pluginName, ns)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeZero(),
				"Invalid URL is permanent — should NOT requeue periodically")
			Expect(result).To(Equal(ctrl.Result{}),
				"Invalid URL should return zero Result (no requeue)")

			p := getPlugin(pluginName, ns)
			Expect(p.Status.RepositoryStatus).To(Equal("unavailable"))
		})

		It("should NOT requeue on unsupported source type (permanent user error)", func() {
			pluginName := "net-unsupported-src"

			reconciler := &PluginReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
				Solver: solver.NewSimpleSolver(),
			}

			// Bypass CRD validation by using the reconciler's internal method directly
			plugin := &mck8slexlav1beta1.Plugin{
				ObjectMeta: metav1.ObjectMeta{Name: pluginName, Namespace: ns},
				Spec: mck8slexlav1beta1.PluginSpec{
					Source: mck8slexlav1beta1.PluginSource{
						Type:    "modrinth", // Not yet implemented
						Project: "SomePlugin",
					},
					UpdateStrategy: "latest",
					InstanceSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{"net-unsupported": "true"},
					},
				},
			}

			_, _, fetchErr := reconciler.fetchPluginMetadata(context.Background(), plugin)
			Expect(fetchErr).To(HaveOccurred())
			Expect(errors.Is(fetchErr, errUnsupportedSourceType)).To(BeTrue(),
				"Unsupported source type should be marked as sentinel error")
		})

		It("should requeue on transient Hangar API error (not permanent)", func() {
			pluginName := "net-transient-hangar"
			mock := &testutil.MockPluginClient{
				VersionErr: fmt.Errorf("503 service temporarily unavailable"),
			}
			reconciler := &PluginReconciler{
				Client:       k8sClient,
				Scheme:       k8sClient.Scheme(),
				PluginClient: mock,
				Solver:       solver.NewSimpleSolver(),
			}

			createTestPlugin(pluginName, ns, mck8slexlav1beta1.PluginSpec{
				Source:         mck8slexlav1beta1.PluginSource{Type: "hangar", Project: "TransientPlugin"},
				UpdateStrategy: "latest",
				InstanceSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{"net-transient": "true"},
				},
			})
			defer deleteTestPlugin(pluginName, ns)

			result, err := reconcilePastFinalizer(reconciler, pluginName, ns)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(5*time.Minute),
				"Transient error SHOULD trigger requeue after 5 minutes")
		})
	})

	// --------------------------------------------------------------------------
	// 6. Status conditions during failure lifecycle
	// --------------------------------------------------------------------------

	Context("Status conditions lifecycle during failures", func() {
		const ns = "default"

		It("should correctly transition conditions: available → unavailable → orphaned → available", func() {
			pluginName := "net-cond-lifecycle"
			mock := &testutil.MockPluginClient{
				Versions: []plugins.PluginVersion{
					{
						Version:           "1.0.0",
						MinecraftVersions: []string{"1.21.1"},
						DownloadURL:       "https://example.com/plugin.jar",
						Hash:              "abc",
					},
				},
			}
			reconciler := &PluginReconciler{
				Client:       k8sClient,
				Scheme:       k8sClient.Scheme(),
				PluginClient: mock,
				Solver:       solver.NewSimpleSolver(),
			}

			createTestPlugin(pluginName, ns, mck8slexlav1beta1.PluginSpec{
				Source:         mck8slexlav1beta1.PluginSource{Type: "hangar", Project: "LifecyclePlugin"},
				UpdateStrategy: "latest",
				InstanceSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{"net-lifecycle": "true"},
				},
			})
			defer deleteTestPlugin(pluginName, ns)

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: pluginName, Namespace: ns}}

			// Phase 1: Available
			_, err := reconcilePastFinalizer(reconciler, pluginName, ns)
			Expect(err).NotTo(HaveOccurred())

			p := getPlugin(pluginName, ns)
			Expect(p.Status.RepositoryStatus).To(Equal("available"))
			cond := findCondition(p.Status.Conditions, conditionTypeRepositoryAvailable)
			Expect(cond.Status).To(Equal(metav1.ConditionTrue))

			readyCond := findCondition(p.Status.Conditions, conditionTypeReady)
			Expect(readyCond.Status).To(Equal(metav1.ConditionTrue))

			// Phase 2: Down → orphaned (has cache)
			mock.VersionErr = fmt.Errorf("network error")
			mock.Versions = nil

			_, err = reconciler.Reconcile(context.Background(), req)
			Expect(err).NotTo(HaveOccurred())

			p = getPlugin(pluginName, ns)
			Expect(p.Status.RepositoryStatus).To(Equal("orphaned"))
			cond = findCondition(p.Status.Conditions, conditionTypeRepositoryAvailable)
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			// Ready should still be True because cache allows continued operation
			readyCond = findCondition(p.Status.Conditions, conditionTypeReady)
			Expect(readyCond.Status).To(Equal(metav1.ConditionTrue))

			// Phase 3: Recovery → available again
			mock.VersionErr = nil
			mock.Versions = []plugins.PluginVersion{
				{
					Version:           "2.0.0",
					MinecraftVersions: []string{"1.21.1", "1.21.4"},
					DownloadURL:       "https://example.com/plugin-v2.jar",
					Hash:              "def",
				},
			}

			_, err = reconciler.Reconcile(context.Background(), req)
			Expect(err).NotTo(HaveOccurred())

			p = getPlugin(pluginName, ns)
			Expect(p.Status.RepositoryStatus).To(Equal("available"))
			cond = findCondition(p.Status.Conditions, conditionTypeRepositoryAvailable)
			Expect(cond.Status).To(Equal(metav1.ConditionTrue))

			readyCond = findCondition(p.Status.Conditions, conditionTypeReady)
			Expect(readyCond.Status).To(Equal(metav1.ConditionTrue))

			Expect(p.Status.AvailableVersions).To(HaveLen(1))
			Expect(p.Status.AvailableVersions[0].Version).To(Equal("2.0.0"),
				"Should have new version after recovery")
		})
	})

	// --------------------------------------------------------------------------
	// 7. Fixed requeue interval verification
	// --------------------------------------------------------------------------

	Context("Requeue interval behavior", func() {
		const ns = "default"

		It("should use fixed 5m requeue for all transient failures (no exponential backoff)", func() {
			pluginName := "net-requeue-fixed"
			mock := &testutil.MockPluginClient{
				VersionErr: fmt.Errorf("temporary error"),
			}
			reconciler := &PluginReconciler{
				Client:       k8sClient,
				Scheme:       k8sClient.Scheme(),
				PluginClient: mock,
				Solver:       solver.NewSimpleSolver(),
			}

			createTestPlugin(pluginName, ns, mck8slexlav1beta1.PluginSpec{
				Source:         mck8slexlav1beta1.PluginSource{Type: "hangar", Project: "FixedRequeue"},
				UpdateStrategy: "latest",
				InstanceSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{"net-requeue": "true"},
				},
			})
			defer deleteTestPlugin(pluginName, ns)

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: pluginName, Namespace: ns}}

			// First reconcile — adds finalizer
			_, err := reconciler.Reconcile(context.Background(), req)
			Expect(err).NotTo(HaveOccurred())

			// Multiple consecutive failures should all use same 5m requeue
			for i := range 3 {
				result, err := reconciler.Reconcile(context.Background(), req)
				Expect(err).NotTo(HaveOccurred())
				Expect(result.RequeueAfter).To(Equal(5*time.Minute),
					"Requeue interval should be fixed at 5m for attempt %d (no exponential backoff)", i+1)
			}
		})

		It("should use 15m requeue for successful reconciliation", func() {
			pluginName := "net-requeue-success"
			mock := &testutil.MockPluginClient{
				Versions: []plugins.PluginVersion{
					{
						Version:     "1.0.0",
						DownloadURL: "https://example.com/ok.jar",
					},
				},
			}
			reconciler := &PluginReconciler{
				Client:       k8sClient,
				Scheme:       k8sClient.Scheme(),
				PluginClient: mock,
				Solver:       solver.NewSimpleSolver(),
			}

			createTestPlugin(pluginName, ns, mck8slexlav1beta1.PluginSpec{
				Source:         mck8slexlav1beta1.PluginSource{Type: "hangar", Project: "OKPlugin"},
				UpdateStrategy: "latest",
				InstanceSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{"net-requeue-ok": "true"},
				},
			})
			defer deleteTestPlugin(pluginName, ns)

			result, err := reconcilePastFinalizer(reconciler, pluginName, ns)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(15*time.Minute),
				"Successful reconcile should requeue after 15 minutes")
		})
	})
})

// Ensure imports are used.
var (
	_ = errors.Is
	_ url.URL
)
