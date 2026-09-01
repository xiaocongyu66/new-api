package gateway

import (
	"errors"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/constant"
	relaycommon "github.com/QuantumNous/new-api/internal/relay/common"
	relayconstant "github.com/QuantumNous/new-api/internal/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/tidwall/gjson"
)

// GenRelayInfoWs creates RelayInfo for WebSocket (realtime) requests.
func GenRelayInfoWs(c contract.Context, ws relaycommon.WSConn) *relaycommon.RelayInfo {
	info := genBaseRelayInfo(c, nil)
	info.RelayFormat = types.RelayFormatOpenAIRealtime
	info.ClientWs = ws
	info.InputAudioFormat = "pcm16"
	info.OutputAudioFormat = "pcm16"
	info.IsFirstRequest = true
	return info
}

// GenRelayInfoClaude creates RelayInfo for Claude format requests.
func GenRelayInfoClaude(c contract.Context, request dto.Request) *relaycommon.RelayInfo {
	info := genBaseRelayInfo(c, request)
	info.RelayFormat = types.RelayFormatClaude
	info.ShouldIncludeUsage = false
	info.ClaudeConvertInfo = &relaycommon.ClaudeConvertInfo{
		LastMessagesType: relaycommon.LastMessageTypeNone,
	}
	info.IsClaudeBetaQuery = c.Query("beta") == "true"
	return info
}

// GenRelayInfoRerank creates RelayInfo for rerank requests.
func GenRelayInfoRerank(c contract.Context, request *dto.RerankRequest) *relaycommon.RelayInfo {
	info := genBaseRelayInfo(c, request)
	info.RelayMode = relayconstant.RelayModeRerank
	info.RelayFormat = types.RelayFormatRerank
	info.RerankerInfo = &relaycommon.RerankerInfo{
		Documents:       request.Documents,
		ReturnDocuments: request.GetReturnDocuments(),
	}
	return info
}

// GenRelayInfoOpenAIAudio creates RelayInfo for OpenAI audio requests.
func GenRelayInfoOpenAIAudio(c contract.Context, request dto.Request) *relaycommon.RelayInfo {
	info := genBaseRelayInfo(c, request)
	info.RelayFormat = types.RelayFormatOpenAIAudio
	return info
}

// GenRelayInfoEmbedding creates RelayInfo for embedding requests.
func GenRelayInfoEmbedding(c contract.Context, request dto.Request) *relaycommon.RelayInfo {
	info := genBaseRelayInfo(c, request)
	info.RelayFormat = types.RelayFormatEmbedding
	return info
}

// GenRelayInfoResponses creates RelayInfo for OpenAI Responses API requests.
func GenRelayInfoResponses(c contract.Context, request *dto.OpenAIResponsesRequest) *relaycommon.RelayInfo {
	info := genBaseRelayInfo(c, request)
	info.RelayMode = relayconstant.RelayModeResponses
	info.RelayFormat = types.RelayFormatOpenAIResponses

	info.ResponsesUsageInfo = &relaycommon.ResponsesUsageInfo{
		BuiltInTools: make(map[string]*relaycommon.BuildInToolInfo),
	}
	if len(request.Tools) > 0 {
		for _, tool := range request.GetToolsMap() {
			toolType := common.Interface2String(tool["type"])
			info.ResponsesUsageInfo.BuiltInTools[toolType] = &relaycommon.BuildInToolInfo{
				ToolName:  toolType,
				CallCount: 0,
			}
			switch toolType {
			case dto.BuildInToolWebSearchPreview:
				searchContextSize := common.Interface2String(tool["search_context_size"])
				if searchContextSize == "" {
					searchContextSize = "medium"
				}
				info.ResponsesUsageInfo.BuiltInTools[toolType].SearchContextSize = searchContextSize
			}
		}
	}
	return info
}

// GenRelayInfoGemini creates RelayInfo for Gemini format requests.
func GenRelayInfoGemini(c contract.Context, request dto.Request) *relaycommon.RelayInfo {
	info := genBaseRelayInfo(c, request)
	info.RelayFormat = types.RelayFormatGemini
	info.ShouldIncludeUsage = false
	return info
}

// GenRelayInfoImage creates RelayInfo for OpenAI image requests.
func GenRelayInfoImage(c contract.Context, request dto.Request) *relaycommon.RelayInfo {
	info := genBaseRelayInfo(c, request)
	info.RelayFormat = types.RelayFormatOpenAIImage
	return info
}

// GenRelayInfoOpenAI creates RelayInfo for standard OpenAI chat/completions requests.
func GenRelayInfoOpenAI(c contract.Context, request dto.Request) *relaycommon.RelayInfo {
	info := genBaseRelayInfo(c, request)
	info.RelayFormat = types.RelayFormatOpenAI
	return info
}

// reasoningEffortFromRequest extracts reasoning_effort from various request types.
func reasoningEffortFromRequest(request dto.Request) string {
	var effort string
	switch req := request.(type) {
	case *dto.GeneralOpenAIRequest:
		if req == nil {
			return ""
		}
		effort = req.ReasoningEffort
		if strings.TrimSpace(effort) == "" && len(req.Reasoning) > 0 {
			value := gjson.GetBytes(req.Reasoning, "effort")
			if value.Type == gjson.String {
				effort = value.String()
			}
		}
	case *dto.OpenAIResponsesRequest:
		if req != nil && req.Reasoning != nil {
			effort = req.Reasoning.Effort
		}
	case *dto.ClaudeRequest:
		if req != nil {
			effort = req.GetEfforts()
		}
	case *dto.GeminiChatRequest:
		if req != nil && req.GenerationConfig.ThinkingConfig != nil {
			effort = req.GenerationConfig.ThinkingConfig.ThinkingLevel
		}
	}
	return strings.TrimSpace(effort)
}

// genBaseRelayInfo builds the base RelayInfo shared by all formats.
func genBaseRelayInfo(c contract.Context, request dto.Request) *relaycommon.RelayInfo {
	tokenGroup := common.GetContextKeyString(c, constant.ContextKeyTokenGroup)
	if tokenGroup == "" {
		tokenGroup = common.GetContextKeyString(c, constant.ContextKeyUserGroup)
	}

	startTime := common.GetContextKeyTime(c, constant.ContextKeyRequestStartTime)
	if startTime.IsZero() {
		startTime = time.Now()
	}

	isStream := false
	if request != nil {
		// TODO(#287) C: permanent. request.IsStream is relaykit public API typed on
		// *http.Request, and relaykit is a separate module that cannot import the app's
		// internal contract. The Fiber adapter must keep synthesizing an *http.Request
		// for this boundary; there is nothing to migrate.
		isStream = request.IsStream(c.HTTPRequest())
	}
	c.Set(string(constant.ContextKeyIsStream), isStream)

	reqId := common.GetContextKeyString(c, common.RequestIdKey)
	if reqId == "" {
		reqId = common.NewRequestId()
	}
	reasoningEffort := reasoningEffortFromRequest(request)
	info := &relaycommon.RelayInfo{
		Request:         request,
		ReasoningEffort: reasoningEffort,

		RequestId:  reqId,
		UserId:     common.GetContextKeyInt(c, constant.ContextKeyUserId),
		UsingGroup: common.GetContextKeyString(c, constant.ContextKeyUsingGroup),
		UserGroup:  common.GetContextKeyString(c, constant.ContextKeyUserGroup),
		UserQuota:  common.GetContextKeyInt(c, constant.ContextKeyUserQuota),
		UserEmail:  common.GetContextKeyString(c, constant.ContextKeyUserEmail),

		OriginModelName: common.GetContextKeyString(c, constant.ContextKeyOriginalModel),

		TokenId:        common.GetContextKeyInt(c, constant.ContextKeyTokenId),
		TokenKey:       common.GetContextKeyString(c, constant.ContextKeyTokenKey),
		TokenUnlimited: common.GetContextKeyBool(c, constant.ContextKeyTokenUnlimited),
		TokenGroup:     tokenGroup,

		RelayMode: relayconstant.Path2RelayMode(c.Path()),
		// RequestURI rather than Path+RawQuery: it is the request target as sent,
		// so a bare trailing '?' survives into the upstream URL exactly as it
		// arrived instead of being dropped by recomposition.
		RequestURLPath: c.RequestURI(),
		RequestHeaders: cloneRequestHeaders(c),
		IsStream:       isStream,

		StartTime:         startTime,
		FirstResponseTime: startTime.Add(-time.Second),
		ThinkingContentInfo: relaycommon.ThinkingContentInfo{
			IsFirstThinkingContent:  true,
			SendLastThinkingContent: false,
		},
	}
	info.MarkFirstResponse()
	info.SetEstimatePromptTokens(common.GetContextKeyInt(c, constant.ContextKeyEstimatedTokens))

	if info.RelayMode == relayconstant.RelayModeUnknown {
		info.RelayMode = c.GetInt("relay_mode")
	}

	if strings.HasPrefix(c.Path(), "/pg") {
		info.IsPlayground = true
		info.RequestURLPath = strings.TrimPrefix(info.RequestURLPath, "/pg")
		info.RequestURLPath = "/v1" + info.RequestURLPath
	}

	userSetting, ok := common.GetContextKeyType[dto.UserSetting](c, constant.ContextKeyUserSetting)
	if ok {
		info.UserSetting = userSetting
	}

	return info
}

func cloneRequestHeaders(c contract.Context) map[string]string {
	if c == nil {
		return nil
	}
	inbound := c.Headers()
	if len(inbound) == 0 {
		return nil
	}
	headers := make(map[string]string, len(inbound))
	for key := range inbound {
		value := strings.TrimSpace(inbound.Get(key))
		if value == "" {
			continue
		}
		headers[key] = value
	}
	if len(headers) == 0 {
		return nil
	}
	return headers
}

// GenRelayInfo is the main entry point: builds RelayInfo for a given format.
func GenRelayInfo(c contract.Context, relayFormat types.RelayFormat, request dto.Request, ws relaycommon.WSConn) (*relaycommon.RelayInfo, error) {
	var info *relaycommon.RelayInfo
	var err error
	switch relayFormat {
	case types.RelayFormatOpenAI:
		info = GenRelayInfoOpenAI(c, request)
	case types.RelayFormatOpenAIAudio:
		info = GenRelayInfoOpenAIAudio(c, request)
	case types.RelayFormatOpenAIImage:
		info = GenRelayInfoImage(c, request)
	case types.RelayFormatOpenAIRealtime:
		info = GenRelayInfoWs(c, ws)
	case types.RelayFormatClaude:
		info = GenRelayInfoClaude(c, request)
	case types.RelayFormatRerank:
		if request, ok := request.(*dto.RerankRequest); ok {
			info = GenRelayInfoRerank(c, request)
			break
		}
		err = errors.New("request is not a RerankRequest")
	case types.RelayFormatGemini:
		info = GenRelayInfoGemini(c, request)
	case types.RelayFormatEmbedding:
		info = GenRelayInfoEmbedding(c, request)
	case types.RelayFormatOpenAIResponses:
		if request, ok := request.(*dto.OpenAIResponsesRequest); ok {
			info = GenRelayInfoResponses(c, request)
			break
		}
		err = errors.New("request is not a OpenAIResponsesRequest")
	case types.RelayFormatOpenAIResponsesCompaction:
		if request, ok := request.(*dto.OpenAIResponsesCompactionRequest); ok {
			return GenRelayInfoResponsesCompaction(c, request), nil
		}
		return nil, errors.New("request is not a OpenAIResponsesCompactionRequest")
	case types.RelayFormatOpenAIAlphaSearch:
		if request, ok := request.(*dto.AlphaSearchRequest); ok {
			return GenRelayInfoAlphaSearch(c, request), nil
		}
		return nil, errors.New("request is not a AlphaSearchRequest")
	case types.RelayFormatTask:
		info = genBaseRelayInfo(c, nil)
		info.TaskRelayInfo = &relaycommon.TaskRelayInfo{}
	case types.RelayFormatMjProxy:
		info = genBaseRelayInfo(c, nil)
		info.TaskRelayInfo = &relaycommon.TaskRelayInfo{}
	default:
		err = errors.New("invalid relay format")
	}

	if err != nil {
		return nil, err
	}
	if info == nil {
		return nil, errors.New("failed to build relay info")
	}

	info.InitRequestConversionChain()
	return info, nil
}

// GenRelayInfoResponsesCompaction creates RelayInfo for Responses compaction requests.
func GenRelayInfoResponsesCompaction(c contract.Context, request *dto.OpenAIResponsesCompactionRequest) *relaycommon.RelayInfo {
	info := genBaseRelayInfo(c, request)
	if info.RelayMode == relayconstant.RelayModeUnknown {
		info.RelayMode = relayconstant.RelayModeResponsesCompact
	}
	info.RelayFormat = types.RelayFormatOpenAIResponsesCompaction
	return info
}

// GenRelayInfoAlphaSearch creates RelayInfo for Alpha Search (Codex web search) requests.
func GenRelayInfoAlphaSearch(c contract.Context, request *dto.AlphaSearchRequest) *relaycommon.RelayInfo {
	info := genBaseRelayInfo(c, request)
	if info.RelayMode == relayconstant.RelayModeUnknown {
		info.RelayMode = relayconstant.RelayModeAlphaSearch
	}
	info.RelayFormat = types.RelayFormatOpenAIAlphaSearch
	info.ResponsesUsageInfo = &relaycommon.ResponsesUsageInfo{
		BuiltInTools: map[string]*relaycommon.BuildInToolInfo{
			dto.BuildInToolWebSearchPreview: {
				ToolName:  dto.BuildInToolWebSearchPreview,
				CallCount: 0,
			},
		},
	}
	return info
}
