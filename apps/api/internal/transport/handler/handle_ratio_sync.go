package handler

import (
	"github.com/QuantumNous/new-api/internal/ops"
	"github.com/QuantumNous/new-api/internal/transport/contract"
)

func FetchUpstreamRatios(c contract.Context) {
	ops.FetchUpstreamRatios(c)
}

func GetSyncableChannels(c contract.Context) {
	ops.GetSyncableChannels(c)
}
