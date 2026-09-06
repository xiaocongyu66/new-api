package usage

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// 占比规则的关键是边界：正好等于阈值要触发，差一点不能触发，
// 样本太少一律不触发。这三种情况是线上误伤与漏判的全部来源。
func TestInsightCodeRatioQualifiedBoundaries(t *testing.T) {
	setting := &UserInsightSetting{
		AutoBanCodeRatioEnabled: true,
		AutoBanCodeRatioPercent: 60,
		AutoBanCodeMinRequests:  20,
	}
	cases := []struct {
		name     string
		total    int
		code     int
		expected bool
	}{
		{"正好达到阈值", 100, 60, true},
		{"略低于阈值", 100, 59, false},
		{"远超阈值", 100, 95, true},
		{"请求数不足门槛", 10, 10, false},
		{"刚好达到门槛且超阈值", 20, 20, true},
		{"刚好达到门槛但未超阈值", 20, 11, false},
		{"无代码请求", 100, 0, false},
		{"零请求", 0, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			profile := &UserInsightProfile{
				TotalRequests: tc.total,
				CodeRequests:  tc.code,
			}
			assert.Equal(t, tc.expected, insightCodeRatioQualified(profile, setting))
		})
	}
}

// 阈值配置越界时必须回落到保守默认，而不是让规则对所有人成立。
func TestInsightCodeRatioNormalizesInvalidConfig(t *testing.T) {
	profile := &UserInsightProfile{TotalRequests: 100, CodeRequests: 50}
	zero := &UserInsightSetting{
		AutoBanCodeRatioEnabled: true,
		AutoBanCodeRatioPercent: 0, // 非法：会让所有人命中
		AutoBanCodeMinRequests:  20,
	}
	// 回落到 60%，50% 不该命中。
	assert.False(t, insightCodeRatioQualified(profile, zero))
	tooBig := &UserInsightSetting{
		AutoBanCodeRatioEnabled: true,
		AutoBanCodeRatioPercent: 500, // 非法：会让规则永不成立
		AutoBanCodeMinRequests:  0,   // 非法：门槛消失
	}
	assert.False(t, insightCodeRatioQualified(profile, tooBig))
	high := &UserInsightSetting{
		AutoBanCodeRatioEnabled: true,
		AutoBanCodeRatioPercent: 500,
		AutoBanCodeMinRequests:  0,
	}
	profile.CodeRequests = 80
	// 回落后阈值为 60、门槛为 20：80/100 应命中。
	assert.True(t, insightCodeRatioQualified(profile, high))
}

// 两条规则各自独立开关，且破甲规则优先——邮件正文按原因分开措辞，
// 判错原因会让用户收到答不上来的通知。
func TestInsightAutoBanReasonPrecedence(t *testing.T) {
	// 破甲达 confirmed 且有代码请求，同时占比也超标。
	profile := &UserInsightProfile{
		TotalRequests:      100,
		CodeRequests:       90,
		JailbreakConfirmed: 5,
		JailbreakMaxScore:  90,
	}
	both := &UserInsightSetting{
		AutoBanEnabled:          true,
		AutoBanMinRisk:          "confirmed",
		AutoBanCodeRatioEnabled: true,
		AutoBanCodeRatioPercent: 60,
		AutoBanCodeMinRequests:  20,
	}
	assert.Equal(t, autoBanReasonJailbreakCode, insightAutoBanReason(profile, both))
	// 只开占比规则时，即便破甲达标也应报占比原因。
	ratioOnly := &UserInsightSetting{
		AutoBanEnabled:          false,
		AutoBanCodeRatioEnabled: true,
		AutoBanCodeRatioPercent: 60,
		AutoBanCodeMinRequests:  20,
	}
	assert.Equal(t, autoBanReasonCodeRatio, insightAutoBanReason(profile, ratioOnly))
	// 只开破甲规则时，纯代码用户不该被封。
	coderOnly := &UserInsightProfile{TotalRequests: 100, CodeRequests: 100}
	jailbreakOnly := &UserInsightSetting{
		AutoBanEnabled:          true,
		AutoBanMinRisk:          "confirmed",
		AutoBanCodeRatioEnabled: false,
	}
	assert.Equal(t, "", insightAutoBanReason(coderOnly, jailbreakOnly))
	// 两条都关时任何画像都不命中。
	off := &UserInsightSetting{}
	assert.Equal(t, "", insightAutoBanReason(profile, off))
}
