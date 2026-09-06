package billing

import (
	"math"
	"testing"

	"github.com/QuantumNous/new-api/internal/common"
)

// unit 一个显示货币单位对应的内部额度（QuotaPerUnit=500000，汇率 1）
const unit = 500000

// TestCalcTransferFeeProgressive 累进费率：分段计算而非整笔按最高档
func TestCalcTransferFeeProgressive(t *testing.T) {
	s := GetQQBotSetting()
	orig := s.TransferFeeBrackets
	defer func() { s.TransferFeeBrackets = orig }()
	s.TransferFeeBrackets = "" // 用默认档位

	cases := []struct {
		name  string
		units float64
		want  float64 // 期望手续费（显示货币单位）
	}{
		// 1 以内全部 3%
		{"0.5 全在首档", 0.5, 0.5 * 0.03},
		{"正好 1 档界", 1, 1 * 0.03},
		// 超过 1 的部分按 5%
		{"2 跨两档", 2, 1*0.03 + 1*0.05},
		{"5 正好第二档界", 5, 1*0.03 + 4*0.05},
		// 超过 5 的部分按 8%
		{"10 跨三档", 10, 1*0.03 + 4*0.05 + 5*0.08},
		{"20 第三档界", 20, 1*0.03 + 4*0.05 + 15*0.08},
		// 超过 20 按 12%
		{"50 跨四档", 50, 1*0.03 + 4*0.05 + 15*0.08 + 30*0.12},
		{"100 第四档界", 100, 1*0.03 + 4*0.05 + 15*0.08 + 80*0.12},
		// 超过 100 按 18%
		{"200 进最高档", 200, 1*0.03 + 4*0.05 + 15*0.08 + 80*0.12 + 100*0.18},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			amount := int(c.units * unit)
			gotQuota := CalcTransferFee(amount)
			wantQuota := int(math.Round(c.want * unit))
			// 允许 1 个额度的舍入误差
			if diff := gotQuota - wantQuota; diff > 1 || diff < -1 {
				t.Fatalf("CalcTransferFee(%.2f单位) = %d, want %d", c.units, gotQuota, wantQuota)
			}
		})
	}
}

// TestTransferFeeRateIncreases 综合费率必须随金额单调不降
// 这是「随数量增加抽成比例也增加」的核心要求
func TestTransferFeeRateIncreases(t *testing.T) {
	s := GetQQBotSetting()
	orig := s.TransferFeeBrackets
	defer func() { s.TransferFeeBrackets = orig }()
	s.TransferFeeBrackets = ""

	prevRate := 0.0
	for _, units := range []float64{0.5, 1, 2, 5, 10, 20, 50, 100, 200, 500} {
		amount := int(units * unit)
		fee := CalcTransferFee(amount)
		rate := float64(fee) / float64(amount)
		if rate < prevRate-1e-9 {
			t.Fatalf("综合费率在 %.1f 单位处下降: %.4f -> %.4f", units, prevRate, rate)
		}
		prevRate = rate
	}
}

// TestTransferFeeNoCliff 相邻金额不出现「多转反而到账更少」的断崖
func TestTransferFeeNoCliff(t *testing.T) {
	s := GetQQBotSetting()
	orig := s.TransferFeeBrackets
	defer func() { s.TransferFeeBrackets = orig }()
	s.TransferFeeBrackets = ""

	// 在每个档位边界附近逐步加钱，到账额必须单调不降
	prevReceived := -1
	for amount := 1 * unit / 10; amount <= 120*unit; amount += unit / 10 {
		received := amount - CalcTransferFee(amount)
		if received < prevReceived {
			t.Fatalf("金额 %d 时到账下降: %d -> %d", amount, prevReceived, received)
		}
		prevReceived = received
	}
}

// TestCalcTransferFeeBounds 手续费边界：至少 1，且不能吞掉全部本金
func TestCalcTransferFeeBounds(t *testing.T) {
	if got := CalcTransferFee(0); got != 0 {
		t.Fatalf("金额 0 时手续费应为 0，实际 %d", got)
	}
	if got := CalcTransferFee(-100); got != 0 {
		t.Fatalf("负金额时手续费应为 0，实际 %d", got)
	}
	// 极小额：手续费算下来不足 1 也要收 1
	if got := CalcTransferFee(2); got < 1 {
		t.Fatalf("极小额手续费应至少为 1，实际 %d", got)
	}
	// 手续费必须留至少 1 给收款方
	for _, amount := range []int{1, 2, 3, 10, 100} {
		fee := CalcTransferFee(amount)
		if fee >= amount && amount > 1 {
			t.Fatalf("金额 %d 的手续费 %d 吞掉了全部本金", amount, fee)
		}
	}
}

// TestFeeBracketsFallback 非法配置回落到默认档位，不能让转账整体瘫掉
func TestFeeBracketsFallback(t *testing.T) {
	s := GetQQBotSetting()
	orig := s.TransferFeeBrackets
	defer func() { s.TransferFeeBrackets = orig }()

	for _, bad := range []string{
		"这不是json",
		"[]",
		`[{"up_to":1,"rate":1.5}]`,  // 费率 >= 1
		`[{"up_to":1,"rate":-0.1}]`, // 负费率
	} {
		s.TransferFeeBrackets = bad
		got := feeBrackets()
		if len(got) != len(defaultFeeBrackets) {
			t.Fatalf("配置 %q 应回落到默认档位，实际 %v", bad, got)
		}
	}

	// 合法配置应生效
	s.TransferFeeBrackets = `[{"up_to":0,"rate":0.1}]`
	got := feeBrackets()
	if len(got) != 1 || got[0].Rate != 0.1 {
		t.Fatalf("合法配置未生效: %v", got)
	}
}

// TestCustomFlatBracket 单一档位配置下就是固定费率
func TestCustomFlatBracket(t *testing.T) {
	s := GetQQBotSetting()
	orig := s.TransferFeeBrackets
	defer func() { s.TransferFeeBrackets = orig }()
	s.TransferFeeBrackets = `[{"up_to":0,"rate":0.03}]`

	amount := 10 * unit
	fee := CalcTransferFee(amount)
	want := int(math.Round(0.03 * float64(amount)))
	if diff := fee - want; diff > 1 || diff < -1 {
		t.Fatalf("固定 3%% 档位下手续费 = %d, want %d", fee, want)
	}
}

// TestParseTransferAmount 金额解析兼容多种写法
func TestParseTransferAmount(t *testing.T) {
	cases := []struct {
		content string
		want    float64
		ok      bool
	}{
		{"/转账 <qqbot-at-user id=\"X\" /> 1.5", 1.5, true},
		{"<qqbot-at-user id=\"X\" /> /转账 2", 2, true},
		// 金额与 @ 顺序可互换
		{"/转账 3.25 <qqbot-at-user id=\"X\" />", 3.25, true},
		// 带货币符号
		{"/转账 <qqbot-at-user id=\"X\" /> 1.5🧀", 1.5, true},
		{"/转账 <qqbot-at-user id=\"X\" /> 0.1", 0.1, true},
		// 没金额
		{"/转账 <qqbot-at-user id=\"X\" />", 0, false},
		{"/转账", 0, false},
		// 零和负数不接受
		{"/转账 <qqbot-at-user id=\"X\" /> 0", 0, false},
	}
	for _, c := range cases {
		got, err := parseTransferAmount(c.content)
		if (err == nil) != c.ok {
			t.Fatalf("parseTransferAmount(%q) ok=%v, want %v (err=%v)",
				c.content, err == nil, c.ok, err)
		}
		if c.ok && math.Abs(got-c.want) > 1e-9 {
			t.Fatalf("parseTransferAmount(%q) = %v, want %v", c.content, got, c.want)
		}
	}
}

// TestIsTransferCommand 转账指令识别
func TestIsTransferCommand(t *testing.T) {
	yes := []string{
		"/转账 @x 1",
		"<qqbot-at-user id=\"X\" /> /转账 1.5",
		"/转账",
		"转账 @x 1",
		"/打钱 @x 1",
	}
	for _, c := range yes {
		if !isTransferCommand(c) {
			t.Fatalf("%q 应被识别为转账指令", c)
		}
	}
	no := []string{
		"/签到",
		"/转账费率",  // 费率查询要能和转账区分开
		"我要转账给你", // 自然语言不触发
		"/开启掉落",
		"/关闭转账",
	}
	for _, c := range no {
		if isTransferCommand(c) {
			t.Fatalf("%q 不该被识别为转账指令", c)
		}
	}
}

// TestTransferInfoBeforeTransfer 费率指令必须能被单独识别出来
func TestTransferInfoBeforeTransfer(t *testing.T) {
	if !isTransferInfoCommand(CmdTransferInfo) {
		t.Fatal("/转账费率 应被识别为费率指令")
	}
	if isTransferInfoCommand("/转账 @x 1") {
		t.Fatal("普通转账不该被识别为费率指令")
	}
}

// TestPickTransferTargetSkipsBotAndSelf 收款人筛选跳过机器人与自己
func TestPickTransferTargetSkipsBotAndSelf(t *testing.T) {
	const sender = "SENDER_OPENID"

	// 只 @ 了机器人：识别不出收款人
	mentions := []Mention{{ID: "BOT_OPENID", Bot: true}}
	if _, _, ok := pickTransferTarget(mentions, sender); ok {
		t.Fatal("只 @ 机器人时不该识别出收款人")
	}
	if hasNonSelfMention(mentions, sender) {
		t.Fatal("机器人不应算作有效 @ 对象")
	}

	// 只 @ 了自己
	mentions = []Mention{{ID: sender}}
	if _, _, ok := pickTransferTarget(mentions, sender); ok {
		t.Fatal("只 @ 自己时不该识别出收款人")
	}
	if hasNonSelfMention(mentions, sender) {
		t.Fatal("自己不应算作有效 @ 对象")
	}

	// @ 了一个未绑定的人：hasNonSelfMention 是纯逻辑，能直接验证；
	// pickTransferTarget 会走到 IsQQBound → DB，单测无 DB 连接，
	// 该分支留给集成验证，这里只断言「存在有效 @ 对象」的判定。
	mentions = []Mention{{ID: "BOT_OPENID", Bot: true}, {ID: "UNBOUND_OPENID"}}
	if !hasNonSelfMention(mentions, sender) {
		t.Fatal("存在未绑定的 @ 对象时应返回 true，用于区分错误提示")
	}

	// 空 mentions
	if hasNonSelfMention(nil, sender) {
		t.Fatal("空 mentions 不该有有效 @ 对象")
	}
}

// TestIsTransferSwitchCommand 群级转账开关指令不与其他指令冲突
func TestIsTransferSwitchCommand(t *testing.T) {
	if cmd, ok := isTransferSwitchCommand("/关闭转账"); !ok || cmd != CmdDisableTransfer {
		t.Fatalf("关闭转账识别失败: %q %v", cmd, ok)
	}
	if cmd, ok := isTransferSwitchCommand("<qqbot-at-user id=\"X\" /> /开启转账"); !ok || cmd != CmdEnableTransfer {
		t.Fatalf("开启转账识别失败: %q %v", cmd, ok)
	}
	// 不能和签到开关、掉落开关串台
	for _, c := range []string{"/关闭签到", "/开启签到", "/开启掉落", "/转账 @x 1"} {
		if _, ok := isTransferSwitchCommand(c); ok {
			t.Fatalf("%q 不该被识别为转账开关", c)
		}
	}
	// 转账开关不该被转账指令吃掉
	for _, c := range []string{CmdDisableTransfer, CmdEnableTransfer} {
		if isTransferCommand(c) {
			t.Fatalf("%q 被误判为转账指令", c)
		}
	}
}

// TestIsTransferDisabledGroup 群级转账黑名单
func TestIsTransferDisabledGroup(t *testing.T) {
	s := GetQQBotSetting()
	orig := s.TransferDisabledGroups
	defer func() { s.TransferDisabledGroups = orig }()

	s.TransferDisabledGroups = ""
	if IsTransferDisabledGroup("G1") {
		t.Fatal("空名单时不该禁用任何群")
	}
	s.TransferDisabledGroups = "G1"
	if !IsTransferDisabledGroup("G1") {
		t.Fatal("G1 应被禁用")
	}
	if IsTransferDisabledGroup("G2") {
		t.Fatal("G2 不该被禁用")
	}
}

// TestIsBalanceCommand 余额指令识别
func TestIsBalanceCommand(t *testing.T) {
	for _, c := range []string{"/余额", "余额", "/我的余额"} {
		if !isBalanceCommand(c) {
			t.Fatalf("%q 应被识别为余额指令", c)
		}
	}
	for _, c := range []string{"/签到", "余额不足", "/转账 @x 1"} {
		if isBalanceCommand(c) {
			t.Fatalf("%q 不该被识别为余额指令", c)
		}
	}
}

// TestQuotaUnitRoundTrip 额度与显示货币互转不失真
func TestQuotaUnitRoundTrip(t *testing.T) {
	for _, units := range []float64{0.1, 0.3, 1, 1.5, 3, 100} {
		quota := unitsToQuota(units)
		back := quotaToUnits(quota)
		if math.Abs(back-units) > 1e-6 {
			t.Fatalf("往返转换失真: %v -> %d -> %v", units, quota, back)
		}
	}
	// QuotaPerUnit 是全局变量，确认测试假设成立
	if common.QuotaPerUnit != 500*1000.0 {
		t.Skipf("QuotaPerUnit 非默认值 (%v)，跳过绝对值断言", common.QuotaPerUnit)
	}
	if got := unitsToQuota(0.3); got != 150000 {
		t.Fatalf("0.3 单位应为 150000 额度，实际 %d", got)
	}
}

// TestBalanceCommandNotCountable 余额与转账指令不计入掉落进度
func TestBalanceCommandNotCountable(t *testing.T) {
	for _, c := range []string{"/余额", "/转账 @x 1", "/转账费率", "/关闭转账", "/关闭签到", "/开启掉落"} {
		if _, ok := isMessageCountable(c); ok {
			t.Fatalf("%q 不该计入掉落进度", c)
		}
	}
}

// TestStickerCountable 表情包计入掉落进度，且不同表情包不互相去重
func TestStickerCountable(t *testing.T) {
	// 相同表情包归一化结果一致（用于去重）
	a, ok := isMessageCountable(`<faceType=6,faceId="123">`)
	if !ok {
		t.Fatal("表情包应计数")
	}
	b, ok := isMessageCountable(`<faceType=6,faceId="123">`)
	if !ok || a != b {
		t.Fatal("相同表情包归一化结果应一致")
	}
	// 不同表情包归一化结果不同
	c, ok := isMessageCountable(`<faceType=6,faceId="456">`)
	if !ok || c == a {
		t.Fatal("不同表情包归一化结果应不同")
	}
}
