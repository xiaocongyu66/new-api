package billing

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withCleanupSetting 临时改 QQ 机器人清理配置，测试结束后还原。
// 这些是包级全局，不还原会污染同包其它测试。
func withCleanupSetting(t *testing.T, mutate func(s *QQBotSetting)) {
	t.Helper()
	original := qqBotSetting
	mutate(&qqBotSetting)
	// 必须用 t.Cleanup 而非 defer：本函数在测试断言之前就返回，
	// defer 会立刻还原配置，断言看到的就只剩默认值。
	t.Cleanup(func() { qqBotSetting = original })
}

// TestParseAtUserID OneBot v11 的 @ CQ 码解析。
// 桥接链路靠它从 NapCat 上报的消息里解出真实 QQ 号，解错或解不到
// 都会导致踢错人或踢不到人。
func TestParseAtUserID(t *testing.T) {
	cases := []struct {
		name    string
		message string
		want    int64
		wantOk  bool
	}{
		{"标准 at", "清理提醒 [CQ:at,qq=123456] 请尽快发言", 123456, true},
		{"消息开头的 at", "[CQ:at,qq=998877] 你好", 998877, true},
		{"多个 at 取第一个", "[CQ:at,qq=111] [CQ:at,qq=222]", 111, true},
		{"大QQ号", "[CQ:at,qq=18446744073686646784] x", 0, false}, // 溢出 int64，拒绝而非截断
		{"没有 at", "今天天气不错", 0, false},
		{"空QQ号", "[CQ:at,qq=] 滑水", 0, false},
		{"非数字", "[CQ:at,qq=abc] 滑水", 0, false},
		{"零", "[CQ:at,qq=0] 滑水", 0, false},
		{"负数", "[CQ:at,qq=-5] 滑水", 0, false},
		{"缺右括号", "[CQ:at,qq=12345", 0, false},
		{"仅前缀", "前缀 [CQ:at,qq=", 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := parseAtUserID(c.message)
			assert.Equal(t, c.wantOk, ok)
			if c.wantOk {
				assert.Equal(t, c.want, got)
			}
		})
	}
}

// TestParseAtUserIDOverflow 大QQ号必须被拒绝，不能静默截断成一个错号。
func TestParseAtUserIDOverflow(t *testing.T) {
	got, ok := parseAtUserID("[CQ:at,qq=18446744073686646784]")
	require.False(t, ok)
	assert.Equal(t, int64(0), got)
}

// TestBuildWarningContent 官方 bot 的 @ 警告文案构造。
// 验收标准之一：警告消息必须携带正确的 <qqbot-at-user> 语法，这是
// 官方 bot 与 NapCat 之间传递真实身份的唯一桥梁。
func TestBuildWarningContent(t *testing.T) {
	withCleanupSetting(t, func(s *QQBotSetting) {
		s.CleanupInactiveDays = 90
		s.CleanupGraceDays = 3
		s.CleanupWarningTemplate = "{@} 已 {天数} 天未发言，{宽限天数} 天内再不说话就要被请出啦"
	})

	got := buildWarningContent("MEMBER_ABC")
	assert.Contains(t, got, `<qqbot-at-user id="MEMBER_ABC"/>`)
	assert.Contains(t, got, "已 90 天未发言")
	assert.Contains(t, got, "3 天内再不说话")
	// 占位符必须全部被替换，残留说明模板与替换逻辑不匹配
	assert.NotContains(t, got, "{@}")
	assert.NotContains(t, got, "{天数}")
	assert.NotContains(t, got, "{宽限天数}")
}

// TestBuildWarningContentDefaultTemplate 模板留空时回落默认文案，
// 且默认文案同样使用官方 @ 语法。
func TestBuildWarningContentDefaultTemplate(t *testing.T) {
	withCleanupSetting(t, func(s *QQBotSetting) {
		s.CleanupWarningTemplate = ""
		s.CleanupInactiveDays = 30
		s.CleanupGraceDays = 7
	})

	got := buildWarningContent("M1")
	assert.Contains(t, got, `<qqbot-at-user id="M1"/>`)
	assert.Contains(t, got, "30 天")
	assert.Contains(t, got, "7 天")
}

// TestStripAtTags 抹掉两种 @ 表示后正文必须一致——桥接匹配就靠这个。
func TestStripAtTags(t *testing.T) {
	withCleanupSetting(t, func(s *QQBotSetting) {
		s.CleanupInactiveDays = 30
		s.CleanupGraceDays = 7
		s.CleanupWarningTemplate = "{@} 提醒：{天数} 天未发言"
	})

	official := buildWarningContent("MEMBER_X") // 官方侧：<qqbot-at-user .../>
	napcat := "[CQ:at,qq=12345] 提醒：30 天未发言"     // NapCat 侧：CQ 码

	assert.Equal(t, stripAtTags(official), stripAtTags(napcat))
	assert.Equal(t, "提醒：30 天未发言", stripAtTags(official))
	// 与本功能无关的其它 @ 消息不应被误判为清理警告
	assert.NotEqual(t, stripAtTags(official), stripAtTags("[CQ:at,qq=12345] 过年好"))
}

// TestDecideCleanupAction 潜水清理的核心策略，直接对应验收标准：
// 宽限期内发言的用户不被踢；超期未发言且已桥接的才踢。
func TestDecideCleanupAction(t *testing.T) {
	const day = int64(86400)
	now := int64(1700000000)

	cases := []struct {
		name       string
		warnedAt   int64
		graceDays  int
		warnWindow int64
		exempt     bool
		bridged    bool
		want       cleanupAction
	}{
		// 豁免优先级最高，不管警告状态与桥接状态
		{"豁免用户即使到期也不动", now - 30*day, 7, day, true, true, actionSkipExempt},
		// 从未被警告：先警告。即使已桥接也不能同轮踢，这是宽限期的意义
		{"未警告先警告", 0, 7, day, false, true, actionWarn},
		{"未警告且未桥接也是先警告", 0, 7, day, false, false, actionWarn},
		// 宽限期内：给用户喘息时间
		{"警告后1天仍在宽限期", now - 1*day, 7, day, false, true, actionSkipRecent},
		{"警告后6天仍在宽限期", now - 6*day, 7, day, false, true, actionSkipRecent},
		{"警告后6天未桥接也不踢", now - 6*day, 7, day, false, false, actionSkipRecent},
		// 已过宽限期
		{"过宽限期但未桥接，等下次", now - 8*day, 7, day, false, false, actionWaitBridge},
		{"过宽限期且已桥接，踢", now - 8*day, 7, day, false, true, actionKick},
		{"过宽限期很久且已桥接，踢", now - 300*day, 7, day, false, true, actionKick},
		// 警告窗口内即使超过宽限期也不重复警告（宽限期设 0 时的保护）
		{"宽限期0但警告窗口内不重复警告", now - 3600, 0, day, false, true, actionSkipRecent},
		{"宽限期0且超过警告窗口直接踢", now - 2*day, 0, day, false, true, actionKick},
		// 时钟偏移（warnedAt 在未来）按宽限期内处理，不踢
		{"警告时间在未来按宽限期内处理", now + 3600, 7, day, false, true, actionSkipRecent},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := &QQGroupMember{WarnedAt: c.warnedAt}
			got := decideCleanupAction(m, now, c.graceDays, c.warnWindow, c.exempt, c.bridged)
			assert.Equal(t, c.want, got)
		})
	}
}

// TestDecideCleanupActionGraceBug 回归：修复前首次被警告的成员会因为
// 使用更新后的 warned_at 而在同轮被踢。策略函数只认传入的档案值，
// 从源头保证「先警告、隔宽限期、再踢」的次序。
func TestDecideCleanupActionGraceBug(t *testing.T) {
	now := int64(1700000000)
	// 一个从未被警告的成员，本轮才第一次见到
	m := &QQGroupMember{WarnedAt: 0, QQNumber: 12345}
	assert.Equal(t, actionWarn, decideCleanupAction(m, now, 7, int64(86400), false, true))
}

// TestGetCleanupKickIntervalRange 踢人间隔区间必须恒满足 min<=max，
// 非法配置要回落到安全默认，不能让 rand.Intn 拿到负数 panic。
func TestGetCleanupKickIntervalRange(t *testing.T) {
	cases := []struct {
		name    string
		minSec  int
		maxSec  int
		wantMin int
		wantMax int
	}{
		{"正常区间", 120, 300, 120, 300},
		{"零值回落默认", 0, 0, 120, 120},
		{"负值回落默认", -10, -5, 120, 120},
		{"min 为零 max 正常", 0, 240, 120, 240},
		{"倒挂区间以 min 为上界", 300, 120, 300, 300},
		{"单点区间", 180, 180, 180, 180},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			withCleanupSetting(t, func(s *QQBotSetting) {
				s.CleanupKickMinSeconds = c.minSec
				s.CleanupKickMaxSeconds = c.maxSec
			})
			min, max := GetCleanupKickIntervalRange()
			assert.Equal(t, c.wantMin, min)
			assert.Equal(t, c.wantMax, max)
			require.LessOrEqual(t, min, max)
		})
	}
}

// TestGetCleanupDayFallbacks 非法阈值必须回落默认，否则配置成 0 会
// 把全群甚至刚发过言的成员都判成潜水。
func TestGetCleanupDayFallbacks(t *testing.T) {
	withCleanupSetting(t, func(s *QQBotSetting) {
		s.CleanupInactiveDays = 0
		// 宽限期允许为 0（警告完下一轮就踢），只有负数才回落
		s.CleanupGraceDays = 0
	})
	assert.Equal(t, 30, GetCleanupInactiveDays())
	assert.Equal(t, 0, GetCleanupGraceDays())

	withCleanupSetting(t, func(s *QQBotSetting) {
		s.CleanupInactiveDays = -5
		s.CleanupGraceDays = -5
	})
	assert.Equal(t, 30, GetCleanupInactiveDays())
	assert.Equal(t, 7, GetCleanupGraceDays())

	withCleanupSetting(t, func(s *QQBotSetting) {
		s.CleanupInactiveDays = 45
		s.CleanupGraceDays = 3
	})
	assert.Equal(t, 45, GetCleanupInactiveDays())
	assert.Equal(t, 3, GetCleanupGraceDays())
}

// TestIsCleanupGroup 只有总开关开启且群在白名单内才参与清理。
// 清理是踢人操作，误开启会动到不该动的群。
func TestIsCleanupGroup(t *testing.T) {
	const inGroup = "GROUP_IN"
	const outGroup = "GROUP_OUT"

	withCleanupSetting(t, func(s *QQBotSetting) {
		s.CleanupEnabled = false
		s.CleanupGroups = inGroup
	})
	assert.False(t, IsCleanupGroup(inGroup), "总开关关闭时任何群都不参与清理")

	withCleanupSetting(t, func(s *QQBotSetting) {
		s.CleanupEnabled = true
		s.CleanupGroups = inGroup
	})
	assert.True(t, IsCleanupGroup(inGroup))
	assert.False(t, IsCleanupGroup(outGroup))
	assert.False(t, IsCleanupGroup(""), "空 openid 不应命中")

	// 逗号分隔的多群白名单
	withCleanupSetting(t, func(s *QQBotSetting) {
		s.CleanupEnabled = true
		s.CleanupGroups = "GROUP_A,GROUP_B"
	})
	assert.True(t, IsCleanupGroup("GROUP_A"))
	assert.True(t, IsCleanupGroup("GROUP_B"))
	assert.False(t, IsCleanupGroup("GROUP_C"))
}

// TestGroupOpenID2Number group_openid 到真实群号的映射，映射缺失时
// 返回 0，调用方据此跳过执行类操作（警告仍可发）。
func TestGroupOpenID2Number(t *testing.T) {
	withCleanupSetting(t, func(s *QQBotSetting) {
		s.CleanupGroupNumbers = `{"GROUP_A": 111, "GROUP_B": 222}`
	})
	assert.Equal(t, int64(111), groupOpenID2Number("GROUP_A"))
	assert.Equal(t, int64(222), groupOpenID2Number("GROUP_B"))
	assert.Equal(t, int64(0), groupOpenID2Number("GROUP_UNKNOWN"))

	// 非法 JSON 不能 panic，记日志后返回 0
	withCleanupSetting(t, func(s *QQBotSetting) {
		s.CleanupGroupNumbers = "not-json"
	})
	assert.Equal(t, int64(0), groupOpenID2Number("GROUP_A"))

	// 空配置
	withCleanupSetting(t, func(s *QQBotSetting) {
		s.CleanupGroupNumbers = ""
	})
	assert.Equal(t, int64(0), groupOpenID2Number("GROUP_A"))
}

// TestBuildWarningContentNoTemplateReuse 占位符写成不会被替换的形式时，
// 文案应原样保留该文本（不产生空替换）。
func TestBuildWarningContentNoTemplateReuse(t *testing.T) {
	withCleanupSetting(t, func(s *QQBotSetting) {
		s.CleanupInactiveDays = 30
		s.CleanupGraceDays = 7
		s.CleanupWarningTemplate = "{@} 固定文案，无其它占位"
	})
	got := buildWarningContent("M9")
	assert.True(t, strings.HasPrefix(got, `<qqbot-at-user id="M9"/>`))
	assert.Contains(t, got, "固定文案，无其它占位")
}
