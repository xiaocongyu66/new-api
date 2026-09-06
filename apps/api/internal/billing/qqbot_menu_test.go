package billing

import (
	"strings"
	"testing"
)

// TestIsRedPacketCommand 红包指令识别
func TestIsRedPacketCommand(t *testing.T) {
	yes := []string{
		"/红包 10 5",
		"/红包",
		"红包 10 5",
		"/发红包 10 5",
		`<qqbot-at-user id="X" /> /红包 10 5`,
	}
	for _, c := range yes {
		if !isRedPacketCommand(c) {
			t.Fatalf("%q 应被识别为红包指令", c)
		}
	}
	no := []string{"/签到", "/转账 @x 1", "抢红包好玩", "/余额", "/菜单"}
	for _, c := range no {
		if isRedPacketCommand(c) {
			t.Fatalf("%q 不该被识别为红包指令", c)
		}
	}
}

// TestParseRedPacketArgs 参数解析：金额、份数、祝福语
func TestParseRedPacketArgs(t *testing.T) {
	amount, count, blessing := parseRedPacketArgs("/红包 10 5")
	if amount != 10 || count != 5 {
		t.Fatalf("解析失败: amount=%v count=%v", amount, count)
	}
	if blessing != "" {
		t.Fatalf("不该有祝福语: %q", blessing)
	}

	amount, count, blessing = parseRedPacketArgs("/红包 20 8 恭喜发财")
	if amount != 20 || count != 8 || blessing != "恭喜发财" {
		t.Fatalf("带祝福语解析失败: %v %v %q", amount, count, blessing)
	}

	// 只给金额，份数留 0 由上层填默认值
	amount, count, _ = parseRedPacketArgs("/红包 15")
	if amount != 15 || count != 0 {
		t.Fatalf("只给金额时解析失败: %v %v", amount, count)
	}

	// 小数金额保留，份数取整
	amount, count, _ = parseRedPacketArgs("/红包 1.5 3.9")
	if amount != 1.5 || count != 3 {
		t.Fatalf("小数解析失败: %v %v", amount, count)
	}

	amount, count, _ = parseRedPacketArgs(`<qqbot-at-user id="X" /> /红包 10 5`)
	if amount != 10 || count != 5 {
		t.Fatalf("带标签解析失败: %v %v", amount, count)
	}

	amount, _, _ = parseRedPacketArgs("/红包")
	if amount != 0 {
		t.Fatalf("无参数时金额应为 0，实际 %v", amount)
	}

	// 祝福语过长应被截断
	long := strings.Repeat("福", 100)
	_, _, blessing = parseRedPacketArgs("/红包 10 5 " + long)
	if len([]rune(blessing)) > 40 {
		t.Fatalf("祝福语未截断，长度 %d", len([]rune(blessing)))
	}
}

// TestRedPacketKeyboardHasBackButton 红包键盘含抢红包与返回主菜单
func TestRedPacketKeyboardHasBackButton(t *testing.T) {
	kb := redPacketKeyboard(42)
	if kb == nil || kb.Content == nil {
		t.Fatal("键盘不该为空")
	}
	var foundGrab, foundBack bool
	for _, row := range kb.Content.Rows {
		for _, btn := range row.Buttons {
			if btn.Action == nil {
				continue
			}
			if btn.Action.Data == ButtonDataRedPacketGrab+"42" {
				foundGrab = true
			}
			if btn.Action.Data == MenuMain {
				foundBack = true
			}
		}
	}
	if !foundGrab {
		t.Fatal("缺少抢红包按钮，或红包 ID 未正确拼接")
	}
	if !foundBack {
		t.Fatal("缺少返回主菜单按钮")
	}
}

// TestIsMenuCommand 菜单指令识别
func TestIsMenuCommand(t *testing.T) {
	for _, c := range []string{"/菜单", "菜单", "/帮助", "帮助", "/功能",
		`<qqbot-at-user id="X" /> /菜单`} {
		if !isMenuCommand(c) {
			t.Fatalf("%q 应被识别为菜单指令", c)
		}
	}
	for _, c := range []string{"/签到", "/红包 10 5", "菜单在哪"} {
		if isMenuCommand(c) {
			t.Fatalf("%q 不该被识别为菜单指令", c)
		}
	}
}

// TestIsMenuCallback 菜单回调前缀判定
func TestIsMenuCallback(t *testing.T) {
	for _, d := range []string{MenuMain, MenuCheckin, MenuActionBalance, MenuActionDropOn} {
		if !isMenuCallback(d) {
			t.Fatalf("%q 应属于菜单回调", d)
		}
	}
	// 红包按钮与签到按钮不能被菜单系统吃掉
	for _, d := range []string{
		ButtonDataRedPacketGrab + "1",
		ButtonDataRedPacketDetail + "1",
		ButtonDataCheckin,
	} {
		if isMenuCallback(d) {
			t.Fatalf("%q 不该属于菜单回调", d)
		}
	}
}

// TestAdminCallbacksAllMatchAdminPrefix 所有管理动作都必须带 admin 前缀
//
// 鉴权是靠前缀统一拦的，漏了前缀就等于把管理功能开放给所有人。
func TestAdminCallbacksAllMatchAdminPrefix(t *testing.T) {
	adminActions := []string{
		MenuActionAdminStatus,
		MenuActionDropOn, MenuActionDropOff,
		MenuActionCheckinOn, MenuActionCheckinOff,
		MenuActionTransferOn, MenuActionTransferOff,
		MenuActionSyncPanel,
	}
	for _, d := range adminActions {
		if !isAdminMenuCallback(d) {
			t.Fatalf("管理动作 %q 未命中 admin 前缀，会绕过鉴权", d)
		}
	}

	// 普通用户动作绝不能被误判为管理动作
	userActions := []string{
		MenuMain, MenuCheckin, MenuWallet, MenuDrop,
		MenuRedPack, MenuTransfer, MenuHelp,
		MenuActionCheckin, MenuActionBalance,
		MenuActionMyStats, MenuActionDropStatus, MenuActionFeeTable,
	}
	for _, d := range userActions {
		if isAdminMenuCallback(d) {
			t.Fatalf("普通动作 %q 被误判为管理动作", d)
		}
	}
}

// TestAdminMenuOnlyShownToAdmin 主菜单的管理入口只对管理员显示
func TestAdminMenuOnlyShownToAdmin(t *testing.T) {
	hasAdminBtn := func(kb *Keyboard) bool {
		for _, row := range kb.Content.Rows {
			for _, btn := range row.Buttons {
				if btn.Action != nil && btn.Action.Data == MenuAdmin {
					return true
				}
			}
		}
		return false
	}

	if hasAdminBtn(mainMenuKeyboard(false)) {
		t.Fatal("普通用户的主菜单不该出现管理入口")
	}
	if !hasAdminBtn(mainMenuKeyboard(true)) {
		t.Fatal("管理员的主菜单应该出现管理入口")
	}
}

// TestAdminCallbackDeniedForNonAdmin 非管理员点管理动作会被拒绝
//
// isDropAdmin 走数据库，单测里查不到绑定关系，
// 因此 TEST_OPENID 必然是非管理员，正好覆盖拒绝路径。
func TestAdminCallbackDeniedForNonAdmin(t *testing.T) {
	for _, d := range []string{
		MenuAdmin,
		MenuActionDropOn,
		MenuActionCheckinOff,
		MenuActionSyncPanel,
	} {
		content, kb := HandleMenuCallback(d, "TEST_OPENID_NOT_ADMIN", "TEST_GROUP")
		if !strings.Contains(content, "权限不足") {
			t.Fatalf("回调 %q 未拦截非管理员，文案: %q", d, content)
		}
		if kb == nil {
			t.Fatalf("回调 %q 被拒绝后应返回主菜单键盘", d)
		}
		// 被拒后给的键盘不能带管理入口
		for _, row := range kb.Content.Rows {
			for _, btn := range row.Buttons {
				if btn.Action != nil && btn.Action.Data == MenuAdmin {
					t.Fatalf("回调 %q 被拒后仍返回了管理入口", d)
				}
			}
		}
	}
}

// TestAdminSwitchesNotInUserMenus 群级开关不出现在普通用户的子菜单里
//
// 这是本轮的要求：只给管理员的功能就该塞进管理菜单，
// 不要散落在掉落、签到等普通子菜单里。
func TestAdminSwitchesNotInUserMenus(t *testing.T) {
	userMenus := map[string]*Keyboard{
		"签到": checkinMenuKeyboard(),
		"钱包": walletMenuKeyboard(),
		"掉落": dropMenuKeyboard(),
		"红包": redPacketMenuKeyboard(),
		"转账": transferMenuKeyboard(),
		"帮助": helpMenuKeyboard(),
	}
	for name, kb := range userMenus {
		for _, row := range kb.Content.Rows {
			for _, btn := range row.Buttons {
				if btn.Action == nil {
					continue
				}
				if isAdminMenuCallback(btn.Action.Data) {
					t.Fatalf("%s 子菜单混入了管理动作 %q",
						name, btn.Action.Data)
				}
			}
		}
	}
}

// TestAdminMenuContainsAllSwitches 管理菜单必须齐备
func TestAdminMenuContainsAllSwitches(t *testing.T) {
	want := map[string]bool{
		MenuActionDropOn:      false,
		MenuActionDropOff:     false,
		MenuActionCheckinOn:   false,
		MenuActionCheckinOff:  false,
		MenuActionTransferOn:  false,
		MenuActionTransferOff: false,
		MenuActionAdminStatus: false,
		MenuActionSyncPanel:   false,
	}
	kb := adminMenuKeyboard()
	for _, row := range kb.Content.Rows {
		for _, btn := range row.Buttons {
			if btn.Action != nil {
				if _, ok := want[btn.Action.Data]; ok {
					want[btn.Action.Data] = true
				}
			}
		}
	}
	for data, found := range want {
		if !found {
			t.Fatalf("管理菜单缺少动作 %q", data)
		}
	}
}

// TestAllSubMenusHaveBackButton 所有子菜单都必须能回到主菜单
func TestAllSubMenusHaveBackButton(t *testing.T) {
	subMenus := map[string]*Keyboard{
		"签到": checkinMenuKeyboard(),
		"钱包": walletMenuKeyboard(),
		"掉落": dropMenuKeyboard(),
		"红包": redPacketMenuKeyboard(),
		"转账": transferMenuKeyboard(),
		"管理": adminMenuKeyboard(),
		"帮助": helpMenuKeyboard(),
	}

	for name, kb := range subMenus {
		if kb == nil || kb.Content == nil {
			t.Fatalf("%s 子菜单键盘为空", name)
		}
		found := false
		for _, row := range kb.Content.Rows {
			for _, btn := range row.Buttons {
				if btn.Action != nil && btn.Action.Data == MenuMain {
					found = true
				}
			}
		}
		if !found {
			t.Fatalf("%s 子菜单缺少返回主菜单按钮", name)
		}
	}
}

// TestMainMenuHasNoBackButton 主菜单本身不需要返回按钮
func TestMainMenuHasNoBackButton(t *testing.T) {
	for _, isAdmin := range []bool{false, true} {
		kb := mainMenuKeyboard(isAdmin)
		for _, row := range kb.Content.Rows {
			for _, btn := range row.Buttons {
				if btn.Action != nil && btn.Action.Data == MenuMain {
					t.Fatal("主菜单不该有指向自己的返回按钮")
				}
			}
		}
	}
}

// TestMenuCallbacksAllReturnKeyboard 每个菜单回调都要返回键盘
//
// 否则用户点进去就出不来了，只能重新发 /菜单。
func TestMenuCallbacksAllReturnKeyboard(t *testing.T) {
	pureCallbacks := []string{
		MenuMain,
		MenuCheckin,
		MenuWallet,
		MenuDrop,
		MenuRedPack,
		MenuTransfer,
		MenuHelp,
		MenuActionDropStatus,
		"nailao_menu:不存在的动作",
	}
	for _, data := range pureCallbacks {
		content, kb := HandleMenuCallback(data, "TEST_OPENID", "TEST_GROUP")
		if content == "" {
			t.Fatalf("回调 %q 返回了空文案", data)
		}
		if kb == nil || kb.Content == nil || len(kb.Content.Rows) == 0 {
			t.Fatalf("回调 %q 没有返回键盘，用户会卡住", data)
		}
	}
}

// TestUnknownCallbackFallsBackToMain 未知回调回落主菜单
func TestUnknownCallbackFallsBackToMain(t *testing.T) {
	content, kb := HandleMenuCallback("nailao_menu:garbage", "X", "G")
	if !strings.Contains(content, "功能菜单") {
		t.Fatalf("未知回调应回落到主菜单，实际文案: %q", content)
	}
	if kb.Content.Rows[0].Buttons[0].Action.Data != MenuCheckin {
		t.Fatal("未知回调未返回主菜单键盘")
	}
}

// TestCommandButtonDoesNotAutoSend 指令按钮不能自动发送
//
// 转账和红包都需要用户补全参数，Enter=true 会导致
// 「/转账 」这种半成品指令被直接发出去。
func TestCommandButtonDoesNotAutoSend(t *testing.T) {
	for name, kb := range map[string]*Keyboard{
		"红包": redPacketMenuKeyboard(),
		"转账": transferMenuKeyboard(),
	} {
		for _, row := range kb.Content.Rows {
			for _, btn := range row.Buttons {
				if btn.Action != nil && btn.Action.Type == 2 && btn.Action.Enter {
					t.Fatalf("%s 菜单的指令按钮 %q 设置了自动发送",
						name, btn.RenderData.Label)
				}
			}
		}
	}
}

// TestNoEmojiInMenuLabels 按钮文案不含 emoji
//
// 用户明确要求禁止 emoji。货币符号由后台配置决定，不在此校验范围。
func TestNoEmojiInMenuLabels(t *testing.T) {
	isEmoji := func(r rune) bool {
		switch {
		case r >= 0x1F000 && r <= 0x1FAFF: // 各类符号与象形文字
			return true
		case r >= 0x2600 && r <= 0x27BF: // 杂项符号、装饰符号
			return true
		case r >= 0x2B00 && r <= 0x2BFF: // 杂项符号与箭头
			return true
		case r >= 0x2190 && r <= 0x21FF: // 箭头
			return true
		case r == 0xFE0F || r == 0x20E3: // 变体选择符、组合键帽
			return true
		}
		return false
	}

	keyboards := map[string]*Keyboard{
		"主菜单(普通)": mainMenuKeyboard(false),
		"主菜单(管理)": mainMenuKeyboard(true),
		"签到":      checkinMenuKeyboard(),
		"钱包":      walletMenuKeyboard(),
		"掉落":      dropMenuKeyboard(),
		"红包":      redPacketMenuKeyboard(),
		"转账":      transferMenuKeyboard(),
		"管理":      adminMenuKeyboard(),
		"帮助":      helpMenuKeyboard(),
		"红包消息":    redPacketKeyboard(1),
	}

	for name, kb := range keyboards {
		for _, row := range kb.Content.Rows {
			for _, btn := range row.Buttons {
				if btn.RenderData == nil {
					continue
				}
				for _, label := range []string{
					btn.RenderData.Label, btn.RenderData.VisitedLabel,
				} {
					for _, r := range label {
						if isEmoji(r) {
							t.Fatalf("%s 的按钮文案 %q 含 emoji U+%04X",
								name, label, r)
						}
					}
				}
			}
		}
	}
}

// TestNoEmojiInMenuTexts 菜单正文不含 emoji
func TestNoEmojiInMenuTexts(t *testing.T) {
	isEmoji := func(r rune) bool {
		return (r >= 0x1F300 && r <= 0x1FAFF) ||
			(r >= 0x2600 && r <= 0x27BF) ||
			(r >= 0x2B00 && r <= 0x2BFF)
	}
	texts := map[string]string{
		"主菜单":    mainMenuText(),
		"帮助(普通)": helpText(false),
		"帮助(管理)": helpText(true),
		"本群状态":   groupStatusText("G"),
		"掉落状态":   dropStatusText(),
	}
	for name, text := range texts {
		for _, r := range text {
			// 货币符号来自后台配置，允许出现
			if r == '🧀' {
				continue
			}
			if isEmoji(r) {
				t.Fatalf("%s 文案含 emoji U+%04X: %q", name, r, text)
			}
		}
	}
}

// TestHelpTextHidesAdminSectionForUsers 普通用户看不到管理员指令
func TestHelpTextHidesAdminSectionForUsers(t *testing.T) {
	userText := helpText(false)
	for _, cmd := range []string{"/开启掉落", "/关闭签到", "/开启转账"} {
		if strings.Contains(userText, cmd) {
			t.Fatalf("普通用户的帮助里不该出现管理员指令 %q", cmd)
		}
	}

	adminText := helpText(true)
	for _, cmd := range []string{"/开启掉落", "/关闭签到", "/开启转账"} {
		if !strings.Contains(adminText, cmd) {
			t.Fatalf("管理员的帮助里应该出现 %q", cmd)
		}
	}
}

// TestIsRedPacketDisabledGroup 群级红包黑名单
func TestIsRedPacketDisabledGroup(t *testing.T) {
	s := GetQQBotSetting()
	orig := s.RedPacketDisabledGroups
	defer func() { s.RedPacketDisabledGroups = orig }()

	s.RedPacketDisabledGroups = ""
	if IsRedPacketDisabledGroup("G1") {
		t.Fatal("空名单时不该禁用任何群")
	}
	s.RedPacketDisabledGroups = "G1,G2"
	if !IsRedPacketDisabledGroup("G1") {
		t.Fatal("G1 应被禁用")
	}
	if IsRedPacketDisabledGroup("G3") {
		t.Fatal("G3 不该被禁用")
	}
}

// TestRedPacketCommandNotCountable 红包与菜单指令不计入掉落进度
func TestRedPacketCommandNotCountable(t *testing.T) {
	for _, c := range []string{"/红包 10 5", "/菜单", "/帮助", "/发红包 5 2"} {
		if _, ok := isMessageCountable(c); ok {
			t.Fatalf("%q 不该计入掉落进度", c)
		}
	}
}
