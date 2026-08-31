package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// Startup calls in main() / InitResources() cannot be exercised at runtime: they
// need a live SQL database, a log database, Redis and IsMasterNode. That is
// exactly why five of them were silently lost in the phase 0 restructure — the
// function moved into its new domain, the bootstrap call did not follow, and
// nothing (compiler, go vet, structural diff, test suite) noticed.
//
// This test pins the bootstrap call set against main's, by parsing main.go. It
// fails if any listed call is removed, which is the only signal available for
// wiring that has no other observable effect until production runs for an hour.
func TestBootstrapStartupCallsPresent(t *testing.T) {
	// Each entry is "pkg.Func" as written in main.go, plus why it matters.
	required := map[string]string{
		"sensitive.StartSensitiveAuditCleanup": "#409 audit rows (type=8) accumulate forever and SensitiveAuditRetentionDays is dead",
	}

	calls := parseCalls(t, "main.go")
	for call, why := range required {
		if !calls[call] {
			t.Errorf("main.go no longer calls %s() at startup: %s", call, why)
		}
	}
}

// parseCalls collects every "pkg.Func" call expression in the file.
func parseCalls(t *testing.T, path string) map[string]bool {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	found := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		found[pkg.Name+"."+sel.Sel.Name] = true
		return true
	})
	if len(found) == 0 {
		t.Fatalf("parsed no calls out of %s; the test is not looking at real source", path)
	}
	// Guard the guard: a typo'd package alias would make every lookup miss.
	if !found["common.InitEnv"] {
		t.Fatalf("sanity check failed: %s does not appear to call common.InitEnv", path)
	}
	return found
}

// The import alias this test asserts on must actually exist, otherwise the
// assertions above could pass against a package that is not the one running in
// production.
func TestBootstrapImportAliases(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "main.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse main.go: %v", err)
	}
	want := map[string]string{
		"sensitive": "github.com/QuantumNous/new-api/internal/sensitive",
	}
	got := map[string]string{}
	for _, spec := range file.Imports {
		path := strings.Trim(spec.Path.Value, `"`)
		name := path[strings.LastIndex(path, "/")+1:]
		if spec.Name != nil {
			name = spec.Name.Name
		}
		got[name] = path
	}
	for alias, path := range want {
		if got[alias] != path {
			t.Errorf("main.go import alias %q resolves to %q, want %q", alias, got[alias], path)
		}
	}
}
