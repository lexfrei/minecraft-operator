package service

import (
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// FormatVersionWithBuild formats a version with build number.
func FormatVersionWithBuild(version string, build int) string {
	if version == "" {
		return "Unknown"
	}
	if build > 0 {
		return fmt.Sprintf("%s-%d", version, build)
	}
	return version
}

// FormatCronSchedule formats a cron schedule for display.
func FormatCronSchedule(cronExpr string) string {
	if cronExpr == "" {
		return "Not scheduled"
	}

	// Parse common cron patterns into human-readable format
	// Format: minute hour day month weekday
	parts := splitCronExpression(cronExpr)
	if len(parts) != 5 {
		return fmt.Sprintf("Cron: %s", cronExpr)
	}

	minute, hour, day, month, weekday := parts[0], parts[1], parts[2], parts[3], parts[4]

	// Daily patterns: "* * * * *" where day and month are wildcards
	if day == "*" && month == "*" && weekday == "*" {
		if minute == "0" && hour != "*" {
			return fmt.Sprintf("Daily at %s:00", hour)
		}
		return fmt.Sprintf("Daily at %s:%s", hour, minute)
	}

	// Weekly patterns: specific weekday
	if day == "*" && month == "*" && weekday != "*" {
		weekdayName := getWeekdayName(weekday)
		if weekdayName != "" {
			if minute == "0" {
				return fmt.Sprintf("Every %s at %s:00", weekdayName, hour)
			}
			return fmt.Sprintf("Every %s at %s:%s", weekdayName, hour, minute)
		}
	}

	// Fallback for complex expressions
	return fmt.Sprintf("Cron: %s", cronExpr)
}

// FormatTimestamp formats a metav1.Time for display.
func FormatTimestamp(t metav1.Time) string {
	if t.IsZero() {
		return "Never"
	}
	return t.Format(time.RFC1123)
}

// FormatTime formats a time.Time for display.
func FormatTime(t time.Time) string {
	if t.IsZero() {
		return "Never"
	}
	return t.Format(time.RFC1123)
}

// FormatPluginCompatibility formats plugin compatibility status.
func FormatPluginCompatibility(compatible bool) string {
	if compatible {
		return "compatible"
	}
	return "incompatible"
}

// IsValidKubernetesName checks if a name is valid for Kubernetes resources.
func IsValidKubernetesName(name string) bool {
	if name == "" || len(name) > 253 {
		return false
	}

	// Must start and end with alphanumeric
	if !isAlphanumeric(rune(name[0])) || !isAlphanumeric(rune(name[len(name)-1])) {
		return false
	}

	// Can only contain alphanumeric, '-', and '.'
	for _, ch := range name {
		if !isAlphanumeric(ch) && ch != '-' && ch != '.' {
			return false
		}
	}

	return true
}

// splitCronExpression splits a cron expression into its components.
func splitCronExpression(expr string) []string {
	parts := make([]string, 0, 5)
	current := ""
	for _, ch := range expr {
		if ch == ' ' || ch == '\t' {
			if current != "" {
				parts = append(parts, current)
				current = ""
			}
		} else {
			current += string(ch)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}
	return parts
}

// getWeekdayName returns the human-readable weekday name for a cron weekday value.
func getWeekdayName(weekday string) string {
	weekdays := map[string]string{
		"0": "Sunday",
		"1": "Monday",
		"2": "Tuesday",
		"3": "Wednesday",
		"4": "Thursday",
		"5": "Friday",
		"6": "Saturday",
		"7": "Sunday", // Sunday can be 0 or 7 in cron
	}
	return weekdays[weekday]
}

// isAlphanumeric checks if a rune is alphanumeric.
func isAlphanumeric(ch rune) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9')
}
