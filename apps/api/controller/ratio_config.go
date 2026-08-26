package controller

import (
	"github.com/QuantumNous/new-api/internal/ops"
	"github.com/QuantumNous/new-api/internal/transport/contract"
)

func GetRatioConfig(c contract.Context) {
	ops.GetRatioConfig(c)
}
