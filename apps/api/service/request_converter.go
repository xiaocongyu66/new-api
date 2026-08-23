package service

import (
	"context"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/gin-gonic/gin"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/types"
)

func init() {
	relayconvert.SetMediaResolver(relayconvert.MediaResolver{
		// relayconvert is gin-free; recover the gin context when the caller
		// passed one so file caching/cleanup keeps working.
		GetBase64Data: func(ctx context.Context, source types.FileSource, reason ...string) (string, string, error) {
			ginCtx, _ := ctx.(*gin.Context)
			return GetBase64Data(ginCtx, source, reason...)
		},
		DecodeBase64FileData: DecodeBase64FileData,
	})
}

func ConvertRequest(c *gin.Context, info *relaycommon.RelayInfo, target types.RelayFormat, request any) (*relayconvert.RequestResult, error) {
	return relayconvert.ConvertRequest(c.Request.Context(), info, target, request)
}

func ConvertRequestByID(c *gin.Context, info *relaycommon.RelayInfo, converter string, request any) (*relayconvert.RequestResult, error) {
	return relayconvert.ConvertRequestByID(c.Request.Context(), info, converter, request)
}

func ConvertRequestVia(c contract.Context, info *relaycommon.RelayInfo, request any, path ...types.RelayFormat) (*relayconvert.RequestResult, error) {
	return relayconvert.ConvertRequestVia(c.Context(), info, request, path...)
}
