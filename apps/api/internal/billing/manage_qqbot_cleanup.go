package billing

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/identity"
	"github.com/QuantumNous/new-api/internal/transport/contract"
)

// cleanupMutex 保证每个群同时只有一个清理任务在跑。
// 踢人间隔 2-5 分钟，一次任务可能跑很久，并发会导致重复踢人。
type groupMutex struct {
	mu    sync.Mutex
	locks map[string]struct{}
}

var cleanupMutex = &groupMutex{locks: make(map[string]struct{})}

func (g *groupMutex) TryLock(groupOpenID string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := g.locks[groupOpenID]; ok {
		return false
	}
	g.locks[groupOpenID] = struct{}{}
	return true
}

func (g *groupMutex) Unlock(groupOpenID string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.locks, groupOpenID)
}

// NapCatWebhook 接收 NapCat 侧的群消息事件回调。
//
// 这是 @ 桥接的入口：官方 bot 发出的警告消息，NapCat 也会收到。
// NapCat 在这里把消息原文推回来，我们从中解出 [CQ:at,qq=真实QQ号]，
// 回填到成员档案里。端口必须公开但需带 token 校验，见 napcat_access_token。
//
// NapCat 的 HTTP POST 上报格式（onebot v11）外层为
// {post_type, message_type, group_id, user_id, message, raw_message, ...}
// 我们只取 message_type/group_id/raw_message 三项。
func NapCatWebhook(c contract.Context) {
	setting := GetQQBotSetting()
	if setting.NapCatOneBotAccessToken == "" {
		// 没配 token 就无法校验来源，拒绝而非信任
		_ = c.JSON(http.StatusServiceUnavailable, common.H{"message": "napcat token not configured"})
		return
	}
	auth := c.Header("Authorization")
	if auth != "Bearer "+setting.NapCatOneBotAccessToken &&
		auth != setting.NapCatOneBotAccessToken {
		_ = c.JSON(http.StatusUnauthorized, common.H{"message": "invalid token"})
		return
	}

	reader, err := c.BodyReader()
	if err != nil {
		_ = c.JSON(http.StatusBadRequest, common.H{"message": "read body failed"})
		return
	}
	defer func() { _ = reader.Close() }()
	body, err := io.ReadAll(io.LimitReader(reader, webhookMaxBodySize))
	if err != nil {
		_ = c.JSON(http.StatusBadRequest, common.H{"message": "read body failed"})
		return
	}

	var event struct {
		PostType    string `json:"post_type"`
		MessageType string `json:"message_type"`
		GroupID     int64  `json:"group_id"`
		RawMessage  string `json:"raw_message"`
	}
	if err := common.Unmarshal(body, &event); err != nil {
		_ = c.JSON(http.StatusBadRequest, common.H{"message": "invalid payload"})
		return
	}

	// 只关心群消息；NapCat 上报的心跳等其它 post_type 直接 ack
	if event.PostType == "message" && event.MessageType == "group" {
		if groupOpenID := lookupGroupOpenIDByNumber(event.GroupID); groupOpenID != "" {
			// 异步处理，避免 NapCat 上报超时导致重试
			go HandleBridgeEvent(groupOpenID, event.RawMessage)
		}
	}

	_ = c.JSON(http.StatusOK, common.H{"status": "ok"})
}

// lookupGroupOpenIDByNumber 反查群号对应的 group_openid。
//
// 配置里的映射是 group_openid -> 群号，NapCat 上报的是群号，需要反向查找。
// 只在参与清理的群里找，缩小搜索范围。
func lookupGroupOpenIDByNumber(groupID int64) string {
	if groupID == 0 {
		return ""
	}
	setting := GetQQBotSetting()
	if setting.CleanupGroupNumbers == "" {
		return ""
	}
	var mapping map[string]int64
	if err := common.Unmarshal([]byte(setting.CleanupGroupNumbers), &mapping); err != nil {
		return ""
	}
	for openID, num := range mapping {
		if num == groupID && IsCleanupGroup(openID) {
			return openID
		}
	}
	return ""
}

// GetCleanupPreview 预览某群的潜水候选名单（只读，不执行任何操作）。
func GetCleanupPreview(c contract.Context) {
	groupOpenID := strings.TrimSpace(c.Query("group_open_id"))
	if groupOpenID == "" {
		common.CtxApiErrorMsg(c, "缺少 group_open_id 参数")
		return
	}
	if !IsCleanupGroup(groupOpenID) {
		common.CtxApiErrorMsg(c, "该群未开启潜水清理")
		return
	}

	beforeTs := time.Now().Unix() - int64(GetCleanupInactiveDays())*86400
	members, err := ListInactiveMembers(groupOpenID, beforeTs, 100)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}

	// 附上豁免原因，管理台据此判断名单是否准确
	type previewItem struct {
		QQGroupMember
		Exempt       bool   `json:"exempt"`
		ExemptReason string `json:"exempt_reason"`
	}
	items := make([]previewItem, 0, len(members))
	for i := range members {
		m := members[i]
		exempt, reason := exemptReason(&m)
		items = append(items, previewItem{QQGroupMember: m, Exempt: exempt, ExemptReason: reason})
	}

	stats, _ := GetGroupMemberStats(groupOpenID, beforeTs)
	common.CtxApiSuccess(c, common.H{
		"members": items,
		"stats":   stats,
		"dry_run": GetQQBotSetting().CleanupDryRun,
	})
}

// exemptReason 返回成员的豁免状态与原因，供预览展示。
func exemptReason(m *QQGroupMember) (bool, string) {
	if GetQQBotSetting().CleanupExemptBoundUsers {
		if _, bound := identity.IsQQBound(m.MemberOpenID); bound {
			return true, "已绑定站点账号"
		}
	}
	return false, ""
}

// RunCleanup 手动触发一次清理扫描。
//
// 清理是低频高影响操作，从管理台手动触发；耗时可能很长（踢人间隔 2-5
// 分钟），同步等待会超时，因此异步执行并立即返回。
func RunCleanup(c contract.Context) {
	var req struct {
		GroupOpenID string `json:"group_open_id"`
	}
	_ = c.BindJSON(&req)
	groupOpenID := strings.TrimSpace(req.GroupOpenID)
	if groupOpenID == "" {
		common.CtxApiErrorMsg(c, "缺少 group_open_id 参数")
		return
	}
	if !IsCleanupGroup(groupOpenID) {
		common.CtxApiErrorMsg(c, "该群未开启潜水清理")
		return
	}
	if !IsNapCatConfigured() {
		common.CtxApiErrorMsg(c, "请先配置 NapCat 地址")
		return
	}

	// 单群同时只允许一个清理任务，防止重复踢人
	if !cleanupMutex.TryLock(groupOpenID) {
		common.CtxApiErrorMsg(c, "该群已有清理任务正在执行")
		return
	}

	go func() {
		defer cleanupMutex.Unlock(groupOpenID)
		result, err := RunCleanupScan(groupOpenID)
		if err != nil {
			common.SysError("QQ 群清理任务失败: " + err.Error())
			return
		}
		common.SysLog(fmt.Sprintf("QQ 群清理完成 group=%s 扫描=%d 警告=%d 踢出=%d 豁免=%d 错误=%d 演练=%v",
			result.GroupOpenID, result.Scanned, result.Warned, result.Kicked,
			result.SkippedExempt, result.Errors, result.DryRun))
	}()

	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"message": "清理任务已异步启动，请查看日志确认进度",
	})
}
