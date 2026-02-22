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

package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/stretchr/testify/assert"
)

const mainGoPath = "main.go"

func TestLogLevel_ShouldWarnOnInvalidValue(t *testing.T) {
	t.Parallel()

	// BUG: The logLevel switch in main.go has a default case that silently
	// sets slog.LevelInfo for invalid input like --log-level=verbose.
	// The user gets no feedback that their requested level was ignored.
	// The default case should log a warning or return an error.
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, mainGoPath, nil, parser.AllErrors)
	if err != nil {
		t.Fatalf("Failed to parse %s: %v", mainGoPath, err)
	}

	var foundSwitch bool

	ast.Inspect(f, func(n ast.Node) bool {
		// Find switch statement on logLevel
		switchStmt, ok := n.(*ast.SwitchStmt)
		if !ok {
			return true
		}

		// Check if this switch is on logLevel
		tag, ok := switchStmt.Tag.(*ast.Ident)
		if !ok || tag.Name != "logLevel" {
			return true
		}

		foundSwitch = true

		// Find the default case
		for _, clause := range switchStmt.Body.List {
			cc, ok := clause.(*ast.CaseClause)
			if !ok || cc.List != nil { // non-nil List = not default
				continue
			}

			// This is the default case. It should contain a warning/error/fatal call,
			// not just a silent assignment.
			hasWarning := false

			for _, stmt := range cc.Body {
				ast.Inspect(stmt, func(inner ast.Node) bool {
					call, ok := inner.(*ast.CallExpr)
					if !ok {
						return true
					}

					sel, ok := call.Fun.(*ast.SelectorExpr)
					if !ok {
						return true
					}

					// Accept: slog.Warn*, log.Fatal*, fmt.Fprint* (to stderr), os.Exit
					name := sel.Sel.Name
					if name == "WarnContext" || name == "Warn" ||
						name == "Fatalf" || name == "Fatal" ||
						name == "Fprintf" || name == "Exit" {
						hasWarning = true
						return false
					}

					return true
				})
			}

			assert.True(t, hasWarning,
				"logLevel switch default case should warn about invalid value, "+
					"not silently default to info. User gets no feedback for --log-level=verbose")
		}

		return false
	})

	assert.True(t, foundSwitch, "logLevel switch statement not found in main.go")
}

func TestLogFormat_ShouldValidateExplicitly(t *testing.T) {
	t.Parallel()

	// BUG: logFormat handling in main.go uses if/else: text → TextHandler,
	// anything else → JSONHandler. Invalid values like --log-format=yaml
	// silently fall through to JSON with no warning.
	// Should explicitly check for "json" and reject unknown formats.
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, mainGoPath, nil, parser.AllErrors)
	if err != nil {
		t.Fatalf("Failed to parse %s: %v", mainGoPath, err)
	}

	// Search for explicit "json" string comparison in logFormat handling.
	// If the code checks `logFormat == "text"` and uses else for JSON,
	// then any typo like "yaml" silently becomes JSON.
	// Correct code should have explicit `logFormat == "json"` check.
	var hasExplicitJSONCheck bool

	ast.Inspect(f, func(n ast.Node) bool {
		// Look for binary expressions comparing logFormat to "json"
		binExpr, ok := n.(*ast.BinaryExpr)
		if !ok {
			return true
		}

		ident, ok := binExpr.X.(*ast.Ident)
		if !ok || ident.Name != "logFormat" {
			return true
		}

		lit, ok := binExpr.Y.(*ast.BasicLit)
		if !ok {
			return true
		}

		if lit.Value == `"json"` {
			hasExplicitJSONCheck = true
			return false
		}

		return true
	})

	assert.True(t, hasExplicitJSONCheck,
		"logFormat handling should explicitly check for \"json\" value, "+
			"not treat everything that isn't \"text\" as JSON. "+
			"Invalid values like --log-format=yaml silently become JSON with no warning")
}
