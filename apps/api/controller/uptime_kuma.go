package controller

import (
	"github.com/QuantumNous/new-api/internal/ops"
	"github.com/QuantumNous/new-api/internal/transport/contract"
)

func GetUptimeKumaStatus(c contract.Context) {
	ops.GetUptimeKumaStatus(c)
}
