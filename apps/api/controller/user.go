package controller

import (
	"github.com/QuantumNous/new-api/internal/capabilities/identity"
	"github.com/QuantumNous/new-api/internal/transport/contract"
)

func Login(c contract.Context) {
	identity.Login(c)
}

func Register(c contract.Context) {
	identity.Register(c)
}

func GetAllUsers(c contract.Context) {
	identity.GetAllUsers(c)
}

func SearchUsers(c contract.Context) {
	identity.SearchUsers(c)
}

func GetUser(c contract.Context) {
	identity.GetUser(c)
}

func GenerateAccessToken(c contract.Context) {
	identity.GenerateAccessToken(c)
}

func TransferAffQuota(c contract.Context) {
	identity.TransferAffQuota(c)
}

func GetAffCode(c contract.Context) {
	identity.GetAffCode(c)
}

func GetSelf(c contract.Context) {
	identity.GetSelf(c)
}

func GetUserModels(c contract.Context) {
	identity.GetUserModels(c)
}

func UpdateUser(c contract.Context) {
	identity.UpdateUser(c)
}

func AdminClearUserBinding(c contract.Context) {
	identity.AdminClearUserBinding(c)
}

func UpdateSelf(c contract.Context) {
	identity.UpdateSelf(c)
}

func DeleteUser(c contract.Context) {
	identity.DeleteUser(c)
}

func DeleteSelf(c contract.Context) {
	identity.DeleteSelf(c)
}

func CreateUser(c contract.Context) {
	identity.CreateUser(c)
}

func ManageUser(c contract.Context) {
	identity.ManageUser(c)
}

func EmailBind(c contract.Context) {
	identity.EmailBind(c)
}

func TopUp(c contract.Context) {
	identity.TopUp(c)
}

func UpdateUserSetting(c contract.Context) {
	identity.UpdateUserSetting(c)
}
