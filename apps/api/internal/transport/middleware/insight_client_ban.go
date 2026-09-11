package middleware

import (
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/internal/logger"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/internal/usage"
	"github.com/QuantumNous/new-api/internal/usage/insight"
)

// InsightClientBan 执行用户画像模块的"客户端封禁"指令：
// 请求被请求头识别为封禁客户端时直接 403，不进渠道分发、不预扣费，
// 封禁的代价为零。
//
// 识别只用请求头（User-Agent + 工具专属头），不依赖画像的分析开关，
// 因此即使画像总开关（enabled）关闭，封禁仍然有效；真正的总开关是
// user_insight_setting.client_ban_enabled，关闭时直接放行。
// 必须挂在 TokenAuth 之后（单用户档封禁依赖上下文里的用户 ID），
// 与 ModelRequestRateLimit 同一位置。
func InsightClientBan() contract.Middleware {
	return func(c contract.Context) {
		if !usage.GetUserInsightSetting().ClientBanEnabled {
			c.Next()
			return
		}
		// 与画像分析共用 ResolveClient：内置规则 + 自动发现走同一套判定，
		// 否则封禁界面里勾的标识与这里算出的对不上，勾了也拦不住。
		client, _, _, _, _, _ := insight.ResolveClient(c.Headers(), "")
		if client == "" {
			c.Next()
			return
		}
		scope := usage.CheckClientBan(c.GetInt("id"), client)
		if scope == "" {
			c.Next()
			return
		}
		rejectBannedClient(c, client, scope)
	}
}

// rejectBannedClient 拒绝命中封禁的请求并留审计日志。
// 403 而不是 404/429：这不是限流，也不是资源不存在，是策略性拒绝。
func rejectBannedClient(c contract.Context, client, scope string) {
	message := "您使用的客户端已被站点管理员封禁"
	if scope == "user" {
		message = "该客户端已被管理员对您的账号禁用"
	}
	logger.LogWarn(c.Context(), fmt.Sprintf(
		"insight client ban rejected: user=%d client=%s scope=%s path=%s",
		c.GetInt("id"), client, scope, c.Path(),
	))
	abortWithOpenAiMessage(c, http.StatusForbidden, message)
}
