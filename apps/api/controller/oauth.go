package controller

import (
	"github.com/QuantumNous/new-api/internal/capabilities/identity"
	"github.com/QuantumNous/new-api/internal/transport/contract"
)

func GenerateOAuthCode(c contract.Context) {
	identity.GenerateOAuthCode(c)
}

func HandleOAuth(c contract.Context) {
	identity.HandleOAuth(c)
}
