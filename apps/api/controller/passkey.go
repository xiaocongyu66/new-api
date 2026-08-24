package controller

import (
	"github.com/QuantumNous/new-api/internal/capabilities/identity"
	"github.com/QuantumNous/new-api/internal/transport/contract"
)

func PasskeyRegisterBegin(c contract.Context) {
	identity.PasskeyRegisterBegin(c)
}

func PasskeyRegisterFinish(c contract.Context) {
	identity.PasskeyRegisterFinish(c)
}

func PasskeyDelete(c contract.Context) {
	identity.PasskeyDelete(c)
}

func PasskeyStatus(c contract.Context) {
	identity.PasskeyStatus(c)
}

func PasskeyLoginBegin(c contract.Context) {
	identity.PasskeyLoginBegin(c)
}

func PasskeyLoginFinish(c contract.Context) {
	identity.PasskeyLoginFinish(c)
}

func AdminResetPasskey(c contract.Context) {
	identity.AdminResetPasskey(c)
}

func PasskeyVerifyBegin(c contract.Context) {
	identity.PasskeyVerifyBegin(c)
}

func PasskeyVerifyFinish(c contract.Context) {
	identity.PasskeyVerifyFinish(c)
}
