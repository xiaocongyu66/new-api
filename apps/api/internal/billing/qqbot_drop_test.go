package billing

import "testing"

// TestIsMessageCountable 覆盖不计数的各类消息
func TestIsMessageCountable(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    bool
	}{
		{"正常聊天", "今天天气不错", true},
		{"带@标签的聊天", "<qqbot-at-user id=\"X\" /> 你好呀朋友", true},
		{"英文短句", "hello world", true},
		{"签到指令", "/签到", false},
		{"签到别名", "签到", false},
		{"带@的签到", "<qqbot-at-user id=\"X\" /> /签到", false},
		{"掉落指令", "/开启掉落", false},
		{"单字符数字", "1", false},
		{"三连数字算有效", "111", true},
		{"单字", "嗯", false},
		{"纯标点", "。。。", false},
		{"空消息", "   ", false},
		{"绑定验证码", "Ab3xYz", false},
		// QQ 表情包：应该计数
		{"表情包", `<faceType=6,faceId="123",bigFaceId="456">`, true},
		{"表情包带前缀空格", ` <faceType=6,faceId="789">`, true},
		// 指令不计数
		{"转账指令", "/转账 @x 1", false},
		{"转账费率", "/转账费率", false},
		{"余额查询", "/余额", false},
		{"关闭签到", "/关闭签到", false},
		{"关闭转账", "/关闭转账", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, ok := isMessageCountable(c.content); ok != c.want {
				t.Fatalf("isMessageCountable(%q) = %v, want %v", c.content, ok, c.want)
			}
		})
	}
}

// TestIsMessageCountableNormalize 大小写与标点差异应归一化为同一条
func TestIsMessageCountableNormalize(t *testing.T) {
	a, ok := isMessageCountable("Hello, World!")
	if !ok {
		t.Fatal("首条消息应计数")
	}
	b, ok := isMessageCountable("hello world")
	if !ok {
		t.Fatal("第二条消息应计数")
	}
	if a != b {
		t.Fatalf("归一化结果不一致: %q vs %q", a, b)
	}
}

// TestTickDropCounterDedup 重复内容不推进计数
func TestTickDropCounterDedup(t *testing.T) {
	const gid = "test_group_dedup"
	dropStates = make(map[string]*groupDropState)

	// 固定阈值为 3 条：min=max 时 randomTarget 直接返回该值
	if triggered := tickDropCounter(gid, "aaa", 3, 3); triggered {
		t.Fatal("第 1 条不该触发")
	}
	// 同一内容重复 5 次都不应推进
	for i := 0; i < 5; i++ {
		if triggered := tickDropCounter(gid, "aaa", 3, 3); triggered {
			t.Fatal("重复内容不该推进计数")
		}
	}
	if got := dropStates[gid].count; got != 1 {
		t.Fatalf("重复内容后计数应为 1，实际 %d", got)
	}

	if triggered := tickDropCounter(gid, "bbb", 3, 3); triggered {
		t.Fatal("第 2 条不该触发")
	}
	if triggered := tickDropCounter(gid, "ccc", 3, 3); !triggered {
		t.Fatal("第 3 条应触发掉落")
	}
}

// TestResetDropCounter 发放成功后计数归零
func TestResetDropCounter(t *testing.T) {
	const gid = "test_group_reset"
	dropStates = make(map[string]*groupDropState)

	tickDropCounter(gid, "x1", 2, 2)
	if !tickDropCounter(gid, "x2", 2, 2) {
		t.Fatal("第 2 条应触发")
	}
	resetDropCounter(gid, 2, 2)
	if got := dropStates[gid].count; got != 0 {
		t.Fatalf("重置后计数应为 0，实际 %d", got)
	}
}

// TestTickDropCounterHoldsWhenNotReset 未重置时计数保留，掉落顺延给下一个人
func TestTickDropCounterHoldsWhenNotReset(t *testing.T) {
	const gid = "test_group_hold"
	dropStates = make(map[string]*groupDropState)

	tickDropCounter(gid, "y1", 2, 2)
	if !tickDropCounter(gid, "y2", 2, 2) {
		t.Fatal("第 2 条应触发")
	}
	// 不调用 resetDropCounter，模拟触发者未绑定账号
	if !tickDropCounter(gid, "y3", 2, 2) {
		t.Fatal("未重置时下一条仍应触发，掉落顺延")
	}
}

// TestMarkMessageSeen 相同消息 ID 只处理一次
func TestMarkMessageSeen(t *testing.T) {
	seenMsgIDs = make(map[string]struct{}, msgIDCacheSize)
	seenMsgOrder = nil

	if markMessageSeen("msg-1") {
		t.Fatal("首次出现不该判定为已处理")
	}
	if !markMessageSeen("msg-1") {
		t.Fatal("重复出现应判定为已处理")
	}
	// 空 ID 不参与去重，避免把所有无 ID 事件当成同一条
	if markMessageSeen("") || markMessageSeen("") {
		t.Fatal("空 ID 不应参与去重")
	}
}

// TestMarkMessageSeenEviction 缓存超限后最旧的 ID 被淘汰
func TestMarkMessageSeenEviction(t *testing.T) {
	seenMsgIDs = make(map[string]struct{}, msgIDCacheSize)
	seenMsgOrder = nil

	markMessageSeen("oldest")
	for i := 0; i < msgIDCacheSize; i++ {
		markMessageSeen(string(rune('a')) + string(rune(i)) + "-fill")
	}
	if len(seenMsgIDs) > msgIDCacheSize {
		t.Fatalf("缓存超出上限: %d", len(seenMsgIDs))
	}
	if markMessageSeen("oldest") {
		t.Fatal("最旧的 ID 应已被淘汰")
	}
}

// TestRandomTargetRange 随机阈值必须落在区间内
func TestRandomTargetRange(t *testing.T) {
	for i := 0; i < 500; i++ {
		got := randomTarget(5, 30)
		if got < 5 || got > 30 {
			t.Fatalf("randomTarget 越界: %d", got)
		}
	}
	if got := randomTarget(0, 0); got != 1 {
		t.Fatalf("非法下界应回落到 1，实际 %d", got)
	}
	if got := randomTarget(10, 3); got != 10 {
		t.Fatalf("上界小于下界时应返回下界，实际 %d", got)
	}
}

// TestRandomDropQuotaRange 随机额度必须落在区间内
func TestRandomDropQuotaRange(t *testing.T) {
	for i := 0; i < 500; i++ {
		got := randomDropQuota(150000, 1500000)
		if got < 150000 || got > 1500000 {
			t.Fatalf("randomDropQuota 越界: %d", got)
		}
	}
}

// TestDropUnitName 小于 1 个货币单位为碎屑，否则为碎片
func TestDropUnitName(t *testing.T) {
	// QuotaPerUnit = 500000，汇率默认 1
	if got := dropUnitName(150000); got != "奶酪碎屑" {
		t.Fatalf("0.3 应为碎屑，实际 %q", got)
	}
	if got := dropUnitName(500000); got != "奶酪碎片" {
		t.Fatalf("1.0 应为碎片，实际 %q", got)
	}
	if got := dropUnitName(1500000); got != "奶酪碎片" {
		t.Fatalf("3.0 应为碎片，实际 %q", got)
	}
}

// TestFormatDropAmount 金额去掉多余尾零
func TestFormatDropAmount(t *testing.T) {
	cases := map[int]string{
		150000:  "0.3",
		500000:  "1",
		1500000: "3",
		250000:  "0.5",
	}
	for quota, want := range cases {
		if got := formatDropAmount(quota); got != want {
			t.Fatalf("formatDropAmount(%d) = %q, want %q", quota, got, want)
		}
	}
}

// TestIsDropCommand 指令识别兼容 @机器人 前缀
func TestIsDropCommand(t *testing.T) {
	if cmd, ok := isDropCommand("<qqbot-at-user id=\"X\" /> /开启掉落"); !ok || cmd != CmdEnableDrop {
		t.Fatalf("带 @ 的开启指令未识别: cmd=%q ok=%v", cmd, ok)
	}
	if _, ok := isDropCommand("/签到"); ok {
		t.Fatal("签到指令不应被识别为掉落指令")
	}
}
