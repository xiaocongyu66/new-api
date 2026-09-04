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
		"identity.StartAuthArtifactCleanup":    "expired dashboard sessions and one-time auth flows are never deleted",
		"catalog.InitChannelModelHealthCache":  "persisted per-model route isolation is not restored, so a quarantined route silently returns to rotation on restart",
		"usage.Init":                           "perf metric hot buckets are never flushed to perf_metrics, so the dashboard stays empty and memory grows",
		"relaycommon.InitTokenEncoders":        "defaultTokenEncoder stays nil, so an unsupported OpenAI text model nil-panics inside CountTextToken",
		"task.GetTaskProviderFuncBinding":      "task.GetTaskProviderFunc stays nil, so RunTaskPollingOnce returns immediately (no async task ever completes) and the video proxy nil-panics",
		"relay.GetTaskAdaptor":                 "the task adaptor factory resolves nothing, so polling and video proxying reach no adaptor",
	}

	calls := parseCalls(t, "main.go")
	for call, why := range required {
		if !calls[call] {
			t.Errorf("main.go no longer calls %s() at startup: %s", call, why)
		}
	}

	// The two task wirings above live inside wireTaskAdaptorFactory, so their
	// presence in the file means nothing unless main() still calls it.
	if !calls["wireTaskAdaptorFactory"] {
		t.Error("main() no longer calls wireTaskAdaptorFactory(): the task adaptor factory and provider port are both left nil")
	}
}

// parseCalls collects every call expression in the file, keyed as "pkg.Func" for
// qualified calls and "Func" for calls to functions in this package.
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
		switch fun := call.Fun.(type) {
		case *ast.SelectorExpr:
			if pkg, ok := fun.X.(*ast.Ident); ok {
				found[pkg.Name+"."+fun.Sel.Name] = true
			}
		case *ast.Ident:
			found[fun.Name] = true
		}
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
// production (relaycommon vs usage both export InitTokenEncoders).
func TestBootstrapImportAliases(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "main.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse main.go: %v", err)
	}
	want := map[string]string{
		"relaycommon": "github.com/QuantumNous/new-api/internal/relay/common",
		"usage":       "github.com/QuantumNous/new-api/internal/usage",
		"sensitive":   "github.com/QuantumNous/new-api/internal/sensitive",
		"identity":    "github.com/QuantumNous/new-api/internal/identity",
		"catalog":     "github.com/QuantumNous/new-api/internal/catalog",
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
