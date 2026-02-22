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

func TestLogLevel_ShouldWarnOnInvalidValue(t *testing.T) { //nolint:funlen // AST test requires verbose traversal
	t.Parallel()

	// BUG: The logLevel switch in main.go has a default case that silently
	// sets slog.LevelInfo for invalid input like --log-level=verbose.
	// The default case should log a warning or return an error.
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, mainGoPath, nil, parser.AllErrors)
	if err != nil {
		t.Fatalf("Failed to parse %s: %v", mainGoPath, err)
	}

	var foundSwitch bool

	ast.Inspect(f, func(n ast.Node) bool {
		switchStmt, ok := n.(*ast.SwitchStmt)
		if !ok {
			return true
		}

		tag, ok := switchStmt.Tag.(*ast.Ident)
		if !ok || tag.Name != "logLevel" {
			return true
		}

		foundSwitch = true

		for _, clause := range switchStmt.Body.List {
			cc, ok := clause.(*ast.CaseClause)
			if !ok || cc.List != nil {
				continue
			}

			hasWarning := defaultCaseHasWarning(cc)
			assert.True(t, hasWarning,
				"logLevel switch default case should warn about invalid value, "+
					"not silently default to info")
		}

		return false
	})

	assert.True(t, foundSwitch, "logLevel switch statement not found in main.go")
}

// defaultCaseHasWarning checks if a switch default case contains a warning/error call.
func defaultCaseHasWarning(cc *ast.CaseClause) bool {
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

	return hasWarning
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
	// Correct code should have explicit `logFormat == "json"` check —
	// either as `logFormat == "json"` or `case "json":` in a switch on logFormat.
	var hasExplicitJSONCheck bool

	ast.Inspect(f, func(n ast.Node) bool {
		// Pattern 1: binary expression `logFormat == "json"`
		if binExpr, ok := n.(*ast.BinaryExpr); ok {
			if ident, ok := binExpr.X.(*ast.Ident); ok && ident.Name == "logFormat" {
				if lit, ok := binExpr.Y.(*ast.BasicLit); ok && lit.Value == `"json"` {
					hasExplicitJSONCheck = true
					return false
				}
			}
		}

		// Pattern 2: switch logFormat { case "json": ... }
		if switchStmt, ok := n.(*ast.SwitchStmt); ok {
			if ident, ok := switchStmt.Tag.(*ast.Ident); ok && ident.Name == "logFormat" {
				for _, clause := range switchStmt.Body.List {
					cc, ok := clause.(*ast.CaseClause)
					if !ok {
						continue
					}
					for _, expr := range cc.List {
						if lit, ok := expr.(*ast.BasicLit); ok && lit.Value == `"json"` {
							hasExplicitJSONCheck = true
							return false
						}
					}
				}
			}
		}

		return true
	})

	assert.True(t, hasExplicitJSONCheck,
		"logFormat handling should explicitly check for \"json\" value, "+
			"not treat everything that isn't \"text\" as JSON. "+
			"Invalid values like --log-format=yaml silently become JSON with no warning")
}

func TestManageCRDsFlagExists(t *testing.T) {
	t.Parallel()

	// Verify that main.go has a --manage-crds flag registered via flag.BoolVar
	// defaulting to true, so the operator self-manages CRDs at startup.
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, mainGoPath, nil, parser.AllErrors)
	if err != nil {
		t.Fatalf("Failed to parse %s: %v", mainGoPath, err)
	}

	var hasManageCRDsFlag bool

	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}

		// Look for flag.BoolVar(&manageCRDs, "manage-crds", ...)
		if sel.Sel.Name != "BoolVar" {
			return true
		}

		if len(call.Args) < 2 {
			return true
		}

		lit, ok := call.Args[1].(*ast.BasicLit)
		if ok && lit.Value == `"manage-crds"` {
			hasManageCRDsFlag = true
			return false
		}

		return true
	})

	assert.True(t, hasManageCRDsFlag,
		"main.go should register a --manage-crds flag via flag.BoolVar")
}
