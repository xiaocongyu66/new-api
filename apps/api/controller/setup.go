package controller

import (
	"github.com/QuantumNous/new-api/internal/capabilities/administration"
	"github.com/QuantumNous/new-api/internal/transport/contract"
)

func GetSetup(c contract.Context) {
	administration.GetSetup(c)
}

func PostSetup(c contract.Context) {
	administration.PostSetup(c)
}
