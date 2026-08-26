package controller

import (
	"github.com/QuantumNous/new-api/internal/identity"
	"github.com/QuantumNous/new-api/internal/transport/contract"
)

func GetAllTokens(c contract.Context) {
	identity.GetAllTokens(c)
}

func SearchTokens(c contract.Context) {
	identity.SearchTokens(c)
}

func GetToken(c contract.Context) {
	identity.GetToken(c)
}

func GetTokenAutoGroups(c contract.Context) {
	identity.GetTokenAutoGroups(c)
}

func GetTokenKey(c contract.Context) {
	identity.GetTokenKey(c)
}

func GetTokenStatus(c contract.Context) {
	identity.GetTokenStatus(c)
}

func GetTokenUsage(c contract.Context) {
	identity.GetTokenUsage(c)
}

func AddToken(c contract.Context) {
	identity.AddToken(c)
}

func DeleteToken(c contract.Context) {
	identity.DeleteToken(c)
}

func UpdateToken(c contract.Context) {
	identity.UpdateToken(c)
}

func DeleteTokenBatch(c contract.Context) {
	identity.DeleteTokenBatch(c)
}

func GetTokenKeysBatch(c contract.Context) {
	identity.GetTokenKeysBatch(c)
}
