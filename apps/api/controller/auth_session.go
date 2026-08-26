package controller

import (
	"github.com/QuantumNous/new-api/internal/identity"
	"github.com/QuantumNous/new-api/internal/transport/contract"
)

func RefreshAuth(c contract.Context) {
	identity.RefreshAuth(c)
}

func AuthLogout(c contract.Context) {
	identity.AuthLogout(c)
}

func GetLoginSessions(c contract.Context) {
	identity.GetLoginSessions(c)
}

func DeleteLoginSession(c contract.Context) {
	identity.DeleteLoginSession(c)
}

func RevokeOtherLoginSessions(c contract.Context) {
	identity.RevokeOtherLoginSessions(c)
}
