package billing

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/dbinfra"
)

// 群级签到开关指令。
//
// 采用黑名单语义：默认所有群都能签到，管理员在某个群发送 /关闭签到
// 只会禁用该群，全局的 QQCheckinEnabled 开关与网页签到都不受影响。
// 反过来用白名单会导致新入驻的群默认不能签到，与现状不兼容。
const (
	CmdDisableCheckin = "/关闭签到"
	CmdEnableCheckin  = "/开启签到"
	CmdCheckinStatus  = "/签到状态"

	// CheckinDisabledGroupsOptionKey 黑名单在 options 表中的键
	CheckinDisabledGroupsOptionKey = "qq_bot_setting.checkin_disabled_groups"
)

// parseGroupIDs 解析逗号/空白分隔的 group_openid 列表
// 兼容中文逗号与换行，便于后台手工粘贴多个群
func parseGroupIDs(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r' || r == ' ' || r == '\t' || r == '，'
	})
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// containsGroup 判断列表中是否含指定群
func containsGroup(list []string, groupOpenID string) bool {
	if groupOpenID == "" {
		return false
	}
	for _, g := range list {
		if g == groupOpenID {
			return true
		}
	}
	return false
}

// checkinDisabledGroupList 解析签到黑名单
func checkinDisabledGroupList() []string {
	return parseGroupIDs(GetQQBotSetting().CheckinDisabledGroups)
}

// IsCheckinDisabledGroup 判断该群是否已关闭签到
// groupOpenID 为空（网页签到等非群场景）时恒为 false
func IsCheckinDisabledGroup(groupOpenID string) bool {
	return containsGroup(checkinDisabledGroupList(), groupOpenID)
}

// setCheckinDisabledGroups 持久化黑名单
// 写 options 表后由 updateOptionMap → config 系统回填内存，无需重启
func setCheckinDisabledGroups(groups []string) error {
	return dbinfra.UpdateOption(CheckinDisabledGroupsOptionKey, strings.Join(groups, ","))
}

// disableCheckinForGroup 把群加入黑名单，返回是否发生变更
func disableCheckinForGroup(groupOpenID string) (bool, error) {
	if IsCheckinDisabledGroup(groupOpenID) {
		return false, nil
	}
	groups := append(checkinDisabledGroupList(), groupOpenID)
	if err := setCheckinDisabledGroups(groups); err != nil {
		return false, err
	}
	return true, nil
}

// enableCheckinForGroup 把群移出黑名单，返回是否发生变更
func enableCheckinForGroup(groupOpenID string) (bool, error) {
	current := checkinDisabledGroupList()
	out := make([]string, 0, len(current))
	for _, g := range current {
		if g != groupOpenID {
			out = append(out, g)
		}
	}
	if len(out) == len(current) {
		return false, nil
	}
	if err := setCheckinDisabledGroups(out); err != nil {
		return false, err
	}
	return true, nil
}

// isCheckinSwitchCommand 判断是否为群级签到开关指令
//
// 这些指令都是完整字符串精确匹配，与 isCheckinCommand 识别的
// "/签到" / "签到" 不会互相误判。
func isCheckinSwitchCommand(content string) (cmd string, ok bool) {
	text := strings.TrimSpace(stripTags(content))
	switch text {
	case CmdDisableCheckin, CmdEnableCheckin, CmdCheckinStatus:
		return text, true
	}
	return "", false
}

// HandleCheckinSwitchCommand 处理群级签到开关指令，返回要回复的 markdown
func HandleCheckinSwitchCommand(cmd, openID, groupOpenID string) string {
	// /签到状态 属于只读查询，允许任何人执行；增删名单必须是管理员
	if cmd != CmdCheckinStatus && !isDropAdmin(openID) {
		return buildPlainMarkdown(openID, "**权限不足！**\n\n只有站点管理员可以管理本群签到开关")
	}

	switch cmd {
	case CmdDisableCheckin:
		changed, err := disableCheckinForGroup(groupOpenID)
		if err != nil {
			return buildPlainMarkdown(openID, "**操作失败！**\n\n"+err.Error())
		}
		if !changed {
			return buildPlainMarkdown(openID, "**本群签到早已关闭**\n\n"+checkinStatusText(groupOpenID))
		}
		common.SysLog("QQ 签到已在群关闭 group_openid=" + groupOpenID)
		return buildPlainMarkdown(openID,
			"**已关闭本群签到！**\n\n仅本群生效，其他群与网页签到不受影响")

	case CmdEnableCheckin:
		changed, err := enableCheckinForGroup(groupOpenID)
		if err != nil {
			return buildPlainMarkdown(openID, "**操作失败！**\n\n"+err.Error())
		}
		if !changed {
			return buildPlainMarkdown(openID, "**本群签到本来就是开启的**\n\n"+checkinStatusText(groupOpenID))
		}
		common.SysLog("QQ 签到已在群恢复 group_openid=" + groupOpenID)
		return buildPlainMarkdown(openID, "**已恢复本群签到！**")

	default: // CmdCheckinStatus
		return buildPlainMarkdown(openID, "**本群签到状态**\n\n"+checkinStatusText(groupOpenID))
	}
}

// checkinStatusText 汇总本群签到的生效情况
func checkinStatusText(groupOpenID string) string {
	s := GetQQBotSetting()

	global := "开"
	if !s.QQCheckinEnabled {
		global = "关"
	}
	local := "开"
	if IsCheckinDisabledGroup(groupOpenID) {
		local = "关"
	}
	effective := "可以签到"
	if !s.QQCheckinEnabled || IsCheckinDisabledGroup(groupOpenID) {
		effective = "不可签到"
	}
	return fmt.Sprintf(
		"全局 QQ 签到：%s\n\n本群签到：%s\n\n当前效果：%s",
		global, local, effective,
	)
}

// checkinDisabledReply 本群签到被关闭时的回复文案
func checkinDisabledReply(openID string) string {
	return buildPlainMarkdown(openID,
		"**本群已关闭签到**\n\n请前往 https://nailao.biz 个人资料→每日签到 领取，或到其他群签到")
}
