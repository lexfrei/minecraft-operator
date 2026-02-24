/*
Copyright 2026, Aleksei Sviridkin.

SPDX-License-Identifier: BSD-3-Clause
*/

package service

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// --- FormatVersionWithBuild tests ---

func TestFormatVersionWithBuild_WithBuild(t *testing.T) {
	t.Parallel()

	result := FormatVersionWithBuild("1.21.1", 91)
	assert.Equal(t, "1.21.1-91", result)
}

func TestFormatVersionWithBuild_NoBuild(t *testing.T) {
	t.Parallel()

	result := FormatVersionWithBuild("1.21.1", 0)
	assert.Equal(t, "1.21.1", result)
}

func TestFormatVersionWithBuild_Empty(t *testing.T) {
	t.Parallel()

	result := FormatVersionWithBuild("", 0)
	assert.Equal(t, "Unknown", result)
}

func TestFormatVersionWithBuild_EmptyWithBuild(t *testing.T) {
	t.Parallel()

	result := FormatVersionWithBuild("", 91)
	assert.Equal(t, "Unknown", result)
}

func TestFormatVersionWithBuild_NegativeBuild(t *testing.T) {
	t.Parallel()

	// Negative builds should be treated as no build
	result := FormatVersionWithBuild("1.21.1", -1)
	assert.Equal(t, "1.21.1", result)
}

// --- FormatCronSchedule tests ---

func TestFormatCronSchedule_Empty(t *testing.T) {
	t.Parallel()

	result := FormatCronSchedule("")
	assert.Equal(t, "Not scheduled", result)
}

func TestFormatCronSchedule_DailyAtMidnight(t *testing.T) {
	t.Parallel()

	result := FormatCronSchedule("0 0 * * *")
	assert.Equal(t, "Daily at 0:00", result)
}

func TestFormatCronSchedule_DailyAt4AM(t *testing.T) {
	t.Parallel()

	result := FormatCronSchedule("0 4 * * *")
	assert.Equal(t, "Daily at 4:00", result)
}

func TestFormatCronSchedule_DailyWithMinutes(t *testing.T) {
	t.Parallel()

	result := FormatCronSchedule("30 4 * * *")
	assert.Equal(t, "Daily at 4:30", result)
}

func TestFormatCronSchedule_WeeklySunday(t *testing.T) {
	t.Parallel()

	result := FormatCronSchedule("0 4 * * 0")
	assert.Equal(t, "Every Sunday at 4:00", result)
}

func TestFormatCronSchedule_WeeklyMonday(t *testing.T) {
	t.Parallel()

	result := FormatCronSchedule("0 9 * * 1")
	assert.Equal(t, "Every Monday at 9:00", result)
}

func TestFormatCronSchedule_WeeklySunday7(t *testing.T) {
	t.Parallel()

	// Sunday can be 0 or 7 in cron
	result := FormatCronSchedule("0 4 * * 7")
	assert.Equal(t, "Every Sunday at 4:00", result)
}

func TestFormatCronSchedule_WeeklyWithMinutes(t *testing.T) {
	t.Parallel()

	result := FormatCronSchedule("30 9 * * 5")
	assert.Equal(t, "Every Friday at 9:30", result)
}

func TestFormatCronSchedule_Complex(t *testing.T) {
	t.Parallel()

	// Specific day of month
	result := FormatCronSchedule("0 4 15 * *")
	assert.Equal(t, "Cron: 0 4 15 * *", result)
}

func TestFormatCronSchedule_InvalidFormat(t *testing.T) {
	t.Parallel()

	// Too few parts
	result := FormatCronSchedule("0 4 *")
	assert.Equal(t, "Cron: 0 4 *", result)
}

func TestFormatCronSchedule_WhitespaceHandling(t *testing.T) {
	t.Parallel()

	// Multiple spaces and tabs
	result := FormatCronSchedule("0  4 	* * *")
	assert.Equal(t, "Daily at 4:00", result)
}

// --- FormatTimestamp tests ---

func TestFormatTimestamp_ValidTime(t *testing.T) {
	t.Parallel()

	ts := metav1.NewTime(time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC))
	result := FormatTimestamp(ts)
	assert.Contains(t, result, "15 Jun 2025")
	assert.Contains(t, result, "10:30")
}

func TestFormatTimestamp_ZeroTime(t *testing.T) {
	t.Parallel()

	result := FormatTimestamp(metav1.Time{})
	assert.Equal(t, "Never", result)
}

// --- FormatTime tests ---

func TestFormatTime_ValidTime(t *testing.T) {
	t.Parallel()

	ts := time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC)
	result := FormatTime(ts)
	assert.Contains(t, result, "15 Jun 2025")
	assert.Contains(t, result, "10:30")
}

func TestFormatTime_ZeroTime(t *testing.T) {
	t.Parallel()

	result := FormatTime(time.Time{})
	assert.Equal(t, "Never", result)
}

// --- FormatPluginCompatibility tests ---

func TestFormatPluginCompatibility_Compatible(t *testing.T) {
	t.Parallel()

	result := FormatPluginCompatibility(true)
	assert.Equal(t, "compatible", result)
}

func TestFormatPluginCompatibility_Incompatible(t *testing.T) {
	t.Parallel()

	result := FormatPluginCompatibility(false)
	assert.Equal(t, "incompatible", result)
}

// --- IsValidKubernetesName tests ---

func TestIsValidKubernetesName_Valid(t *testing.T) {
	t.Parallel()

	testCases := []string{
		"my-server",
		"server1",
		"a",
		"test-server-1",
		"mc.server.example",
		"123",
		"a1b2c3",
	}

	for _, name := range testCases {
		assert.True(t, IsValidKubernetesName(name), "expected valid: %s", name)
	}
}

func TestIsValidKubernetesName_Empty(t *testing.T) {
	t.Parallel()

	assert.False(t, IsValidKubernetesName(""))
}

func TestIsValidKubernetesName_TooLong(t *testing.T) {
	t.Parallel()

	// 254 characters - exceeds max 253
	longName := strings.Repeat("a", 254)
	assert.False(t, IsValidKubernetesName(longName))
}

func TestIsValidKubernetesName_MaxLength(t *testing.T) {
	t.Parallel()

	// 253 characters - exactly at max
	maxName := strings.Repeat("a", 253)
	assert.True(t, IsValidKubernetesName(maxName))
}

func TestIsValidKubernetesName_InvalidChars(t *testing.T) {
	t.Parallel()

	testCases := []string{
		"My_Server",  // underscore
		"my server",  // space
		"server@1",   // special char
		"UPPERCASE",  // uppercase
		"my:server",  // colon
		"my/server",  // slash
		"server!",    // exclamation
		"test\nname", // newline
	}

	for _, name := range testCases {
		assert.False(t, IsValidKubernetesName(name), "expected invalid: %s", name)
	}
}

func TestIsValidKubernetesName_InvalidStart(t *testing.T) {
	t.Parallel()

	testCases := []string{
		"-server",
		".server",
	}

	for _, name := range testCases {
		assert.False(t, IsValidKubernetesName(name), "expected invalid start: %s", name)
	}
}

func TestIsValidKubernetesName_InvalidEnd(t *testing.T) {
	t.Parallel()

	testCases := []string{
		"server-",
		"server.",
	}

	for _, name := range testCases {
		assert.False(t, IsValidKubernetesName(name), "expected invalid end: %s", name)
	}
}

// --- splitCronExpression tests (internal helper) ---

func TestSplitCronExpression_Valid(t *testing.T) {
	t.Parallel()

	parts := splitCronExpression("0 4 * * *")
	assert.Len(t, parts, 5)
	assert.Equal(t, "0", parts[0])
	assert.Equal(t, "4", parts[1])
	assert.Equal(t, "*", parts[2])
	assert.Equal(t, "*", parts[3])
	assert.Equal(t, "*", parts[4])
}

func TestSplitCronExpression_ExtraSpaces(t *testing.T) {
	t.Parallel()

	parts := splitCronExpression("0  4   *  *  *")
	assert.Len(t, parts, 5)
}

func TestSplitCronExpression_Tabs(t *testing.T) {
	t.Parallel()

	parts := splitCronExpression("0\t4\t*\t*\t*")
	assert.Len(t, parts, 5)
}

// --- getWeekdayName tests (internal helper) ---

func TestGetWeekdayName_AllDays(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "Sunday", getWeekdayName("0"))
	assert.Equal(t, "Monday", getWeekdayName("1"))
	assert.Equal(t, "Tuesday", getWeekdayName("2"))
	assert.Equal(t, "Wednesday", getWeekdayName("3"))
	assert.Equal(t, "Thursday", getWeekdayName("4"))
	assert.Equal(t, "Friday", getWeekdayName("5"))
	assert.Equal(t, "Saturday", getWeekdayName("6"))
	assert.Equal(t, "Sunday", getWeekdayName("7"))
}

func TestGetWeekdayName_Invalid(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "", getWeekdayName("8"))
	assert.Equal(t, "", getWeekdayName("invalid"))
	assert.Equal(t, "", getWeekdayName(""))
}

// --- isAlphanumeric tests (internal helper) ---

func TestIsAlphanumeric_Valid(t *testing.T) {
	t.Parallel()

	// Lowercase letters
	for ch := 'a'; ch <= 'z'; ch++ {
		assert.True(t, isAlphanumeric(ch), "expected alphanumeric: %c", ch)
	}
	// Digits
	for ch := '0'; ch <= '9'; ch++ {
		assert.True(t, isAlphanumeric(ch), "expected alphanumeric: %c", ch)
	}
}

func TestIsAlphanumeric_Invalid(t *testing.T) {
	t.Parallel()

	testCases := []rune{'A', 'Z', '-', '.', '_', ' ', '!', '@'}
	for _, ch := range testCases {
		assert.False(t, isAlphanumeric(ch), "expected non-alphanumeric: %c", ch)
	}
}
