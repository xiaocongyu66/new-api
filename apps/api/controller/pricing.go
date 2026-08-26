package controller

import (
	"github.com/QuantumNous/new-api/internal/capabilities/administration"
	"github.com/QuantumNous/new-api/internal/transport/contract"
)

func GetPricing(c contract.Context) {
	administration.GetPricing(c)
}

func ResetModelRatio(c contract.Context) {
	administration.ResetModelRatio(c)
}
