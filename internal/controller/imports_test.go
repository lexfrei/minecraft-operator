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
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNoTestutilImportInProductionCode verifies that production code does not
// import the pkg/testutil package.
//
// Regression: update_controller.go and cmd/main.go used to import pkg/testutil
// in production code. The CronScheduler interface and RealCronScheduler wrapper
// live in pkg/testutil, which is a test-only package by convention.
// Production code should not depend on test utilities.
func TestNoTestutilImportInProductionCode(t *testing.T) {
	// Walk all Go files in the controller package directory
	controllerDir := "."
	entries, err := os.ReadDir(controllerDir)
	require.NoError(t, err)

	fset := token.NewFileSet()

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !strings.HasSuffix(name, ".go") {
			continue
		}

		// Skip test files — they are allowed to import testutil
		if strings.HasSuffix(name, "_test.go") {
			continue
		}

		filePath := filepath.Join(controllerDir, name)

		f, parseErr := parser.ParseFile(fset, filePath, nil, parser.ImportsOnly)
		require.NoError(t, parseErr, "Failed to parse %s", name)

		for _, imp := range f.Imports {
			importPath := strings.Trim(imp.Path.Value, "\"")
			assert.NotContains(t, importPath, "pkg/testutil",
				"Production file %s imports pkg/testutil. "+
					"CronScheduler interface and RealCronScheduler should be moved "+
					"out of the testutil package into a production package "+
					"(e.g., pkg/cron or internal/cron).",
				name)
		}
	}

	// Also check cmd/main.go
	mainGoPath := filepath.Join("..", "..", "cmd", "main.go")
	if _, err := os.Stat(mainGoPath); err == nil {
		f, parseErr := parser.ParseFile(fset, mainGoPath, nil, parser.ImportsOnly)
		require.NoError(t, parseErr, "Failed to parse cmd/main.go")

		for _, imp := range f.Imports {
			importPath := strings.Trim(imp.Path.Value, "\"")
			assert.NotContains(t, importPath, "pkg/testutil",
				"cmd/main.go imports pkg/testutil. "+
					"Production binary should not depend on test utilities.")
		}
	}
}

// TestCronSchedulerInterfaceLocation verifies that the CronScheduler interface
// is defined in a production package, not in a test utility package.
//
// This is a design-level test: production interfaces should live in production
// packages so that importing them doesn't pull in test dependencies.
func TestCronSchedulerInterfaceLocation(t *testing.T) {
	// The CronScheduler interface is currently in pkg/testutil/cron.go.
	// It should be in a production package (e.g., pkg/cron/).

	// Check if CronScheduler is defined in testutil
	testutilDir := filepath.Join("..", "..", "pkg", "testutil")
	entries, err := os.ReadDir(testutilDir)
	require.NoError(t, err, "pkg/testutil directory should exist")

	fset := token.NewFileSet()
	foundInTestutil := false

	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		filePath := filepath.Join(testutilDir, name)

		f, parseErr := parser.ParseFile(fset, filePath, nil, 0)
		require.NoError(t, parseErr, "Failed to parse %s", name)

		for _, decl := range f.Decls {
			genDecl, ok := decl.(*ast.GenDecl)
			if !ok {
				continue
			}

			for _, spec := range genDecl.Specs {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}

				if typeSpec.Name.Name == "CronScheduler" {
					foundInTestutil = true
				}
			}
		}
	}

	assert.False(t, foundInTestutil,
		"CronScheduler interface is defined in pkg/testutil/cron.go. "+
			"Production interfaces should live in production packages, not test utilities. "+
			"Move CronScheduler and RealCronScheduler to a dedicated package (e.g., pkg/cron/).")
}
