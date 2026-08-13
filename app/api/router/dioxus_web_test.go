package router

import (
	"embed"
	"testing"
	"io/fs"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//go:embed web/dist
var dioxusWebTestFS embed.FS

func TestDioxusIsEmbedded(t *testing.T) {
	sub, err := fs.Sub(dioxusWebTestFS, "web/dist/dioxus")
	require.NoError(t, err)
	entries, err := fs.ReadDir(sub, ".")
	require.NoError(t, err)
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	assert.Contains(t, names, "index.html")
}
