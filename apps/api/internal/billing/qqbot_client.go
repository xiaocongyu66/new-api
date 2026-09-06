package billing

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/QuantumNous/new-api/internal/common"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

const (
	apiBase  = "https://api.bot.qq.com"
	tokenURL = "https://bots.qq.com/app/getAppAccessToken"
)

// tokenManager 负责 AccessToken 的获取与缓存刷新
type tokenManager struct {
	mu        sync.RWMutex
	appID     string
	appSecret string
	token     string
	expireAt  time.Time
	client    *http.Client
}

func newTokenManager(appID, appSecret string) *tokenManager {
	return &tokenManager{
		appID:     appID,
		appSecret: appSecret,
		client:    &http.Client{Timeout: 15 * time.Second},
	}
}

// credentialsChanged 判断后台配置是否已被修改
func (tm *tokenManager) credentialsChanged(appID, appSecret string) bool {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.appID != appID || tm.appSecret != appSecret
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   any    `json:"expires_in"`
	Message     string `json:"message"`
	Code        int    `json:"code"`
}

// Token 返回可用的 AccessToken，提前 60 秒刷新
func (tm *tokenManager) Token() (string, error) {
	tm.mu.RLock()
	if tm.token != "" && time.Now().Before(tm.expireAt.Add(-60*time.Second)) {
		token := tm.token
		tm.mu.RUnlock()
		return token, nil
	}
	tm.mu.RUnlock()

	tm.mu.Lock()
	defer tm.mu.Unlock()
	// 双检，避免并发重复请求
	if tm.token != "" && time.Now().Before(tm.expireAt.Add(-60*time.Second)) {
		return tm.token, nil
	}

	payload, _ := json.Marshal(map[string]string{
		"appId":        tm.appID,
		"clientSecret": tm.appSecret,
	})
	req, err := http.NewRequest(http.MethodPost, tokenURL, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := tm.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("获取 AccessToken 失败 HTTP %d: %s", resp.StatusCode, string(body))
	}

	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return "", fmt.Errorf("解析 AccessToken 响应失败: %w", err)
	}
	if tr.AccessToken == "" {
		return "", fmt.Errorf("获取 AccessToken 失败: %s", string(body))
	}

	// expires_in 在文档中为字符串，这里兼容数字与字符串两种形式
	expiresIn := 7200
	switch v := tr.ExpiresIn.(type) {
	case float64:
		expiresIn = int(v)
	case string:
		if n, err := strconv.Atoi(v); err == nil {
			expiresIn = n
		}
	}

	tm.token = tr.AccessToken
	tm.expireAt = time.Now().Add(time.Duration(expiresIn) * time.Second)
	return tm.token, nil
}

// apiClient 封装带鉴权的开放平台 API 调用
type apiClient struct {
	tm     *tokenManager
	client *http.Client
}

func newAPIClient(tm *tokenManager) *apiClient {
	return &apiClient{
		tm:     tm,
		client: &http.Client{Timeout: 20 * time.Second},
	}
}

// do 执行一次 API 请求，返回响应体
func (ac *apiClient) do(method, path string, body any) ([]byte, int, error) {
	token, err := ac.tm.Token()
	if err != nil {
		return nil, 0, err
	}

	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		reader = bytes.NewReader(payload)
	}

	req, err := http.NewRequest(method, apiBase+path, reader)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "QQBot "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := ac.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return respBody, resp.StatusCode, nil
}

// GatewayInfo /gateway/bot 的响应
type GatewayInfo struct {
	URL    string `json:"url"`
	Shards int    `json:"shards"`
}

// Gateway 获取 WebSocket 接入点
func (ac *apiClient) Gateway() (*GatewayInfo, error) {
	body, status, err := ac.do(http.MethodGet, "/gateway/bot", nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("获取网关地址失败 HTTP %d: %s", status, string(body))
	}
	var info GatewayInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, err
	}
	if info.URL == "" {
		return nil, fmt.Errorf("网关地址为空: %s", string(body))
	}
	if info.Shards <= 0 {
		info.Shards = 1
	}
	return &info, nil
}

// Button 内嵌键盘按钮
type Button struct {
	ID         string      `json:"id,omitempty"`
	RenderData *RenderData `json:"render_data,omitempty"`
	Action     *Action     `json:"action,omitempty"`
}

// RenderData 按钮渲染信息
type RenderData struct {
	Label        string `json:"label"`
	VisitedLabel string `json:"visited_label,omitempty"`
	Style        int    `json:"style"`
}

// Action 按钮行为
type Action struct {
	Type          int         `json:"type"`
	Permission    *Permission `json:"permission,omitempty"`
	Data          string      `json:"data,omitempty"`
	UnsupportTips string      `json:"unsupport_tips,omitempty"`
	Enter         bool        `json:"enter,omitempty"`
	Reply         bool        `json:"reply,omitempty"`
}

// Permission 按钮操作权限
type Permission struct {
	Type           int      `json:"type"`
	SpecifyUserIDs []string `json:"specify_user_ids,omitempty"`
}

// Row 键盘按钮行
type Row struct {
	Buttons []Button `json:"buttons"`
}

// KeyboardContent 自定义键盘布局
type KeyboardContent struct {
	Rows []Row `json:"rows"`
}

// Keyboard 内嵌键盘
type Keyboard struct {
	Content *KeyboardContent `json:"content,omitempty"`
}

// MessageMarkdown Markdown 消息体
type MessageMarkdown struct {
	Content string `json:"content,omitempty"`
}

// GroupMessageRequest 发送群消息请求体
type GroupMessageRequest struct {
	MsgType  int              `json:"msg_type"`
	Content  string           `json:"content,omitempty"`
	Markdown *MessageMarkdown `json:"markdown,omitempty"`
	Keyboard *Keyboard        `json:"keyboard,omitempty"`
	MsgID    string           `json:"msg_id,omitempty"`
	EventID  string           `json:"event_id,omitempty"`
	MsgSeq   int              `json:"msg_seq,omitempty"`
}

// SendGroupMessage 发送群聊消息
//
// 平台有一类失败是 HTTP 200 + body 里带 code/message，
// 所以成功路径也要把响应体记下来，否则「接口成功但群里没消息」无法排查。
func (ac *apiClient) SendGroupMessage(groupOpenID string, req *GroupMessageRequest) ([]byte, error) {
	path := fmt.Sprintf("/v2/groups/%s/messages", url.PathEscape(groupOpenID))
	reqPreview, _ := json.Marshal(req)
	body, status, err := ac.do(http.MethodPost, path, req)
	if err != nil {
		common.SysError(fmt.Sprintf("群消息请求失败 group=%s req=%s err=%v",
			groupOpenID, truncateForLog(string(reqPreview), 600), err))
		return nil, err
	}
	if status != http.StatusOK && status != http.StatusCreated && status != http.StatusNoContent {
		common.SysError(fmt.Sprintf("群消息响应异常 group=%s HTTP=%d req=%s resp=%s",
			groupOpenID, status,
			truncateForLog(string(reqPreview), 600), truncateForLog(string(body), 600)))
		return body, fmt.Errorf("发送群消息失败 HTTP %d: %s", status, string(body))
	}
	common.SysLog(fmt.Sprintf("群消息响应 group=%s HTTP=%d resp=%s",
		groupOpenID, status, truncateForLog(string(body), 400)))
	return body, nil
}

// truncateForLog 截断过长字符串，避免日志被单条消息刷爆
func truncateForLog(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "...(truncated)"
}

// AnswerInteraction 回应互动事件，code=0 表示成功
func (ac *apiClient) AnswerInteraction(interactionID string, code int) error {
	path := fmt.Sprintf("/interactions/%s", url.PathEscape(interactionID))
	body, status, err := ac.do(http.MethodPut, path, map[string]int{"code": code})
	if err != nil {
		return err
	}
	if status != http.StatusOK && status != http.StatusNoContent {
		return fmt.Errorf("回应互动事件失败 HTTP %d: %s", status, string(body))
	}
	return nil
}

// JoinRequestItem 入群申请条目
type JoinRequestItem struct {
	JoinRequestID string `json:"join_request_id"`
	RiskTips      string `json:"risk_tips"`
	UnionOpenID   string `json:"union_openid"`
	MemberOpenID  string `json:"member_openid"`
	Username      string `json:"username"`
	ApplyAt       string `json:"apply_at"`
	ApplySource   string `json:"apply_source"`
	InvitedBy     string `json:"invited_by"`
	Bot           bool   `json:"bot"`
	VerifyInfo    struct {
		Method        string `json:"method"`
		VerifyMessage string `json:"verify_message"`
		ReviewQAList  []struct {
			Question string `json:"question"`
			Answer   string `json:"answer"`
		} `json:"review_qa_list"`
	} `json:"verify_info"`
}

// JoinRequestListResponse 入群申请列表响应
type JoinRequestListResponse struct {
	List       []JoinRequestItem `json:"list"`
	NextCursor string            `json:"next_cursor"`
}

// ListJoinRequests 拉取入群申请列表
func (ac *apiClient) ListJoinRequests(groupOpenID, cursor string, limit int) (*JoinRequestListResponse, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	path := fmt.Sprintf("/v2/groups/%s/join_request_list?limit=%d",
		url.PathEscape(groupOpenID), limit)
	if cursor != "" {
		path += "&cursor=" + url.QueryEscape(cursor)
	}

	body, status, err := ac.do(http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("拉取入群申请失败 HTTP %d: %s", status, string(body))
	}
	var resp JoinRequestListResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ApproveJoinRequest 通过入群申请
func (ac *apiClient) ApproveJoinRequest(groupOpenID, memberOpenID, joinRequestID string) error {
	path := fmt.Sprintf("/v2/groups/%s/approval_join_request/%s",
		url.PathEscape(groupOpenID), url.PathEscape(memberOpenID))
	payload := map[string]any{
		"op":              "approve",
		"join_request_id": joinRequestID,
	}
	body, status, err := ac.do(http.MethodPost, path, payload)
	if err != nil {
		return err
	}
	if status != http.StatusOK && status != http.StatusNoContent {
		return fmt.Errorf("审批入群申请失败 HTTP %d: %s", status, string(body))
	}
	return nil
}

// PanelItem 指令面板元素
type PanelItem struct {
	Name      string `json:"name,omitempty"`
	Desc      string `json:"desc,omitempty"`
	Type      string `json:"type,omitempty"` // command / link
	OnlyAdmin bool   `json:"only_admin,omitempty"`
	Link      string `json:"link,omitempty"`
}

// Panel 指令面板配置
type Panel struct {
	Items   []PanelItem `json:"items,omitempty"`
	Remark  string      `json:"remark,omitempty"`
	Version int         `json:"version,omitempty"`
}

// CreatePanelRequest 创建指令面板请求体
type CreatePanelRequest struct {
	Scope        string   `json:"scope"`                   // c2c / group / channel / dm
	TargetType   string   `json:"target_type,omitempty"`   // all / specific
	UserOpenIDs  []string `json:"user_openids,omitempty"`  // c2c specific
	GroupOpenIDs []string `json:"group_openids,omitempty"` // group specific
	Panel        Panel    `json:"panel"`
}

// PanelBrief 指令面板列表条目
type PanelBrief struct {
	PanelID    string `json:"panel_id"`
	Scope      string `json:"scope"`
	TargetType string `json:"target_type"`
}

// ListPanels 查询指令面板列表
// scope 必填（c2c/group/channel/dm），不传平台会返回 40030011 生效场景不合法
func (ac *apiClient) ListPanels(scope string) ([]PanelBrief, error) {
	path := "/v2/panels?scope=" + url.QueryEscape(scope)
	body, status, err := ac.do(http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("查询指令面板列表失败 HTTP %d: %s", status, string(body))
	}
	var resp struct {
		List []PanelBrief `json:"list"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return resp.List, nil
}

// CreatePanel 创建指令面板，返回 panel_id
func (ac *apiClient) CreatePanel(req *CreatePanelRequest) (string, error) {
	body, status, err := ac.do(http.MethodPost, "/v2/panels", req)
	if err != nil {
		return "", err
	}
	if status != http.StatusOK && status != http.StatusCreated {
		return "", fmt.Errorf("创建指令面板失败 HTTP %d: %s", status, string(body))
	}
	var resp struct {
		PanelID string `json:"panel_id"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", err
	}
	return resp.PanelID, nil
}

// DeletePanel 删除指令面板
func (ac *apiClient) DeletePanel(panelID string) error {
	path := fmt.Sprintf("/v2/panels/%s", url.PathEscape(panelID))
	body, status, err := ac.do(http.MethodDelete, path, nil)
	if err != nil {
		return err
	}
	if status != http.StatusOK && status != http.StatusNoContent {
		return fmt.Errorf("删除指令面板失败 HTTP %d: %s", status, string(body))
	}
	return nil
}
