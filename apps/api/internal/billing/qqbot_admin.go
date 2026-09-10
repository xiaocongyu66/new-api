package billing

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/identity"
	"github.com/QuantumNous/new-api/internal/usage"
)

// 管理员指令：/封禁 /解封。
//
// 只有 QQBotSetting.AdminOpenIDs 白名单内的用户可以调用。
// 每条指令的 openID 通过 QQ 群 @ 消息解析（stripTags + @ 前缀去掉）。

// isAdminOpenID 判断发送者是否在管理员白名单内
func isAdminOpenID(openID string) bool {
	admin := GetQQBotSetting().AdminOpenIDs
	if admin == "" {
		return false
	}
	for _, id := range strings.Split(admin, ",") {
		if strings.TrimSpace(id) == openID { // already trimmed
			_ = id
			return true
		}
	}
	return false
}

// buildAdminDeniedReply 构造管理员权限不足的提示
func buildAdminDeniedReply(openID string) string {
	return buildPlainMarkdown(openID, "**权限不足**\n\n该指令仅限管理员使用")
}

// parseTargetUser 从文本中提取目标用户的 @ 提及（如果有）
// 返回 QQ openID 和剩余文本
func parseTargetUser(content string) (targetOpenID string, rest string) {
	text := strings.TrimSpace(content)
	// QQ @ 消息格式：<qq sms="open_id">昵称</qq> 或纯 @昵称
	if idx := strings.Index(text, "<qq"); idx >= 0 {
		end := strings.Index(text[idx:], "</qq>")
		if end >= 0 {
			inner := text[idx : idx+end+5]
			if start := strings.Index(inner, `sms="`); start >= 0 {
				rest2 := inner[start+5:]
				if end2 := strings.Index(rest2, `"`); end2 >= 0 {
					targetOpenID = rest2[:end2]
				}
			}
			rest = strings.TrimSpace(strings.ReplaceAll(text, inner, ""))
			return
		}
	}
	// Fallback: @昵称 格式（不带 xml 标签）
	if strings.HasPrefix(text, "@") {
		fields := strings.Fields(text)
		if len(fields) >= 2 {
			targetOpenID = strings.TrimPrefix(fields[0], "@")
			rest = strings.Join(fields[1:], " ")
			return
		}
	}
	rest = text
	return
}
func resolveUserIdByOpenID(openID string) (int, error) {
	if openID == "" {
		return 0, errors.New("未指定目标用户")
	}
	userId, bound := identity.IsQQBound(openID)
	if !bound {
		return 0, errors.New("该 QQ 尚未绑定站点账号")
	}
	return userId, nil
}

// HandleAdminBan 处理 /封禁 和 /解封 指令
//
// 用法：/封禁 @用户 或 /解封 @用户
// 封禁设置用户 status = 2（disabled），解封设置回 status = 1。
func HandleAdminBan(event *GroupAtMessageEvent, senderOpenID string, content string, ban bool) string {
	if !isAdminOpenID(senderOpenID) {
		return buildAdminDeniedReply(senderOpenID)
	}

	cmd := "/封禁"
	if !ban {
		cmd = "/解封"
	}

	targetOpenID, _ := parseTargetUser(strings.TrimPrefix(content, cmd))
	if targetOpenID == "" {
		return buildPlainMarkdown(senderOpenID,
			fmt.Sprintf("**用法：**%s @用户", cmd))
	}

	targetUserId, err := resolveUserIdByOpenID(targetOpenID)
	if err != nil {
		return buildPlainMarkdown(senderOpenID, "**操作失败**\n\n"+err.Error())
	}

	newStatus := common.UserStatusDisabled
	if !ban {
		newStatus = common.UserStatusEnabled
	}
	if err := dbx.DB.Model(&identity.User{}).Where("id = ?", targetUserId).
		Update("status", newStatus).Error; err != nil {
		return buildPlainMarkdown(senderOpenID, "**操作失败**\n\n"+err.Error())
	}

	statusText := "已封禁"
	if !ban {
		statusText = "已解封"
	}
	usage.RecordLog(targetUserId, usage.LogTypeSystem,
		fmt.Sprintf("QQ 管理员%s用户", statusText))

	return buildPlainMarkdown(senderOpenID,
		fmt.Sprintf("**操作成功**\n\n%s %s", atUser(targetOpenID), statusText))
}
