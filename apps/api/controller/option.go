package controller

import (
	"github.com/QuantumNous/new-api/internal/capabilities/administration"
	"github.com/QuantumNous/new-api/internal/transport/contract"
)

func GetOptions(c contract.Context) {
	administration.GetOptions(c)
}

func UpdateOption(c contract.Context) {
	administration.UpdateOption(c)
}
