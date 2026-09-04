package security

import (
	"github.com/QuantumNous/new-api/internal/settings"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/egress"
	"github.com/QuantumNous/new-api/internal/identity"
)

var defaultPasskeySettings = identity.PasskeySettings{
	Enabled:              false,
	RPDisplayName:        common.SystemName,
	RPID:                 "",
	Origins:              "",
	AllowInsecureOrigin:  false,
	UserVerification:     "preferred",
	AttachmentPreference: "",
}

func init() {
	settings.GlobalConfig.Register("passkey", &defaultPasskeySettings)
	identity.OnGetPasskeySettings = GetPasskeySettings
}

func GetPasskeySettings() *identity.PasskeySettings {
	if defaultPasskeySettings.RPID == "" && egress.ServerAddress != "" {
		// 从ServerAddress提取域名作为RPID
		// ServerAddress可能是 "https://newapi.pro" 这种格式
		serverAddr := strings.TrimSpace(egress.ServerAddress)
		if parsed, err := url.Parse(serverAddr); err == nil && parsed.Host != "" {
			defaultPasskeySettings.RPID = parsed.Host
		} else {
			defaultPasskeySettings.RPID = serverAddr
		}
	}
	if defaultPasskeySettings.Origins == "" || defaultPasskeySettings.Origins == "[]" {
		defaultPasskeySettings.Origins = egress.ServerAddress
	}
	return &defaultPasskeySettings
}
