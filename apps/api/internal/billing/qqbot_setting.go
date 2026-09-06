package billing

import "github.com/QuantumNous/new-api/internal/settings"

// QQBotSetting QQ 机器人配置
type QQBotSetting struct {
	AppID     string `json:"app_id"`     // QQ 开放平台 AppID
	AppSecret string `json:"app_secret"` // QQ 开放平台 AppSecret

	// QQ 签到额度范围（与网页签到独立配置）
	MinQuota int `json:"min_quota"` // QQ 签到最小额度奖励
	MaxQuota int `json:"max_quota"` // QQ 签到最大额度奖励

	QQCheckinEnabled bool `json:"qq_checkin_enabled"` // 启动 QQ 签到

	// CheckinDisabledGroups 是逗号分隔的 group_openid 黑名单：
	// 列表内的群禁止签到，其他群不受影响。管理员在目标群发送
	// /关闭签到 即可把当前群加入该名单，全局开关与网页签到不受影响。
	CheckinDisabledGroups string `json:"checkin_disabled_groups"`

	WebCheckinEnabled  bool `json:"web_checkin_enabled"`  // 启动网页签到
	SinglePlatformOnly bool `json:"single_platform_only"` // 仅单平台签到（QQ 与网页共享每日额度）

	// 签到通知样式，支持占位符 {货币} {金额}
	NotifyTemplate string `json:"notify_template"`

	// 自动审批入群申请（关键词匹配）
	AutoApproveEnabled bool   `json:"auto_approve_enabled"`
	AutoApproveKeyword string `json:"auto_approve_keyword"`

	// 群聊消息掉落
	//
	// DropGroups 是逗号分隔的 group_openid 白名单。QQ webhook 事件里不含群名称，
	// 官方 API 也没有按群名查询的接口，所以「哪个群开启」只能由管理员在目标群内
	// 发送 /开启掉落 指令来登记，或在后台手工填写 openid。
	DropEnabled     bool   `json:"drop_enabled"`
	DropGroups      string `json:"drop_groups"`
	DropMinMessages int    `json:"drop_min_messages"` // 触发掉落所需的最少消息数
	DropMaxMessages int    `json:"drop_max_messages"` // 触发掉落所需的最多消息数
	DropMinQuota    int    `json:"drop_min_quota"`    // 单次掉落最小额度
	DropMaxQuota    int    `json:"drop_max_quota"`    // 单次掉落最大额度
	DropDailyLimit  int    `json:"drop_daily_limit"`  // 每人每日最多领取次数，<=0 为不限

	// 掉落文案，支持占位符 {@} {金额} {货币} {单位} {余额}
	DropTemplate string `json:"drop_template"`

	// 群内转账
	//
	// TransferDisabledGroups 为逗号分隔的 group_openid 黑名单，
	// 语义与 CheckinDisabledGroups 一致：默认所有群可用，名单内的群禁用。
	TransferEnabled        bool   `json:"transfer_enabled"`
	TransferDisabledGroups string `json:"transfer_disabled_groups"`

	// TransferDailyLimit 每人每日可参与的转账次数，收发双向都计入。
	// A 转给 B 会同时消耗 A 和 B 各一次，防止用小号中转绕过限制。
	TransferDailyLimit int `json:"transfer_daily_limit"`

	TransferMinAmount int `json:"transfer_min_amount"` // 单笔最低额度
	TransferMaxAmount int `json:"transfer_max_amount"` // 单笔上限，<=0 为不限

	// 群红包
	//
	// RedPacketDisabledGroups 语义同其他黑名单：默认所有群可用。
	// 发红包时一次性扣款，过期未领完的部分由定时任务退回发送者。
	RedPacketEnabled        bool   `json:"red_packet_enabled"`
	RedPacketDisabledGroups string `json:"red_packet_disabled_groups"`
	RedPacketDailyLimit     int    `json:"red_packet_daily_limit"`    // 每人每日可发红包个数
	RedPacketMinAmount      int    `json:"red_packet_min_amount"`     // 红包总额下限
	RedPacketMaxAmount      int    `json:"red_packet_max_amount"`     // 红包总额上限，<=0 不限
	RedPacketDefaultCount   int    `json:"red_packet_default_count"`  // 未指定份数时的默认值
	RedPacketMaxCount       int    `json:"red_packet_max_count"`      // 单个红包最多份数
	RedPacketExpireSeconds  int    `json:"red_packet_expire_seconds"` // 多久未领完退回
	RedPacketAllowOwnGrab   bool   `json:"red_packet_allow_own_grab"` // 是否允许抢自己的红包

	// TransferFeeBrackets 累进手续费档位的 JSON 数组，形如
	// [{"up_to":1,"rate":0.03},{"up_to":0,"rate":0.18}]
	// up_to 以显示货币为单位，<=0 表示最高档（无上界）。
	// 计费方式与个税一致：只有超出前一档上界的部分按本档费率计。
	// 留空则使用代码内置的默认档位。
	TransferFeeBrackets string `json:"transfer_fee_brackets"`
}

// DefaultNotifyTemplate 默认签到通知样式
const DefaultNotifyTemplate = "签到成功！获得 {货币} {金额}"

// DefaultDropTemplate 默认掉落文案
// QQ 群 markdown 中单个 \n 会被折叠成空格，需要换行时用 \n\n
const DefaultDropTemplate = "{@} 杰瑞在逃跑时掉落了 {金额}{货币} {单位}被你捡到！\n\n你当前的余额为 {余额}{货币}"

var qqBotSetting = QQBotSetting{
	AppID:            "",
	AppSecret:        "",
	MinQuota:         1000,
	MaxQuota:         10000,
	QQCheckinEnabled: false,

	CheckinDisabledGroups: "",

	WebCheckinEnabled:  true,
	SinglePlatformOnly: true,
	NotifyTemplate:     DefaultNotifyTemplate,
	AutoApproveEnabled: false,
	AutoApproveKeyword: "",

	// 默认区间对应 0.3 - 3 个货币单位（QuotaPerUnit = 500000）
	DropEnabled:     false,
	DropGroups:      "",
	DropMinMessages: 5,
	DropMaxMessages: 30,
	DropMinQuota:    150000,
	DropMaxQuota:    1500000,
	DropDailyLimit:  3,
	DropTemplate:    DefaultDropTemplate,

	TransferEnabled:        false,
	TransferDisabledGroups: "",
	TransferDailyLimit:     2,
	// 最低 0.1、上限 100 个货币单位（QuotaPerUnit = 500000）
	RedPacketEnabled:        false,
	RedPacketDisabledGroups: "",
	RedPacketDailyLimit:     3,
	// 最低 1、上限 200 个货币单位（QuotaPerUnit = 500000）
	RedPacketMinAmount:     500000,
	RedPacketMaxAmount:     100000000,
	RedPacketDefaultCount:  5,
	RedPacketMaxCount:      50,
	RedPacketExpireSeconds: 24 * 3600,
	RedPacketAllowOwnGrab:  false,

	TransferMinAmount:   50000,
	TransferMaxAmount:   50000000,
	TransferFeeBrackets: "",
}

func init() {
	settings.GlobalConfig.Register("qq_bot_setting", &qqBotSetting)
}

// GetQQBotSetting 获取 QQ 机器人配置
func GetQQBotSetting() *QQBotSetting {
	return &qqBotSetting
}

// IsQQBotConfigured 是否已填写 AppID / AppSecret
func IsQQBotConfigured() bool {
	return qqBotSetting.AppID != "" && qqBotSetting.AppSecret != ""
}

// IsQQCheckinEnabled 是否启用 QQ 签到
func IsQQCheckinEnabled() bool {
	return qqBotSetting.QQCheckinEnabled
}

// IsWebCheckinEnabled 是否启用网页签到
func IsWebCheckinEnabled() bool {
	return qqBotSetting.WebCheckinEnabled
}

// IsSinglePlatformOnly 是否仅允许单平台签到
func IsSinglePlatformOnly() bool {
	return qqBotSetting.SinglePlatformOnly
}

// GetQQCheckinQuotaRange 获取 QQ 签到额度范围
func GetQQCheckinQuotaRange() (min, max int) {
	return qqBotSetting.MinQuota, qqBotSetting.MaxQuota
}

// GetNotifyTemplate 获取签到通知样式，为空时回落到默认值
func GetNotifyTemplate() string {
	if qqBotSetting.NotifyTemplate == "" {
		return DefaultNotifyTemplate
	}
	return qqBotSetting.NotifyTemplate
}

// IsDropEnabled 是否启用群聊掉落
func IsDropEnabled() bool {
	return qqBotSetting.DropEnabled
}

// GetDropTemplate 获取掉落文案，为空时回落到默认值
func GetDropTemplate() string {
	if qqBotSetting.DropTemplate == "" {
		return DefaultDropTemplate
	}
	return qqBotSetting.DropTemplate
}

// GetDropMessageRange 获取触发掉落的消息数区间
func GetDropMessageRange() (min, max int) {
	return qqBotSetting.DropMinMessages, qqBotSetting.DropMaxMessages
}

// GetDropQuotaRange 获取单次掉落的额度区间
func GetDropQuotaRange() (min, max int) {
	return qqBotSetting.DropMinQuota, qqBotSetting.DropMaxQuota
}

// GetDropDailyLimit 获取每人每日掉落次数上限
func GetDropDailyLimit() int {
	return qqBotSetting.DropDailyLimit
}

// GetCheckinDisabledGroups 获取禁止签到的群 openid 原始配置串
func GetCheckinDisabledGroups() string {
	return qqBotSetting.CheckinDisabledGroups
}

// IsTransferEnabled 是否启用群内转账
func IsTransferEnabled() bool {
	return qqBotSetting.TransferEnabled
}

// GetTransferDailyLimit 获取每人每日转账次数上限
func GetTransferDailyLimit() int {
	return qqBotSetting.TransferDailyLimit
}

// GetTransferAmountRange 获取单笔转账的额度区间
func GetTransferAmountRange() (min, max int) {
	return qqBotSetting.TransferMinAmount, qqBotSetting.TransferMaxAmount
}

// GetTransferFeeBrackets 获取累进费率表的原始 JSON 配置
func GetTransferFeeBrackets() string {
	return qqBotSetting.TransferFeeBrackets
}

// IsRedPacketEnabled 是否启用群红包
func IsRedPacketEnabled() bool {
	return qqBotSetting.RedPacketEnabled
}

// GetRedPacketDailyLimit 获取每人每日发红包个数上限
func GetRedPacketDailyLimit() int {
	return qqBotSetting.RedPacketDailyLimit
}

// GetRedPacketAmountRange 获取红包总额区间
func GetRedPacketAmountRange() (min, max int) {
	return qqBotSetting.RedPacketMinAmount, qqBotSetting.RedPacketMaxAmount
}

// GetRedPacketExpireSeconds 获取红包过期秒数
func GetRedPacketExpireSeconds() int {
	if qqBotSetting.RedPacketExpireSeconds <= 0 {
		return 24 * 3600
	}
	return qqBotSetting.RedPacketExpireSeconds
}
