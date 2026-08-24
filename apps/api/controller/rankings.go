package controller

import (
	"github.com/QuantumNous/new-api/internal/capabilities/usage"
	"github.com/QuantumNous/new-api/internal/transport/contract"
)

func GetRankings(c contract.Context) {
	usage.GetRankings(c)
}