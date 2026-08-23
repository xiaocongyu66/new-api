package middleware

import (
	"github.com/QuantumNous/new-api/internal/transport/contract"

	"github.com/QuantumNous/new-api/common"
)

func RequestId() func(c contract.Context) {
	return func(c contract.Context) {
		id := common.NewRequestId()
		c.Set(common.RequestIdKey, id)
		c.SetContextValue(common.RequestIdKey, id)
		c.SetHeader(common.RequestIdKey, id)
		c.Next()
	}
}
