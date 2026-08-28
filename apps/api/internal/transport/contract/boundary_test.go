package contract_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestContractPackageDoesNotImportHTTPFramework enforces the boundary rule the
// transport layer exists to create: business code depends on this contract, so
// if the contract itself imported an HTTP framework the abstraction would be
// worthless and swapping frameworks would again touch every caller.
func TestContractPackageDoesNotImportHTTPFramework(t *testing.T) {
	entries, err := os.ReadDir(".")
	require.NoError(t, err)

	forbidden := []string{
		"github.com/gin-gonic/gin",
		"github.com/gofiber/fiber",
		"github.com/valyala/fasthttp",
	}

	inspected := 0
	fileSet := token.NewFileSet()
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		if strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}

		parsed, err := parser.ParseFile(fileSet, filepath.Join(".", entry.Name()), nil, parser.ImportsOnly)
		require.NoError(t, err)
		inspected++

		for _, spec := range parsed.Imports {
			path := strings.Trim(spec.Path.Value, `"`)
			for _, banned := range forbidden {
				assert.False(t,
					strings.HasPrefix(path, banned),
					"%s imports %s; the transport contract must stay framework-neutral",
					entry.Name(), path,
				)
			}
		}
	}

	require.NotZero(t, inspected, "no contract source files were inspected")
}

// TestContractPackageImportsOnlyStandardLibrary asserts the contract depends on
// nothing but the standard library, keeping it importable from any layer without
// creating a dependency cycle.
func TestContractPackageImportsOnlyStandardLibrary(t *testing.T) {
	entries, err := os.ReadDir(".")
	require.NoError(t, err)

	fileSet := token.NewFileSet()
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}

		parsed, err := parser.ParseFile(fileSet, filepath.Join(".", entry.Name()), nil, parser.ImportsOnly)
		require.NoError(t, err)

		for _, spec := range parsed.Imports {
			path := strings.Trim(spec.Path.Value, `"`)
			firstSegment := strings.SplitN(path, "/", 2)[0]
			assert.False(t,
				strings.Contains(firstSegment, "."),
				"%s imports external module %s; the contract must depend only on the standard library",
				entry.Name(), path,
			)
		}
	}
}
