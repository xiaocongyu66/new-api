package controller

import (
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/internal/usage"
)

func GetPerfMetricsSummary(c contract.Context) {
	usage.GetPerfMetricsSummary(c)
}

func GetPerfMetrics(c contract.Context) {
	usage.GetPerfMetrics(c)
}
