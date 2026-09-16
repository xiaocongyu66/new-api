package billing

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/internal/common"
)

// napCatClient 调用 NapCat 暴露的 OneBot v11 HTTP 接口。
//
// 与官方 apiClient（api.bot.qq.com）不同，这里走的是 NapCat 本地端口，
// 用真实 QQ 号（user_id）而非 member_openid 标识用户。踢人、禁言、
// 拉成员列表这类官方 bot 没权限的操作，都由挂了老号的 NapCat 执行。
type napCatClient struct {
	baseURL string
	token   string
	client  *http.Client
}

func newNapCatClient() *napCatClient {
	s := GetQQBotSetting()
	base := strings.TrimRight(strings.TrimSpace(s.NapCatOneBotHTTPAddress), "/")
	return &napCatClient{
		baseURL: base,
		token:   s.NapCatOneBotAccessToken,
		client:  &http.Client{Timeout: 20 * time.Second},
	}
}

// IsNapCatConfigured NapCat 地址是否已配置
func IsNapCatConfigured() bool {
	return GetQQBotSetting().NapCatOneBotHTTPAddress != ""
}

// oneBotResponse OneBot v11 HTTP 响应统一信封
type oneBotResponse struct {
	Status  string          `json:"status"`
	RetCode int             `json:"retcode"`
	Data    json.RawMessage `json:"data"`
	Message string          `json:"message"`
	Wording string          `json:"wording"`
}

// call 发送一个 OneBot v11 HTTP 请求。action 形如 /get_group_member_info。
func (nc *napCatClient) call(action string, params any) ([]byte, error) {
	if nc.baseURL == "" {
		return nil, fmt.Errorf("NapCat 地址未配置")
	}
	payload, _ := common.Marshal(params)
	req, err := http.NewRequest(http.MethodPost, nc.baseURL+action, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if nc.token != "" {
		req.Header.Set("Authorization", "Bearer "+nc.token)
	}

	resp, err := nc.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("调用 NapCat %s 失败: %w", action, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取 NapCat 响应失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("NapCat %s HTTP %d: %s", action, resp.StatusCode, string(body))
	}

	var envelope oneBotResponse
	if err := common.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("解析 NapCat 响应失败: %w", err)
	}
	if envelope.Status != "ok" {
		msg := envelope.Message
		if msg == "" {
			msg = envelope.Wording
		}
		return nil, fmt.Errorf("NapCat %s 返回 %s(ret=%d): %s",
			action, envelope.Status, envelope.RetCode, msg)
	}
	return envelope.Data, nil
}

// GroupMemberInfo OneBot v11 get_group_member_info 返回的成员信息
type GroupMemberInfo struct {
	UserID       int64  `json:"user_id"`
	Nickname     string `json:"nickname"`
	Card         string `json:"card"`
	Role         string `json:"role"` // owner/admin/member
	JoinTime     int64  `json:"join_time"`
	LastSentTime int64  `json:"last_sent_time"`
}

// GetGroupMemberList 拉取群成员列表（真实 QQ 号 + 最后发言时间）。
// OneBot v11 的该接口不含 last_sent_time，需逐个 get_group_member_info
// 补齐；这里一次拿全量基础信息。
func (nc *napCatClient) GetGroupMemberList(groupID int64) ([]GroupMemberInfo, error) {
	data, err := nc.call("/get_group_member_list", map[string]any{"group_id": groupID})
	if err != nil {
		return nil, err
	}
	var members []GroupMemberInfo
	if err := common.Unmarshal(data, &members); err != nil {
		return nil, fmt.Errorf("解析群成员列表失败: %w", err)
	}
	return members, nil
}

// GetGroupMemberInfo 拉取单个成员详情（含 last_sent_time）。
// noCache=true 强制 NapCat 不走缓存，潜水判定必须用最新发言时间。
func (nc *napCatClient) GetGroupMemberInfo(groupID, userID int64) (*GroupMemberInfo, error) {
	data, err := nc.call("/get_group_member_info", map[string]any{
		"group_id": groupID,
		"user_id":  userID,
		"no_cache": true,
	})
	if err != nil {
		return nil, err
	}
	var info GroupMemberInfo
	if err := common.Unmarshal(data, &info); err != nil {
		return nil, fmt.Errorf("解析群成员信息失败: %w", err)
	}
	return &info, nil
}

// SetGroupKick 将成员踢出群。rejectAddRequest=true 时同时拒绝其再次入群。
func (nc *napCatClient) SetGroupKick(groupID, userID int64, rejectAddRequest bool) error {
	_, err := nc.call("/set_group_kick", map[string]any{
		"group_id":           groupID,
		"user_id":            userID,
		"reject_add_request": rejectAddRequest,
	})
	return err
}

// SetGroupBan 禁言成员 durationSeconds 秒，0 表示解禁。
func (nc *napCatClient) SetGroupBan(groupID, userID int64, durationSeconds int) error {
	_, err := nc.call("/set_group_ban", map[string]any{
		"group_id": groupID,
		"user_id":  userID,
		"duration": durationSeconds,
	})
	return err
}

// SendGroupMsg 发送群消息（CQ 码文本）。
func (nc *napCatClient) SendGroupMsg(groupID int64, message string) error {
	_, err := nc.call("/send_group_msg", map[string]any{
		"group_id":    groupID,
		"message":     message,
		"auto_escape": false,
	})
	return err
}
