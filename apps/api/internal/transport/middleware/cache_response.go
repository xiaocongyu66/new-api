package middleware

import (
	"strings"

	"github.com/QuantumNous/new-api/internal/transport/contract"
)

// cacheDirectives maps a URI prefix to its Cache-Control value. Entries are
// checked in order; the first matching prefix wins. "/" is matched exactly by
// RequestURI equality (separate branch below), so this table only carries the
// "/static/" prefix.
var cacheDirectives = []struct {
	prefix    string
	directive string
}{
	{prefix: "/static/", directive: "public, max-age=31536000, immutable"},
}

// hashContentVersion is the cache-busting version stamp for the Cache-Version
// response header. It is intentionally unchanged across all Cache() branches.
const hashContentVersion = "b688f2fb5be447c25e5aa3bd063087a83db32a288bf6a4f35f2d8db310e40b14"

// defaultDirective is the Cache-Control value for routes that match no entry in
// cacheDirectives and are not the root path.
const defaultDirective = "max-age=604800"

func Cache() func(c contract.Context) {
	return func(c contract.Context) {
		uri := c.RequestURI()

		// "/" → no-cache: the landing document must never be cached by clients.
		// "/static/*" → immutable: rsbuild emits hashed filenames, so content
		// changes change the URL; browsers may cache forever.
		// everything else → one week.
		directive := defaultDirective
		if uri == "/" {
			directive = "no-cache"
		} else {
			for _, entry := range cacheDirectives {
				if strings.HasPrefix(uri, entry.prefix) {
					directive = entry.directive
					break
				}
			}
		}

		c.SetHeader("Cache-Control", directive)
		c.SetHeader("Cache-Version", hashContentVersion)
		c.Next()
	}
}
