package billing

import (
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/identity"
	"gorm.io/gorm"
)

// CleanupResult 一次清理扫描的结果，供管理台展示与审计
type CleanupResult struct {
	GroupOpenID   string `json:"group_open_id"`
	Scanned       int    `json:"scanned"`        // 识别出的潜水候选
	Warned        int    `json:"warned"`         // 本次发送警告的数量
	Kicked        int    `json:"kicked"`         // 本次踢出的数量
	SkippedExempt int    `json:"skipped_exempt"` // 因豁免规则跳过
	SkippedRecent int    `json:"skipped_recent"` // 宽限期内或近期已警告
	Errors        int    `json:"errors"`         // 失败计数
	DryRun        bool   `json:"dry_run"`        // 是否演练模式
}

// parseAtUserID 从 OneBot v11 收到的消息文本里提取被 @ 的真实 QQ 号。
//
// NapCat 把 @ 渲染成 CQ 码 [CQ:at,qq=123456] 。一条警告消息里可能有多个 @，
// 但官方 bot 发出的清理警告每次只 @ 一个目标，取第一个即可。
func parseAtUserID(message string) (int64, bool) {
	idx := strings.Index(message, "[CQ:at,qq=")
	if idx < 0 {
		return 0, false
	}
	rest := message[idx+len("[CQ:at,qq="):]
	end := strings.Index(rest, "]")
	if end <= 0 {
		return 0, false
	}
	var qq int64
	if _, err := fmt.Sscanf(rest[:end], "%d", &qq); err != nil || qq <= 0 {
		return 0, false
	}
	return qq, true
}

// buildWarningContent 用官方 markdown 的 @ 语法拼出警告文案。
//
// 官方 bot 侧只知道 member_openid，用 <qqbot-at-user id="..."/> 指定目标；
// NapCat 收到这条消息时会把同一处渲染成 [CQ:at,qq=真实QQ号] ，
// 这就是两个 bot 之间传递真实身份的桥梁。
func buildWarningContent(memberOpenID string) string {
	template := GetCleanupWarningTemplate()
	text := strings.ReplaceAll(template, "{@}", fmt.Sprintf("<qqbot-at-user id=\"%s\"/>", memberOpenID))
	text = strings.ReplaceAll(text, "{天数}", fmt.Sprintf("%d", GetCleanupInactiveDays()))
	text = strings.ReplaceAll(text, "{宽限天数}", fmt.Sprintf("%d", GetCleanupGraceDays()))
	return text
}

// stripAtTags 抹掉两种 @ 表示形式，只比较正文
func stripAtTags(s string) string {
	out := s
	for {
		i := strings.Index(out, "<qqbot-at-user")
		if i < 0 {
			break
		}
		j := strings.Index(out[i:], "/>")
		if j < 0 {
			break
		}
		out = out[:i] + out[i+j+2:]
	}
	for {
		i := strings.Index(out, "[CQ:at,qq=")
		if i < 0 {
			break
		}
		j := strings.Index(out[i:], "]")
		if j < 0 {
			break
		}
		out = out[:i] + out[i+j+1:]
	}
	return strings.TrimSpace(out)
}

// isMemberExempt 判断成员是否应被豁免清理。
//
// 已绑定站点账号的用户是付费/活跃基本盘，默认豁免；身份是管理员或
// 机器人的成员没有清理价值（机器人也不是真实潜水用户）。
func isMemberExempt(member *QQGroupMember) bool {
	s := GetQQBotSetting()
	if s.CleanupExemptBoundUsers {
		if _, bound := identity.IsQQBound(member.MemberOpenID); bound {
			return true
		}
	}
	return false
}

// RunCleanupScan 执行一次清理扫描。
//
// 流程：识别潜水候选 → 对未警告的发送 @ 警告 → 对已过宽限期的执行踢人。
// 踢人由 NapCat 侧老号执行，两次踢人之间随机休眠，降低封号风险。
// dryRun=true 时只识别和警告，不真正踢人。
//
// 该函数设计为从管理台手动触发（SystemTask 或 goroutine），不在热路径调用。
func RunCleanupScan(groupOpenID string) (*CleanupResult, error) {
	result := &CleanupResult{GroupOpenID: groupOpenID, DryRun: GetQQBotSetting().CleanupDryRun}

	if !IsCleanupGroup(groupOpenID) {
		return result, fmt.Errorf("该群未开启潜水清理")
	}

	inactiveDays := GetCleanupInactiveDays()
	graceDays := GetCleanupGraceDays()
	now := time.Now().Unix()
	beforeTs := now - int64(inactiveDays)*86400

	members, err := ListInactiveMembers(groupOpenID, beforeTs, 0)
	if err != nil {
		return result, fmt.Errorf("拉取潜水成员失败: %w", err)
	}
	result.Scanned = len(members)

	warnHours := GetQQBotSetting().CleanupWarnHours
	warnWindow := int64(warnHours) * 3600
	if warnHours <= 0 {
		warnWindow = int64(24) * 3600
	}

	client, clientErr := getClient()
	if clientErr != nil {
		return result, fmt.Errorf("官方 bot 客户端不可用: %w", clientErr)
	}

	batchSize := GetQQBotSetting().CleanupBatchSize
	acted := 0 // 警告与踢人合计，批次上限同时约束两者
	groupNumber := groupOpenID2Number(groupOpenID)
	for i := range members {
		m := members[i]
		bridged := m.QQNumber > 0 && groupNumber > 0
		action := decideCleanupAction(&m, now, graceDays, warnWindow, isMemberExempt(&m), bridged)

		switch action {
		case actionSkipExempt:
			result.SkippedExempt++
			continue
		case actionSkipRecent, actionWaitBridge:
			// WaitBridge 是已过宽限期但尚未桥接真实 QQ 号，同样等下一次扫描
			result.SkippedRecent++
			continue
		}

		// 警告与踢人都消耗批次配额：警告是官方 bot 的公开广播，踢人有
		// 封号风险，两者都不能一次来一大片
		if batchSize > 0 && acted >= batchSize {
			break
		}

		if action == actionKick {
			acted++
			if result.DryRun {
				continue
			}
			napCat := newNapCatClient()
			if err := napCat.SetGroupKick(groupNumber, m.QQNumber, true); err != nil {
				common.SysError(fmt.Sprintf("踢出成员失败 qq=%d: %s", m.QQNumber, err.Error()))
				result.Errors++
				continue
			}
			if err := SetMemberRemoved(groupOpenID, m.MemberOpenID); err != nil {
				common.SysError("记录移出状态失败: " + err.Error())
			}
			result.Kicked++
			// 随机间隔，让执行账号看起来像人工操作
			minSec, maxSec := GetCleanupKickIntervalRange()
			delay := minSec
			if maxSec > minSec {
				delay = minSec + rand.Intn(maxSec-minSec+1)
			}
			time.Sleep(time.Duration(delay) * time.Second)
			continue
		}

		// actionWarn：发送 @ 警告（官方 bot 侧，合规操作）
		warning := buildWarningContent(m.MemberOpenID)
		if _, err := client.SendGroupMessage(groupOpenID, &GroupMessageRequest{
			MsgType:  2, // Markdown
			Markdown: &MessageMarkdown{Content: warning},
			MsgSeq:   int(nextMsgSeq(groupOpenID)),
		}); err != nil {
			common.SysError(fmt.Sprintf("发送清理警告失败 member=%s: %s", m.MemberOpenID, err.Error()))
			result.Errors++
			continue
		}
		if err := SetMemberWarned(groupOpenID, m.MemberOpenID, now); err != nil {
			common.SysError("记录警告状态失败: " + err.Error())
		}
		result.Warned++
		acted++

		// 尚未桥接到真实 QQ 号：警告已发，等 NapCat 收到这条消息回填后，
		// 由下一次扫描走踢人分支。桥接见 HandleBridgeEvent。
	}

	return result, nil
}

// cleanupAction 一次扫描对一个潜水成员的处理方式
type cleanupAction int

const (
	actionSkipExempt cleanupAction = iota // 命中豁免规则
	actionSkipRecent                      // 宽限期或警告窗口内，本轮不动
	actionWarn                            // 发送 @ 警告
	actionWaitBridge                      // 已过宽限期但未桥接真实 QQ 号，等下次
	actionKick                            // 已过宽限期且已桥接，执行踢人
)

// decideCleanupAction 判定一个潜水成员本轮应如何处理，这是潜水清理的
// 核心策略，直接对应验收标准：没警告过的先警告，警告后给宽限期，只有
// 宽限期已过且已桥接到真实 QQ 号才踢。
//
// 判定一律用成员档案里的 warnedAt（即扫描开始时的值）。RunCleanupScan
// 发完警告会把库里的 warned_at 改成当前时间，但那不代表宽限期已过——
// 用更新后的值会让首次被警告的成员同轮就被踢，宽限期形同虚设。
// bridged 表示该成员是否已具备执行踢人的条件（真实 QQ 号 + 群号映射）。
func decideCleanupAction(m *QQGroupMember, now int64, graceDays int, warnWindow int64, exempt, bridged bool) cleanupAction {
	if exempt {
		return actionSkipExempt
	}
	if m.WarnedAt <= 0 {
		return actionWarn
	}
	sinceWarn := now - m.WarnedAt
	if sinceWarn < int64(graceDays)*86400 {
		return actionSkipRecent
	}
	if sinceWarn < warnWindow {
		return actionSkipRecent
	}
	if !bridged {
		return actionWaitBridge
	}
	return actionKick
}

// HandleBridgeEvent 处理 NapCat 侧收到的群消息事件，完成 @ 桥接。
//
// 官方 bot 发出的警告消息，NapCat 也会收到（它在同一个群里）。官方 bot
// 用 <qqbot-at-user id="member_openid"/> 指定目标，NapCat 把同一处渲染成
// [CQ:at,qq=真实QQ号]。把两者关联，成员档案就补上了真实 QQ 号。
//
// 匹配依据是正文相同（抹掉两种 @ 标签后比较）且群里存在一条状态为 warned
// 但 QQ 号仍为 0 的记录——即我们刚发出去的那条警告。只处理能精确匹配的
// 消息，避免误桥接群里其他 @ 消息。
func HandleBridgeEvent(groupOpenID, message string) {
	if !IsCleanupGroup(groupOpenID) {
		return
	}
	qq, ok := parseAtUserID(message)
	if !ok {
		return
	}

	var candidates []QQGroupMember
	if err := dbxWhereWarnedWithoutQQ(groupOpenID).Find(&candidates).Error; err != nil || len(candidates) == 0 {
		return
	}

	for i := range candidates {
		c := &candidates[i]
		if stripAtTags(buildWarningContent(c.MemberOpenID)) != stripAtTags(message) {
			continue
		}
		if err := SetMemberQQNumber(groupOpenID, c.MemberOpenID, qq); err != nil {
			common.SysError(fmt.Sprintf("@ 桥接回填 QQ 号失败 member=%s: %s", c.MemberOpenID, err.Error()))
			return
		}
		common.SysLog(fmt.Sprintf("QQ 群清理 @ 桥接成功 member=%s qq=%d", c.MemberOpenID, qq))
		return
	}
}

// dbxWhereWarnedWithoutQQ 构造「已警告但尚未桥接 QQ 号」的查询。
func dbxWhereWarnedWithoutQQ(groupOpenID string) *gorm.DB {
	return dbx.DB.Model(&QQGroupMember{}).
		Where("group_open_id = ? AND status = ? AND (qq_number = 0 OR qq_number IS NULL)",
			groupOpenID, MemberStatusWarned).
		Order("warned_at DESC").Limit(20)
}

// groupOpenID2Number 把官方 bot 的 group_openid 换成 NapCat 用的真实群号。
//
// 两套 ID 体系无法互相推导，只能由管理员在配置清理群时把群号一并填好
// （群号在手机 QQ 的群资料页可见）。映射缺失时返回 0，调用方据此跳过
// 执行类操作——警告仍可发出，因为官方 bot 只认 group_openid。
func groupOpenID2Number(groupOpenID string) int64 {
	raw := GetQQBotSetting().CleanupGroupNumbers
	if raw == "" {
		return 0
	}
	var mapping map[string]int64
	if err := common.Unmarshal([]byte(raw), &mapping); err != nil {
		common.SysError("解析清理群号映射失败: " + err.Error())
		return 0
	}
	return mapping[groupOpenID]
}
