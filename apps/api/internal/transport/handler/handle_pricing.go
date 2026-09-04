package handler

import (
	"github.com/QuantumNous/new-api/internal/ops"
	"github.com/QuantumNous/new-api/internal/transport/contract"
)

func GetPricing(c contract.Context) {
	ops.GetPricing(c)
}

func ResetModelRatio(c contract.Context) {
	ops.ResetModelRatio(c)
}
