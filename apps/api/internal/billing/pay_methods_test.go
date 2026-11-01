package billing

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUpdatePayMethodsByJsonStringValidatesGatewayAndType(t *testing.T) {
	originalPayMethods := PayMethods
	t.Cleanup(func() { PayMethods = originalPayMethods })

	tests := []struct {
		name    string
		json    string
		wantErr string
	}{
		{
			name: "vmq alipay is valid",
			json: `[{"name":"V免签支付宝","type":"alipay","gateway":"vmq"}]`,
		},
		{
			name: "vmq wxpay is valid",
			json: `[{"name":"V免签微信","type":"wxpay","gateway":"vmq"}]`,
		},
		{
			name: "codepay alipay is valid",
			json: `[{"name":"码支付支付宝","type":"alipay","gateway":"codepay"}]`,
		},
		{
			name: "codepay wxpay is valid",
			json: `[{"name":"码支付微信","type":"wxpay","gateway":"codepay"}]`,
		},
		{
			name: "missing gateway stays epay and any type is accepted",
			json: `[{"name":"自定义1","type":"custom1","min_topup":"50"}]`,
		},
		{
			name:    "vmq rejects non alipay/wxpay type",
			json:    `[{"name":"V免签自定义","type":"custom1","gateway":"vmq"}]`,
			wantErr: "V免签/码支付 付款方式的类型只能是 alipay 或 wxpay",
		},
		{
			name:    "codepay rejects non alipay/wxpay type",
			json:    `[{"name":"码支付自定义","type":"custom1","gateway":"codepay"}]`,
			wantErr: "V免签/码支付 付款方式的类型只能是 alipay 或 wxpay",
		},
		{
			name:    "unknown gateway is rejected",
			json:    `[{"name":"bad","type":"alipay","gateway":"foo"}]`,
			wantErr: "不支持的支付网关",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := UpdatePayMethodsByJsonString(tt.json)
			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tt.wantErr)
			}
		})
	}
}

func TestUpdatePayMethodsByJsonStringDoesNotClobberOnInvalid(t *testing.T) {
	originalPayMethods := PayMethods
	t.Cleanup(func() { PayMethods = originalPayMethods })

	PayMethods = []map[string]string{{"name": "sentinel", "type": "alipay"}}
	err := UpdatePayMethodsByJsonString(`[{"name":"x","type":"alipay","gateway":"vmqx"}]`)
	require.ErrorContains(t, err, "不支持的支付网关")
	require.Equal(t, []map[string]string{{"name": "sentinel", "type": "alipay"}}, PayMethods)
}

func TestPayMethodGatewayNormalizesMissingToEpay(t *testing.T) {
	require.Equal(t, "epay", payMethodGateway(map[string]string{}))
	require.Equal(t, "epay", payMethodGateway(map[string]string{"gateway": ""}))
	require.Equal(t, "epay", payMethodGateway(map[string]string{"gateway": "epay"}))
	require.Equal(t, "vmq", payMethodGateway(map[string]string{"gateway": "vmq"}))
	require.Equal(t, "codepay", payMethodGateway(map[string]string{"gateway": "codepay"}))
}

func TestFindPayMethodMatchesTypeAndGateway(t *testing.T) {
	originalPayMethods := PayMethods
	t.Cleanup(func() { PayMethods = originalPayMethods })

	PayMethods = []map[string]string{
		{"name": "支付宝", "type": "alipay"},
		{"name": "微信", "type": "wxpay", "gateway": "vmq"},
		{"name": "码支付", "type": "alipay", "gateway": "codepay"},
	}

	m, ok := findPayMethod("alipay", "epay")
	require.True(t, ok)
	require.Equal(t, "支付宝", m["name"])

	m, ok = findPayMethod("wxpay", "vmq")
	require.True(t, ok)
	require.Equal(t, "微信", m["name"])

	m, ok = findPayMethod("alipay", "codepay")
	require.True(t, ok)
	require.Equal(t, "码支付", m["name"])

	// Type without a gateway never matches an explicit gateway query.
	m, ok = findPayMethod("alipay", "vmq")
	require.False(t, ok)
	m, ok = findPayMethod("wxpay", "epay")
	require.False(t, ok)
	m, ok = findPayMethod("custom1", "codepay")
	require.False(t, ok)
}
