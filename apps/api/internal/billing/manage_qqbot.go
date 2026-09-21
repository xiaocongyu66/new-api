package billing

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"github.com/QuantumNous/new-api/internal/common"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/internal/identity"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/internal/usage"
)

// QQ open-platform opcodes.
const (
	opCodeDispatch   = 0  // server-pushed event
	opCodeHTTPAck    = 12 // webhook ack
	opCodeValidation = 13 // callback URL validation
)

// webhookMaxBodySize caps the webhook body so an oversized payload cannot
// exhaust memory on a publicly reachable endpoint.
const webhookMaxBodySize = 1 << 20 // 1 MiB

const eventDedupeTTL = 24 * time.Hour       // 24 hours TTL for id-based event deduplication
const eventFingerprintTTL = 2 * time.Second // content fingerprints only need to cover the
// platform's immediate redelivery window (observed 0-11s, mostly <4s); the user
// explicitly wants minimal restriction — a repeat command after the window is a
// NEW command and must be answered normally.

type webhookPayload struct {
	ID string          `json:"id"`
	Op int             `json:"op"`
	D  json.RawMessage `json:"d"`
	T  string          `json:"t"`
	S  int64           `json:"s"`
}

type validationRequest struct {
	PlainToken string `json:"plain_token"`
	EventTs    string `json:"event_ts"`
}

type validationResponse struct {
	PlainToken string `json:"plain_token"`
	Signature  string `json:"signature"`
}

// makeEventDedupeKey creates a normalized deduplication key from event type and ID.
//
// QQ 平台重推同一条消息时会变换 id 尾部（生产观察到同前缀不同后缀，例如
// Om8nbpB.qMl1erhaO.GCzPtogn… / Om8nbpB.qMl1erhaO.GCzIAGd…），按 id 去重会漏，
// 导致同一指令被处理多次、机器人重复回复并连发冷却提示。因此消息与按钮事件
// 改用语义指纹（群+作者+内容 / 群+成员+按钮数据）：类型不进消息指纹键，因为
// @消息可能同时以 GROUP_AT_MESSAGE_CREATE 与 GROUP_MESSAGE_CREATE 两条事件到达。
// 指纹键使用短 TTL（eventFingerprintTTL），语义字段缺失时回退到 id 键（长 TTL）。
func makeEventDedupeKey(eventType, eventID string, data []byte) string {
	switch eventType {
	case "GROUP_AT_MESSAGE_CREATE", "GROUP_MESSAGE_CREATE", "C2C_MESSAGE_CREATE":
		var ev GroupAtMessageEvent
		if err := common.Unmarshal(data, &ev); err == nil && ev.Content != "" {
			sum := sha1.Sum([]byte(ev.GroupOpenID + "|" + ev.Author.ID + "|" + ev.Content))
			return "qq:evt:msg:" + hex.EncodeToString(sum[:])
		}
		if eventID != "" {
			return "qq:evt:" + eventType + ":" + eventID
		}
	case "INTERACTION_CREATE":
		var ev InteractionEvent
		if err := common.Unmarshal(data, &ev); err == nil && ev.Data.Resolved.ButtonData != "" {
			sum := sha1.Sum([]byte(ev.GroupOpenID + "|" + ev.GroupMemberOpenID + "|" +
				ev.UserOpenID + "|" + ev.Data.Resolved.ButtonData))
			return "qq:evt:btn:" + hex.EncodeToString(sum[:])
		}
		if eventID != "" {
			return "qq:evt:" + eventType + ":" + eventID
		}
	}
	return ""
}

// markEventSeen records an event as processed and returns true if it was already seen.
// Uses Redis SETNX with 24h TTL when Redis is enabled, otherwise falls back to in-memory cache.
// ponytail: in-memory fallback is bounded at 256 entries per event type; upgrade to per-account sharding if throughput matters.
func markEventSeen(key string, ttl time.Duration) bool {
	if key == "" {
		return false
	}
	if common.RedisEnabled && common.RDB != nil {
		ctx := context.Background()
		// SETNX with TTL: NX = only set if not exists, EX = expire seconds
		ok, err := common.RDB.SetNX(ctx, key, "1", ttl).Result()
		if err != nil {
			common.SysError("Redis 事件去重失败，回退内存: " + err.Error())
		} else if ok {
			return false // newly set, not seen before
		}
		return true // already existed
	}
	// In-memory fallback (process-local, bounded). TTL-aware: without Redis
	// (production runs this way) a fingerprint entry must still expire so a
	// repeat command after the window is answered normally.
	eventDedupeMu.Lock()
	defer eventDedupeMu.Unlock()
	if exp, ok := eventDedupeSeen[key]; ok {
		if time.Now().Before(exp) {
			return true
		}
		delete(eventDedupeSeen, key) // expired: treat as unseen, refresh below
	}
	eventDedupeSeen[key] = time.Now().Add(ttl)
	eventDedupeOrder = append(eventDedupeOrder, key)
	if len(eventDedupeOrder) > eventDedupeCacheSize {
		delete(eventDedupeSeen, eventDedupeOrder[0])
		eventDedupeOrder = eventDedupeOrder[1:]
	}
	return false
}

var (
	eventDedupeMu        sync.Mutex
	eventDedupeSeen      = make(map[string]time.Time)
	eventDedupeOrder     []string
	eventDedupeCacheSize = 256 // per-type limit for in-memory fallback
)

// QQBotWebhook receives QQ open-platform callbacks. The endpoint is public, so
// every dispatch must pass Ed25519 signature verification before it is handled.
func QQBotWebhook(c contract.Context) {
	setting := GetQQBotSetting()
	if setting.AppSecret == "" {
		// Without a secret there is no way to verify: reject rather than trust.
		_ = c.JSON(http.StatusServiceUnavailable, common.H{"message": "QQ bot not configured"})
		return
	}

	// Enforce webhook path token: if WebhookPathToken is set, legacy path must 404.
	// The integrator wires the route via GetQQWebhookPath(); if the request reaches
	// here on the legacy path /api/qqbot/webhook while a token is configured,
	// we return 404 to avoid the signing oracle and reduce attack surface.
	if setting.WebhookPathToken != "" {
		reqPath := c.HTTPRequest().URL.Path
		expected := GetQQWebhookPath(setting.WebhookPathToken)
		if reqPath != expected {
			_ = c.JSON(http.StatusNotFound, common.H{"message": "not found"})
			return
		}
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

	var payload webhookPayload
	if err := common.Unmarshal(body, &payload); err != nil {
		_ = c.JSON(http.StatusBadRequest, common.H{"message": "invalid payload"})
		return
	}

	// Callback validation signs event_ts + plain_token with a key derived from AppSecret.
	if payload.Op == opCodeValidation {
		var vr validationRequest
		if err := common.Unmarshal(payload.D, &vr); err != nil {
			_ = c.JSON(http.StatusBadRequest, common.H{"message": "invalid validation payload"})
			return
		}
		signature, err := SignValidation(setting.AppSecret, vr.EventTs, vr.PlainToken)
		if err != nil {
			common.SysError("QQ webhook 验证签名生成失败: " + err.Error())
			_ = c.JSON(http.StatusInternalServerError, common.H{"message": "sign failed"})
			return
		}
		_ = c.JSON(http.StatusOK, validationResponse{
			PlainToken: vr.PlainToken,
			Signature:  signature,
		})
		return
	}

	// Dispatches must verify, otherwise a forged request could mint quota.
	if err := VerifySignature(setting.AppSecret, c.Header("X-Signature-Ed25519"), c.Header("X-Signature-Timestamp"), body); err != nil {
		common.SysError("QQ webhook 签名校验失败: " + err.Error())
		_ = c.JSON(http.StatusUnauthorized, common.H{"message": "signature verification failed"})
		return
	}

	if payload.Op != opCodeDispatch {
		_ = c.JSON(http.StatusOK, common.H{"op": opCodeHTTPAck, "d": common.H{}})
		return
	}

	// Handle asynchronously: the platform re-delivers if the ack is slow.
	eventType := payload.T
	eventID := payload.ID
	data := make([]byte, len(payload.D))
	copy(data, payload.D)

	// Event deduplication: skip already-processed events.
	// Covers GROUP_AT_MESSAGE_CREATE, GROUP_MESSAGE_CREATE, INTERACTION_CREATE, C2C_MESSAGE_CREATE.
	dedupeKey := makeEventDedupeKey(eventType, eventID, data)
	dedupeTTL := eventDedupeTTL
	if strings.HasPrefix(dedupeKey, "qq:evt:msg:") || strings.HasPrefix(dedupeKey, "qq:evt:btn:") {
		dedupeTTL = eventFingerprintTTL
	}
	if markEventSeen(dedupeKey, dedupeTTL) {
		common.SysLog("QQ webhook 重复事件，已跳过 t=" + eventType + " id=" + eventID)
		_ = c.JSON(http.StatusOK, common.H{"op": opCodeHTTPAck, "d": common.H{}})
		return
	}

	go handleQQBotEvent(eventType, eventID, data)

	_ = c.JSON(http.StatusOK, common.H{"op": opCodeHTTPAck, "d": common.H{}})
}

func handleQQBotEvent(eventType, eventID string, data []byte) {
	defer func() {
		if r := recover(); r != nil {
			common.SysError("QQ webhook 事件处理 panic: " + eventType)
		}
	}()

	preview := string(data)
	if len(preview) > 500 {
		preview = preview[:500]
	}
	common.SysLog("QQ webhook 收到事件 t=" + eventType + " d=" + preview)

	switch eventType {
	case "GROUP_AT_MESSAGE_CREATE", "GROUP_MESSAGE_CREATE":
		// GROUP_AT_MESSAGE_CREATE is an @-mention; GROUP_MESSAGE_CREATE arrives
		// when "receive all messages" is enabled. Same payload shape.
		var event GroupAtMessageEvent
		if err := common.Unmarshal(data, &event); err != nil {
			common.SysError("解析群消息事件失败: " + err.Error())
			return
		}
		HandleGroupAtMessage(&event)

	case "INTERACTION_CREATE":
		var event InteractionEvent
		if err := common.Unmarshal(data, &event); err != nil {
			common.SysError("解析互动事件失败: " + err.Error())
			return
		}
		// d.id is the interaction id (PUT /interactions/{id}); the outer payload
		// id is the event_id needed to send a passive message. They differ.
		if event.ID == "" {
			event.ID = eventID
		}
		event.PayloadEventID = eventID
		HandleInteraction(&event)

	case "GROUP_JOIN_REQUEST":
		var event GroupJoinRequestEvent
		if err := common.Unmarshal(data, &event); err != nil {
			common.SysError("解析入群申请事件失败: " + err.Error())
			return
		}
		HandleGroupJoinRequest(&event)

	case "C2C_MESSAGE_CREATE":
		// C2C private message for safe binding path (Fix 2).
		// Payload shape mirrors GroupAtMessageEvent; reuse Author.MemberOpenID for sender.
		var event GroupAtMessageEvent
		if err := common.Unmarshal(data, &event); err != nil {
			common.SysError("解析 C2C 消息事件失败: " + err.Error())
			return
		}
		HandleC2CMessageForBind(&event)
	}
}

// HandleC2CMessageForBind handles private messages containing bind codes.
// This is the safe channel for binding; group bind keeps working.
func HandleC2CMessageForBind(event *GroupAtMessageEvent) {
	if event == nil || event.Author.Bot {
		return
	}
	openID := event.Author.MemberOpenID
	if openID == "" {
		openID = event.Author.ID
	}
	content := strings.TrimSpace(event.Content)
	if !looksLikeBindCode(content) {
		return
	}

	code := extractBindCode(content)
	userId, err := identity.ConsumeQQBindCode(
		code, openID, event.Author.UnionOpenID, event.Author.Username)
	if err != nil {
		// Passive reply in C2C (no group context)
		if client, err2 := getClient(); err2 == nil {
			_ = client.SendC2CMessage(openID, buildPlainMarkdown(openID, "**绑定失败！**\n\n"+err.Error()), event.ID)
		}
		return
	}

	usage.RecordLog(userId, usage.LogTypeSystem, "已通过私信绑定 QQ 账号，可使用 QQ 签到")
	if client, err2 := getClient(); err2 == nil {
		reply := buildPlainMarkdown(openID,
			"**绑定成功！**\n\n现在可以直接发送 /签到 领取每日额度")
		_ = client.SendC2CMessage(openID, reply, event.ID)
	}
}

// GetQQBindStatus reports the caller's QQ binding state.
func GetQQBindStatus(c contract.Context) {
	userId := c.GetInt("id")
	setting := GetQQBotSetting()

	binding, err := identity.GetQQBindingByUserId(userId)
	bound := err == nil && binding != nil

	data := common.H{
		"qq_checkin_enabled":      setting.QQCheckinEnabled,
		"bound":                   bound,
		"rebind_cooldown_seconds": setting.RebindCooldownSeconds,
	}
	if bound {
		data["qq_username"] = binding.Username
		data["bound_at"] = binding.CreatedAt
	}

	common.CtxApiSuccess(c, data)
}

// GenerateQQBindCode issues a short-lived bind code for the caller.
func GenerateQQBindCode(c contract.Context) {
	setting := GetQQBotSetting()
	if !setting.QQCheckinEnabled {
		common.CtxApiErrorMsg(c, "QQ 签到功能未启用")
		return
	}

	userId := c.GetInt("id")
	bindCode, err := identity.CreateQQBindCode(userId)
	if err != nil {
		common.CtxApiErrorMsg(c, err.Error())
		return
	}

	common.CtxApiSuccess(c, common.H{
		"code":       bindCode.Code,
		"expired_at": bindCode.ExpiredAt,
		"expires_in": int(time.Until(time.Unix(bindCode.ExpiredAt, 0)).Seconds()),
	})
}

// UnbindQQ removes the caller's QQ binding.
func UnbindQQ(c contract.Context) {
	userId := c.GetInt("id")
	if err := identity.DeleteQQBinding(userId); err != nil {
		common.CtxApiErrorMsg(c, err.Error())
		return
	}
	usage.RecordLog(userId, usage.LogTypeSystem, "已解绑 QQ 账号")
	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"message": "解绑成功",
	})
}

// SyncQQPanel pushes the slash-command panel to the QQ platform.
func SyncQQPanel(c contract.Context) {
	if err := SyncCommandPanel(); err != nil {
		common.CtxApiErrorMsg(c, err.Error())
		return
	}
	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"message": "指令面板已同步到 QQ 平台",
	})
}
