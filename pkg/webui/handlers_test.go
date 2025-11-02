package webui

import (
	"testing"

	mck8slexlav1alpha1 "github.com/lexfrei/minecraft-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
