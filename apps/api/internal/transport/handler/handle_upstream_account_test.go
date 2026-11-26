package handler

import (
	"testing"

	channelpkg "github.com/QuantumNous/new-api/internal/catalog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 「留空 = 保持原值」是整个密钥编辑流程安全性的支点：前端不回传明文，
// 表单值为空串。这条契约一旦被改坏，用户每次打开弹窗点保存都会静默清空
// 已存的凭据。掩码同理：前端可能手抄 placeholder 提交回来。
func TestApplyUpstreamAccountRequestKeepsStoredSecrets(t *testing.T) {
	base := channelpkg.UpstreamAccount{
		Id:             7,
		Host:           "keep.example.com",
		BaseURL:        "https://keep.example.com",
		Kind:           channelpkg.UpstreamAccountKindSub2API,
		Username:       "old@example.com",
		Password:       "stored-password",
		AccessToken:    "stored-access",
		RefreshToken:   "stored-refresh",
		TokenExpiresAt: 1700000000,
		JKGroupID:      "99",
		LastTestAt:     123,
		LastTestOK:     true,
	}

	t.Run("empty strings keep stored secrets", func(t *testing.T) {
		account := base
		err := applyUpstreamAccountRequest(&account, upstreamAccountRequest{
			BaseURL: "https://keep.example.com",
			Kind:    "sub2api",
			// 三个密钥全部留空：编辑页「不改密码」的正常路径。
		})
		require.NoError(t, err)
		assert.Equal(t, "stored-password", account.Password)
		assert.Equal(t, "stored-access", account.AccessToken)
		assert.Equal(t, "stored-refresh", account.RefreshToken)
		assert.EqualValues(t, 1700000000, account.TokenExpiresAt, "没换 token 就不该动过期时间")
	})

	t.Run("masked sentinel keeps stored secrets", func(t *testing.T) {
		account := base
		err := applyUpstreamAccountRequest(&account, upstreamAccountRequest{
			BaseURL:      "https://keep.example.com",
			Kind:         "sub2api",
			Password:     upstreamAccountMaskedSecret,
			AccessToken:  upstreamAccountMaskedSecret,
			RefreshToken: upstreamAccountMaskedSecret,
		})
		require.NoError(t, err)
		assert.Equal(t, "stored-password", account.Password)
		assert.Equal(t, "stored-access", account.AccessToken)
		assert.Equal(t, "stored-refresh", account.RefreshToken)
	})

	t.Run("new values replace stored secrets", func(t *testing.T) {
		account := base
		err := applyUpstreamAccountRequest(&account, upstreamAccountRequest{
			BaseURL:      "https://keep.example.com",
			Kind:         "sub2api",
			Password:     "new-password",
			AccessToken:  "new-access",
			RefreshToken: "new-refresh",
		})
		require.NoError(t, err)
		assert.Equal(t, "new-password", account.Password)
		assert.Equal(t, "new-access", account.AccessToken)
		assert.Equal(t, "new-refresh", account.RefreshToken)
		assert.EqualValues(t, 0, account.TokenExpiresAt, "换了 access token 后旧过期时间必须作废")
	})

	t.Run("changing base url rewrites host but keeps secrets", func(t *testing.T) {
		account := base
		err := applyUpstreamAccountRequest(&account, upstreamAccountRequest{
			BaseURL: "https://moved.example.com/v1",
			Kind:    "sub2api",
		})
		require.NoError(t, err)
		assert.Equal(t, "moved.example.com", account.Host, "host 必须跟着 base_url 走")
		assert.Equal(t, "stored-password", account.Password)
	})
}
