package billing

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/internal/identity"
)

// 菜单系统
//
// 设计原则：
//  1. 能用按钮完成的操作一律用回调按钮（Action.Type=1），点一下直接出结果。
//     只有必须输入参数的操作（转账要填对象和金额、红包要填金额和份数）
//     才用指令按钮（Action.Type=2），点击后在输入框预填模板由用户补全。
//  2. 每个子菜单都带「返回主菜单」按钮。
//  3. 管理员专属能力全部收在「管理」子菜单里，普通用户的主菜单上
//     根本不出现「管理」入口，避免点进去只收到一句「权限不足」。
//  4. 文案不使用 emoji。
const (
	CmdMenu = "/菜单"

	// 主菜单与各子菜单的回调 data
	MenuMain     = "nailao_menu:main"
	MenuCheckin  = "nailao_menu:checkin"
	MenuWallet   = "nailao_menu:wallet"
	MenuDrop     = "nailao_menu:drop"
	MenuRedPack  = "nailao_menu:redpacket"
	MenuTransfer = "nailao_menu:transfer"
	MenuAdmin    = "nailao_menu:admin"
	MenuHelp     = "nailao_menu:help"

	// 普通用户可用的动作
	MenuActionCheckin    = "nailao_menu:do_checkin"
	MenuActionBalance    = "nailao_menu:do_balance"
	MenuActionMyStats    = "nailao_menu:do_my_stats"
	MenuActionDropStatus = "nailao_menu:do_drop_status"
	MenuActionFeeTable   = "nailao_menu:do_fee_table"

	// 管理员专属动作
	MenuActionAdminStatus = "nailao_menu:admin_status"
	MenuActionDropOn      = "nailao_menu:admin_drop_on"
	MenuActionDropOff     = "nailao_menu:admin_drop_off"
	MenuActionCheckinOn   = "nailao_menu:admin_checkin_on"
	MenuActionCheckinOff  = "nailao_menu:admin_checkin_off"
	MenuActionTransferOn  = "nailao_menu:admin_transfer_on"
	MenuActionTransferOff = "nailao_menu:admin_transfer_off"
	MenuActionSyncPanel   = "nailao_menu:admin_sync_panel"

	// menuButtonID 菜单类按钮统一用这个 ID，具体动作看 data
	menuButtonID = "nailao_menu_btn"

	// menuPrefix 菜单回调的公共前缀，用于快速判定
	menuPrefix = "nailao_menu:"

	// adminActionPrefix 管理员专属回调的前缀。
	// 统一前缀让权限校验只需一次判断，新增管理动作不会漏掉鉴权。
	adminActionPrefix = "nailao_menu:admin"
)

// isMenuCommand 判断是否为菜单指令
func isMenuCommand(content string) bool {
	text := strings.TrimSpace(stripTags(content))
	switch text {
	case CmdMenu, "菜单", "/帮助", "帮助", "/功能":
		return true
	}
	return false
}

// isMenuCallback 判断按钮 data 是否属于菜单系统
func isMenuCallback(data string) bool {
	return strings.HasPrefix(data, menuPrefix)
}

// isAdminMenuCallback 判断该回调是否为管理员专属
func isAdminMenuCallback(data string) bool {
	return strings.HasPrefix(data, adminActionPrefix)
}

// callbackBtn 构造一个回调按钮（点击直接执行，无需用户发消息）
func callbackBtn(label, data string, style int) Button {
	return Button{
		ID: menuButtonID,
		RenderData: &RenderData{
			Label: label,
			Style: style,
		},
		Action: &Action{
			Type:          1, // 回调
			Permission:    &Permission{Type: 2},
			Data:          data,
			UnsupportTips: "请升级 QQ 版本后使用",
		},
	}
}

// commandBtn 构造一个指令按钮（在输入框预填文本，由用户补全参数后发送）
func commandBtn(label, command string) Button {
	return Button{
		ID: menuButtonID,
		RenderData: &RenderData{
			Label: label,
			Style: 0,
		},
		Action: &Action{
			Type:          2, // 指令
			Permission:    &Permission{Type: 2},
			Data:          command,
			UnsupportTips: "请升级 QQ 版本后使用",
			Reply:         true,
			Enter:         false, // 不自动发送，留给用户补参数
		},
	}
}

// menuBackRow 「返回主菜单」按钮行，所有子菜单都要挂上
func menuBackRow() Row {
	return Row{
		Buttons: []Button{
			callbackBtn("返回主菜单", MenuMain, 0),
		},
	}
}

// mainMenuKeyboard 主菜单键盘
//
// isAdmin 为 true 时额外显示「管理」入口。普通用户看不到该按钮，
// 而不是点进去再被拒绝。
func mainMenuKeyboard(isAdmin bool) *Keyboard {
	rows := []Row{
		{Buttons: []Button{
			callbackBtn("每日签到", MenuCheckin, 1),
			callbackBtn("我的钱包", MenuWallet, 1),
		}},
		{Buttons: []Button{
			callbackBtn("奶酪掉落", MenuDrop, 0),
			callbackBtn("群红包", MenuRedPack, 0),
		}},
		{Buttons: []Button{
			callbackBtn("转账", MenuTransfer, 0),
			callbackBtn("帮助", MenuHelp, 0),
		}},
	}
	if isAdmin {
		rows = append(rows, Row{Buttons: []Button{
			callbackBtn("管理", MenuAdmin, 2),
		}})
	}
	return &Keyboard{Content: &KeyboardContent{Rows: rows}}
}

// mainMenuText 主菜单文案
func mainMenuText() string {
	return "**奶酪公益站 · 功能菜单**\n\n" +
		"点下面的按钮选择功能\n\n" +
		"随时发送 /菜单 可以重新打开"
}

// checkinMenuKeyboard 签到子菜单
func checkinMenuKeyboard() *Keyboard {
	return &Keyboard{
		Content: &KeyboardContent{
			Rows: []Row{
				{Buttons: []Button{
					callbackBtn("立即签到", MenuActionCheckin, 1),
				}},
				{Buttons: []Button{
					callbackBtn("查看余额", MenuActionBalance, 0),
				}},
				menuBackRow(),
			},
		},
	}
}

// walletMenuKeyboard 钱包子菜单
func walletMenuKeyboard() *Keyboard {
	return &Keyboard{
		Content: &KeyboardContent{
			Rows: []Row{
				{Buttons: []Button{
					callbackBtn("查看余额", MenuActionBalance, 1),
				}},
				{Buttons: []Button{
					callbackBtn("我的战绩", MenuActionMyStats, 0),
				}},
				menuBackRow(),
			},
		},
	}
}

// dropMenuKeyboard 掉落子菜单
//
// 掉落开关属于管理能力，这里只保留只读的状态查询。
func dropMenuKeyboard() *Keyboard {
	return &Keyboard{
		Content: &KeyboardContent{
			Rows: []Row{
				{Buttons: []Button{
					callbackBtn("掉落状态", MenuActionDropStatus, 1),
				}},
				menuBackRow(),
			},
		},
	}
}

// redPacketMenuKeyboard 红包子菜单
//
// 发红包必须输入金额和份数，所以用指令按钮预填模板。
func redPacketMenuKeyboard() *Keyboard {
	return &Keyboard{
		Content: &KeyboardContent{
			Rows: []Row{
				{Buttons: []Button{
					commandBtn("发红包", "/红包 10 5"),
				}},
				{Buttons: []Button{
					callbackBtn("查看余额", MenuActionBalance, 0),
				}},
				menuBackRow(),
			},
		},
	}
}

// transferMenuKeyboard 转账子菜单
func transferMenuKeyboard() *Keyboard {
	return &Keyboard{
		Content: &KeyboardContent{
			Rows: []Row{
				{Buttons: []Button{
					commandBtn("发起转账", "/转账 "),
				}},
				{Buttons: []Button{
					callbackBtn("手续费费率", MenuActionFeeTable, 0),
					callbackBtn("查看余额", MenuActionBalance, 0),
				}},
				menuBackRow(),
			},
		},
	}
}

// adminMenuKeyboard 管理员子菜单
//
// 所有需要管理员权限的能力都集中在这里：三个功能的群级开关、
// 本群状态总览、指令面板同步。
func adminMenuKeyboard() *Keyboard {
	return &Keyboard{
		Content: &KeyboardContent{
			Rows: []Row{
				{Buttons: []Button{
					callbackBtn("掉落 开", MenuActionDropOn, 1),
					callbackBtn("掉落 关", MenuActionDropOff, 2),
				}},
				{Buttons: []Button{
					callbackBtn("签到 开", MenuActionCheckinOn, 1),
					callbackBtn("签到 关", MenuActionCheckinOff, 2),
				}},
				{Buttons: []Button{
					callbackBtn("转账 开", MenuActionTransferOn, 1),
					callbackBtn("转账 关", MenuActionTransferOff, 2),
				}},
				{Buttons: []Button{
					callbackBtn("本群状态", MenuActionAdminStatus, 0),
					callbackBtn("同步指令面板", MenuActionSyncPanel, 0),
				}},
				menuBackRow(),
			},
		},
	}
}

// helpMenuKeyboard 帮助子菜单
func helpMenuKeyboard() *Keyboard {
	return &Keyboard{
		Content: &KeyboardContent{
			Rows: []Row{
				menuBackRow(),
			},
		},
	}
}

// helpText 帮助文案：列出所有可用指令
//
// isAdmin 为 true 时追加管理员指令，普通用户看不到这些内容。
func helpText(isAdmin bool) string {
	var sb strings.Builder
	sb.WriteString("**指令一览**\n\n")
	sb.WriteString("/菜单 — 打开功能菜单\n\n")
	sb.WriteString("/签到 — 领取每日额度\n\n")
	sb.WriteString("/余额 — 查看余额与剩余次数\n\n")
	sb.WriteString("/转账 @某人 金额 — 转账给他人\n\n")
	sb.WriteString("/转账费率 — 查看手续费档位\n\n")
	sb.WriteString("/红包 金额 份数 — 发群红包\n\n")
	sb.WriteString("水群会随机掉落奶酪，绑定账号后自动入账")

	if isAdmin {
		sb.WriteString("\n\n**管理员指令**\n\n")
		sb.WriteString("/开启掉落 /关闭掉落 /掉落状态\n\n")
		sb.WriteString("/开启签到 /关闭签到 /签到状态\n\n")
		sb.WriteString("/开启转账 /关闭转账\n\n")
		sb.WriteString("以上开关只影响当前群")
	}
	return sb.String()
}

// myStatsText 个人战绩：余额、掉落、转账、红包汇总
func myStatsText(openID string) string {
	userId, bound := identity.IsQQBound(openID)
	if !bound {
		return buildPlainMarkdown(openID,
			"**查询失败！**\n\n请先绑定站点账号")
	}

	symbol := currencySymbolOrEmpty()
	var sb strings.Builder
	sb.WriteString(atUser(openID))
	sb.WriteString(" **我的战绩**\n\n")

	if balance, err := identity.GetUserQuota(userId, true); err == nil {
		sb.WriteString(fmt.Sprintf("当前余额 %s%s\n\n",
			trimFloat(quotaToUnits(balance)), symbol))
	}

	if drops, err := GetUserQQDropRecords(userId,
		"1970-01-01", "9999-12-31"); err == nil {
		var total int
		for _, d := range drops {
			total += d.QuotaAwarded
		}
		sb.WriteString(fmt.Sprintf("捡到掉落 %d 次，共 %s%s\n\n",
			len(drops), trimFloat(quotaToUnits(total)), symbol))
	}

	sentCount, sentAmount, grabCount, grabAmount := GetUserQQRedPacketStats(userId)
	sb.WriteString(fmt.Sprintf("发出红包 %d 个，共 %s%s\n\n",
		sentCount, trimFloat(quotaToUnits(int(sentAmount))), symbol))
	sb.WriteString(fmt.Sprintf("抢到红包 %d 次，共 %s%s\n\n",
		grabCount, trimFloat(quotaToUnits(int(grabAmount))), symbol))

	if transfers, err := GetUserQQTransfers(userId, 200); err == nil {
		var out, in int
		for _, t := range transfers {
			if t.FromUserId == userId {
				out += t.Amount
			} else {
				in += t.Received
			}
		}
		sb.WriteString(fmt.Sprintf("转出 %s%s，转入 %s%s",
			trimFloat(quotaToUnits(out)), symbol,
			trimFloat(quotaToUnits(in)), symbol))
	}
	return sb.String()
}

// groupStatusText 本群各功能的开关状态
func groupStatusText(groupOpenID string) string {
	s := GetQQBotSetting()

	onOff := func(b bool) string {
		if b {
			return "开"
		}
		return "关"
	}

	var sb strings.Builder
	sb.WriteString("**本群功能状态**\n\n")
	sb.WriteString(fmt.Sprintf("签到：%s\n\n",
		onOff(s.QQCheckinEnabled && !IsCheckinDisabledGroup(groupOpenID))))
	sb.WriteString(fmt.Sprintf("掉落：%s\n\n",
		onOff(s.DropEnabled && IsDropGroup(groupOpenID))))
	sb.WriteString(fmt.Sprintf("转账：%s\n\n",
		onOff(s.TransferEnabled && !IsTransferDisabledGroup(groupOpenID))))
	sb.WriteString(fmt.Sprintf("红包：%s",
		onOff(s.RedPacketEnabled && !IsRedPacketDisabledGroup(groupOpenID))))
	return sb.String()
}

// HandleMenuCallback 处理菜单类按钮回调
//
// 返回要回复的文案与键盘。管理员专属回调在这里统一鉴权，
// 单点拦截比每个分支各写一遍更难漏。
func HandleMenuCallback(data, openID, groupOpenID string) (content string, keyboard *Keyboard) {
	isAdmin := isDropAdmin(openID)

	// 管理员专属回调统一鉴权
	if isAdminMenuCallback(data) && !isAdmin {
		return buildPlainMarkdown(openID,
			"**权限不足！**\n\n该功能仅站点管理员可用"), mainMenuKeyboard(false)
	}

	switch data {
	case MenuMain:
		return mainMenuText(), mainMenuKeyboard(isAdmin)

	case MenuCheckin:
		return "**每日签到**\n\n每天签到可领取随机额度", checkinMenuKeyboard()

	case MenuWallet:
		return "**我的钱包**\n\n查看余额与各项战绩", walletMenuKeyboard()

	case MenuDrop:
		return "**奶酪掉落**\n\n" + dropStatusText(), dropMenuKeyboard()

	case MenuRedPack:
		s := GetQQBotSetting()
		text := fmt.Sprintf("**群红包**\n\n用法：/红包 金额 份数\n\n"+
			"例如 /红包 10 5 表示 10%s 分给 5 个人\n\n每人每天可发 %d 个",
			currencySymbolOrEmpty(), s.RedPacketDailyLimit)
		return text, redPacketMenuKeyboard()

	case MenuTransfer:
		return "**转账**\n\n用法：/转账 @某人 金额\n\n" +
			"手续费按金额累进，收发双方每天各限次数", transferMenuKeyboard()

	case MenuAdmin:
		return "**管理面板**\n\n" + groupStatusText(groupOpenID), adminMenuKeyboard()

	case MenuHelp:
		return helpText(isAdmin), helpMenuKeyboard()

	// ==== 普通用户动作 ====
	case MenuActionCheckin:
		if IsCheckinDisabledGroup(groupOpenID) {
			return checkinDisabledReply(openID), checkinMenuKeyboard()
		}
		reply, _ := doCheckinForOpenID(openID, groupOpenID)
		return reply, checkinMenuKeyboard()

	case MenuActionBalance:
		return HandleMyBalance(openID), walletMenuKeyboard()

	case MenuActionMyStats:
		return myStatsText(openID), walletMenuKeyboard()

	case MenuActionDropStatus:
		state := "未开启"
		if IsDropGroup(groupOpenID) {
			state = "已开启"
		}
		return fmt.Sprintf("**本群掉落：%s**\n\n%s", state, dropStatusText()),
			dropMenuKeyboard()

	case MenuActionFeeTable:
		return HandleTransferInfo(openID), transferMenuKeyboard()

	// ==== 管理员动作（已在上方统一鉴权）====
	case MenuActionAdminStatus:
		return groupStatusText(groupOpenID), adminMenuKeyboard()

	case MenuActionDropOn:
		return menuAdminAction(openID, groupOpenID, CmdEnableDrop)
	case MenuActionDropOff:
		return menuAdminAction(openID, groupOpenID, CmdDisableDrop)
	case MenuActionCheckinOn:
		return menuAdminAction(openID, groupOpenID, CmdEnableCheckin)
	case MenuActionCheckinOff:
		return menuAdminAction(openID, groupOpenID, CmdDisableCheckin)
	case MenuActionTransferOn:
		return menuAdminAction(openID, groupOpenID, CmdEnableTransfer)
	case MenuActionTransferOff:
		return menuAdminAction(openID, groupOpenID, CmdDisableTransfer)

	case MenuActionSyncPanel:
		if err := SyncCommandPanel(); err != nil {
			return buildPlainMarkdown(openID,
				"**同步失败！**\n\n"+err.Error()), adminMenuKeyboard()
		}
		return buildPlainMarkdown(openID,
			"**指令面板已同步到 QQ 平台**"), adminMenuKeyboard()

	default:
		// 未知回调回落到主菜单，不让用户卡在死路上
		return mainMenuText(), mainMenuKeyboard(isAdmin)
	}
}

// menuAdminAction 执行管理员开关动作并回到管理面板
func menuAdminAction(openID, groupOpenID, cmd string) (string, *Keyboard) {
	var reply string
	switch cmd {
	case CmdEnableDrop, CmdDisableDrop:
		reply = HandleDropCommand(cmd, openID, groupOpenID)
	case CmdEnableCheckin, CmdDisableCheckin:
		reply = HandleCheckinSwitchCommand(cmd, openID, groupOpenID)
	case CmdEnableTransfer, CmdDisableTransfer:
		reply = HandleTransferSwitchCommand(cmd, openID, groupOpenID)
	}
	// 操作结果后面附上最新状态，省去用户再点一次「本群状态」
	return reply + "\n\n" + groupStatusText(groupOpenID), adminMenuKeyboard()
}

// HandleMenuCommand 处理 /菜单 指令
func HandleMenuCommand(openID string) (string, *Keyboard) {
	return mainMenuText(), mainMenuKeyboard(isDropAdmin(openID))
}
