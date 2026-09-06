package billing

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/internal/common"
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

// QQBotWebhook receives QQ open-platform callbacks. The endpoint is public, so
// every dispatch must pass Ed25519 signature verification before it is handled.
func QQBotWebhook(c contract.Context) {
	setting := GetQQBotSetting()
	if setting.AppSecret == "" {
		// Without a secret there is no way to verify: reject rather than trust.
		_ = c.JSON(http.StatusServiceUnavailable, common.H{"message": "QQ bot not configured"})
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
	}
}

// GetQQBindStatus reports the caller's QQ binding state.
func GetQQBindStatus(c contract.Context) {
	userId := c.GetInt("id")
	setting := GetQQBotSetting()

	binding, err := identity.GetQQBindingByUserId(userId)
	bound := err == nil && binding != nil

	data := common.H{
		"qq_checkin_enabled": setting.QQCheckinEnabled,
		"bound":              bound,
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
