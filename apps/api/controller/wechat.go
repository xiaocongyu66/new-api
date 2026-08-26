package controller

import (
	"github.com/QuantumNous/new-api/internal/identity"
	"github.com/QuantumNous/new-api/internal/transport/contract"
)

func WeChatAuth(c contract.Context) {
	identity.WeChatAuth(c)
}

func WeChatBind(c contract.Context) {
	identity.WeChatBind(c)
}
