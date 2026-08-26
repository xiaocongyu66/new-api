package controller

import (
	"github.com/QuantumNous/new-api/internal/ops"
	"github.com/QuantumNous/new-api/internal/transport/contract"
)

func GetSetup(c contract.Context) {
	ops.GetSetup(c)
}

func PostSetup(c contract.Context) {
	ops.PostSetup(c)
}
