package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncryptSecretRoundTrip(t *testing.T) {
	plaintext := "apiVersion: v1\nkind: Config\n"
	encrypted, err := EncryptSecret(plaintext)
	require.NoError(t, err)
	// ciphertext must never equal plaintext nor remain discoverable in it
	assert.NotEqual(t, plaintext, encrypted)
	assert.NotContains(t, encrypted, "apiVersion")
	assert.NotContains(t, encrypted, "Config")

	decrypted, err := DecryptSecret(encrypted)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted)
}

func TestEncryptSecretProducesUniqueCiphertexts(t *testing.T) {
	a, err := EncryptSecret("same-secret")
	require.NoError(t, err)
	b, err := EncryptSecret("same-secret")
	require.NoError(t, err)
	// random nonce means two encryptions of the same input must differ
	assert.NotEqual(t, a, b)
	da, err := DecryptSecret(a)
	require.NoError(t, err)
	db, err := DecryptSecret(b)
	require.NoError(t, err)
	assert.Equal(t, "same-secret", da)
	assert.Equal(t, "same-secret", db)
}

func TestEncryptSecretRejectsEmptyPlaintext(t *testing.T) {
	_, err := EncryptSecret("")
	require.Error(t, err)
}

func TestDecryptSecretRejectsTamperedCiphertext(t *testing.T) {
	encrypted, err := EncryptSecret("attack-target")
	require.NoError(t, err)

	// flip a byte in the payload portion of the base64 string
	mutated := []byte(encrypted)
	nonceLen := 16 // 12-byte nonce base64-ish; flip far past it to hit ciphertext+tag
	for i := nonceLen + 4; i < len(mutated) && i < nonceLen+16; i++ {
		if mutated[i] != 'A' {
			mutated[i] = 'A'
			break
		}
	}
	_, err = DecryptSecret(string(mutated))
	require.Error(t, err)
}

func TestDecryptSecretRejectsGarbage(t *testing.T) {
	_, err := DecryptSecret("definitely-not-base64-!!!")
	require.Error(t, err)
	_, err = DecryptSecret("")
	require.Error(t, err)
}