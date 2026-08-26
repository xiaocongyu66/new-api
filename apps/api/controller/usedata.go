package controller

import (
	"github.com/QuantumNous/new-api/internal/usage"
	"github.com/QuantumNous/new-api/internal/transport/contract"
)

func GetAllQuotaDates(c contract.Context) {
	usage.GetAllQuotaDates(c)
}

func GetQuotaDatesByUser(c contract.Context) {
	usage.GetQuotaDatesByUser(c)
}

func GetUserQuotaDates(c contract.Context) {
	usage.GetUserQuotaDates(c)
}

func GetAllFlowQuotaDates(c contract.Context) {
	usage.GetAllFlowQuotaDates(c)
}

func GetUserFlowQuotaDates(c contract.Context) {
	usage.GetUserFlowQuotaDates(c)
}
