package ops

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/billing"
	"github.com/QuantumNous/new-api/internal/i18n"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/console_setting"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

func GetOptions(c contract.Context) {
	options := ListVisibleOptions()
	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"message": "",
		"data":    options,
	})
}

type OptionUpdateRequest struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
}

func UpdateOption(c contract.Context) {
	var option OptionUpdateRequest
	err := c.BindJSON(&option)
	if err != nil {
		_ = c.JSON(http.StatusBadRequest, common.H{
			"success": false,
			"message": "无效的参数",
		})
		return
	}
	switch option.Value.(type) {
	case bool:
		option.Value = common.Interface2String(option.Value.(bool))
	case float64:
		option.Value = common.Interface2String(option.Value.(float64))
	case int:
		option.Value = common.Interface2String(option.Value.(int))
	default:
		option.Value = fmt.Sprintf("%v", option.Value)
	}
	switch option.Key {
	case "QuotaForInviter", "QuotaForInvitee":
		if settingIsPositiveOptionValue(option.Value.(string)) && !operation_setting.IsPaymentComplianceConfirmed() {
			common.CtxApiErrorI18n(c, i18n.MsgPaymentComplianceRequired)
			return
		}
	default:
		if strings.HasPrefix(option.Key, "payment_setting.compliance_") {
			common.CtxApiErrorMsg(c, "合规确认字段不允许通过通用设置接口修改")
			return
		}
	}
	switch option.Key {
	case "GitHubOAuthEnabled":
		if option.Value == "true" && common.GitHubClientId == "" {
			_ = c.JSON(http.StatusOK, common.H{
				"success": false,
				"message": "无法启用 GitHub OAuth，请先填入 GitHub Client Id 以及 GitHub Client Secret！",
			})
			return
		}
	case "discord.enabled":
		if option.Value == "true" && system_setting.GetDiscordSettings().ClientId == "" {
			_ = c.JSON(http.StatusOK, common.H{
				"success": false,
				"message": "无法启用 Discord OAuth，请先填入 Discord Client Id 以及 Discord Client Secret！",
			})
			return
		}
	case "oidc.enabled":
		if option.Value == "true" && system_setting.GetOIDCSettings().ClientId == "" {
			_ = c.JSON(http.StatusOK, common.H{
				"success": false,
				"message": "无法启用 OIDC 登录，请先填入 OIDC Client Id 以及 OIDC Client Secret！",
			})
			return
		}
	case "LinuxDOOAuthEnabled":
		if option.Value == "true" && common.LinuxDOClientId == "" {
			_ = c.JSON(http.StatusOK, common.H{
				"success": false,
				"message": "无法启用 LinuxDO OAuth，请先填入 LinuxDO Client Id 以及 LinuxDO Client Secret！",
			})
			return
		}
	case "EmailDomainRestrictionEnabled":
		if option.Value == "true" && len(common.EmailDomainWhitelist) == 0 {
			_ = c.JSON(http.StatusOK, common.H{
				"success": false,
				"message": "无法启用邮箱域名限制，请先填入限制的邮箱域名！",
			})
			return
		}
	case "WeChatAuthEnabled":
		if option.Value == "true" && common.WeChatServerAddress == "" {
			_ = c.JSON(http.StatusOK, common.H{
				"success": false,
				"message": "无法启用微信登录，请先填入微信登录相关配置信息！",
			})
			return
		}
	case "TurnstileCheckEnabled":
		if option.Value == "true" && common.TurnstileSiteKey == "" {
			_ = c.JSON(http.StatusOK, common.H{
				"success": false,
				"message": "无法启用 Turnstile 校验，请先填入 Turnstile 校验相关配置信息！",
			})

			return
		}
	case "TelegramOAuthEnabled":
		if option.Value == "true" && common.TelegramBotToken == "" {
			_ = c.JSON(http.StatusOK, common.H{
				"success": false,
				"message": "无法启用 Telegram OAuth，请先填入 Telegram Bot Token！",
			})
			return
		}
	case "theme.frontend":
		if option.Value != "default" {
			_ = c.JSON(http.StatusOK, common.H{
				"success": false,
				"message": "Classic 前端已移除，主题只能设置为 default",
			})
			return
		}
	case "GroupRatio":
		err = ratio_setting.CheckGroupRatio(option.Value.(string))
		if err != nil {
			_ = c.JSON(http.StatusOK, common.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
	case "gemini.safety_settings":
		err = model_setting.ValidateGeminiSafetySettings(option.Value.(string))
		if err != nil {
			_ = c.JSON(http.StatusOK, common.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
	case "claude.default_max_tokens":
		err = model_setting.ValidateClaudeDefaultMaxTokens(option.Value.(string))
		if err != nil {
			_ = c.JSON(http.StatusOK, common.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
	case operation_setting.ToolPriceOptionKey:
		err = operation_setting.ValidateToolPricesJSON(option.Value.(string))
		if err != nil {
			_ = c.JSON(http.StatusOK, common.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
	case "ImageRatio":
		err = ratio_setting.UpdateImageRatioByJSONString(option.Value.(string))
		if err != nil {
			_ = c.JSON(http.StatusOK, common.H{
				"success": false,
				"message": "图片倍率设置失败: " + err.Error(),
			})
			return
		}
	case "AudioRatio":
		err = ratio_setting.UpdateAudioRatioByJSONString(option.Value.(string))
		if err != nil {
			_ = c.JSON(http.StatusOK, common.H{
				"success": false,
				"message": "音频倍率设置失败: " + err.Error(),
			})
			return
		}
	case "AudioCompletionRatio":
		err = ratio_setting.UpdateAudioCompletionRatioByJSONString(option.Value.(string))
		if err != nil {
			_ = c.JSON(http.StatusOK, common.H{
				"success": false,
				"message": "音频补全倍率设置失败: " + err.Error(),
			})
			return
		}
	case "CreateCacheRatio":
		err = ratio_setting.UpdateCreateCacheRatioByJSONString(option.Value.(string))
		if err != nil {
			_ = c.JSON(http.StatusOK, common.H{
				"success": false,
				"message": "缓存创建倍率设置失败: " + err.Error(),
			})
			return
		}
	case "ModelRequestRateLimitGroup":
		err = setting.CheckModelRequestRateLimitGroup(option.Value.(string))
		if err != nil {
			_ = c.JSON(http.StatusOK, common.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
	case "AutomaticDisableStatusCodes":
		_, err = operation_setting.ParseHTTPStatusCodeRanges(option.Value.(string))
		if err != nil {
			_ = c.JSON(http.StatusOK, common.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
	case "AutomaticRetryStatusCodes":
		_, err = operation_setting.ParseHTTPStatusCodeRanges(option.Value.(string))
		if err != nil {
			_ = c.JSON(http.StatusOK, common.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
	case "console_setting.api_info":
		err = console_setting.ValidateConsoleSettings(option.Value.(string), "ApiInfo")
		if err != nil {
			_ = c.JSON(http.StatusOK, common.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
	case "console_setting.announcements":
		err = console_setting.ValidateConsoleSettings(option.Value.(string), "Announcements")
		if err != nil {
			_ = c.JSON(http.StatusOK, common.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
	case "console_setting.faq":
		err = console_setting.ValidateConsoleSettings(option.Value.(string), "FAQ")
		if err != nil {
			_ = c.JSON(http.StatusOK, common.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
	case "console_setting.uptime_kuma_groups":
		err = console_setting.ValidateConsoleSettings(option.Value.(string), "UptimeKumaGroups")
		if err != nil {
			_ = c.JSON(http.StatusOK, common.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
	}
	err = model.UpdateOption(option.Key, option.Value.(string))
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	// 出于安全考虑只记录被修改的配置项名称，不记录配置值（可能含密钥等敏感信息）。
	billing.RecordManageAudit(c, "option.update", map[string]interface{}{
		"key": option.Key,
	})
	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"message": "",
	})
}
