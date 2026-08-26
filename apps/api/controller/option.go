package controller

import (
	"github.com/QuantumNous/new-api/internal/ops"
	"github.com/QuantumNous/new-api/internal/transport/contract"
)

func GetOptions(c contract.Context) {
	ops.GetOptions(c)
}

func UpdateOption(c contract.Context) {
	ops.UpdateOption(c)
}
