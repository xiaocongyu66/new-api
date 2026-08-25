package controller

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	taskdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	"github.com/QuantumNous/new-api/pkg/routestats"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/samber/lo"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

func relayHandler(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
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

func geminiRelayHandler(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	var err *types.NewAPIError
	if strings.Contains(c.Request.URL.Path, "embed") {
		err = relay.GeminiEmbeddingHandler(c, info)
	} else {
		err = relay.GeminiHelper(c, info)
	}
	return err
}

// canWriteErrorBody reports whether the deferred error handler may still write a
// response body. Once the body has started, the status line is spent and a JSON
// error would land inside the already-open stream as bare, prefix-less bytes that
// no SSE client can parse; handlers that fail mid-stream report the failure
// in-band instead (#394). Realtime relays are exempt: they answer over the
// websocket, not the HTTP body.
func canWriteErrorBody(relayFormat types.RelayFormat, bodyStarted bool) bool {
	if relayFormat == types.RelayFormatOpenAIRealtime {
		return true
	}
	return !bodyStarted
}

func Relay(c *gin.Context, relayFormat types.RelayFormat) {

	requestId := c.GetString(common.RequestIdKey)
	//group := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	//originalModel := common.GetContextKeyString(c, constant.ContextKeyOriginalModel)

	var (
		newAPIError *types.NewAPIError
		ws          *websocket.Conn
	)

	if relayFormat == types.RelayFormatOpenAIRealtime {
		var err error
		ws, err = upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			helper.WssError(c, ws, types.NewError(err, types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry()).ToOpenAIError())
			return
		}
		defer ws.Close()
	}

	defer func() {
		if newAPIError != nil {
			logger.LogError(c, fmt.Sprintf("relay error: %s", common.LocalLogPreview(newAPIError.Error())))
			newAPIError.SetMessage(common.MessageWithRequestId(newAPIError.Error(), requestId))
			if !canWriteErrorBody(relayFormat, c.Writer.Written()) {
				return
			}
			switch relayFormat {
			case types.RelayFormatOpenAIRealtime:
				helper.WssError(c, ws, newAPIError.ToOpenAIError())
			case types.RelayFormatClaude:
				c.JSON(newAPIError.StatusCode, gin.H{
					"type":  "error",
					"error": newAPIError.ToClaudeError(),
				})
			default:
				c.JSON(newAPIError.StatusCode, gin.H{
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

	relayInfo, err := relaycommon.GenRelayInfo(c, relayFormat, request, ws)
	if err != nil {
		newAPIError = types.NewError(err, types.ErrorCodeGenRelayInfoFailed)
		return
	}

	needSensitiveCheck := setting.ShouldCheckPromptSensitive()
	needCountToken := constant.CountToken
	// Avoid building huge CombineText (strings.Join) when token counting and sensitive check are both disabled.
	var meta *types.TokenCountMeta
	if needSensitiveCheck || needCountToken {
		meta = request.GetTokenCountMeta()
	} else {
		meta = fastTokenCountMetaForPricing(request)
	}

	// 敏感内容静默过滤：目标域名与词库敏感词从请求文本中删除后照常转发，
	// 不再 403（攻击者无感知）。破甲组合层已随旧拦截路径一并删除。
	if needSensitiveCheck {
		if labels := service.SanitizeRelayRequest(request); len(labels) > 0 {
			logger.LogInfo(c, fmt.Sprintf("input sanitized by sensitive filter: %s", strings.Join(labels, ",")))
			service.RecordSensitiveBlock(c, "input", "sanitize:"+strings.Join(labels, ","), meta.CombineText)
			// 消息已被净化，按净化后文本重建 token 统计。
			meta = request.GetTokenCountMeta()
		}
	}

	tokens, err := service.EstimateRequestToken(c, meta, relayInfo)
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

	// common.SetContextKey(c, constant.ContextKeyTokenCountMeta, meta)

	if priceData.FreeModel {
		logger.LogInfo(c, fmt.Sprintf("模型 %s 免费，跳过预扣费", relayInfo.OriginModelName))
	} else {
		newAPIError = service.PreConsumeBilling(c, priceData.QuotaToPreConsume, relayInfo)
		if newAPIError != nil {
			return
		}
	}

	defer func() {
		// Only return quota if downstream failed and quota was actually pre-consumed
		if newAPIError != nil {
			newAPIError = service.NormalizeViolationFeeError(newAPIError)
			if relayInfo.Billing != nil {
				relayInfo.Billing.Refund(c)
			}
			service.ChargeViolationFeeIfNeeded(c, relayInfo, newAPIError)
		}
	}()

	retryParam := &service.RetryParam{
		Ctx:           c,
		TokenGroup:    relayInfo.TokenGroup,
		ModelName:     relayInfo.OriginModelName,
		RequestPath:   c.Request.URL.Path,
		Retry:         common.GetPointer(0),
		ExcludeRoutes: make(map[model.RouteKey]bool),
	}
	relayInfo.RetryIndex = 0
	relayInfo.LastError = nil

	for ; retryParam.GetRetry() <= common.RetryTimes; retryParam.IncreaseRetry() {
		relayInfo.RetryIndex = retryParam.GetRetry()
		route, channelErr := getChannel(c, relayInfo, retryParam)
		if channelErr != nil {
			logger.LogError(c, channelErr.Error())
			newAPIError = channelErr
			break
		}
		channel := route.Channel
		addUsedChannel(c, channel.Id)
		if billingErr := service.PrepareTieredBillingForSelectedGroup(c, relayInfo); billingErr != nil {
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
		c.Request.Body = io.NopCloser(bodyStorage)

		attemptStart := time.Now()

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

		attemptLatencyMs := time.Since(attemptStart).Seconds() * 1000
		handle := relayInfo.StatsHandle

		// observeAttemptTiming feeds the latency-family EWMAs. It is deliberately
		// NOT called for neutral outcomes: a user 4xx or a client cancel says
		// nothing about the route, and charging its timing would let a caller's
		// own aborted request move the route's score.
		observeAttemptTiming := func() {
			if handle == nil {
				return
			}
			if relayInfo.IsStream && relayInfo.HasSendResponse() {
				handle.ObserveTTFT(relayInfo.FirstResponseTime.Sub(relayInfo.StartTime).Seconds() * 1000)
			}
			handle.ObserveLatency(attemptLatencyMs)
		}

		if newAPIError == nil {
			// Two independent signals are charged for one attempt, and both must
			// fire. The soft signal (#405 EWMA) only nudges a continuous
			// preference inside [0.5, 1.5]; the hard signal (#368 state machine)
			// owns discrete isolation and is the only thing that can zero a route
			// out. Recording one and not the other silently disables half the
			// scheduler.
			observeAttemptTiming()
			if handle != nil {
				handle.ObserveSuccess(routestats.SuccessObservation)
			}
			if healthErr := model.RecordSuccess(model.RouteKey{
				ChannelId: channel.Id,
				KeyIndex:  common.GetContextKeyInt(c, constant.ContextKeyChannelMultiKeyIndex),
				Model:     relayInfo.OriginModelName,
			}, time.Now()); healthErr != nil {
				logger.LogError(c, fmt.Sprintf("record route success failed: %s", healthErr.Error()))
			}
			relayInfo.LastError = nil
			return
		}

		newAPIError = service.NormalizeViolationFeeError(newAPIError)
		relayInfo.LastError = newAPIError

		// Record each retry-eligible upstream failure against its route as it
		// happens, rather than deferring to a single terminal record. This
		// ensures every failing attempt in a retry chain is counted.
		retryEligible := shouldRetry(c, newAPIError, common.RetryTimes-retryParam.GetRetry())
		if retryEligible && retryParam.ExcludeRoutes != nil {
			// Exclude the exact route unit that failed, not the whole channel: a
			// dead key on a multi-key channel must not cost its siblings, and the
			// route unit is what selection actually picks.
			retryParam.ExcludeRoutes[model.RouteKey{
				ChannelId: channel.Id,
				KeyIndex:  common.GetContextKeyInt(c, constant.ContextKeyChannelMultiKeyIndex),
				Model:     relayInfo.OriginModelName,
			}] = true
		}
		if common.RetryTimes > 0 && wouldRetryWithOneBudget(c, newAPIError) {
			recordRouteIsolation(c, model.RouteKey{ChannelId: channel.Id, KeyIndex: common.GetContextKeyInt(c, constant.ContextKeyChannelMultiKeyIndex), Model: relayInfo.OriginModelName}, newAPIError, classifyChatFailureSource(newAPIError))
		}
		processChannelError(c, *types.NewChannelError(channel.Id, channel.Type, channel.Name, channel.ChannelInfo.IsMultiKey, common.GetContextKeyString(c, constant.ContextKeyChannelKey), channel.GetAutoBan()), newAPIError)

		// Route stats are charged per attempt (D1), and a failed attempt still
		// feeds the latency family (D3) so a route that fails fast cannot look
		// like the fastest route in the pool. A 429 is derated to 0.7 rather than
		// 0.0 because the route is busy, not broken; a caller's own 4xx says
		// nothing about the route and is recorded as nothing at all.
		if handle != nil {
			switch classifyRouteStatsOutcome(newAPIError) {
			case routeStatsThrottled:
				// Observe429 folds in the synthetic TTFT penalty itself, so the
				// observed timing must not be charged a second time here.
				handle.Observe429(0)
			case routeStatsFatal:
				observeAttemptTiming()
				handle.ObserveSuccess(routestats.FatalObservation)
			}
		}

		if !retryEligible {
			break
		}
	}

	useChannel := c.GetStringSlice("use_channel")
	if len(useChannel) > 1 {
		retryLogStr := fmt.Sprintf("重试：%s", strings.Trim(strings.Join(strings.Fields(fmt.Sprint(useChannel)), "->"), "[]"))
		logger.LogInfo(c, retryLogStr)
	}
	if newAPIError != nil {
		gopool.Go(func() {
			perfmetrics.RecordRelaySample(relayInfo, false, 0)
		})
	}

}

var upgrader = websocket.Upgrader{
	Subprotocols: []string{"realtime"}, // WS 握手支持的协议，如果有使用 Sec-WebSocket-Protocol，则必须在此声明对应的 Protocol TODO add other protocol
	CheckOrigin: func(r *http.Request) bool {
		return true // 允许跨域
	},
}

func addUsedChannel(c *gin.Context, channelId int) {
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

func getChannel(c *gin.Context, info *relaycommon.RelayInfo, retryParam *service.RetryParam) (*model.SelectedRoute, *types.NewAPIError) {
	if info.ChannelMeta == nil {
		autoBan := c.GetBool("auto_ban")
		autoBanInt := 1
		if !autoBan {
			autoBanInt = 0
		}
		// Build a minimal SelectedRoute from context for specific channel case
		return &model.SelectedRoute{
			Channel: &model.Channel{
				Id:      c.GetInt("channel_id"),
				Type:    c.GetInt("channel_type"),
				Name:    c.GetString("channel_name"),
				AutoBan: &autoBanInt,
			},
			ChannelId: c.GetInt("channel_id"),
		}, nil
	}
	route, selectGroup, err := service.CacheGetRandomSatisfiedChannel(retryParam)
	if err != nil {
		return nil, types.NewError(fmt.Errorf("获取分组 %s 下模型 %s 的可用渠道失败（retry）: %s", selectGroup, info.OriginModelName, err.Error()), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
	}
	if route == nil {
		return nil, types.NewError(fmt.Errorf("分组 %s 下模型 %s 的可用渠道不存在（retry）", selectGroup, info.OriginModelName), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
	}

	// Pass route stats handle to relayInfo for per-attempt attribution
	info.StatsHandle = route.StatsHandle

	info.PriceData.GroupRatioInfo = helper.HandleGroupRatio(c, info)

	newAPIError := middleware.SetupContextForSelectedChannel(c, route, info.OriginModelName)
	if newAPIError != nil {
		return nil, newAPIError
	}
	return route, nil
}

// recordRouteIsolation persists one retry-eligible failure against the route and
// logs the resulting transition. The log line carries the request id, so an
// operator can tell a RouteKey transition apart from a plain upstream failure
// (logged by processChannelError) and from a system performance rejection
// (logged by the SystemPerformanceCheck middleware).
func recordRouteIsolation(c *gin.Context, routeKey model.RouteKey, apiErr *types.NewAPIError, source model.FailureSource) {
	now := time.Now()
	if healthErr := model.RecordRetryableFailure(routeKey, string(apiErr.GetErrorCode()), source, now); healthErr != nil {
		logger.LogError(c, healthErr.Error())
		return
	}
	state, level, until, ok := model.GetRouteIsolation(routeKey)
	if !ok {
		return
	}
	// A disabled route has no deadline, and a clock adjustment could leave an
	// elapsed one behind; clamp so the log never reports a negative countdown.
	remaining := int64(0)
	if until > now.Unix() {
		remaining = until - now.Unix()
	}
	logger.LogWarn(c, fmt.Sprintf("route isolation: channel #%d model %s -> %s level=%d remaining=%ds error_code=%s",
		routeKey.ChannelId, routeKey.Model, state, level, remaining, apiErr.GetErrorCode()))
}

// wouldRetryWithOneBudget reports whether a chat relay error would retry if a
// single retry were available, independent of the remaining budget. It reuses
// shouldRetry with retryTimes=1 so the skip/channel/specific-channel/status-code
// rules live in exactly one place. The RetryTimes>0 gate is applied at the call
// site to preserve #376 acceptance: RetryTimes=0 writes no state.
func wouldRetryWithOneBudget(c *gin.Context, err *types.NewAPIError) bool {
	return shouldRetry(c, err, 1)
}

// classifyChatFailureSource maps a chat relay error to its failure source.
// ErrorCodeDoRequestFailed is a local transport failure (DNS, connection
// refused, TLS handshake) — our infrastructure could not reach the provider,
// not a provider response. Provider/status transaction failures
// (bad_response_status_code, bad_response, bad_response_body, empty_response)
// are upstream: the provider answered with an error.
func classifyChatFailureSource(err *types.NewAPIError) model.FailureSource {
	if err == nil {
		return model.FailureSourceUpstream
	}
	if err.GetErrorCode() == types.ErrorCodeDoRequestFailed {
		return model.FailureSourceLocal
	}
	return model.FailureSourceUpstream
}

// routeStatsOutcome tells the EWMA how much of a failure an attempt was. It
// exists only to shape the soft signal: isolation, disabling and retry
// eligibility are decided elsewhere by the state machine and shouldRetry.
type routeStatsOutcome int

const (
	// routeStatsNeutral records nothing. A caller's own 4xx, a client cancel or
	// a local transport failure says nothing about the route's quality, and
	// charging it would let a caller move a healthy route's score.
	routeStatsNeutral routeStatsOutcome = iota
	// routeStatsThrottled is a 429: the route is busy, not broken.
	routeStatsThrottled
	// routeStatsFatal is a failure the route is answerable for: a channel error,
	// an unusable response body, or a 5xx.
	routeStatsFatal
)

// classifyRouteStatsOutcome maps a relay error onto the soft signal. It replaces
// the channel-level ClassifyChannelOutcome that #368 removed along with the old
// per-channel EWMA layer; the 401 escalation that classifier carried is gone on
// purpose, because a dead credential is now handled by the key probe and the
// state machine rather than by derating a score.
func classifyRouteStatsOutcome(err *types.NewAPIError) routeStatsOutcome {
	if err == nil {
		return routeStatsNeutral
	}
	if types.IsChannelError(err) || err.GetErrorCode() == types.ErrorCodeBadResponseBody {
		return routeStatsFatal
	}
	if err.StatusCode == http.StatusTooManyRequests {
		return routeStatsThrottled
	}
	if err.StatusCode >= http.StatusInternalServerError {
		return routeStatsFatal
	}
	return routeStatsNeutral
}

func shouldRetry(c *gin.Context, openaiErr *types.NewAPIError, retryTimes int) bool {
	if openaiErr == nil {
		return false
	}
	if service.ShouldSkipRetryAfterChannelAffinityFailure(c) {
		return false
	}
	if types.IsSkipRetryError(openaiErr) {
		return false
	}
	if retryTimes <= 0 {
		return false
	}
	if types.IsChannelError(openaiErr) {
		return true
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
	if operation_setting.IsAlwaysSkipRetryCode(openaiErr.GetErrorCode()) {
		return false
	}
	return operation_setting.ShouldRetryByStatusCode(code)
}

func processChannelError(c *gin.Context, channelError types.ChannelError, err *types.NewAPIError) {
	logger.LogError(c, fmt.Sprintf("channel error (channel #%d, status code: %d): %s", channelError.ChannelId, err.StatusCode, common.LocalLogPreview(err.Error())))
	// 不要使用context获取渠道信息，异步处理时可能会出现渠道信息不一致的情况
	// do not use context to get channel info, there may be inconsistent channel info when processing asynchronously
	if service.ShouldDisableChannel(err) && channelError.AutoBan {
		gopool.Go(func() {
			service.DisableChannel(channelError, err.ErrorWithStatusCode())
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
		if c.Request != nil && c.Request.URL != nil {
			other["request_path"] = c.Request.URL.Path
		}
		other["error_type"] = err.GetErrorType()
		other["error_code"] = err.GetErrorCode()
		other["status_code"] = err.StatusCode
		other["channel_id"] = channelId
		other["channel_name"] = c.GetString("channel_name")
		other["channel_type"] = c.GetInt("channel_type")
		adminInfo := make(map[string]interface{})
		adminInfo["use_channel"] = c.GetStringSlice("use_channel")
		isMultiKey := common.GetContextKeyBool(c, constant.ContextKeyChannelIsMultiKey)
		if isMultiKey {
			adminInfo["is_multi_key"] = true
			adminInfo["multi_key_index"] = common.GetContextKeyInt(c, constant.ContextKeyChannelMultiKeyIndex)
		}
		service.AppendChannelAffinityAdminInfo(c, adminInfo)
		other["admin_info"] = adminInfo
		startTime := common.GetContextKeyTime(c, constant.ContextKeyRequestStartTime)
		if startTime.IsZero() {
			startTime = time.Now()
		}
		useTimeSeconds := int(time.Since(startTime).Seconds())
		model.RecordErrorLog(c, userId, channelId, modelName, tokenName, err.MaskSensitiveErrorWithStatusCode(), tokenId, useTimeSeconds, common.GetContextKeyBool(c, constant.ContextKeyIsStream), userGroup, other)
	}

}

func RelayMidjourney(c *gin.Context) {
	relayInfo, err := relaycommon.GenRelayInfo(c, types.RelayFormatMjProxy, nil, nil)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
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
		c.JSON(statusCode, gin.H{
			"description": fmt.Sprintf("%s %s", mjErr.Description, mjErr.Result),
			"type":        "upstream_error",
			"code":        mjErr.Code,
		})
		channelId := c.GetInt("channel_id")
		logger.LogError(c, fmt.Sprintf("relay error (channel #%d, status code %d): %s", channelId, statusCode, fmt.Sprintf("%s %s", mjErr.Description, mjErr.Result)))
	}
}

func RelayNotImplemented(c *gin.Context) {
	err := types.OpenAIError{
		Message: "API not implemented",
		Type:    "new_api_error",
		Param:   "",
		Code:    "api_not_implemented",
	}
	c.JSON(http.StatusNotImplemented, gin.H{
		"error": err,
	})
}

func RelayNotFound(c *gin.Context) {
	err := types.OpenAIError{
		Message: fmt.Sprintf("Invalid URL (%s %s)", c.Request.Method, c.Request.URL.Path),
		Type:    "invalid_request_error",
		Param:   "",
		Code:    "",
	}
	c.JSON(http.StatusNotFound, gin.H{
		"error": err,
	})
}

func RelayTaskFetch(c *gin.Context) {
	relayInfo, err := relaycommon.GenRelayInfo(c, types.RelayFormatTask, nil, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, &taskdto.TaskError{
			Code:       "gen_relay_info_failed",
			Message:    err.Error(),
			StatusCode: http.StatusInternalServerError,
		})
		return
	}
	if taskErr := relay.RelayTaskFetch(c, relayInfo.RelayMode); taskErr != nil {
		respondTaskError(c, taskErr)
	}
}

func RelayTask(c *gin.Context) {
	relayInfo, err := relaycommon.GenRelayInfo(c, types.RelayFormatTask, nil, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, &taskdto.TaskError{
			Code:       "gen_relay_info_failed",
			Message:    err.Error(),
			StatusCode: http.StatusInternalServerError,
		})
		return
	}

	if taskErr := relay.ResolveOriginTask(c, relayInfo); taskErr != nil {
		respondTaskError(c, taskErr)
		return
	}

	var result *relay.TaskSubmitResult
	var taskErr *taskdto.TaskError
	defer func() {
		if taskErr != nil && relayInfo.Billing != nil {
			relayInfo.Billing.Refund(c)
		}
	}()

	retryParam := &service.RetryParam{
		Ctx:           c,
		TokenGroup:    relayInfo.TokenGroup,
		ModelName:     relayInfo.OriginModelName,
		RequestPath:   c.Request.URL.Path,
		Retry:         common.GetPointer(0),
		ExcludeRoutes: make(map[model.RouteKey]bool),
	}

	for ; retryParam.GetRetry() <= common.RetryTimes; retryParam.IncreaseRetry() {
		var route *model.SelectedRoute

		if lockedCh, ok := relayInfo.LockedChannel.(*model.Channel); ok && lockedCh != nil {
			// Locked channel replay: build SelectedRoute from the channel
			var err error
			route, err = model.SelectedRouteFromChannel(lockedCh, relayInfo.OriginModelName)
			if err != nil {
				taskErr = service.TaskErrorWrapperLocal(err, "setup_locked_channel_failed", http.StatusInternalServerError)
				break
			}
			if retryParam.GetRetry() > 0 {
				if setupErr := middleware.SetupContextForSelectedChannel(c, route, relayInfo.OriginModelName); setupErr != nil {
					taskErr = service.TaskErrorWrapperLocal(setupErr.Err, "setup_locked_channel_failed", http.StatusInternalServerError)
					break
				}
			}
		} else {
			var channelErr *types.NewAPIError
			route, channelErr = getChannel(c, relayInfo, retryParam)
			if channelErr != nil {
				logger.LogError(c, channelErr.Error())
				taskErr = service.TaskErrorWrapperLocal(channelErr.Err, "get_channel_failed", http.StatusInternalServerError)
				break
			}
		}
		channel := route.Channel

		addUsedChannel(c, channel.Id)
		bodyStorage, bodyErr := common.GetBodyStorage(c)
		if bodyErr != nil {
			if common.IsRequestBodyTooLargeError(bodyErr) || errors.Is(bodyErr, common.ErrRequestBodyTooLarge) {
				taskErr = service.TaskErrorWrapperLocal(bodyErr, "read_request_body_failed", http.StatusRequestEntityTooLarge)
			} else {
				taskErr = service.TaskErrorWrapperLocal(bodyErr, "read_request_body_failed", http.StatusBadRequest)
			}
			break
		}
		c.Request.Body = io.NopCloser(bodyStorage)

		result, taskErr = relay.RelayTaskSubmit(c, relayInfo)
		if taskErr == nil {
			break
		}

		retryEligible := shouldRetryTaskRelay(c, channel.Id, taskErr, common.RetryTimes-retryParam.GetRetry())
		// Record each retry-eligible upstream failure against its route as it
		// happens. Only upstream (non-local) errors reflect channel health.
		if !taskErr.LocalError {
			taskAPIError := types.NewOpenAIError(taskErr.Error, types.ErrorCodeBadResponseStatusCode, taskErr.StatusCode)
			if retryEligible && retryParam.ExcludeRoutes != nil {
				// Exclude the exact route unit that failed: (ChannelId, KeyIndex, Model).
				retryParam.ExcludeRoutes[model.RouteKey{
					ChannelId: channel.Id,
					KeyIndex:  common.GetContextKeyInt(c, constant.ContextKeyChannelMultiKeyIndex),
					Model:     relayInfo.OriginModelName,
				}] = true
			}
			if common.RetryTimes > 0 && shouldRetryTaskRelay(c, channel.Id, taskErr, 1) {
				recordRouteIsolation(c, model.RouteKey{ChannelId: channel.Id, KeyIndex: common.GetContextKeyInt(c, constant.ContextKeyChannelMultiKeyIndex), Model: relayInfo.OriginModelName}, taskAPIError, model.FailureSourceUpstream)
			}
			processChannelError(c,
				*types.NewChannelError(channel.Id, channel.Type, channel.Name, channel.ChannelInfo.IsMultiKey,
					common.GetContextKeyString(c, constant.ContextKeyChannelKey), channel.GetAutoBan()),
				taskAPIError)
		}

		if !retryEligible {
			break
		}
	}

	useChannel := c.GetStringSlice("use_channel")
	if len(useChannel) > 1 {
		retryLogStr := fmt.Sprintf("重试：%s", strings.Trim(strings.Join(strings.Fields(fmt.Sprint(useChannel)), "->"), "[]"))
		logger.LogInfo(c, retryLogStr)
	}

	// ── 成功：结算 + 日志 + 插入任务 ──
	if taskErr == nil {
		if settleErr := service.SettleBilling(c, relayInfo, result.Quota); settleErr != nil {
			common.SysError("settle task billing error: " + settleErr.Error())
		}
		service.LogTaskConsumption(c, relayInfo)

		task := model.InitTask(result.Platform, relayInfo)
		task.PrivateData.UpstreamTaskID = result.UpstreamTaskID
		task.PrivateData.BillingSource = relayInfo.BillingSource
		task.PrivateData.SubscriptionId = relayInfo.SubscriptionId
		task.PrivateData.TokenId = relayInfo.TokenId
		task.PrivateData.NodeName = common.NodeName
		task.PrivateData.BillingContext = &model.TaskBillingContext{
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
func respondTaskError(c *gin.Context, taskErr *taskdto.TaskError) {
	if taskErr.StatusCode == http.StatusTooManyRequests {
		taskErr.Message = "当前分组上游负载已饱和，请稍后再试"
	}
	c.JSON(taskErr.StatusCode, taskErr)
}

func shouldRetryTaskRelay(c *gin.Context, channelId int, taskErr *taskdto.TaskError, retryTimes int) bool {
	if taskErr == nil {
		return false
	}
	if service.ShouldSkipRetryAfterChannelAffinityFailure(c) {
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
		if operation_setting.IsAlwaysSkipRetryStatusCode(taskErr.StatusCode) {
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
