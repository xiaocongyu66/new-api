package middleware

import "github.com/QuantumNous/new-api/internal/transport/contract"

func DisableCache() contract.Middleware {
	return func(c contract.Context) {
		c.SetHeader("Cache-Control", "no-store, no-cache, must-revalidate, private, max-age=0")
		c.SetHeader("Pragma", "no-cache")
		c.SetHeader("Expires", "0")
		c.Next()
	}
}
