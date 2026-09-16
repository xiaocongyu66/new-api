package billing

import "github.com/QuantumNous/new-api/internal/settings"

// CheckinSetting 签到功能配置
//
// 这是每日签到的唯一配置归属：网页签到与 QQ 群签到共用同一套额度区间与
// 判定规则，避免两个渠道各读各的配置导致行为漂移。MinQuota / MaxQuota 是
// 内部 quota 单位，管理页通过 checkin_setting.min_quota_display 等 display
// 键以展示货币读写，后端在 option 写入路径换算。
type CheckinSetting struct {
	Enabled  bool `json:"enabled"`   // 是否启用签到功能
	MinQuota int  `json:"min_quota"` // 签到最小额度奖励（内部 quota）
	MaxQuota int  `json:"max_quota"` // 签到最大额度奖励（内部 quota）

	// SinglePlatformOnly 仅允许单平台签到：网页或 QQ 任一渠道签到过即视为
	// 今日已签到，两侧入口都按同一规则拦截。
	SinglePlatformOnly bool `json:"single_platform_only"`

	// RequireQQBound 是否要求用户先绑定 QQ 才能在网页签到。
	// 适用于希望把每日奖励限定到 QQ 群用户的场景。
	RequireQQBound bool `json:"require_qq_bound"`

	// ShowBindCodeCard 是否在个人资料页展示绑定验证码卡片。
	ShowBindCodeCard bool `json:"show_bind_code_card"`
}

// 默认配置
var checkinSetting = CheckinSetting{
	Enabled:            false, // 默认关闭
	MinQuota:           1000,  // 默认最小额度 1000 (约 0.002 USD)
	MaxQuota:           10000, // 默认最大额度 10000 (约 0.02 USD)
	SinglePlatformOnly: true,  // 默认单平台签到：一个渠道签过即算今天签过
	RequireQQBound:     false, // 默认不要求 QQ 绑定
	ShowBindCodeCard:   true,  // 默认展示绑定卡片
}

func init() {
	// 注册到全局配置管理器
	settings.GlobalConfig.Register("checkin_setting", &checkinSetting)
}

// GetCheckinSetting 获取签到配置
func GetCheckinSetting() *CheckinSetting {
	return &checkinSetting
}

// IsCheckinEnabled 是否启用签到功能
func IsCheckinEnabled() bool {
	return checkinSetting.Enabled
}
