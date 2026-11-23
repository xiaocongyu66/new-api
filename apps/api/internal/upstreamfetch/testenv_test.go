package upstreamfetch

// 测试脚手架：放开回环地址，好让 httptest 的临时端口可达。

import (
	"testing"

	"github.com/QuantumNous/new-api/internal/egress"
)

func allowLoopback(t *testing.T) {
	t.Helper()
	setting := egress.GetFetchSetting()
	original := *setting
	t.Cleanup(func() { *setting = original })
	setting.EnableSSRFProtection = true
	setting.AllowPrivateIp = true
	setting.AllowedPorts = nil
}
