package ops

import (
	"fmt"
	"github.com/QuantumNous/new-api/internal/common/dbx"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/model"
)

// egress.ProxyConfig holds the sing-box global proxy configuration stored in the
// Option table under the "proxy_config" key.

// egress.OutboundConfig describes the sing-box outbound parameters entered on the
// admin proxy page. It is stored as JSON and rendered into singbox-config.json.

// LoadProxyConfigJSON reads the "proxy_config" Option row, decrypts it if
// encrypted, and returns the plaintext JSON. For backward compatibility, if
// decryption fails the raw value is returned as-is (legacy plaintext stored
// before #141 introduced encryption).
func LoadProxyConfigJSON() (string, error) {
	if dbx.DB == nil {
		return "", fmt.Errorf("database not initialised")
	}
	var option model.Option
	if err := dbx.DB.Where("key = ?", "proxy_config").First(&option).Error; err != nil {
		return "", err
	}
	plain, err := common.DecryptAESGCM(option.Value, "proxy-config")
	if err != nil {
		// Legacy plaintext value — return as-is.
		return option.Value, nil
	}
	return plain, nil
}

// SaveProxyConfigJSON encrypts and persists the proxy config JSON to the
// Option table.
func SaveProxyConfigJSON(plaintext string) error {
	encrypted, err := common.EncryptAESGCM(plaintext, "proxy-config")
	if err != nil {
		return fmt.Errorf("encrypt proxy config: %w", err)
	}
	return model.UpdateOption("proxy_config", encrypted)
}
