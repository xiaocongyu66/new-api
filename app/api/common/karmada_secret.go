package common

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
)

// karmadaSecretKey derives a fixed 32-byte AES-256 key from CryptoSecret.
// CryptoSecret is the existing process-wide secret (env CRYPTO_SECRET) that
// already backs the HMAC helpers in crypto.go.
func karmadaSecretKey() []byte {
	sum := sha256.Sum256([]byte(CryptoSecret))
	return sum[:]
}

// EncryptSecret encrypts a plaintext string with AES-256-GCM keyed from
// CryptoSecret and returns a base64 string of nonce||ciphertext. The same
// CryptoSecret is required to decrypt.
func EncryptSecret(plaintext string) (string, error) {
	if plaintext == "" {
		return "", errors.New("karmada: empty plaintext")
	}
	block, err := aes.NewCipher(karmadaSecretKey())
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
	ciphertext := gcm.Seal(nil, nonce, []byte(plaintext), nil)
	out := make([]byte, 0, len(nonce)+len(ciphertext))
	out = append(out, nonce...)
	out = append(out, ciphertext...)
	return base64.StdEncoding.EncodeToString(out), nil
}

// DecryptSecret reverses EncryptSecret. It fails on tampering (GCM auth tag)
// or a CryptoSecret mismatch.
func DecryptSecret(encoded string) (string, error) {
	if encoded == "" {
		return "", errors.New("karmada: empty ciphertext")
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(karmadaSecretKey())
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("karmada: ciphertext too short")
	}
	nonce, ciphertext := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}
