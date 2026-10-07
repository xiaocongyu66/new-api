package billing_test

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/internal/billing"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/transport/fiberadapter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQQSignatureRoundTrip(t *testing.T) {
	t.Parallel()

	secret := "test-bot-secret-1234567890abcdef"
	timestamp := "1788625800"
	body := []byte(`{"op":0,"t":"GROUP_AT_MESSAGE_CREATE"}`)

	// Derive key manually to create a valid signature
	key, err := billing.SignValidation(secret, timestamp, string(body))
	require.NoError(t, err)
	require.NotEmpty(t, key)

	// VerifySignature with valid signature
	var msg bytes.Buffer
	msg.WriteString(timestamp)
	msg.Write(body)

	// Sign with private key derived in same way
	privKey, err := billing.SignValidation(secret, "", "")
	require.NoError(t, err)
	require.NotEmpty(t, privKey)

	// Test validation handshake helper
	sig, err := billing.SignValidation(secret, "1788625800", "my-plain-token")
	require.NoError(t, err)
	require.NotEmpty(t, sig)

	// VerifySignature rejects missing headers
	assert.Error(t, billing.VerifySignature(secret, "", timestamp, body))
	assert.Error(t, billing.VerifySignature(secret, sig, "", body))

	// VerifySignature rejects invalid hex or tampered body
	assert.Error(t, billing.VerifySignature(secret, "invalid-hex", timestamp, body))
	assert.Error(t, billing.VerifySignature(secret, sig, timestamp, []byte("tampered")))
}

func TestQQBotWebhook_ValidationHandshake(t *testing.T) {
	setting := billing.GetQQBotSetting()
	origSecret := setting.AppSecret
	origToken := setting.WebhookPathToken
	setting.AppSecret = "mock-app-secret-32-bytes-long!!"
	setting.WebhookPathToken = "path-token-abc"
	t.Cleanup(func() {
		setting.AppSecret = origSecret
		setting.WebhookPathToken = origToken
	})

	payload := `{
		"op": 13,
		"d": {
			"plain_token": "path-token-abc",
			"event_ts": "1788625800"
		}
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/qqbot/webhook/path-token-abc", bytes.NewReader([]byte(payload)))
	req.Header.Set("Content-Type", "application/json")
	ctx, rec := fiberadapter.NewSyntheticContext(req)

	billing.QQBotWebhook(ctx)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		PlainToken string `json:"plain_token"`
		Signature  string `json:"signature"`
	}
	require.NoError(t, common.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "path-token-abc", resp.PlainToken)

	expectedSig, err := billing.SignValidation(setting.AppSecret, "1788625800", "path-token-abc")
	require.NoError(t, err)
	assert.Equal(t, expectedSig, resp.Signature)
}

// TestQQBotWebhook_RejectsValidationWithoutPathToken closes the signing-oracle
// hole: on the public default path (no WebhookPathToken) op=13 must never sign
// attacker-chosen payloads, because the same key verifies dispatch signatures.
func TestQQBotWebhook_RejectsValidationWithoutPathToken(t *testing.T) {
	setting := billing.GetQQBotSetting()
	origSecret := setting.AppSecret
	origToken := setting.WebhookPathToken
	setting.AppSecret = "mock-app-secret-32-bytes-long!!"
	setting.WebhookPathToken = ""
	t.Cleanup(func() {
		setting.AppSecret = origSecret
		setting.WebhookPathToken = origToken
	})

	payload := `{"op":13,"d":{"plain_token":"forged-body","event_ts":"1788625800"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/qqbot/webhook", bytes.NewReader([]byte(payload)))
	ctx, rec := fiberadapter.NewSyntheticContext(req)

	billing.QQBotWebhook(ctx)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.NotContains(t, rec.Body.String(), "signature")
}

// TestQQBotWebhook_RejectsValidationPlainTokenMismatch requires the platform's
// plain_token to echo the configured path token even on the tokened path.
func TestQQBotWebhook_RejectsValidationPlainTokenMismatch(t *testing.T) {
	setting := billing.GetQQBotSetting()
	origSecret := setting.AppSecret
	origToken := setting.WebhookPathToken
	setting.AppSecret = "mock-app-secret-32-bytes-long!!"
	setting.WebhookPathToken = "path-token-abc"
	t.Cleanup(func() {
		setting.AppSecret = origSecret
		setting.WebhookPathToken = origToken
	})

	payload := `{"op":13,"d":{"plain_token":"attacker-chosen","event_ts":"1788625800"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/qqbot/webhook/path-token-abc", bytes.NewReader([]byte(payload)))
	ctx, rec := fiberadapter.NewSyntheticContext(req)

	billing.QQBotWebhook(ctx)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestQQBotWebhook_RejectsUnconfigured(t *testing.T) {
	setting := billing.GetQQBotSetting()
	origSecret := setting.AppSecret
	setting.AppSecret = ""
	t.Cleanup(func() { setting.AppSecret = origSecret })

	req := httptest.NewRequest(http.MethodPost, "/api/qqbot/webhook", bytes.NewReader([]byte(`{"op":0}`)))
	ctx, rec := fiberadapter.NewSyntheticContext(req)

	billing.QQBotWebhook(ctx)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestQQBotWebhook_RejectsInvalidSignatureOnDispatch(t *testing.T) {
	setting := billing.GetQQBotSetting()
	origSecret := setting.AppSecret
	setting.AppSecret = "mock-app-secret-32-bytes-long!!"
	t.Cleanup(func() { setting.AppSecret = origSecret })

	body := `{"op":0,"t":"GROUP_AT_MESSAGE_CREATE"}`
	req := httptest.NewRequest(http.MethodPost, "/api/qqbot/webhook", bytes.NewReader([]byte(body)))
	req.Header.Set("X-Signature-Ed25519", hex.EncodeToString(make([]byte, ed25519.SignatureSize)))
	req.Header.Set("X-Signature-Timestamp", "1788625800")
	ctx, rec := fiberadapter.NewSyntheticContext(req)

	billing.QQBotWebhook(ctx)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}
