package controller

import (
	"github.com/QuantumNous/new-api/internal/capabilities/administration"
	"github.com/QuantumNous/new-api/internal/transport/contract"
)

func FetchUpstreamRatios(c contract.Context) {
	administration.FetchUpstreamRatios(c)
}

func GetSyncableChannels(c contract.Context) {
	administration.GetSyncableChannels(c)
}
