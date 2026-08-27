package helper

import (
	"github.com/QuantumNous/new-api/internal/gateway"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/relaykit/dto"

	relaycommon "github.com/QuantumNous/new-api/internal/relay/common"
	"github.com/QuantumNous/new-api/internal/transport/contract"
)

func ResolveIncomingBillingExprRequestInput(c contract.Context, info *relaycommon.RelayInfo) (billingexpr.RequestInput, error) {
	return gateway.ResolveIncomingBillingExprRequestInput(c, info)
}

func BuildBillingExprRequestInputFromRequest(request dto.Request, headers map[string]string) (billingexpr.RequestInput, error) {
	return gateway.BuildBillingExprRequestInputFromRequest(request, headers)
}
