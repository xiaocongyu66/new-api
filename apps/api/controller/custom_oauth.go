package controller

import (
	"github.com/QuantumNous/new-api/internal/capabilities/identity"
	"github.com/QuantumNous/new-api/internal/transport/contract"
)

func GetCustomOAuthProviders(c contract.Context) {
	identity.GetCustomOAuthProviders(c)
}

func GetCustomOAuthProvider(c contract.Context) {
	identity.GetCustomOAuthProvider(c)
}

func FetchCustomOAuthDiscovery(c contract.Context) {
	identity.FetchCustomOAuthDiscovery(c)
}

func CreateCustomOAuthProvider(c contract.Context) {
	identity.CreateCustomOAuthProvider(c)
}

func UpdateCustomOAuthProvider(c contract.Context) {
	identity.UpdateCustomOAuthProvider(c)
}

func DeleteCustomOAuthProvider(c contract.Context) {
	identity.DeleteCustomOAuthProvider(c)
}

func GetUserOAuthBindings(c contract.Context) {
	identity.GetUserOAuthBindings(c)
}

func GetUserOAuthBindingsByAdmin(c contract.Context) {
	identity.GetUserOAuthBindingsByAdmin(c)
}

func UnbindCustomOAuth(c contract.Context) {
	identity.UnbindCustomOAuth(c)
}

func UnbindCustomOAuthByAdmin(c contract.Context) {
	identity.UnbindCustomOAuthByAdmin(c)
}
