package controller

import (
	"github.com/QuantumNous/new-api/internal/capabilities/integration"
	"github.com/QuantumNous/new-api/internal/transport/contract"
)

func GetUptimeKumaStatus(c contract.Context) {
	integration.GetUptimeKumaStatus(c)
}