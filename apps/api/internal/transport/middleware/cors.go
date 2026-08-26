package middleware

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

// CORS wraps gin-contrib/cors, which produces a gin handler directly. It stays
// gin-typed because a framework swap replaces the CORS library too.
func CORS() gin.HandlerFunc {
	config := cors.DefaultConfig()
	config.AllowAllOrigins = true
	config.AllowCredentials = true
	config.AllowMethods = []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"}
	config.AllowHeaders = []string{"*"}
	return cors.New(config)
}

func Version() contract.Middleware {
	return func(c contract.Context) {
		c.SetHeader("X-New-Api-Version", common.Version)
		c.Next()
	}
}
