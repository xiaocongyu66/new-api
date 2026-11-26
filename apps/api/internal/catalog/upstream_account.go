package channel

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"

	"gorm.io/gorm"
)

// 上游账号按 host 全局唯一，而不是挂在单个渠道的 settings 上。
//
// 同一个上游域名（例如 xcmapi.org）往往有多个渠道，凭据属于“这个站点”，
// 不属于“这条路由”。存成一行之后，新增渠道只填 base_url 就能自动复用；
// 账号改密码也只改一处。渠道自己的 settings 仍然可以覆盖它，用于个别渠道
// 走不同账号的场景。
const (
	UpstreamAccountKindSub2API = "sub2api"
	UpstreamAccountKindNewAPI  = "newapi"
)

type UpstreamAccount struct {
	Id      int    `json:"id" gorm:"primaryKey"`
	Host    string `json:"host" gorm:"size:191;uniqueIndex"` // 唯一键，如 xcmapi.org
	BaseURL string `json:"base_url" gorm:"size:512"`         // https://xcmapi.org
	Kind    string `json:"kind" gorm:"size:32"`              // newapi | sub2api
	// Username/Password 是面板登录账号。Password 永不外发，handler 用
	// has_password 告诉前端“已配置”，避免把秘密回显到浏览器。
	Username  string `json:"username" gorm:"size:191"`
	Password  string `json:"-" gorm:"size:512"`
	JKGroupID string `json:"jk_group_id" gorm:"size:64"`
	// AccessToken/RefreshToken 是 sub2api 用户会话凭据。与密码同理永不外发。
	AccessToken    string `json:"-" gorm:"size:1024"`
	RefreshToken   string `json:"-" gorm:"size:1024"`
	TokenExpiresAt int64  `json:"token_expires_at"`
	Enabled        bool   `json:"enabled"`
	LastTestAt     int64  `json:"last_test_at"`
	LastTestOK     bool   `json:"last_test_ok"`
	// LastTestMessage 只记录“哪一步失败 + 状态码”，绝不带响应体，避免把
	// 上游返回的 token 片段带进列表页。
	LastTestMessage string `json:"last_test_message" gorm:"size:512"`
	CreatedAt       int64  `json:"created_at"`
	UpdatedAt       int64  `json:"updated_at"`
}

func (UpstreamAccount) TableName() string { return "upstream_accounts" }

func init() {
	dbx.RegisterMigrations(dbx.Migration{Model: &UpstreamAccount{}, Name: "UpstreamAccount"})
}

// UpstreamAccountResponse 是唯一允许穿过 HTTP 边界的账号形状。
// 密码与 token 只以布尔状态出现，前端编辑时留空即保持原值。
type UpstreamAccountResponse struct {
	Id              int    `json:"id"`
	Host            string `json:"host"`
	BaseURL         string `json:"base_url"`
	Kind            string `json:"kind"`
	Username        string `json:"username"`
	JKGroupID       string `json:"jk_group_id"`
	HasPassword     bool   `json:"has_password"`
	HasToken        bool   `json:"has_token"`
	TokenExpiresAt  int64  `json:"token_expires_at"`
	Enabled         bool   `json:"enabled"`
	LastTestAt      int64  `json:"last_test_at"`
	LastTestOK      bool   `json:"last_test_ok"`
	LastTestMessage string `json:"last_test_message"`
	CreatedAt       int64  `json:"created_at"`
	UpdatedAt       int64  `json:"updated_at"`
}

func (account *UpstreamAccount) ToResponse() UpstreamAccountResponse {
	return UpstreamAccountResponse{
		Id:              account.Id,
		Host:            account.Host,
		BaseURL:         account.BaseURL,
		Kind:            account.Kind,
		Username:        account.Username,
		JKGroupID:       account.JKGroupID,
		HasPassword:     strings.TrimSpace(account.Password) != "",
		HasToken:        strings.TrimSpace(account.AccessToken) != "" || strings.TrimSpace(account.RefreshToken) != "",
		TokenExpiresAt:  account.TokenExpiresAt,
		Enabled:         account.Enabled,
		LastTestAt:      account.LastTestAt,
		LastTestOK:      account.LastTestOK,
		LastTestMessage: account.LastTestMessage,
		CreatedAt:       account.CreatedAt,
		UpdatedAt:       account.UpdatedAt,
	}
}

// NormalizeUpstreamAccountHost 从 base_url 提取唯一键。
// 端口、路径、结尾斜杠都不参与匹配：面板的登录接口永远在同一 host 的根路径，
// 而用户填 https://xcmapi.org/v1 还是 https://xcmapi.org/ 其实是同一个站点。
func NormalizeUpstreamAccountHost(rawURL string) (string, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return "", fmt.Errorf("base_url is required")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid base_url: %w", err)
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("base_url must use http or https")
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	if host == "" {
		return "", fmt.Errorf("base_url has no host")
	}
	return host, nil
}

// UpstreamAccountOrigin 返回 scheme://host[:port]。sub2api 的 auth/usage/keys
// 都挂在站点根路径，用户把面板地址连同子路径一起粘进来时，必须按 origin
// 拼接，否则会请求到 /some/path/api/v1/auth/login。
func UpstreamAccountOrigin(rawURL string) (string, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return "", fmt.Errorf("base_url is required")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid base_url: %w", err)
	}
	scheme := strings.ToLower(parsed.Scheme)
	if (scheme != "http" && scheme != "https") || parsed.Host == "" {
		return "", fmt.Errorf("base_url must be an absolute http(s) url")
	}
	return scheme + "://" + parsed.Host, nil
}

func NormalizeUpstreamAccountKind(kind string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case UpstreamAccountKindSub2API:
		return UpstreamAccountKindSub2API, nil
	case UpstreamAccountKindNewAPI:
		return UpstreamAccountKindNewAPI, nil
	default:
		return "", fmt.Errorf("kind must be %s or %s", UpstreamAccountKindSub2API, UpstreamAccountKindNewAPI)
	}
}

func ListUpstreamAccounts() ([]*UpstreamAccount, error) {
	accounts := make([]*UpstreamAccount, 0)
	if err := dbx.DB.Order("host ASC, id ASC").Find(&accounts).Error; err != nil {
		return nil, err
	}
	return accounts, nil
}

func GetUpstreamAccountById(id int) (*UpstreamAccount, error) {
	if id <= 0 {
		return nil, fmt.Errorf("invalid upstream account id")
	}
	var account UpstreamAccount
	if err := dbx.DB.First(&account, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &account, nil
}

// FindUpstreamAccountByHost 按 host 查启用中的账号。
// 返回 (nil, nil) 表示“没有配置”，调用方据此走“凭据缺失”分支，而不是把
// 未找到当成数据库故障。
func FindUpstreamAccountByHost(host string) (*UpstreamAccount, error) {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" || dbx.DB == nil {
		return nil, nil
	}
	var account UpstreamAccount
	err := dbx.DB.Where("host = ? AND enabled = ?", host, true).Order("id ASC").First(&account).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &account, nil
}

func IsUpstreamAccountHostTaken(host string, excludeId int) (bool, error) {
	var count int64
	query := dbx.DB.Model(&UpstreamAccount{}).Where("host = ?", host)
	if excludeId > 0 {
		query = query.Where("id <> ?", excludeId)
	}
	if err := query.Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (account *UpstreamAccount) Insert() error {
	now := common.GetTimestamp()
	account.CreatedAt = now
	account.UpdatedAt = now
	return dbx.DB.Create(account).Error
}

// Save 只用于 handler 已经按 id 读回并合并过的行，避免拿半截对象覆盖密码。
func (account *UpstreamAccount) Save() error {
	account.UpdatedAt = common.GetTimestamp()
	return dbx.DB.Save(account).Error
}

func (account *UpstreamAccount) Delete() error {
	if account.Id <= 0 {
		return fmt.Errorf("invalid upstream account id")
	}
	return dbx.DB.Delete(&UpstreamAccount{}, account.Id).Error
}

// SaveUpstreamAccountToken 只更新续期得到的三个字段，不碰账号密码。
// refreshToken 为空时保留原值：部分上游刷新接口只返回新的 access token。
func SaveUpstreamAccountToken(id int, accessToken, refreshToken string, expiresAt int64) error {
	if id <= 0 {
		return fmt.Errorf("invalid upstream account id")
	}
	updates := map[string]any{
		"access_token":     strings.TrimSpace(accessToken),
		"token_expires_at": expiresAt,
		"updated_at":       common.GetTimestamp(),
	}
	if refreshToken = strings.TrimSpace(refreshToken); refreshToken != "" {
		updates["refresh_token"] = refreshToken
	}
	return dbx.DB.Model(&UpstreamAccount{}).Where("id = ?", id).Updates(updates).Error
}

// ClearUpstreamAccountAccessToken 只作废 access token，保留 refresh token 与
// 账号密码，供 401 后的强制续期继续使用。
func ClearUpstreamAccountAccessToken(id int) error {
	if id <= 0 {
		return fmt.Errorf("invalid upstream account id")
	}
	return dbx.DB.Model(&UpstreamAccount{}).Where("id = ?", id).Updates(map[string]any{
		"access_token":     "",
		"token_expires_at": 0,
		"updated_at":       common.GetTimestamp(),
	}).Error
}

func (account *UpstreamAccount) RecordTestResult(ok bool, message string) {
	now := common.GetTimestamp()
	account.LastTestAt = now
	account.LastTestOK = ok
	account.LastTestMessage = message
	account.UpdatedAt = now
	if account.Id <= 0 {
		return
	}
	if err := dbx.DB.Model(&UpstreamAccount{}).Where("id = ?", account.Id).Updates(map[string]any{
		"last_test_at":      now,
		"last_test_ok":      ok,
		"last_test_message": message,
		"updated_at":        now,
	}).Error; err != nil {
		common.SysLog(fmt.Sprintf("upstream account test result persist failed: id=%d: %v", account.Id, err))
	}
}
