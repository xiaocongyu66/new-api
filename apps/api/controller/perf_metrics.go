package controller

import (
	"github.com/QuantumNous/new-api/internal/capabilities/usage"
	"github.com/QuantumNous/new-api/internal/transport/contract"
)

func GetPerfMetricsSummary(c contract.Context) {
	usage.GetPerfMetricsSummary(c)
}

func GetPerfMetrics(c contract.Context) {
	usage.GetPerfMetrics(c)
}
