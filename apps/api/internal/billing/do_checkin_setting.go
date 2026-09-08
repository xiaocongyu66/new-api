package billing

import "github.com/QuantumNous/new-api/internal/settings"

// CheckinSetting 签到功能配置
type CheckinSetting struct {
	Enabled  bool `json:"enabled"`   // 是否启用签到功能
	MinQuota int  `json:"min_quota"` // 签到最小额度奖励
	MaxQuota int  `json:"max_quota"` // 签到最大额度奖励

	// RequireQQBound 是否要求用户先绑定 QQ 才能在网页签到。
	// 适用于希望把每日奖励限定到 QQ 群用户的场景。
	RequireQQBound bool `json:"require_qq_bound"`

	// ShowBindCodeCard 是否在个人资料页展示绑定验证码卡片。
	ShowBindCodeCard bool `json:"show_bind_code_card"`
}

// 默认配置
var checkinSetting = CheckinSetting{
	Enabled:         false, // 默认关闭
	MinQuota:        1000,  // 默认最小额度 1000 (约 0.002 USD)
	MaxQuota:        10000, // 默认最大额度 10000 (约 0.02 USD)
	RequireQQBound:  false, // 默认不要求 QQ 绑定
	ShowBindCodeCard: true,  // 默认展示绑定卡片
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

// GetCheckinQuotaRange 获取签到额度范围
func GetCheckinQuotaRange() (min, max int) {
	return checkinSetting.MinQuota, checkinSetting.MaxQuota
}
