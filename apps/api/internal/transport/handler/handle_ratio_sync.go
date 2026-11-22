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

func PricingAnalysis(c contract.Context) {
	ops.PricingAnalysis(c)
}

func PriceFluctuation(c contract.Context) {
	ops.PriceFluctuation(c)
}

func ListManualUpstreamPrices(c contract.Context) {
	ops.ListManualUpstreamPrices(c)
}

func UpsertManualUpstreamPrice(c contract.Context) {
	ops.UpsertManualUpstreamPrice(c)
}

func DeleteManualUpstreamPrice(c contract.Context) {
	ops.DeleteManualUpstreamPrice(c)
}
