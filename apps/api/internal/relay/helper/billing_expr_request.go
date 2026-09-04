package helper

import (
	"github.com/QuantumNous/new-api/internal/billing/price_expression"
	"github.com/QuantumNous/new-api/internal/gateway"
	"github.com/QuantumNous/new-api/relaykit/dto"

	relaycommon "github.com/QuantumNous/new-api/internal/relay/common"
	"github.com/QuantumNous/new-api/internal/transport/contract"
)

func ResolveIncomingBillingExprRequestInput(c contract.Context, info *relaycommon.RelayInfo) (price_expression.RequestInput, error) {
	return gateway.ResolveIncomingBillingExprRequestInput(c, info)
}

func BuildBillingExprRequestInputFromRequest(request dto.Request, headers map[string]string) (price_expression.RequestInput, error) {
	return gateway.BuildBillingExprRequestInputFromRequest(request, headers)
}
