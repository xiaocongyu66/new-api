package controller

import (
	"github.com/QuantumNous/new-api/internal/capabilities/identity"
	"github.com/QuantumNous/new-api/internal/transport/contract"
)

func TelegramBindStart(c contract.Context) {
	identity.TelegramBindStart(c)
}

func TelegramBind(c contract.Context) {
	identity.TelegramBind(c)
}

func TelegramLogin(c contract.Context) {
	identity.TelegramLogin(c)
}
