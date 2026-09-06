package usage

import "github.com/QuantumNous/new-api/internal/settings"

// UserInsightSetting 控制用户画像分析的开关与阈值。
// 画像会读取请求体前缀做关键词匹配，因此提供总开关便于运营方随时停用。
type UserInsightSetting struct {
	// Enabled 关闭后中间件直接放行，不读请求体、不写画像。
	Enabled bool `json:"enabled"`
	// RecordInLog 决定是否把单次请求的画像写入消费日志的 other.insight。
	// 关闭后仅保留用户级聚合，日志体积更小。
	RecordInLog bool `json:"record_in_log"`
	// JailbreakAlertScore 是在系统日志中告警的破甲分阈值。
	JailbreakAlertScore int `json:"jailbreak_alert_score"`
	// GenderInferenceEnabled 控制是否做角色扮演性别倾向推断。
	// 这是概率推断，默认开启但可关闭以满足更严格的隐私要求。
	GenderInferenceEnabled bool `json:"gender_inference_enabled"`
	// SampleEnabled 控制是否留存"命中关键词原句"供人工复核。
	// 关闭后只有分数和标签，管理员无法回看判定依据。
	SampleEnabled bool `json:"sample_enabled"`
	// SampleRatePercent 是普通请求的采样率（0-100）。
	// 破甲与中转站命中的请求不受该比例限制，始终留存。
	SampleRatePercent int `json:"sample_rate_percent"`
	// SampleKeepBody 决定是否连请求体原文一起留存。
	// 原文体积远大于片段，默认关闭；开启时配额会被很快吃满。
	SampleKeepBody bool `json:"sample_keep_body"`
	// SampleQuotaMB 是样本缓存表的容量上限（MB），超出后按优先级淘汰旧数据。
	SampleQuotaMB int `json:"sample_quota_mb"`
	// SampleRetentionDays 是样本保留天数，0 表示只受容量限制。
	SampleRetentionDays int `json:"sample_retention_days"`
	// AutoBanEnabled 控制"破甲 + 写代码双风险"自动封禁。
	// 默认关闭：自动封禁是不可逆的用户体验事件，必须由运营方显式开启。
	AutoBanEnabled bool `json:"auto_ban_enabled"`
	// AutoBanMinRisk 是触发自动封禁所需的最低风险等级
	// （suspect / likely / confirmed），默认 confirmed 以降低误伤。
	AutoBanMinRisk string `json:"auto_ban_min_risk"`
	// AutoBanCodeRatioEnabled 控制"写代码占比过高"这条独立封禁规则。
	//
	// 与"破甲 + 写代码"不同，本规则不要求任何破甲信号，因此误伤面更大：
	// 一个正经的程序员用户占比天然接近 100%。默认关闭，且需要同时满足
	// 最小请求数门槛，避免只发了三条请求就被 100% 占比判定。
	AutoBanCodeRatioEnabled bool `json:"auto_ban_code_ratio_enabled"`
	// AutoBanCodeRatioPercent 是触发封禁的写代码请求占比阈值（1-100）。
	AutoBanCodeRatioPercent int `json:"auto_ban_code_ratio_percent"`
	// AutoBanCodeMinRequests 是应用占比规则前所需的最小总请求数。
	// 样本太少时占比没有统计意义，这个门槛是必需的。
	AutoBanCodeMinRequests int `json:"auto_ban_code_min_requests"`
}

var userInsightSetting = UserInsightSetting{
	Enabled:                true,
	RecordInLog:            true,
	JailbreakAlertScore:    70,
	GenderInferenceEnabled: true,
	SampleEnabled:          true,
	SampleRatePercent:      5,
	SampleKeepBody:         false,
	SampleQuotaMB:          1024,
	SampleRetentionDays:    30,
	AutoBanEnabled:         false,
	AutoBanMinRisk:         "confirmed",
	// 占比规则默认关闭：它不依赖破甲信号，开启前应由运营方自行评估误伤面。
	AutoBanCodeRatioEnabled: false,
	AutoBanCodeRatioPercent: 60,
	AutoBanCodeMinRequests:  20,
}

func init() {
	settings.GlobalConfig.Register("user_insight_setting", &userInsightSetting)
}

// GetUserInsightSetting 获取用户画像配置。
func GetUserInsightSetting() *UserInsightSetting {
	return &userInsightSetting
}

// JailbreakAlertThreshold 返回破甲告警阈值，非法值回落到默认 70。
func (s *UserInsightSetting) JailbreakAlertThreshold() int {
	if s.JailbreakAlertScore <= 0 || s.JailbreakAlertScore > 100 {
		return 70
	}
	return s.JailbreakAlertScore
}

// SampleQuotaBytes 返回样本缓存的字节配额。
// 非法值回落到 1 GB；同时设 4 GB 硬顶，避免误配把磁盘写满。
func (s *UserInsightSetting) SampleQuotaBytes() int64 {
	quota := s.SampleQuotaMB
	if quota <= 0 {
		quota = 1024
	}
	if quota > 4096 {
		quota = 4096
	}
	return int64(quota) * 1024 * 1024
}

// SampleRate 返回归一化后的采样率百分比。
func (s *UserInsightSetting) SampleRate() int {
	if s.SampleRatePercent < 0 {
		return 0
	}
	if s.SampleRatePercent > 100 {
		return 100
	}
	return s.SampleRatePercent
}

// AutoBanRiskLevel 返回归一化后的自动封禁最低风险等级。
// 非法或空值一律回落到 confirmed —— 配置写错时应当更保守，而不是更激进。
func (s *UserInsightSetting) AutoBanRiskLevel() string {
	switch s.AutoBanMinRisk {
	case "suspect", "likely", "confirmed":
		return s.AutoBanMinRisk
	default:
		return "confirmed"
	}
}

// CodeRatioThreshold 返回归一化后的写代码占比阈值（1-100）。
// 越界值回落到 60：0 会让规则对所有人成立，>100 会让规则永不成立，
// 两者都不是运营方填错时想要的结果。
func (s *UserInsightSetting) CodeRatioThreshold() int {
	if s.AutoBanCodeRatioPercent < 1 || s.AutoBanCodeRatioPercent > 100 {
		return 60
	}
	return s.AutoBanCodeRatioPercent
}

// CodeRatioMinRequests 返回占比规则的最小总请求数门槛，非法值回落到 20。
func (s *UserInsightSetting) CodeRatioMinRequests() int {
	if s.AutoBanCodeMinRequests < 1 {
		return 20
	}
	return s.AutoBanCodeMinRequests
}
