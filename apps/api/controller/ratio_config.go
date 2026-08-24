package controller

import (
	"github.com/QuantumNous/new-api/internal/capabilities/administration"
	"github.com/QuantumNous/new-api/internal/transport/contract"
)

func GetRatioConfig(c contract.Context) {
	administration.GetRatioConfig(c)
}
