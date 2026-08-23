package middleware

import (
	"github.com/QuantumNous/new-api/internal/transport/contract"
)

func Cache() func(c contract.Context) {
	return func(c contract.Context) {
		if c.RequestURI() == "/" {
			c.SetHeader("Cache-Control", "no-cache")
		} else {
			c.SetHeader("Cache-Control", "max-age=604800") // one week
		}
		c.SetHeader("Cache-Version", "b688f2fb5be447c25e5aa3bd063087a83db32a288bf6a4f35f2d8db310e40b14")
		c.Next()
	}
}
