package billing

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/dbinfra"
	"github.com/QuantumNous/new-api/internal/identity"
)

// 群级转账开关指令，语义与签到开关一致：黑名单，默认所有群可用。
const (
	CmdDisableTransfer = "/关闭转账"
	CmdEnableTransfer  = "/开启转账"

	// TransferDisabledGroupsOptionKey 黑名单在 options 表中的键
	TransferDisabledGroupsOptionKey = "qq_bot_setting.transfer_disabled_groups"
)

// transferDisabledGroupList 解析转账黑名单
func transferDisabledGroupList() []string {
	return parseGroupIDs(GetQQBotSetting().TransferDisabledGroups)
}

// IsTransferDisabledGroup 判断该群是否已关闭转账
func IsTransferDisabledGroup(groupOpenID string) bool {
	return containsGroup(transferDisabledGroupList(), groupOpenID)
}

// setTransferDisabledGroups 持久化黑名单
func setTransferDisabledGroups(groups []string) error {
	return dbinfra.UpdateOption(TransferDisabledGroupsOptionKey, strings.Join(groups, ","))
}

// isTransferSwitchCommand 判断是否为群级转账开关指令
func isTransferSwitchCommand(content string) (cmd string, ok bool) {
	text := strings.TrimSpace(stripTags(content))
	switch text {
	case CmdDisableTransfer, CmdEnableTransfer:
		return text, true
	}
	return "", false
}

// HandleTransferSwitchCommand 处理群级转账开关指令
func HandleTransferSwitchCommand(cmd, openID, groupOpenID string) string {
	if !isDropAdmin(openID) {
		return buildPlainMarkdown(openID, "**权限不足！**\n\n只有站点管理员可以管理本群转账开关")
	}

	current := transferDisabledGroupList()

	switch cmd {
	case CmdDisableTransfer:
		if containsGroup(current, groupOpenID) {
			return buildPlainMarkdown(openID, "**本群转账早已关闭**")
		}
		if err := setTransferDisabledGroups(append(current, groupOpenID)); err != nil {
			return buildPlainMarkdown(openID, "**操作失败！**\n\n"+err.Error())
		}
		common.SysLog("QQ 转账已在群关闭 group_openid=" + groupOpenID)
		return buildPlainMarkdown(openID, "**已关闭本群转账！**\n\n仅本群生效")

	default: // CmdEnableTransfer
		out := make([]string, 0, len(current))
		for _, g := range current {
			if g != groupOpenID {
				out = append(out, g)
			}
		}
		if len(out) == len(current) {
			return buildPlainMarkdown(openID, "**本群转账本来就是开启的**")
		}
		if err := setTransferDisabledGroups(out); err != nil {
			return buildPlainMarkdown(openID, "**操作失败！**\n\n"+err.Error())
		}
		common.SysLog("QQ 转账已在群恢复 group_openid=" + groupOpenID)
		return buildPlainMarkdown(openID, "**已恢复本群转账！**")
	}
}

// HandleMyBalance 处理 /余额 查询，顺带展示今日剩余转账次数
func HandleMyBalance(openID string) string {
	userId, bound := identity.IsQQBound(openID)
	if !bound {
		return buildPlainMarkdown(openID,
			"**查询失败！**\n\n请先绑定站点账号：登陆后在 个人资料→每日签到→QQ签到 获取验证码")
	}
	balance, err := identity.GetUserQuota(userId, true)
	if err != nil {
		common.SysError("查询余额失败: " + err.Error())
		return buildPlainMarkdown(openID, "**查询失败！**\n\n系统繁忙，请稍后重试")
	}

	s := GetQQBotSetting()
	symbol := currencySymbolOrEmpty()

	var sb strings.Builder
	sb.WriteString(atUser(openID))
	sb.WriteString(" **账户信息**\n\n")
	sb.WriteString(fmt.Sprintf("余额 %s%s\n\n", trimFloat(quotaToUnits(balance)), symbol))

	if used, cErr := CountQQTransfersToday(userId); cErr == nil && s.TransferDailyLimit > 0 {
		left := s.TransferDailyLimit - used
		if left < 0 {
			left = 0
		}
		sb.WriteString(fmt.Sprintf("今日剩余转账次数 %d/%d\n\n", left, s.TransferDailyLimit))
	}
	if used, cErr := CountQQDropsToday(userId); cErr == nil && s.DropDailyLimit > 0 {
		left := s.DropDailyLimit - used
		if left < 0 {
			left = 0
		}
		sb.WriteString(fmt.Sprintf("今日剩余掉落次数 %d/%d", left, s.DropDailyLimit))
	}
	return sb.String()
}

// isBalanceCommand 判断是否为余额查询指令
func isBalanceCommand(content string) bool {
	text := strings.TrimSpace(stripTags(content))
	return text == "/余额" || text == "余额" || text == "/我的余额"
}
