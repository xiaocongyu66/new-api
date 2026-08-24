package controller

import (
	"github.com/QuantumNous/new-api/internal/capabilities/usage"
	"github.com/QuantumNous/new-api/internal/transport/contract"
)

func GetPerformanceStats(c contract.Context) {
	usage.GetPerformanceStats(c)
}

func ClearDiskCache(c contract.Context) {
	usage.ClearDiskCache(c)
}

func ResetPerformanceStats(c contract.Context) {
	usage.ResetPerformanceStats(c)
}

func ForceGC(c contract.Context) {
	usage.ForceGC(c)
}

func GetLogFiles(c contract.Context) {
	usage.GetLogFiles(c)
}

func CleanupLogFiles(c contract.Context) {
	usage.CleanupLogFiles(c)
}