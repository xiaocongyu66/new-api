package billing

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/internal/common"
)

// 命令冷却：同一用户在 CommandCooldownSeconds 内重复发指令会被拒绝。
// 使用内存 map 做简单实现；进程重启清空，不影响正确性。

type cooldownEntry struct {
	lastSeen time.Time
}

var (
	cooldownMu      sync.Mutex
	cooldownTracker = make(map[string]cooldownEntry)
)

// CheckCooldown 检查该用户是否在 scope 指令的冷却期内。
// scope 是指令作用域（消息取首个命令词，按钮取 button_data 首段），
// 冷却只拦同一指令的连点/重推，不拦该用户的其他指令——
// 防重复领取的正确性由各自的 DB 约束（唯一索引、每日限额）兜底。
// 返回 nil 表示通过（并更新 lastSeen）；返回 error 表示仍在冷却中。
func CheckCooldown(openID, scope string) error {
	if scope == "" {
		return nil // 无作用域不冷却
	}
	seconds := GetQQBotSetting().CommandCooldownSeconds
	if seconds <= 0 {
		return nil // 冷却关闭
	}

	now := time.Now()
	key := openID + "|" + scope
	cooldownMu.Lock()
	defer cooldownMu.Unlock()

	// 定期清理过期条目（简单策略：每 1000 条清理一次）
	if len(cooldownTracker) > 1000 {
		for k, v := range cooldownTracker {
			if now.Sub(v.lastSeen) > time.Duration(seconds)*time.Second {
				delete(cooldownTracker, k)
			}
		}
	}

	if entry, ok := cooldownTracker[key]; ok {
		remaining := seconds - int(now.Sub(entry.lastSeen).Seconds())
		if remaining > 0 {
			return fmt.Errorf("指令冷却中，请 %d 秒后再试", remaining)
		}
	}
	cooldownTracker[key] = cooldownEntry{lastSeen: now}
	return nil
}

// commandScope 从消息内容或按钮数据提取冷却作用域：
// 取首个非 @ 提及的词，并在 ':' 处截断（去掉红包 id 等实例参数），
// 使「同一种指令的连点」共享冷却，「不同指令」互不影响。
func commandScope(content string) string {
	for _, f := range strings.Fields(content) {
		if strings.HasPrefix(f, "@") {
			continue
		}
		if i := strings.IndexByte(f, ':'); i >= 0 {
			f = f[:i]
		}
		return f
	}
	return ""
}

// isCommandMessage 判断是否为指令消息。冷却只针对指令——普通聊天和
// 聊天触发掉落不属于指令，不应被冷却闸拦截（掉落有自己的每日限额）。
// 复用各分发分支的匹配器本身，避免别名清单双份维护漂移。
func isCommandMessage(content string) bool {
	if _, ok := isDropCommand(content); ok {
		return true
	}
	if _, ok := isCheckinSwitchCommand(content); ok {
		return true
	}
	if _, ok := isTransferSwitchCommand(content); ok {
		return true
	}
	return isTransferInfoCommand(content) || isTransferCommand(content) ||
		isBalanceCommand(content) || isMenuCommand(content) ||
		isRedPacketCommand(content) || isStealCommand(content) ||
		isCheckinCommand(content)
}

// isFailureReply 判断回复文案是否为失败提示。
// 自动撤回只针对失败类回复（签到失败、抢红包失败、没抢到等），
// 成功、菜单、查询类文案一律不撤。
func isFailureReply(content string) bool {
	return strings.Contains(content, "失败") ||
		strings.Contains(content, "没抢到") ||
		strings.Contains(content, "红包无效") ||
		strings.Contains(content, "红包不存在")
}

// AutoRecallFailed 检查是否应自动撤回失败回复。
// 依赖 RecallFailedMessages 开关；延迟由 RecallDelaySeconds 控制。
// 这里只返回是否启用；实际的 QQ 平台 delete_msg 调用由 qqbot_client 处理。
func AutoRecallFailed() (bool, int) {
	s := GetQQBotSetting()
	if !s.RecallFailedMessages {
		return false, 0
	}
	delay := s.RecallDelaySeconds
	if delay <= 0 {
		delay = 10
	}
	common.SysLog(fmt.Sprintf("auto-recall enabled: delay=%ds", delay))
	return true, delay
}
