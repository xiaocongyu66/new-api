package handler

import (
	"errors"
	"fmt"
	"github.com/QuantumNous/new-api/internal/billing"
	"github.com/QuantumNous/new-api/internal/gateway"
	taskcap "github.com/QuantumNous/new-api/internal/task"
	taskdomain "github.com/QuantumNous/new-api/internal/task"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	usage "github.com/QuantumNous/new-api/internal/usage"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	catalog "github.com/QuantumNous/new-api/internal/catalog"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/constant"
	taskdto "github.com/QuantumNous/new-api/internal/dto"
	"github.com/QuantumNous/new-api/internal/logger"
	"github.com/QuantumNous/new-api/internal/relay"
	relaycommon "github.com/QuantumNous/new-api/internal/relay/common"
	relayconstant "github.com/QuantumNous/new-api/internal/relay/constant"
	"github.com/QuantumNous/new-api/internal/relay/helper"
	"github.com/QuantumNous/new-api/internal/transport/middleware"
	"github.com/QuantumNous/new-api/internal/usage/record_perf"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/QuantumNous/new-api/internal/transport/middleware/status_code"
	"github.com/bytedance/gopkg/util/gopool"
	"github.com/samber/lo"

	"github.com/QuantumNous/new-api/internal/sensitive"
	"github.com/gorilla/websocket"
)

func relayHandler(c contract.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	var err *types.NewAPIError
	switch info.RelayMode {
	case relayconstant.RelayModeImagesGenerations, relayconstant.RelayModeImagesEdits:
		err = relay.ImageHelper(c, info)
	case relayconstant.RelayModeAudioSpeech:
		fallthrough
	case relayconstant.RelayModeAudioTranslation:
		fallthrough
	case relayconstant.RelayModeAudioTranscription:
		err = relay.AudioHelper(c, info)
	case relayconstant.RelayModeRerank:
		err = relay.RerankHelper(c, info)
	case relayconstant.RelayModeEmbeddings:
		err = relay.EmbeddingHelper(c, info)
	case relayconstant.RelayModeResponses, relayconstant.RelayModeResponsesCompact:
		err = relay.ResponsesHelper(c, info)
	case relayconstant.RelayModeAlphaSearch:
		err = relay.AlphaSearchHelper(c, info)
	default:
		err = relay.TextHelper(c, info)
	}
	return err
}

func geminiRelayHandler(c contract.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	var err *types.NewAPIError
	if strings.Contains(c.Path(), "embed") {
		err = relay.GeminiEmbeddingHandler(c, info)
	} else {
		err = relay.GeminiHelper(c, info)
	}
	return err
}

func Relay(c contract.Context, relayFormat types.RelayFormat) {

	requestId := c.GetString(common.RequestIdKey)
	//group := common.GetCtxKeyString(c, constant.ContextKeyUsingGroup)
	//originalModel := common.GetCtxKeyString(c, constant.ContextKeyOriginalModel)

	var (
		newAPIError *types.NewAPIError
		ws          *websocket.Conn
	)

	if relayFormat == types.RelayFormatOpenAIRealtime {
		var err error
		ws, err = upgrader.Upgrade(c.ResponseWriter(), c.HTTPRequest(), nil)
		if err != nil {
			helper.WssError(c, ws, types.NewError(err, types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry()).ToOpenAIError())
			return
		}
		defer ws.Close()
	}

	defer func() {
		if newAPIError != nil {
			logger.LogError(c.Context(), fmt.Sprintf("relay error: %s", common.LocalLogPreview(newAPIError.Error())))
			newAPIError.SetMessage(common.MessageWithRequestId(newAPIError.Error(), requestId))
			switch relayFormat {
			case types.RelayFormatOpenAIRealtime:
				helper.WssError(c, ws, newAPIError.ToOpenAIError())
			case types.RelayFormatClaude:
				_ = c.JSON(newAPIError.StatusCode, common.H{
					"type":  "error",
					"error": newAPIError.ToClaudeError(),
				})
			default:
				_ = c.JSON(newAPIError.StatusCode, common.H{
					"error": newAPIError.ToOpenAIError(),
				})
			}
		}
	}()

	request, err := helper.GetAndValidateRequest(c, relayFormat)
	if err != nil {
		// Map "request body too large" to 413 so clients can handle it correctly
		if common.IsRequestBodyTooLargeError(err) || errors.Is(err, common.ErrRequestBodyTooLarge) {
			newAPIError = types.NewErrorWithStatusCode(err, types.ErrorCodeReadRequestBodyFailed, http.StatusRequestEntityTooLarge, types.ErrOptionWithSkipRetry())
		} else {
			newAPIError = types.NewError(err, types.ErrorCodeInvalidRequest)
		}
		return
	}

	relayInfo, err := gateway.GenRelayInfo(c, relayFormat, request, ws)
	if err != nil {
		newAPIError = types.NewError(err, types.ErrorCodeGenRelayInfoFailed)
		return
	}

	needSensitiveCheck := sensitive.ShouldCheckPromptSensitive()
	needCountToken := constant.CountToken
	// Avoid building huge CombineText (strings.Join) when token counting and sensitive check are both disabled.
	var meta *types.TokenCountMeta
	if needSensitiveCheck || needCountToken {
		meta = request.GetTokenCountMeta()
	} else {
		meta = fastTokenCountMetaForPricing(request)
	}

	if needSensitiveCheck && meta != nil {
		if hit, labels := sensitive.CheckSensitiveText(meta.CombineText); hit && len(labels) > 0 {
			logger.LogWarn(c.Context(), fmt.Sprintf("input blocked by sensitive filter: %s", labels[0]))
			newAPIError = types.NewError(err, types.ErrorCodeSensitiveWordsDetected, types.ErrOptionWithStatusCode(http.StatusForbidden))
			return
		}
	}

	// 目标域名硬闸独立于敏感词开关：请求包含攻击目标站点即无条件终止，
	// 不受 CheckSensitiveOnPromptEnabled 等开关影响（用户要求任何输入输出都终止）。
	if meta != nil {
		if d := sensitive.CheckSensitiveTargets(meta.CombineText); d != "" {
			logger.LogWarn(c.Context(), fmt.Sprintf("input blocked by target domain: %s", d))
			newAPIError = types.NewErrorWithStatusCode(err, types.ErrorCodeSensitiveWordsDetected, http.StatusForbidden)
			return
		}
	}

	tokens, err := relaycommon.EstimateRequestToken(c, meta, relayInfo)
	if err != nil {
		newAPIError = types.NewError(err, types.ErrorCodeCountTokenFailed)
		return
	}

	relayInfo.SetEstimatePromptTokens(tokens)

	priceData, err := helper.ModelPriceHelper(c, relayInfo, tokens, meta)
	if err != nil {
		newAPIError = types.NewError(err, types.ErrorCodeModelPriceError, types.ErrOptionWithStatusCode(http.StatusBadRequest))
		return
	}

	// common.SetCtxKey(c, constant.ContextKeyTokenCountMeta, meta)

	if priceData.FreeModel {
		logger.LogInfo(c.Context(), fmt.Sprintf("模型 %s 免费，跳过预扣费", relayInfo.OriginModelName))
	} else {
		newAPIError = billing.PreConsumeBilling(c, priceData.QuotaToPreConsume, relayInfo)
		if newAPIError != nil {
			return
		}
	}

	defer func() {
		// Only return quota if downstream failed and quota was actually pre-consumed
		if newAPIError != nil {
			newAPIError = billing.NormalizeViolationFeeError(newAPIError)
			if relayInfo.Billing != nil {
				relayInfo.Billing.Refund(c)
			}
			billing.ChargeViolationFeeIfNeeded(c, relayInfo, newAPIError)
		}
	}()

	retryParam := &catalog.SelectParams{
		Ctx:         c,
		TokenGroup:  relayInfo.TokenGroup,
		ModelName:   relayInfo.OriginModelName,
		RequestPath: c.Path(),
		Retry:       common.GetPointer(0),
		ExcludeSet:  make(map[int]bool),
	}
	relayInfo.RetryIndex = 0
	relayInfo.LastError = nil
	// Health accounting is deferred until the loop ends: a failure a retry
	// recovered from is not evidence against the channel. See
	// model.RecordRequestAttempts.
	var attempts []catalog.ChannelAttempt
	winnerID, requestSucceeded := 0, false
	defer func() {
		catalog.GetChannelHealthManager().RecordRequestAttempts(attempts, winnerID, requestSucceeded)
	}()

	for ; retryParam.GetRetry() <= common.RetryTimes; retryParam.IncreaseRetry() {
		relayInfo.RetryIndex = retryParam.GetRetry()
		channel, channelErr := getChannel(c, relayInfo, retryParam)
		if channelErr != nil {
			logger.LogError(c.Context(), channelErr.Error())
			newAPIError = channelErr
			break
		}
		addUsedChannel(c, channel.Id)
		if billingErr := billing.PrepareTieredBillingForSelectedGroup(c, relayInfo); billingErr != nil {
			newAPIError = billingErr
			break
		}

		bodyStorage, bodyErr := common.GetBodyStorage(c)
		if bodyErr != nil {
			// Ensure consistent 413 for oversized bodies even when error occurs later (e.g., retry path)
			if common.IsRequestBodyTooLargeError(bodyErr) || errors.Is(bodyErr, common.ErrRequestBodyTooLarge) {
				newAPIError = types.NewErrorWithStatusCode(bodyErr, types.ErrorCodeReadRequestBodyFailed, http.StatusRequestEntityTooLarge, types.ErrOptionWithSkipRetry())
			} else {
				newAPIError = types.NewErrorWithStatusCode(bodyErr, types.ErrorCodeReadRequestBodyFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
			}
			break
		}
		c.ResetBody(io.NopCloser(bodyStorage))

		switch relayFormat {
		case types.RelayFormatOpenAIRealtime:
			newAPIError = relay.WssHelper(c, relayInfo)
		case types.RelayFormatClaude:
			newAPIError = relay.ClaudeHelper(c, relayInfo)
		case types.RelayFormatGemini:
			newAPIError = geminiRelayHandler(c, relayInfo)
		default:
			newAPIError = relayHandler(c, relayInfo)
		}

		if newAPIError == nil {
			relayInfo.LastError = nil
			winnerID, requestSucceeded = channel.Id, true
			return
		}

		newAPIError = billing.NormalizeViolationFeeError(newAPIError)
		relayInfo.LastError = newAPIError

		// Classify by what the failure implies about the channel, not by whether the
		// error code happens to carry a "channel:" prefix. Upstream 5xx and empty
		// bodies are the channel's fault, 429 means it is merely throttled, and a
		// 4xx such as 400 is the caller's problem and must not cost the channel.
		outcome := catalog.ClassifyChannelOutcome(newAPIError, channel.Id)
		attempts = append(attempts, catalog.ChannelAttempt{ChannelID: channel.Id, ModelName: relayInfo.OriginModelName, Outcome: outcome})
		if outcome.ExcludesChannel() && retryParam.ExcludeSet != nil {
			retryParam.ExcludeSet[channel.Id] = true
		}
		processChannelError(c, *types.NewChannelError(channel.Id, channel.Type, channel.Name, channel.ChannelInfo.IsMultiKey, common.GetCtxKeyString(c, constant.ContextKeyChannelKey), channel.GetAutoBan()), newAPIError)

		if !shouldRetry(c, newAPIError, common.RetryTimes-retryParam.GetRetry()) {
			break
		}
	}

	useChannel := c.GetStringSlice("use_channel")
	if len(useChannel) > 1 {
		retryLogStr := fmt.Sprintf("重试：%s", strings.Trim(strings.Join(strings.Fields(fmt.Sprint(useChannel)), "->"), "[]"))
		logger.LogInfo(c.Context(), retryLogStr)
	}
	if newAPIError != nil {
		gopool.Go(func() {
			record_perf.RecordRelaySample(relayInfo, false, 0)
		})
	}
}

var upgrader = websocket.Upgrader{
	Subprotocols: []string{"realtime"}, // WS 握手支持的协议，如果有使用 Sec-WebSocket-Protocol，则必须在此声明对应的 Protocol TODO add other protocol
	CheckOrigin: func(r *http.Request) bool {
		return true // 允许跨域
	},
}

func addUsedChannel(c contract.Context, channelId int) {
	useChannel := c.GetStringSlice("use_channel")
	useChannel = append(useChannel, fmt.Sprintf("%d", channelId))
	c.Set("use_channel", useChannel)
}

func fastTokenCountMetaForPricing(request dto.Request) *types.TokenCountMeta {
	if request == nil {
		return &types.TokenCountMeta{}
	}
	meta := &types.TokenCountMeta{
		TokenType: types.TokenTypeTokenizer,
	}
	switch r := request.(type) {
	case *dto.GeneralOpenAIRequest:
		maxCompletionTokens := lo.FromPtrOr(r.MaxCompletionTokens, uint(0))
		maxTokens := lo.FromPtrOr(r.MaxTokens, uint(0))
		if maxCompletionTokens > maxTokens {
			meta.MaxTokens = int(maxCompletionTokens)
		} else {
			meta.MaxTokens = int(maxTokens)
		}
	case *dto.OpenAIResponsesRequest:
		meta.MaxTokens = int(lo.FromPtrOr(r.MaxOutputTokens, uint(0)))
	case *dto.ClaudeRequest:
		meta.MaxTokens = int(lo.FromPtr(r.MaxTokens))
	case *dto.ImageRequest:
		// Pricing for image requests depends on ImagePriceRatio; safe to compute even when CountToken is disabled.
		return r.GetTokenCountMeta()
	default:
		// Best-effort: leave CombineText empty to avoid large allocations.
	}
	return meta
}

func getChannel(c contract.Context, info *relaycommon.RelayInfo, retryParam *catalog.SelectParams) (*catalog.Channel, *types.NewAPIError) {
	if info.ChannelMeta == nil {
		autoBan := c.GetBool("auto_ban")
		autoBanInt := 1
		if !autoBan {
			autoBanInt = 0
		}
		return &catalog.Channel{
			Id:      c.GetInt("channel_id"),
			Type:    c.GetInt("channel_type"),
			Name:    c.GetString("channel_name"),
			AutoBan: &autoBanInt,
		}, nil
	}
	channel, selectGroup, err := catalog.SelectChannel(retryParam)
	if err != nil {
		return nil, types.NewError(fmt.Errorf("获取分组 %s 下模型 %s 的可用渠道失败（retry）: %s", selectGroup, info.OriginModelName, err.Error()), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
	}
	if channel == nil {
		return nil, types.NewError(fmt.Errorf("分组 %s 下模型 %s 的可用渠道不存在（retry）", selectGroup, info.OriginModelName), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
	}

	info.PriceData.GroupRatioInfo = helper.HandleGroupRatio(c, info)

	newAPIError := middleware.SetupContextForSelectedChannel(c, channel, info.OriginModelName)
	if newAPIError != nil {
		return nil, newAPIError
	}
	return channel, nil
}

func shouldRetry(c contract.Context, openaiErr *types.NewAPIError, retryTimes int) bool {
	if openaiErr == nil {
		return false
	}
	if catalog.ShouldSkipRetryAfterChannelAffinityFailure(c) {
		return false
	}
	if types.IsChannelError(openaiErr) {
		return true
	}
	if types.IsSkipRetryError(openaiErr) {
		return false
	}
	if retryTimes <= 0 {
		return false
	}
	if _, ok := c.Get("specific_channel_id"); ok {
		return false
	}
	code := openaiErr.StatusCode
	if code >= 200 && code < 300 {
		return false
	}
	if code < 100 || code > 599 {
		return true
	}
	if status_code.IsAlwaysSkipRetryCode(openaiErr.GetErrorCode()) {
		return false
	}
	return status_code.ShouldRetryByStatusCode(code)
}

func processChannelError(c contract.Context, channelError types.ChannelError, err *types.NewAPIError) {
	logger.LogError(c.Context(), fmt.Sprintf("channel error (channel #%d, status code: %d): %s", channelError.ChannelId, err.StatusCode, common.LocalLogPreview(err.Error())))
	// 不要使用context获取渠道信息，异步处理时可能会出现渠道信息不一致的情况
	// do not use context to get channel info, there may be inconsistent channel info when processing asynchronously
	if catalog.ShouldDisableChannel(err) && channelError.AutoBan {
		gopool.Go(func() {
			catalog.DisableChannel(channelError, err.ErrorWithStatusCode())
		})
	}

	if constant.ErrorLogEnabled && types.IsRecordErrorLog(err) {
		// 保存错误日志到mysql中
		userId := c.GetInt("id")
		tokenName := c.GetString("token_name")
		modelName := c.GetString("original_model")
		tokenId := c.GetInt("token_id")
		userGroup := c.GetString("group")
		channelId := c.GetInt("channel_id")
		other := make(map[string]interface{})
		if c.HTTPRequest() != nil {
			other["request_path"] = c.Path()
		}
		other["error_type"] = err.GetErrorType()
		other["error_code"] = err.GetErrorCode()
		other["status_code"] = err.StatusCode
		other["channel_id"] = channelId
		other["channel_name"] = c.GetString("channel_name")
		other["channel_type"] = c.GetInt("channel_type")
		adminInfo := make(map[string]interface{})
		adminInfo["use_channel"] = c.GetStringSlice("use_channel")
		isMultiKey := common.GetCtxKeyBool(c, constant.ContextKeyChannelIsMultiKey)
		if isMultiKey {
			adminInfo["is_multi_key"] = true
			adminInfo["multi_key_index"] = common.GetCtxKeyInt(c, constant.ContextKeyChannelMultiKeyIndex)
		}
		catalog.AppendChannelAffinityAdminInfo(c, adminInfo)
		other["admin_info"] = adminInfo
		startTime := common.GetCtxKeyTime(c, constant.ContextKeyRequestStartTime)
		if startTime.IsZero() {
			startTime = time.Now()
		}
		useTimeSeconds := int(time.Since(startTime).Seconds())
		usage.RecordErrorLog(c, userId, channelId, modelName, tokenName, err.MaskSensitiveErrorWithStatusCode(), tokenId, useTimeSeconds, common.GetCtxKeyBool(c, constant.ContextKeyIsStream), userGroup, other)
	}

}

func RelayMidjourney(c contract.Context) {
	relayInfo, err := gateway.GenRelayInfo(c, types.RelayFormatMjProxy, nil, nil)

	if err != nil {
		_ = c.JSON(http.StatusInternalServerError, common.H{
			"description": fmt.Sprintf("failed to generate relay info: %s", err.Error()),
			"type":        "upstream_error",
			"code":        4,
		})
		return
	}

	var mjErr *taskdto.MidjourneyResponse
	switch relayInfo.RelayMode {
	case relayconstant.RelayModeMidjourneyNotify:
		mjErr = relay.RelayMidjourneyNotify(c)
	case relayconstant.RelayModeMidjourneyTaskFetch, relayconstant.RelayModeMidjourneyTaskFetchByCondition:
		mjErr = relay.RelayMidjourneyTask(c, relayInfo.RelayMode)
	case relayconstant.RelayModeMidjourneyTaskImageSeed:
		mjErr = relay.RelayMidjourneyTaskImageSeed(c)
	case relayconstant.RelayModeSwapFace:
		mjErr = relay.RelaySwapFace(c, relayInfo)
	default:
		mjErr = relay.RelayMidjourneySubmit(c, relayInfo)
	}
	//err = relayMidjourneySubmit(c, relayMode)
	log.Println(mjErr)
	if mjErr != nil {
		statusCode := http.StatusBadRequest
		if mjErr.Code == 30 {
			mjErr.Result = "当前分组负载已饱和，请稍后再试，或升级账户以提升服务质量。"
			statusCode = http.StatusTooManyRequests
		}
		_ = c.JSON(statusCode, common.H{
			"description": fmt.Sprintf("%s %s", mjErr.Description, mjErr.Result),
			"type":        "upstream_error",
			"code":        mjErr.Code,
		})
		channelId := c.GetInt("channel_id")
		logger.LogError(c.Context(), fmt.Sprintf("relay error (channel #%d, status code %d): %s", channelId, statusCode, fmt.Sprintf("%s %s", mjErr.Description, mjErr.Result)))
	}
}

func RelayNotImplemented(c contract.Context) {
	err := types.OpenAIError{
		Message: "API not implemented",
		Type:    "new_api_error",
		Param:   "",
		Code:    "api_not_implemented",
	}
	_ = c.JSON(http.StatusNotImplemented, common.H{
		"error": err,
	})
}

func RelayNotFound(c contract.Context) {
	err := types.OpenAIError{
		Message: fmt.Sprintf("Invalid URL (%s %s)", c.Method(), c.Path()),
		Type:    "invalid_request_error",
		Param:   "",
		Code:    "",
	}
	_ = c.JSON(http.StatusNotFound, common.H{
		"error": err,
	})
}

func RelayTaskFetch(c contract.Context) {
	relayInfo, err := gateway.GenRelayInfo(c, types.RelayFormatTask, nil, nil)
	if err != nil {
		_ = c.JSON(http.StatusInternalServerError, &taskdto.TaskError{
			Code:       "gen_relay_info_failed",
			Message:    err.Error(),
			StatusCode: http.StatusInternalServerError,
		})
		return
	}
	if taskErr := taskcap.FetchTask(c, relayInfo.RelayMode); taskErr != nil {
		respondTaskError(c, taskErr)
	}
}

func RelayTask(c contract.Context) {
	relayInfo, err := gateway.GenRelayInfo(c, types.RelayFormatTask, nil, nil)
	if err != nil {
		_ = c.JSON(http.StatusInternalServerError, &taskdto.TaskError{
			Code:       "gen_relay_info_failed",
			Message:    err.Error(),
			StatusCode: http.StatusInternalServerError,
		})
		return
	}

	// Build SubmitInfo from RelayInfo for capability functions
	submitInfo := &taskdomain.SubmitInfo{
		UserId:            relayInfo.UserId,
		TokenId:           relayInfo.TokenId,
		TokenGroup:        relayInfo.TokenGroup,
		SubscriptionId:    relayInfo.SubscriptionId,
		OriginModelName:   relayInfo.OriginModelName,
		UpstreamModelName: relayInfo.UpstreamModelName,
		Action:            relayInfo.Action,
		PublicTaskID:      relayInfo.PublicTaskID,
		OriginTaskID:      relayInfo.OriginTaskID,
		LockedChannel:     relayInfo.LockedChannel.(*catalog.Channel),
		ChannelId:         relayInfo.ChannelId,
		ChannelType:       relayInfo.ChannelType,
		ChannelBaseUrl:    relayInfo.ChannelBaseUrl,
		ApiKey:            relayInfo.ApiKey,
	}

	if taskErr := taskcap.ResolveOriginTask(c, submitInfo); taskErr != nil {
		respondTaskError(c, taskErr)
		return
	}

	// Copy back updated fields from SubmitInfo to RelayInfo for retry loop
	relayInfo.OriginModelName = submitInfo.OriginModelName
	relayInfo.UpstreamModelName = submitInfo.UpstreamModelName
	relayInfo.Action = submitInfo.Action
	relayInfo.LockedChannel = any(submitInfo.LockedChannel)
	relayInfo.ChannelId = submitInfo.ChannelId
	relayInfo.ChannelType = submitInfo.ChannelType
	relayInfo.ChannelBaseUrl = submitInfo.ChannelBaseUrl
	relayInfo.ApiKey = submitInfo.ApiKey

	var result *taskdomain.TaskSubmitResult
	var taskErr *taskdto.TaskError
	defer func() {
		if taskErr != nil && relayInfo.Billing != nil {
			relayInfo.Billing.Refund(c)
		}
	}()

	retryParam := &catalog.SelectParams{
		Ctx:         c,
		TokenGroup:  relayInfo.TokenGroup,
		ModelName:   relayInfo.OriginModelName,
		RequestPath: c.Path(),
		Retry:       common.GetPointer(0),
		ExcludeSet:  make(map[int]bool),
	}

	// Same deferral as Relay: only a request that exhausted its retries counts
	// against the channels it tried.
	var attempts []catalog.ChannelAttempt
	winnerID, requestSucceeded := 0, false
	defer func() {
		catalog.GetChannelHealthManager().RecordRequestAttempts(attempts, winnerID, requestSucceeded)
	}()

	for ; retryParam.GetRetry() <= common.RetryTimes; retryParam.IncreaseRetry() {
		var channel *catalog.Channel

		if lockedCh, ok := relayInfo.LockedChannel.(*catalog.Channel); ok && lockedCh != nil {
			channel = lockedCh
			if retryParam.GetRetry() > 0 {
				if setupErr := middleware.SetupContextForSelectedChannel(c, channel, relayInfo.OriginModelName); setupErr != nil {
					taskErr = taskdomain.TaskErrorWrapperLocal(setupErr.Err, "setup_locked_channel_failed", http.StatusInternalServerError)
					break
				}
			}
		} else {
			var channelErr *types.NewAPIError
			channel, channelErr = getChannel(c, relayInfo, retryParam)
			if channelErr != nil {
				logger.LogError(c.Context(), channelErr.Error())
				taskErr = taskdomain.TaskErrorWrapperLocal(channelErr.Err, "get_channel_failed", http.StatusInternalServerError)
				break
			}
		}

		addUsedChannel(c, channel.Id)
		bodyStorage, bodyErr := common.GetBodyStorage(c)
		if bodyErr != nil {
			if common.IsRequestBodyTooLargeError(bodyErr) || errors.Is(bodyErr, common.ErrRequestBodyTooLarge) {
				taskErr = taskdomain.TaskErrorWrapperLocal(bodyErr, "read_request_body_failed", http.StatusRequestEntityTooLarge)
			} else {
				taskErr = taskdomain.TaskErrorWrapperLocal(bodyErr, "read_request_body_failed", http.StatusBadRequest)
			}
			break
		}
		c.ResetBody(io.NopCloser(bodyStorage))

		submitInfo := taskdomain.SubmitInfo{
			UserId:            relayInfo.UserId,
			TokenId:           relayInfo.TokenId,
			TokenGroup:        relayInfo.TokenGroup,
			SubscriptionId:    relayInfo.SubscriptionId,
			OriginModelName:   relayInfo.OriginModelName,
			UpstreamModelName: relayInfo.UpstreamModelName,
			Action:            relayInfo.Action,
			PublicTaskID:      relayInfo.PublicTaskID,
			OriginTaskID:      relayInfo.OriginTaskID,
			LockedChannel:     channel,
			ChannelId:         channel.Id,
			ChannelType:       channel.Type,
			ChannelBaseUrl:    channel.GetBaseURL(),
			ApiKey:            channel.Key,
			ForcePreConsume:   relayInfo.ForcePreConsume,
		}
		result, taskErr = taskcap.SubmitTask(c, submitInfo)
		if taskErr == nil {
			winnerID, requestSucceeded = channel.Id, true
			break
		}

		if !taskErr.LocalError {
			// TaskError carries a StatusCode, so the same classification applies here:
			// build the equivalent NewAPIError once and reuse it for both the health
			// decision and the existing channel-error reporting.
			taskAPIError := types.NewOpenAIError(taskErr.Error, types.ErrorCodeBadResponseStatusCode, taskErr.StatusCode)
			outcome := catalog.ClassifyChannelOutcome(taskAPIError, channel.Id)
			attempts = append(attempts, catalog.ChannelAttempt{ChannelID: channel.Id, ModelName: relayInfo.OriginModelName, Outcome: outcome})
			if outcome.ExcludesChannel() && retryParam.ExcludeSet != nil {
				retryParam.ExcludeSet[channel.Id] = true
			}
			processChannelError(c,
				*types.NewChannelError(channel.Id, channel.Type, channel.Name, channel.ChannelInfo.IsMultiKey,
					common.GetCtxKeyString(c, constant.ContextKeyChannelKey), channel.GetAutoBan()),
				taskAPIError)
		}

		if !shouldRetryTaskRelay(c, channel.Id, taskErr, common.RetryTimes-retryParam.GetRetry()) {
			break
		}
	}

	useChannel := c.GetStringSlice("use_channel")
	if len(useChannel) > 1 {
		retryLogStr := fmt.Sprintf("重试：%s", strings.Trim(strings.Join(strings.Fields(fmt.Sprint(useChannel)), "->"), "[]"))
		logger.LogInfo(c.Context(), retryLogStr)
	}

	// ── 成功：结算 + 日志 + 插入任务 ──
	if taskErr == nil {
		if settleErr := billing.SettleBilling(c, relayInfo, result.Quota); settleErr != nil {
			common.SysError("settle task billing error: " + settleErr.Error())
		}
		taskdomain.LogTaskConsumption(c, relayInfo)

		task := taskdomain.InitTask(constant.TaskPlatform(result.Platform), relayInfo)
		task.PrivateData.UpstreamTaskID = result.UpstreamTaskID
		task.PrivateData.BillingSource = relayInfo.BillingSource
		task.PrivateData.SubscriptionId = relayInfo.SubscriptionId
		task.PrivateData.TokenId = relayInfo.TokenId
		task.PrivateData.NodeName = common.NodeName
		task.PrivateData.BillingContext = &taskdomain.TaskBillingContext{
			ModelPrice:      relayInfo.PriceData.ModelPrice,
			GroupRatio:      relayInfo.PriceData.GroupRatioInfo.GroupRatio,
			ModelRatio:      relayInfo.PriceData.ModelRatio,
			OtherRatios:     relayInfo.PriceData.OtherRatios(),
			OriginModelName: relayInfo.OriginModelName,
			PerCallBilling:  common.StringsContains(constant.TaskPricePatches, relayInfo.OriginModelName) || relayInfo.PriceData.UsePrice,
		}
		task.Quota = result.Quota
		task.Data = result.TaskData
		task.Action = relayInfo.Action
		if insertErr := task.Insert(); insertErr != nil {
			common.SysError("insert task error: " + insertErr.Error())
		}
	}

	if taskErr != nil {
		respondTaskError(c, taskErr)
	}
}

// respondTaskError 统一输出 Task 错误响应（含 429 限流提示改写）
func respondTaskError(c contract.Context, taskErr *taskdto.TaskError) {
	if taskErr.StatusCode == http.StatusTooManyRequests {
		taskErr.Message = "当前分组上游负载已饱和，请稍后再试"
	}
	_ = c.JSON(taskErr.StatusCode, taskErr)
}

func shouldRetryTaskRelay(c contract.Context, channelId int, taskErr *taskdto.TaskError, retryTimes int) bool {
	if taskErr == nil {
		return false
	}
	if catalog.ShouldSkipRetryAfterChannelAffinityFailure(c) {
		return false
	}
	if retryTimes <= 0 {
		return false
	}
	if _, ok := c.Get("specific_channel_id"); ok {
		return false
	}
	if taskErr.StatusCode == http.StatusTooManyRequests {
		return true
	}
	if taskErr.StatusCode == 307 {
		return true
	}
	if taskErr.StatusCode/100 == 5 {
		// 超时不重试
		if status_code.IsAlwaysSkipRetryStatusCode(taskErr.StatusCode) {
			return false
		}
		return true
	}
	if taskErr.StatusCode == http.StatusBadRequest {
		return false
	}
	if taskErr.StatusCode == 408 {
		// azure处理超时不重试
		return false
	}
	if taskErr.LocalError {
		return false
	}
	if taskErr.StatusCode/100 == 2 {
		return false
	}
	return true
}
