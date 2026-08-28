// Package settings owns the runtime configuration mechanism: seeding the
// option map with typed defaults, applying option updates to their typed
// targets, validating option values, and the gateway-routing key contract.
//
// It deliberately holds NO admin HTTP use cases and performs NO database
// access: callers (model layer) feed option rows in via ApplyOption.
package settings

import (
	"sort"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/catalog/resolve_group"
	"github.com/QuantumNous/new-api/internal/settings/config"
	"github.com/QuantumNous/new-api/internal/billing/pay_subscription"
	"github.com/QuantumNous/new-api/internal/billing/price_expression"
	"github.com/QuantumNous/new-api/internal/catalog/health_store"
	"github.com/QuantumNous/new-api/internal/catalog/manage_channels"
	"github.com/QuantumNous/new-api/internal/transport/middleware/status_code"
		ratio_setting "github.com/QuantumNous/new-api/internal/catalog/configure_ratio"
	"github.com/QuantumNous/new-api/internal/egress/fetch_url"
	"github.com/QuantumNous/new-api/internal/sensitive"
	"github.com/QuantumNous/new-api/internal/usage/record_perf_config"
	"github.com/QuantumNous/new-api/internal/transport/middleware/rate_limit"
)

// RetiredThemeOptionKey is the option key of the removed classic frontend
// theme. It must never re-enter the option map.
const RetiredThemeOptionKey = "theme.frontend"

// OnBillingSettingChanged invalidates caches that live outside this package
// when a billing_setting.* tiered config changes. Wired by the host module to
// avoid an import cycle.
var OnBillingSettingChanged func()

// OnPerformanceSettingChanged invalidates caches that live outside this package
// when performance_setting.* config changes. Wired by main to keep settings
// free of internal/usage/record_perf import (record_perf depends on model for
// flush ops, which would cycle with the model layer).
var OnPerformanceSettingChanged func()


// GatewayRoutingOptionKeys is deliberately explicit. New settings must be
// reviewed before they become part of the gateway snapshot contract.
var GatewayRoutingOptionKeys = map[string]struct{}{
	"ModelRatio": {}, "ModelPrice": {}, "CompletionRatio": {},
	"CacheRatio": {}, "CreateCacheRatio": {}, "ImageRatio": {},
	"AudioRatio": {}, "AudioCompletionRatio": {}, "GroupRatio": {},
	"GroupGroupRatio": {}, "UserUsableGroups": {}, "AutoGroups": {},
	"MaxTokenAutoGroups": {},
}

func IsGatewayRoutingOptionKey(key string) bool {
	_, ok := GatewayRoutingOptionKeys[key]
	return ok
}

func GatewayRoutingOptionKeyList() []string {
	keys := make([]string, 0, len(GatewayRoutingOptionKeys))
	for key := range GatewayRoutingOptionKeys {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func ValidateOptionValue(key string, value string) error {
	if key == price_expression.ToolPriceOptionKey {
		return price_expression.ValidateToolPricesJSON(value)
	}
	if key == "MaxTokenAutoGroups" {
		return resolve_group.ValidateMaxTokenAutoGroups(value)
	}
	if health_store.IsChannelModelHealthOptionKey(key) {
		return health_store.ValidateChannelModelHealthSettingValue(key, value)
	}
	return nil
}

// SeedOptionMap initializes common.OptionMap with typed defaults from every
// config domain. Callers load persisted overrides afterwards via ApplyOption.
func SeedOptionMap() {
	common.OptionMapRWMutex.Lock()
	common.OptionMap = make(map[string]string)

	// 添加原有的系统配置
	common.OptionMap["FileUploadPermission"] = strconv.Itoa(common.FileUploadPermission)
	common.OptionMap["FileDownloadPermission"] = strconv.Itoa(common.FileDownloadPermission)
	common.OptionMap["ImageUploadPermission"] = strconv.Itoa(common.ImageUploadPermission)
	common.OptionMap["ImageDownloadPermission"] = strconv.Itoa(common.ImageDownloadPermission)
	common.OptionMap["PasswordLoginEnabled"] = strconv.FormatBool(common.PasswordLoginEnabled)
	common.OptionMap["PasswordRegisterEnabled"] = strconv.FormatBool(common.PasswordRegisterEnabled)
	common.OptionMap["EmailVerificationEnabled"] = strconv.FormatBool(common.EmailVerificationEnabled)
	common.OptionMap["GitHubOAuthEnabled"] = strconv.FormatBool(common.GitHubOAuthEnabled)
	common.OptionMap["LinuxDOOAuthEnabled"] = strconv.FormatBool(common.LinuxDOOAuthEnabled)
	common.OptionMap["TelegramOAuthEnabled"] = strconv.FormatBool(common.TelegramOAuthEnabled)
	common.OptionMap["WeChatAuthEnabled"] = strconv.FormatBool(common.WeChatAuthEnabled)
	common.OptionMap["TurnstileCheckEnabled"] = strconv.FormatBool(common.TurnstileCheckEnabled)
	common.OptionMap["RegisterEnabled"] = strconv.FormatBool(common.RegisterEnabled)
	common.OptionMap["AutomaticDisableChannelEnabled"] = strconv.FormatBool(common.AutomaticDisableChannelEnabled)
	common.OptionMap["AutomaticEnableChannelEnabled"] = strconv.FormatBool(common.AutomaticEnableChannelEnabled)
	common.OptionMap["LogConsumeEnabled"] = strconv.FormatBool(common.LogConsumeEnabled)
	common.OptionMap["DisplayInCurrencyEnabled"] = strconv.FormatBool(common.DisplayInCurrencyEnabled)
	common.OptionMap["DisplayTokenStatEnabled"] = strconv.FormatBool(common.DisplayTokenStatEnabled)
	common.OptionMap["DrawingEnabled"] = strconv.FormatBool(common.DrawingEnabled)
	common.OptionMap["TaskEnabled"] = strconv.FormatBool(common.TaskEnabled)
	common.OptionMap["DataExportEnabled"] = strconv.FormatBool(common.DataExportEnabled)
	common.OptionMap["ChannelDisableThreshold"] = strconv.FormatFloat(common.ChannelDisableThreshold, 'f', -1, 64)
	common.OptionMap["EmailDomainRestrictionEnabled"] = strconv.FormatBool(common.EmailDomainRestrictionEnabled)
	common.OptionMap["EmailAliasRestrictionEnabled"] = strconv.FormatBool(common.EmailAliasRestrictionEnabled)
	common.OptionMap["EmailDomainWhitelist"] = strings.Join(common.EmailDomainWhitelist, ",")
	common.OptionMap["SMTPServer"] = ""
	common.OptionMap["SMTPFrom"] = ""
	common.OptionMap["SMTPPort"] = strconv.Itoa(common.SMTPPort)
	common.OptionMap["SMTPAccount"] = ""
	common.OptionMap["SMTPToken"] = ""
	common.OptionMap["SMTPSSLEnabled"] = strconv.FormatBool(common.SMTPSSLEnabled)
	common.OptionMap["SMTPStartTLSEnabled"] = strconv.FormatBool(common.SMTPStartTLSEnabled)
	common.OptionMap["SMTPInsecureSkipVerify"] = strconv.FormatBool(common.SMTPInsecureSkipVerify)
	common.OptionMap["SMTPForceAuthLogin"] = strconv.FormatBool(common.SMTPForceAuthLogin)
	common.OptionMap["Notice"] = ""
	common.OptionMap["About"] = ""
	common.OptionMap["HomePageContent"] = ""
	common.OptionMap["Footer"] = common.Footer
	common.OptionMap["SystemName"] = common.SystemName
	common.OptionMap["Logo"] = common.Logo
	common.OptionMap["ServerAddress"] = ""
	common.OptionMap["WorkerUrl"] = fetch_url.WorkerUrl
	common.OptionMap["WorkerValidKey"] = fetch_url.WorkerValidKey
	common.OptionMap["WorkerAllowHttpImageRequestEnabled"] = strconv.FormatBool(fetch_url.WorkerAllowHttpImageRequestEnabled)
	common.OptionMap["PayAddress"] = ""
	common.OptionMap["CustomCallbackAddress"] = ""
	common.OptionMap["EpayId"] = ""
	common.OptionMap["EpayKey"] = ""
	common.OptionMap["Price"] = strconv.FormatFloat(pay_subscription.Price, 'f', -1, 64)
	common.OptionMap["USDExchangeRate"] = strconv.FormatFloat(pay_subscription.USDExchangeRate, 'f', -1, 64)
	common.OptionMap["MinTopUp"] = strconv.Itoa(pay_subscription.MinTopUp)
	common.OptionMap["StripeMinTopUp"] = strconv.Itoa(pay_subscription.StripeMinTopUp)
	common.OptionMap["StripeApiSecret"] = pay_subscription.StripeApiSecret
	common.OptionMap["StripeWebhookSecret"] = pay_subscription.StripeWebhookSecret
	common.OptionMap["StripePriceId"] = pay_subscription.StripePriceId
	common.OptionMap["StripeUnitPrice"] = strconv.FormatFloat(pay_subscription.StripeUnitPrice, 'f', -1, 64)
	common.OptionMap["StripePromotionCodesEnabled"] = strconv.FormatBool(pay_subscription.StripePromotionCodesEnabled)
	common.OptionMap["CreemApiKey"] = pay_subscription.CreemApiKey
	common.OptionMap["CreemProducts"] = pay_subscription.CreemProducts
	common.OptionMap["CreemTestMode"] = strconv.FormatBool(pay_subscription.CreemTestMode)
	common.OptionMap["CreemWebhookSecret"] = pay_subscription.CreemWebhookSecret
	common.OptionMap["WaffoEnabled"] = strconv.FormatBool(pay_subscription.WaffoEnabled)
	common.OptionMap["WaffoApiKey"] = pay_subscription.WaffoApiKey
	common.OptionMap["WaffoPrivateKey"] = pay_subscription.WaffoPrivateKey
	common.OptionMap["WaffoPublicCert"] = pay_subscription.WaffoPublicCert
	common.OptionMap["WaffoSandboxPublicCert"] = pay_subscription.WaffoSandboxPublicCert
	common.OptionMap["WaffoSandboxApiKey"] = pay_subscription.WaffoSandboxApiKey
	common.OptionMap["WaffoSandboxPrivateKey"] = pay_subscription.WaffoSandboxPrivateKey
	common.OptionMap["WaffoSandbox"] = strconv.FormatBool(pay_subscription.WaffoSandbox)
	common.OptionMap["WaffoMerchantId"] = pay_subscription.WaffoMerchantId
	common.OptionMap["WaffoNotifyUrl"] = pay_subscription.WaffoNotifyUrl
	common.OptionMap["WaffoReturnUrl"] = pay_subscription.WaffoReturnUrl
	common.OptionMap["WaffoSubscriptionReturnUrl"] = pay_subscription.WaffoSubscriptionReturnUrl
	common.OptionMap["WaffoCurrency"] = pay_subscription.WaffoCurrency
	common.OptionMap["WaffoUnitPrice"] = strconv.FormatFloat(pay_subscription.WaffoUnitPrice, 'f', -1, 64)
	common.OptionMap["WaffoMinTopUp"] = strconv.Itoa(pay_subscription.WaffoMinTopUp)
	common.OptionMap["WaffoPayMethods"] = pay_subscription.WaffoPayMethods2JsonString()
	common.OptionMap["WaffoPancakeMerchantID"] = pay_subscription.WaffoPancakeMerchantID
	common.OptionMap["WaffoPancakePrivateKey"] = pay_subscription.WaffoPancakePrivateKey
	common.OptionMap["WaffoPancakeReturnURL"] = pay_subscription.WaffoPancakeReturnURL
	common.OptionMap["WaffoPancakeUnitPrice"] = strconv.FormatFloat(pay_subscription.WaffoPancakeUnitPrice, 'f', -1, 64)
	common.OptionMap["WaffoPancakeMinTopUp"] = strconv.Itoa(pay_subscription.WaffoPancakeMinTopUp)
	common.OptionMap["WaffoPancakeStoreID"] = pay_subscription.WaffoPancakeStoreID
	common.OptionMap["WaffoPancakeProductID"] = pay_subscription.WaffoPancakeProductID
	common.OptionMap["TopupGroupRatio"] = common.TopupGroupRatio2JSONString()
	common.OptionMap["Chats"] = record_perf_config.Chats2JsonString()
	common.OptionMap["AutoGroups"] = resolve_group.AutoGroups2JsonString()
	common.OptionMap["DefaultUseAutoGroup"] = strconv.FormatBool(resolve_group.DefaultUseAutoGroup)
	common.OptionMap["MaxTokenAutoGroups"] = strconv.Itoa(resolve_group.GetMaxTokenAutoGroups())
	common.OptionMap["PayMethods"] = pay_subscription.PayMethods2JsonString()
	common.OptionMap["GitHubClientId"] = ""
	common.OptionMap["GitHubClientSecret"] = ""
	common.OptionMap["TelegramBotToken"] = ""
	common.OptionMap["TelegramBotName"] = ""
	common.OptionMap["WeChatServerAddress"] = ""
	common.OptionMap["WeChatServerToken"] = ""
	common.OptionMap["WeChatAccountQRCodeImageURL"] = ""
	common.OptionMap["TurnstileSiteKey"] = ""
	common.OptionMap["TurnstileSecretKey"] = ""
	common.OptionMap["QuotaForNewUser"] = strconv.Itoa(common.QuotaForNewUser)
	common.OptionMap["QuotaForInviter"] = strconv.Itoa(common.QuotaForInviter)
	common.OptionMap["QuotaForInvitee"] = strconv.Itoa(common.QuotaForInvitee)
	common.OptionMap["QuotaRemindThreshold"] = strconv.Itoa(common.QuotaRemindThreshold)
	common.OptionMap["PreConsumedQuota"] = strconv.Itoa(common.PreConsumedQuota)
	common.OptionMap["ModelRequestRateLimitCount"] = strconv.Itoa(rate_limit.ModelRequestRateLimitCount)
	common.OptionMap["ModelRequestRateLimitDurationMinutes"] = strconv.Itoa(rate_limit.ModelRequestRateLimitDurationMinutes)
	common.OptionMap["ModelRequestRateLimitSuccessCount"] = strconv.Itoa(rate_limit.ModelRequestRateLimitSuccessCount)
	common.OptionMap["ModelRequestRateLimitGroup"] = rate_limit.ModelRequestRateLimitGroup2JSONString()
	common.OptionMap["ModelRatio"] = ratio_setting.ModelRatio2JSONString()
	common.OptionMap["ModelPrice"] = ratio_setting.ModelPrice2JSONString()
	common.OptionMap["CacheRatio"] = ratio_setting.CacheRatio2JSONString()
	common.OptionMap["CreateCacheRatio"] = ratio_setting.CreateCacheRatio2JSONString()
	common.OptionMap["GroupRatio"] = ratio_setting.GroupRatio2JSONString()
	common.OptionMap["GroupGroupRatio"] = ratio_setting.GroupGroupRatio2JSONString()
	common.OptionMap["UserUsableGroups"] = resolve_group.UserUsableGroups2JSONString()
	common.OptionMap["CompletionRatio"] = ratio_setting.CompletionRatio2JSONString()
	common.OptionMap["ImageRatio"] = ratio_setting.ImageRatio2JSONString()
	common.OptionMap["AudioRatio"] = ratio_setting.AudioRatio2JSONString()
	common.OptionMap["AudioCompletionRatio"] = ratio_setting.AudioCompletionRatio2JSONString()
	common.OptionMap["TopUpLink"] = common.TopUpLink
	common.OptionMap["QuotaPerUnit"] = strconv.FormatFloat(common.QuotaPerUnit, 'f', -1, 64)
	common.OptionMap["RetryTimes"] = strconv.Itoa(common.RetryTimes)
	common.OptionMap["DataExportInterval"] = strconv.Itoa(common.DataExportInterval)
	common.OptionMap["DataExportDefaultTime"] = common.DataExportDefaultTime
	common.OptionMap["DefaultCollapseSidebar"] = strconv.FormatBool(common.DefaultCollapseSidebar)
	common.OptionMap["MjNotifyEnabled"] = strconv.FormatBool(record_perf_config.MjNotifyEnabled)
	common.OptionMap["MjAccountFilterEnabled"] = strconv.FormatBool(record_perf_config.MjAccountFilterEnabled)
	common.OptionMap["MjModeClearEnabled"] = strconv.FormatBool(record_perf_config.MjModeClearEnabled)
	common.OptionMap["MjForwardUrlEnabled"] = strconv.FormatBool(record_perf_config.MjForwardUrlEnabled)
	common.OptionMap["MjActionCheckSuccessEnabled"] = strconv.FormatBool(record_perf_config.MjActionCheckSuccessEnabled)
	common.OptionMap["CheckSensitiveEnabled"] = strconv.FormatBool(sensitive.CheckSensitiveEnabled)
	common.OptionMap["DemoSiteEnabled"] = strconv.FormatBool(manage_channels.DemoSiteEnabled)
	common.OptionMap["SelfUseModeEnabled"] = strconv.FormatBool(manage_channels.SelfUseModeEnabled)
	common.OptionMap["ModelRequestRateLimitEnabled"] = strconv.FormatBool(rate_limit.ModelRequestRateLimitEnabled)
	common.OptionMap["CheckSensitiveOnPromptEnabled"] = strconv.FormatBool(sensitive.CheckSensitiveOnPromptEnabled)
	common.OptionMap["CheckSensitiveOnCompletionEnabled"] = strconv.FormatBool(sensitive.CheckSensitiveOnCompletionEnabled)
	common.OptionMap["StopOnSensitiveEnabled"] = strconv.FormatBool(sensitive.StopOnSensitiveEnabled)
	common.OptionMap["SensitiveWords"] = sensitive.SensitiveWordsToString()
	common.OptionMap["SensitiveBlockGroups"] = sensitive.SensitiveGroupsToString()
	common.OptionMap["StreamCacheQueueLength"] = strconv.Itoa(sensitive.StreamCacheQueueLength)
	common.OptionMap["AutomaticDisableKeywords"] = manage_channels.AutomaticDisableKeywordsToString()
	common.OptionMap["AutomaticDisableStatusCodes"] = status_code.AutomaticDisableStatusCodesToString()
	common.OptionMap["AutomaticRetryStatusCodes"] = status_code.AutomaticRetryStatusCodesToString()
	common.OptionMap["ExposeRatioEnabled"] = strconv.FormatBool(ratio_setting.IsExposeRatioEnabled())
	common.OptionMap["proxy_config"] = ""

	// Channel model health state-machine option keys, seeded from the
	// runtime atomic config so the option-map snapshot reflects the
	// live defaults.
	healthCfg := health_store.GetChannelModelHealthSetting()
	if healthCfg != nil {
		common.OptionMap["CalmFastBase"] = strconv.Itoa(healthCfg.CalmFastBase)
		common.OptionMap["CalmFastInterval"] = strconv.Itoa(healthCfg.CalmFastInterval)
		common.OptionMap["CalmSlowBase"] = strconv.Itoa(healthCfg.CalmSlowBase)
		common.OptionMap["CalmSlowInterval"] = strconv.Itoa(healthCfg.CalmSlowInterval)
		common.OptionMap["DormantBase"] = strconv.Itoa(healthCfg.DormantBase)
		common.OptionMap["DormantInterval"] = strconv.Itoa(healthCfg.DormantInterval)
		common.OptionMap["DormantMaxBase"] = strconv.Itoa(healthCfg.DormantMaxBase)
		common.OptionMap["DormantDisableThreshold"] = strconv.Itoa(healthCfg.DormantDisableThreshold)
		common.OptionMap["LocalFailureThreshold"] = strconv.Itoa(healthCfg.LocalFailureThreshold)
		common.OptionMap["UpstreamFailureThreshold"] = strconv.Itoa(healthCfg.UpstreamFailureThreshold)
		common.OptionMap["CalmWeightScale"] = strconv.Itoa(healthCfg.CalmWeightScale)
		common.OptionMap["DormantWeightScale"] = strconv.Itoa(healthCfg.DormantWeightScale)
		common.OptionMap["EmergencyThreshold"] = strconv.Itoa(healthCfg.EmergencyThreshold)
		common.OptionMap["WarningThreshold"] = strconv.Itoa(healthCfg.WarningThreshold)
		common.OptionMap["AcceleratedDecayStep"] = strconv.Itoa(healthCfg.AcceleratedDecayStep)
		common.OptionMap["NormalDecayStep"] = strconv.Itoa(healthCfg.NormalDecayStep)
		common.OptionMap["KeyProbeEnabled"] = strconv.FormatBool(healthCfg.KeyProbeEnabled)
	}

	// 自动添加所有注册的模型配置
	modelConfigs := config.GlobalConfig.ExportAllConfigs()
	for k, v := range modelConfigs {
		common.OptionMap[k] = v
	}

	common.OptionMapRWMutex.Unlock()
}

// ApplyOption dispatches one persisted option value onto its typed target and
// records it in common.OptionMap.
func ApplyOption(key string, value string) (err error) {
	if health_store.IsChannelModelHealthOptionKey(key) {
		// Health state-machine options are dispatched to the atomic runtime
		// config before the OptionMap lock is taken; a parse/validation
		// error returns without storing an invalid value.
		if err := health_store.UpdateChannelModelHealthSettingValue(key, value); err != nil {
			return err
		}
		common.OptionMapRWMutex.Lock()
		common.OptionMap[key] = value
		common.OptionMapRWMutex.Unlock()
		return nil
	}
	if key == RetiredThemeOptionKey {
		common.OptionMapRWMutex.Lock()
		delete(common.OptionMap, key)
		common.OptionMapRWMutex.Unlock()
		return nil
	}
	common.OptionMapRWMutex.Lock()
	defer common.OptionMapRWMutex.Unlock()
	common.OptionMap[key] = value

	// 检查是否是模型配置 - 使用更规范的方式处理
	if handleConfigUpdate(key, value) {
		return nil // 已由配置系统处理
	}

	// 处理传统配置项...
	if strings.HasSuffix(key, "Permission") {
		intValue, _ := strconv.Atoi(value)
		switch key {
		case "FileUploadPermission":
			common.FileUploadPermission = intValue
		case "FileDownloadPermission":
			common.FileDownloadPermission = intValue
		case "ImageUploadPermission":
			common.ImageUploadPermission = intValue
		case "ImageDownloadPermission":
			common.ImageDownloadPermission = intValue
		}
	}
	if strings.HasSuffix(key, "Enabled") || key == "DefaultCollapseSidebar" || key == "DefaultUseAutoGroup" || key == "SMTPForceAuthLogin" || key == "SMTPInsecureSkipVerify" {
		boolValue := value == "true"
		switch key {
		case "PasswordRegisterEnabled":
			common.PasswordRegisterEnabled = boolValue
		case "PasswordLoginEnabled":
			common.PasswordLoginEnabled = boolValue
		case "EmailVerificationEnabled":
			common.EmailVerificationEnabled = boolValue
		case "GitHubOAuthEnabled":
			common.GitHubOAuthEnabled = boolValue
		case "LinuxDOOAuthEnabled":
			common.LinuxDOOAuthEnabled = boolValue
		case "WeChatAuthEnabled":
			common.WeChatAuthEnabled = boolValue
		case "TelegramOAuthEnabled":
			common.TelegramOAuthEnabled = boolValue
		case "TurnstileCheckEnabled":
			common.TurnstileCheckEnabled = boolValue
		case "RegisterEnabled":
			common.RegisterEnabled = boolValue
		case "EmailDomainRestrictionEnabled":
			common.EmailDomainRestrictionEnabled = boolValue
		case "EmailAliasRestrictionEnabled":
			common.EmailAliasRestrictionEnabled = boolValue
		case "AutomaticDisableChannelEnabled":
			common.AutomaticDisableChannelEnabled = boolValue
		case "AutomaticEnableChannelEnabled":
			common.AutomaticEnableChannelEnabled = boolValue
		case "LogConsumeEnabled":
			common.LogConsumeEnabled = boolValue
		case "DisplayInCurrencyEnabled":
			// 兼容旧字段：同步到新配置 general_setting.quota_display_type（运行时生效）
			// true -> USD, false -> TOKENS
			newVal := "USD"
			if !boolValue {
				newVal = "TOKENS"
			}
			if cfg := config.GlobalConfig.Get("general_setting"); cfg != nil {
				_ = config.UpdateConfigFromMap(cfg, map[string]string{"quota_display_type": newVal})
			}
		case "DisplayTokenStatEnabled":
			common.DisplayTokenStatEnabled = boolValue
		case "DrawingEnabled":
			common.DrawingEnabled = boolValue
		case "TaskEnabled":
			common.TaskEnabled = boolValue
		case "DataExportEnabled":
			common.DataExportEnabled = boolValue
		case "DefaultCollapseSidebar":
			common.DefaultCollapseSidebar = boolValue
		case "MjNotifyEnabled":
			record_perf_config.MjNotifyEnabled = boolValue
		case "MjAccountFilterEnabled":
			record_perf_config.MjAccountFilterEnabled = boolValue
		case "MjModeClearEnabled":
			record_perf_config.MjModeClearEnabled = boolValue
		case "MjForwardUrlEnabled":
			record_perf_config.MjForwardUrlEnabled = boolValue
		case "MjActionCheckSuccessEnabled":
			record_perf_config.MjActionCheckSuccessEnabled = boolValue
		case "CheckSensitiveEnabled":
			sensitive.CheckSensitiveEnabled = boolValue
		case "DemoSiteEnabled":
			manage_channels.DemoSiteEnabled = boolValue
		case "SelfUseModeEnabled":
			manage_channels.SelfUseModeEnabled = boolValue
		case "CheckSensitiveOnPromptEnabled":
			sensitive.CheckSensitiveOnPromptEnabled = boolValue
		case "CheckSensitiveOnCompletionEnabled":
			sensitive.CheckSensitiveOnCompletionEnabled = boolValue
		case "ModelRequestRateLimitEnabled":
			rate_limit.ModelRequestRateLimitEnabled = boolValue
		case "StopOnSensitiveEnabled":
			sensitive.StopOnSensitiveEnabled = boolValue
		case "SMTPSSLEnabled":
			common.SMTPSSLEnabled = boolValue
		case "SMTPStartTLSEnabled":
			common.SMTPStartTLSEnabled = boolValue
		case "SMTPInsecureSkipVerify":
			common.SMTPInsecureSkipVerify = boolValue
		case "SMTPForceAuthLogin":
			common.SMTPForceAuthLogin = boolValue
		case "WorkerAllowHttpImageRequestEnabled":
			fetch_url.WorkerAllowHttpImageRequestEnabled = boolValue
		case "DefaultUseAutoGroup":
			resolve_group.DefaultUseAutoGroup = boolValue
		case "ExposeRatioEnabled":
			ratio_setting.SetExposeRatioEnabled(boolValue)
		}
	}
	switch key {
	case "EmailDomainWhitelist":
		common.EmailDomainWhitelist = strings.Split(value, ",")
	case "SMTPServer":
		common.SMTPServer = value
	case "SMTPPort":
		intValue, _ := strconv.Atoi(value)
		common.SMTPPort = intValue
	case "SMTPAccount":
		common.SMTPAccount = value
	case "SMTPFrom":
		common.SMTPFrom = value
	case "SMTPToken":
		common.SMTPToken = value
	case "ServerAddress":
		fetch_url.ServerAddress = value
	case "WorkerUrl":
		fetch_url.WorkerUrl = value
	case "WorkerValidKey":
		fetch_url.WorkerValidKey = value
	case "PayAddress":
		pay_subscription.PayAddress = value
	case "Chats":
		err = record_perf_config.UpdateChatsByJsonString(value)
	case "AutoGroups":
		err = resolve_group.UpdateAutoGroupsByJsonString(value)
	case "MaxTokenAutoGroups":
		err = resolve_group.UpdateMaxTokenAutoGroups(value)
	case "CustomCallbackAddress":
		pay_subscription.CustomCallbackAddress = value
	case "EpayId":
		pay_subscription.EpayId = value
	case "EpayKey":
		pay_subscription.EpayKey = value
	case "Price":
		pay_subscription.Price, _ = strconv.ParseFloat(value, 64)
	case "USDExchangeRate":
		pay_subscription.USDExchangeRate, _ = strconv.ParseFloat(value, 64)
	case "MinTopUp":
		pay_subscription.MinTopUp, _ = strconv.Atoi(value)
	case "StripeApiSecret":
		pay_subscription.StripeApiSecret = value
	case "StripeWebhookSecret":
		pay_subscription.StripeWebhookSecret = value
	case "StripePriceId":
		pay_subscription.StripePriceId = value
	case "StripeUnitPrice":
		pay_subscription.StripeUnitPrice, _ = strconv.ParseFloat(value, 64)
	case "StripeMinTopUp":
		pay_subscription.StripeMinTopUp, _ = strconv.Atoi(value)
	case "StripePromotionCodesEnabled":
		pay_subscription.StripePromotionCodesEnabled = value == "true"
	case "CreemApiKey":
		pay_subscription.CreemApiKey = value
	case "CreemProducts":
		pay_subscription.CreemProducts = value
	case "CreemTestMode":
		pay_subscription.CreemTestMode = value == "true"
	case "CreemWebhookSecret":
		pay_subscription.CreemWebhookSecret = value
	case "WaffoEnabled":
		pay_subscription.WaffoEnabled = value == "true"
	case "WaffoApiKey":
		pay_subscription.WaffoApiKey = value
	case "WaffoPrivateKey":
		pay_subscription.WaffoPrivateKey = value
	case "WaffoPublicCert":
		pay_subscription.WaffoPublicCert = value
	case "WaffoSandboxPublicCert":
		pay_subscription.WaffoSandboxPublicCert = value
	case "WaffoSandboxApiKey":
		pay_subscription.WaffoSandboxApiKey = value
	case "WaffoSandboxPrivateKey":
		pay_subscription.WaffoSandboxPrivateKey = value
	case "WaffoSandbox":
		pay_subscription.WaffoSandbox = value == "true"
	case "WaffoMerchantId":
		pay_subscription.WaffoMerchantId = value
	case "WaffoNotifyUrl":
		pay_subscription.WaffoNotifyUrl = value
	case "WaffoReturnUrl":
		pay_subscription.WaffoReturnUrl = value
	case "WaffoSubscriptionReturnUrl":
		pay_subscription.WaffoSubscriptionReturnUrl = value
	case "WaffoCurrency":
		pay_subscription.WaffoCurrency = value
	case "WaffoUnitPrice":
		pay_subscription.WaffoUnitPrice, _ = strconv.ParseFloat(value, 64)
	case "WaffoMinTopUp":
		pay_subscription.WaffoMinTopUp, _ = strconv.Atoi(value)
	case "WaffoPancakeMerchantID":
		pay_subscription.WaffoPancakeMerchantID = value
	case "WaffoPancakePrivateKey":
		pay_subscription.WaffoPancakePrivateKey = value
	case "WaffoPancakeReturnURL":
		pay_subscription.WaffoPancakeReturnURL = value
	case "WaffoPancakeStoreID":
		pay_subscription.WaffoPancakeStoreID = value
	case "WaffoPancakeProductID":
		pay_subscription.WaffoPancakeProductID = value
	case "WaffoPancakeUnitPrice":
		pay_subscription.WaffoPancakeUnitPrice, _ = strconv.ParseFloat(value, 64)
	case "WaffoPancakeMinTopUp":
		pay_subscription.WaffoPancakeMinTopUp, _ = strconv.Atoi(value)
	case "TopupGroupRatio":
		err = common.UpdateTopupGroupRatioByJSONString(value)
	case "GitHubClientId":
		common.GitHubClientId = value
	case "GitHubClientSecret":
		common.GitHubClientSecret = value
	case "LinuxDOClientId":
		common.LinuxDOClientId = value
	case "LinuxDOClientSecret":
		common.LinuxDOClientSecret = value
	case "LinuxDOMinimumTrustLevel":
		common.LinuxDOMinimumTrustLevel, _ = strconv.Atoi(value)
	case "Footer":
		common.Footer = value
	case "SystemName":
		common.SystemName = value
	case "Logo":
		common.Logo = value
	case "WeChatServerAddress":
		common.WeChatServerAddress = value
	case "WeChatServerToken":
		common.WeChatServerToken = value
	case "WeChatAccountQRCodeImageURL":
		common.WeChatAccountQRCodeImageURL = value
	case "TelegramBotToken":
		common.TelegramBotToken = value
	case "TelegramBotName":
		common.TelegramBotName = value
	case "TurnstileSiteKey":
		common.TurnstileSiteKey = value
	case "TurnstileSecretKey":
		common.TurnstileSecretKey = value
	case "QuotaForNewUser":
		common.QuotaForNewUser, _ = strconv.Atoi(value)
	case "QuotaForInviter":
		common.QuotaForInviter, _ = strconv.Atoi(value)
	case "QuotaForInvitee":
		common.QuotaForInvitee, _ = strconv.Atoi(value)
	case "QuotaRemindThreshold":
		common.QuotaRemindThreshold, _ = strconv.Atoi(value)
	case "PreConsumedQuota":
		common.PreConsumedQuota, _ = strconv.Atoi(value)
	case "ModelRequestRateLimitCount":
		rate_limit.ModelRequestRateLimitCount, _ = strconv.Atoi(value)
	case "ModelRequestRateLimitDurationMinutes":
		rate_limit.ModelRequestRateLimitDurationMinutes, _ = strconv.Atoi(value)
	case "ModelRequestRateLimitSuccessCount":
		rate_limit.ModelRequestRateLimitSuccessCount, _ = strconv.Atoi(value)
	case "ModelRequestRateLimitGroup":
		err = rate_limit.UpdateModelRequestRateLimitGroupByJSONString(value)
	case "RetryTimes":
		common.RetryTimes, _ = strconv.Atoi(value)
	case "DataExportInterval":
		common.DataExportInterval, _ = strconv.Atoi(value)
	case "DataExportDefaultTime":
		common.DataExportDefaultTime = value
	case "ModelRatio":
		err = ratio_setting.UpdateModelRatioByJSONString(value)
	case "GroupRatio":
		err = ratio_setting.UpdateGroupRatioByJSONString(value)
	case "GroupGroupRatio":
		err = ratio_setting.UpdateGroupGroupRatioByJSONString(value)
	case "UserUsableGroups":
		err = resolve_group.UpdateUserUsableGroupsByJSONString(value)
	case "CompletionRatio":
		err = ratio_setting.UpdateCompletionRatioByJSONString(value)
	case "ModelPrice":
		err = ratio_setting.UpdateModelPriceByJSONString(value)
	case "CacheRatio":
		err = ratio_setting.UpdateCacheRatioByJSONString(value)
	case "CreateCacheRatio":
		err = ratio_setting.UpdateCreateCacheRatioByJSONString(value)
	case "ImageRatio":
		err = ratio_setting.UpdateImageRatioByJSONString(value)
	case "AudioRatio":
		err = ratio_setting.UpdateAudioRatioByJSONString(value)
	case "AudioCompletionRatio":
		err = ratio_setting.UpdateAudioCompletionRatioByJSONString(value)
	case "TopUpLink":
		common.TopUpLink = value
	case "ChannelDisableThreshold":
		common.ChannelDisableThreshold, _ = strconv.ParseFloat(value, 64)
	case "QuotaPerUnit":
		common.QuotaPerUnit, _ = strconv.ParseFloat(value, 64)
	case "SensitiveWords":
		sensitive.SensitiveWordsFromString(value)
	case "SensitiveBlockGroups":
		sensitive.SensitiveGroupsFromString(value)
	case "AutomaticDisableKeywords":
		manage_channels.AutomaticDisableKeywordsFromString(value)
	case "AutomaticDisableStatusCodes":
		err = status_code.AutomaticDisableStatusCodesFromString(value)
	case "AutomaticRetryStatusCodes":
		err = status_code.AutomaticRetryStatusCodesFromString(value)
	case "StreamCacheQueueLength":
		sensitive.StreamCacheQueueLength, _ = strconv.Atoi(value)
	case "PayMethods":
		err = pay_subscription.UpdatePayMethodsByJsonString(value)
	case "WaffoPayMethods":
		// WaffoPayMethods is read directly from OptionMap via pay_subscription.GetWaffoPayMethods().
		// The value is already stored in OptionMap at the top of this function.
		// No additional in-memory variable to update.
	}
	return err
}

// handleConfigUpdate 处理分层配置更新，返回是否已处理
func handleConfigUpdate(key, value string) bool {
	if key == price_expression.ToolPriceOptionKey {
		price_expression.LoadToolPricesFromJSONString(value)
		return true
	}

	parts := strings.SplitN(key, ".", 2)
	if len(parts) != 2 {
		return false // 不是分层配置
	}

	configName := parts[0]
	configKey := parts[1]

	// 获取配置对象
	cfg := config.GlobalConfig.Get(configName)
	if cfg == nil {
		return false // 未注册的配置
	}

	// 更新配置
	configMap := map[string]string{
		configKey: value,
	}
	config.UpdateConfigFromMap(cfg, configMap)

	// 特定配置的后处理
	if configName == "performance_setting" && OnPerformanceSettingChanged != nil {
		OnPerformanceSettingChanged()
	} else if configName == "billing_setting" && OnBillingSettingChanged != nil {
		OnBillingSettingChanged()
	}

	return true // 已处理
}
