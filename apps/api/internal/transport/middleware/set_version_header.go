package middleware

import (
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/transport/contract"
)

func Version() contract.Middleware {
	return func(c contract.Context) {
		c.SetHeader("X-New-Api-Version", common.Version)
		c.Next()
	}
}
