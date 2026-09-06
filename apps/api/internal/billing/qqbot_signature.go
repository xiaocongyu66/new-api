package billing

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"strings"
)

// deriveKey 按官方规则从 AppSecret 派生 Ed25519 密钥对
// 规则：将 secret 重复拼接直到长度 >= ed25519.SeedSize(32)，然后截断到 32 字节作为 seed
func deriveKey(botSecret string) (ed25519.PrivateKey, error) {
	if botSecret == "" {
		return nil, errors.New("AppSecret 为空")
	}
	seed := botSecret
	for len(seed) < ed25519.SeedSize {
		seed = strings.Repeat(seed, 2)
	}
	seed = seed[:ed25519.SeedSize]
	return ed25519.NewKeyFromSeed([]byte(seed)), nil
}

// SignValidation 处理回调地址验证（op=13）
// 对 event_ts + plain_token 进行签名，返回 hex 编码结果
func SignValidation(botSecret, eventTs, plainToken string) (string, error) {
	privateKey, err := deriveKey(botSecret)
	if err != nil {
		return "", err
	}
	var msg bytes.Buffer
	msg.WriteString(eventTs)
	msg.WriteString(plainToken)
	signature := ed25519.Sign(privateKey, msg.Bytes())
	return hex.EncodeToString(signature), nil
}

// VerifySignature 校验事件推送签名
// 签名内容为 timestamp + body，签名值来自 X-Signature-Ed25519 头
func VerifySignature(botSecret, signatureHex, timestamp string, body []byte) error {
	if signatureHex == "" {
		return errors.New("缺少签名头 X-Signature-Ed25519")
	}
	if timestamp == "" {
		return errors.New("缺少时间戳头 X-Signature-Timestamp")
	}

	privateKey, err := deriveKey(botSecret)
	if err != nil {
		return err
	}
	publicKey := privateKey.Public().(ed25519.PublicKey)

	signature, err := hex.DecodeString(signatureHex)
	if err != nil {
		return errors.New("签名格式非法")
	}
	if len(signature) != ed25519.SignatureSize {
		return errors.New("签名长度非法")
	}
	// Ed25519 签名的高位标识位必须为 0
	if signature[63]&224 != 0 {
		return errors.New("签名校验失败")
	}

	var msg bytes.Buffer
	msg.WriteString(timestamp)
	msg.Write(body)

	if !ed25519.Verify(publicKey, msg.Bytes(), signature) {
		return errors.New("签名校验失败")
	}
	return nil
}
