package helper

import (
	"github.com/QuantumNous/new-api/internal/gateway"
	relaycommon "github.com/QuantumNous/new-api/internal/relay/common"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

func ModelMappedHelper(c contract.Context, info *relaycommon.RelayInfo, request dto.Request) error {
	return gateway.ModelMappedHelper(c, info, request)
}
