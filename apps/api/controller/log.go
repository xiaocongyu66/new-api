package controller

import (
	"github.com/QuantumNous/new-api/internal/capabilities/usage"
	"github.com/QuantumNous/new-api/internal/transport/contract"
)

func GetAllLogs(c contract.Context) {
	usage.GetAllLogs(c)
}

func GetUserLogs(c contract.Context) {
	usage.GetUserLogs(c)
}

// Deprecated: SearchAllLogs 已废弃，前端未使用该接口。
func SearchAllLogs(c contract.Context) {
	usage.SearchAllLogs(c)
}

// Deprecated: SearchUserLogs 已废弃，前端未使用该接口。
func SearchUserLogs(c contract.Context) {
	usage.SearchUserLogs(c)
}

func GetLogByKey(c contract.Context) {
	usage.GetLogByKey(c)
}

func GetLogsStat(c contract.Context) {
	usage.GetLogsStat(c)
}

func GetLogsSelfStat(c contract.Context) {
	usage.GetLogsSelfStat(c)
}