package controller

import (
	"github.com/QuantumNous/new-api/internal/identity"
	"github.com/QuantumNous/new-api/internal/transport/contract"
)

func Setup2FA(c contract.Context) {
	identity.Setup2FA(c)
}

func Enable2FA(c contract.Context) {
	identity.Enable2FA(c)
}

func Disable2FA(c contract.Context) {
	identity.Disable2FA(c)
}

func Get2FAStatus(c contract.Context) {
	identity.Get2FAStatus(c)
}

func RegenerateBackupCodes(c contract.Context) {
	identity.RegenerateBackupCodes(c)
}

func Verify2FALogin(c contract.Context) {
	identity.Verify2FALogin(c)
}

func Admin2FAStats(c contract.Context) {
	identity.Admin2FAStats(c)
}

func AdminDisable2FA(c contract.Context) {
	identity.AdminDisable2FA(c)
}
