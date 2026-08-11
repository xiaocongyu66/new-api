package service

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

type ProxyNodeInput struct {
	Name       string
	Enabled    bool
	Proxy      string
	ScopeType  string
	ScopeValue string
}

func CreateProxyNode(input ProxyNodeInput) (*model.ProxyNode, error) {
	parsed, err := ParseProxyNodeShareLink(input.Proxy)
	if err != nil {
		return nil, err
	}
	scopeType, scopeValue, err := model.NormalizeProxyNodeScope(input.ScopeType, input.ScopeValue)
	if err != nil {
		return nil, err
	}
	encrypted, err := encryptProxyNodeConfig(parsed.CanonicalInput)
	if err != nil {
		return nil, err
	}
	node := &model.ProxyNode{
		Name:                 strings.TrimSpace(input.Name),
		Enabled:              input.Enabled,
		EncryptedProxyConfig: encrypted,
		Protocol:             parsed.Protocol,
		ScopeType:            scopeType,
		ScopeValue:           scopeValue,
		Health:               1,
	}
	if node.Name == "" {
		return nil, fmt.Errorf("proxy node name must not be empty")
	}
	if err := model.DB.Create(node).Error; err != nil {
		return nil, err
	}
	return node, nil
}

func DecryptProxyNodeConfig(node *model.ProxyNode) (*ProxyNodeParsed, error) {
	if node == nil {
		return nil, fmt.Errorf("proxy node is nil")
	}
	raw, err := decryptProxyNodeConfig(node.EncryptedProxyConfig)
	if err != nil {
		return nil, err
	}
	return ParseProxyNodeShareLink(raw)
}

func encryptProxyNodeConfig(value string) (string, error) {
	block, err := aes.NewCipher(proxyNodeEncryptionKey())
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(value), nil)
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

func decryptProxyNodeConfig(value string) (string, error) {
	encoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return "", fmt.Errorf("decode proxy node configuration: %w", err)
	}
	block, err := aes.NewCipher(proxyNodeEncryptionKey())
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(encoded) < gcm.NonceSize() {
		return "", fmt.Errorf("proxy node configuration is truncated")
	}
	plain, err := gcm.Open(nil, encoded[:gcm.NonceSize()], encoded[gcm.NonceSize():], nil)
	if err != nil {
		return "", fmt.Errorf("decrypt proxy node configuration: %w", err)
	}
	return string(plain), nil
}

func proxyNodeEncryptionKey() []byte {
	sum := sha256.Sum256([]byte("proxy-node:" + common.CryptoSecret))
	return sum[:]
}

func ProxyNodeChannelScopeValue(channelID int) string {
	return strconv.Itoa(channelID)
}
