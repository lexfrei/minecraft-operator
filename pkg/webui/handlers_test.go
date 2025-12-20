package webui

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	mck8slexlav1alpha1 "github.com/lexfrei/minecraft-operator/api/v1alpha1"
	"github.com/lexfrei/minecraft-operator/internal/controller"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestFormatCronSchedule(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: "Not scheduled",
		},
		{
			name:     "daily at 3am",
			input:    "0 3 * * *",
			expected: "Daily at 3:00",
		},
		{
			name:     "daily at 3:30am",
			input:    "30 3 * * *",
			expected: "Daily at 3:30",
		},
		{
			name:     "sunday at 4am",
			input:    "0 4 * * 0",
			expected: "Every Sunday at 4:00",
		},
		{
			name:     "monday at 4am",
			input:    "0 4 * * 1",
			expected: "Every Monday at 4:00",
		},
		{
			name:     "tuesday at 4:30am",
			input:    "30 4 * * 2",
			expected: "Every Tuesday at 4:30",
		},
		{
			name:     "complex expression",
			input:    "0 0 1 * *",
			expected: "Cron: 0 0 1 * *",
		},
		{
			name:     "invalid format",
			input:    "invalid",
			expected: "Cron: invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := formatCronSchedule(tt.input)
			if result != tt.expected {
				t.Errorf("formatCronSchedule(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestDetermineServerStatusRunningWithCondition(t *testing.T) {
	t.Parallel()
	server := &mck8slexlav1alpha1.PaperMCServer{
		Status: mck8slexlav1alpha1.PaperMCServerStatus{
			Conditions: []metav1.Condition{
				{Type: "StatefulSetReady", Status: metav1.ConditionTrue},
			},
		},
	}
	if got := determineServerStatus(server); got != statusRunning {
		t.Errorf("expected %s, got %q", statusRunning, got)
	}
}

func TestDetermineServerStatusUpdating(t *testing.T) {
	t.Parallel()
	server := &mck8slexlav1alpha1.PaperMCServer{
		Status: mck8slexlav1alpha1.PaperMCServerStatus{
			Conditions: []metav1.Condition{
				{Type: "StatefulSetReady", Status: metav1.ConditionFalse, Reason: "Updating"},
			},
		},
	}
	if got := determineServerStatus(server); got != statusUpdating {
		t.Errorf("expected %s, got %q", statusUpdating, got)
	}
}

func TestDetermineServerStatusUnknownWithFalseCondition(t *testing.T) {
	t.Parallel()
	server := &mck8slexlav1alpha1.PaperMCServer{
		Status: mck8slexlav1alpha1.PaperMCServerStatus{
			Conditions: []metav1.Condition{
				{Type: "StatefulSetReady", Status: metav1.ConditionFalse, Reason: "NotReady"},
			},
		},
	}
	if got := determineServerStatus(server); got != statusUnknown {
		t.Errorf("expected %s, got %q", statusUnknown, got)
	}
}

func TestDetermineServerStatusRunningWithVersionFallback(t *testing.T) {
	t.Parallel()
	server := &mck8slexlav1alpha1.PaperMCServer{
		Status: mck8slexlav1alpha1.PaperMCServerStatus{CurrentVersion: "1.21.10"},
	}
	if got := determineServerStatus(server); got != statusRunning {
		t.Errorf("expected %s, got %q", statusRunning, got)
	}
}

func TestDetermineServerStatusUnknownWithNoData(t *testing.T) {
	t.Parallel()
	server := &mck8slexlav1alpha1.PaperMCServer{
		Status: mck8slexlav1alpha1.PaperMCServerStatus{},
	}
	if got := determineServerStatus(server); got != statusUnknown {
		t.Errorf("expected %s, got %q", statusUnknown, got)
	}
}

func TestSplitCronExpression(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "standard cron",
			input:    "0 4 * * 0",
			expected: []string{"0", "4", "*", "*", "0"},
		},
		{
			name:     "multiple spaces",
			input:    "0  4  *  *  0",
			expected: []string{"0", "4", "*", "*", "0"},
		},
		{
			name:     "tabs",
			input:    "0\t4\t*\t*\t0",
			expected: []string{"0", "4", "*", "*", "0"},
		},
		{
			name:     "empty string",
			input:    "",
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := splitCronExpression(tt.input)
			if len(result) != len(tt.expected) {
				t.Errorf("splitCronExpression(%q) length = %d, want %d", tt.input, len(result), len(tt.expected))
				return
			}

			for i := range result {
				if result[i] != tt.expected[i] {
					t.Errorf("splitCronExpression(%q)[%d] = %q, want %q", tt.input, i, result[i], tt.expected[i])
				}
			}
		})
	}
}

func TestGetWeekdayName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input    string
		expected string
	}{
		{"0", "Sunday"},
		{"1", "Monday"},
		{"2", "Tuesday"},
		{"3", "Wednesday"},
		{"4", "Thursday"},
		{"5", "Friday"},
		{"6", "Saturday"},
		{"7", "Sunday"},
		{"invalid", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()

			result := getWeekdayName(tt.input)
			if result != tt.expected {
				t.Errorf("getWeekdayName(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// newTestScheme creates a scheme with required types registered.
func newTestScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	_ = mck8slexlav1alpha1.AddToScheme(scheme)

	return scheme
}

// newTestServer creates a Server with fake client for testing.
func newTestServer(objs ...client.Object) *Server {
	scheme := newTestScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).WithStatusSubresource(objs...).Build()

	return &Server{
		client:    fakeClient,
		namespace: "default",
		sse:       NewSSEBroker(),
	}
}

func TestHandlePluginListShowsAllPlugins(t *testing.T) {
	t.Parallel()

	plugin1 := &mck8slexlav1alpha1.Plugin{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "essentialsx",
			Namespace: "default",
		},
		Spec: mck8slexlav1alpha1.PluginSpec{
			Source: mck8slexlav1alpha1.PluginSource{
				Type:    "hangar",
				Project: "EssentialsX",
			},
			UpdateStrategy: "latest",
		},
	}
	plugin2 := &mck8slexlav1alpha1.Plugin{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "bluemap",
			Namespace: "default",
		},
		Spec: mck8slexlav1alpha1.PluginSpec{
			Source: mck8slexlav1alpha1.PluginSource{
				Type:    "hangar",
				Project: "BlueMap",
			},
			UpdateStrategy: "pinned",
			Version:        "5.4",
		},
	}

	srv := newTestServer(plugin1, plugin2)

	req := httptest.NewRequest(http.MethodGet, "/ui/plugins", nil)
	w := httptest.NewRecorder()

	srv.handlePluginList(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "essentialsx") {
		t.Error("expected response to contain 'essentialsx'")
	}
	if !strings.Contains(body, "bluemap") {
		t.Error("expected response to contain 'bluemap'")
	}
}

func TestHandlePluginCreateGetShowsForm(t *testing.T) {
	t.Parallel()

	srv := newTestServer()

	req := httptest.NewRequest(http.MethodGet, "/ui/plugin/new", nil)
	w := httptest.NewRecorder()

	srv.handlePluginCreate(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "form") {
		t.Error("expected response to contain a form")
	}
}

func TestHandlePluginCreatePostCreatesPlugin(t *testing.T) {
	t.Parallel()

	srv := newTestServer()

	form := url.Values{}
	form.Set("name", "test-plugin")
	form.Set("namespace", "default")
	form.Set("sourceType", "hangar")
	form.Set("project", "TestProject")
	form.Set("updateStrategy", "latest")

	req := httptest.NewRequest(http.MethodPost, "/ui/plugin/new", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	srv.handlePluginCreate(w, req)

	// Should redirect after successful creation
	if w.Code != http.StatusSeeOther && w.Code != http.StatusFound {
		t.Errorf("expected redirect status, got %d", w.Code)
	}

	// Verify plugin was created
	var plugin mck8slexlav1alpha1.Plugin
	err := srv.client.Get(context.Background(), client.ObjectKey{
		Name:      "test-plugin",
		Namespace: "default",
	}, &plugin)
	if err != nil {
		t.Errorf("failed to get created plugin: %v", err)
	}
	if plugin.Spec.Source.Project != "TestProject" {
		t.Errorf("expected project 'TestProject', got %q", plugin.Spec.Source.Project)
	}
}

func TestHandlePluginCreateValidatesForm(t *testing.T) {
	t.Parallel()

	srv := newTestServer()

	// Missing required fields
	form := url.Values{}
	form.Set("name", "")
	form.Set("namespace", "default")

	req := httptest.NewRequest(http.MethodPost, "/ui/plugin/new", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	srv.handlePluginCreate(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d for invalid form, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestHandlePluginDeleteRemovesPlugin(t *testing.T) {
	t.Parallel()

	plugin := &mck8slexlav1alpha1.Plugin{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "to-delete",
			Namespace: "default",
		},
		Spec: mck8slexlav1alpha1.PluginSpec{
			Source: mck8slexlav1alpha1.PluginSource{
				Type:    "hangar",
				Project: "DeleteMe",
			},
			UpdateStrategy: "latest",
		},
	}

	srv := newTestServer(plugin)

	req := httptest.NewRequest(http.MethodPost, "/ui/plugin/to-delete/delete?namespace=default", nil)
	w := httptest.NewRecorder()

	srv.handlePluginDelete(w, req)

	// Should redirect after successful deletion
	if w.Code != http.StatusSeeOther && w.Code != http.StatusFound {
		t.Errorf("expected redirect status, got %d", w.Code)
	}

	// Verify plugin was deleted
	var deletedPlugin mck8slexlav1alpha1.Plugin
	err := srv.client.Get(context.Background(), client.ObjectKey{
		Name:      "to-delete",
		Namespace: "default",
	}, &deletedPlugin)
	if err == nil {
		t.Error("expected plugin to be deleted, but it still exists")
	}
}

func TestHandleApplyNowSetsAnnotation(t *testing.T) {
	t.Parallel()

	server := &mck8slexlav1alpha1.PaperMCServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-server",
			Namespace: "default",
		},
		Spec: mck8slexlav1alpha1.PaperMCServerSpec{
			UpdateStrategy: "auto",
		},
	}

	srv := newTestServer(server)

	req := httptest.NewRequest(http.MethodPost, "/ui/server/test-server/apply-now?namespace=default", nil)
	w := httptest.NewRecorder()

	srv.handleApplyNow(w, req)

	if w.Code != http.StatusOK && w.Code != http.StatusSeeOther {
		t.Errorf("expected success status, got %d", w.Code)
	}

	// Verify annotation was set
	var updatedServer mck8slexlav1alpha1.PaperMCServer
	err := srv.client.Get(context.Background(), client.ObjectKey{
		Name:      "test-server",
		Namespace: "default",
	}, &updatedServer)
	if err != nil {
		t.Fatalf("failed to get server: %v", err)
	}

	annotation, ok := updatedServer.Annotations[controller.AnnotationApplyNow]
	if !ok {
		t.Error("expected apply-now annotation to be set")
	}

	// Verify timestamp is recent (within last minute)
	timestamp, err := time.Parse(time.RFC3339, annotation)
	if err != nil {
		// Try Unix timestamp format
		var unixTs int64
		if _, parseErr := fmt.Sscanf(annotation, "%d", &unixTs); parseErr != nil {
			t.Errorf("failed to parse annotation timestamp: %v", err)
		} else {
			timestamp = time.Unix(unixTs, 0)
		}
	}

	if time.Since(timestamp) > time.Minute {
		t.Error("expected annotation timestamp to be recent")
	}
}

func TestHandleServerDeleteRemovesServer(t *testing.T) {
	t.Parallel()

	server := &mck8slexlav1alpha1.PaperMCServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "server-to-delete",
			Namespace: "default",
		},
		Spec: mck8slexlav1alpha1.PaperMCServerSpec{
			UpdateStrategy: "auto",
		},
	}

	srv := newTestServer(server)

	req := httptest.NewRequest(http.MethodPost, "/ui/server/server-to-delete/delete?namespace=default", nil)
	w := httptest.NewRecorder()

	srv.handleServerDelete(w, req)

	// Should redirect after successful deletion
	if w.Code != http.StatusSeeOther && w.Code != http.StatusFound {
		t.Errorf("expected redirect status, got %d", w.Code)
	}

	// Verify server was deleted
	var deletedServer mck8slexlav1alpha1.PaperMCServer
	err := srv.client.Get(context.Background(), client.ObjectKey{
		Name:      "server-to-delete",
		Namespace: "default",
	}, &deletedServer)
	if err == nil {
		t.Error("expected server to be deleted, but it still exists")
	}
}
