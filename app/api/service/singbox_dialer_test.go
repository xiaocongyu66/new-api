package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildOptionsJSONSSH(t *testing.T) {
	t.Run("password auth", func(t *testing.T) {
		input := `{"type":"ssh","server":"example.com","server_port":22,"username":"root","password":"secret"}`
		out, err := buildOptionsJSON([]byte(input))
		require.NoError(t, err)

		var result map[string]any
		require.NoError(t, common.Unmarshal(out, &result))
		outbounds := result["outbounds"].([]any)
		ob := outbounds[0].(map[string]any)

		assert.Equal(t, "ssh", ob["type"])
		assert.Equal(t, "example.com", ob["server"])
		assert.Equal(t, float64(22), ob["server_port"])
		assert.Equal(t, "root", ob["user"])
		assert.Equal(t, "secret", ob["password"])
		// private_key must NOT be set for password auth
		_, hasPrivateKey := ob["private_key"]
		assert.False(t, hasPrivateKey, "private_key should not be set for password auth")
	})

	t.Run("key auth", func(t *testing.T) {
		input := `{"type":"ssh","server":"example.com","server_port":22,"username":"root","private_key":"-----BEGIN OPENSSH PRIVATE KEY-----\nfake\n-----END OPENSSH PRIVATE KEY-----"}`
		out, err := buildOptionsJSON([]byte(input))
		require.NoError(t, err)

		var result map[string]any
		require.NoError(t, common.Unmarshal(out, &result))
		outbounds := result["outbounds"].([]any)
		ob := outbounds[0].(map[string]any)

		assert.Equal(t, "ssh", ob["type"])
		assert.Equal(t, "root", ob["user"])
		assert.Contains(t, ob["private_key"], "BEGIN OPENSSH PRIVATE KEY")
		// password must NOT be set for key auth
		_, hasPassword := ob["password"]
		assert.False(t, hasPassword, "password should not be set for key auth")
	})
}

func TestBuildOptionsJSONSocks5UsernamePassword(t *testing.T) {
	input := `{"type":"socks5","server":"example.com","server_port":1080,"username":"user","password":"pass"}`
	out, err := buildOptionsJSON([]byte(input))
	require.NoError(t, err)

	var result map[string]any
	require.NoError(t, common.Unmarshal(out, &result))
	outbounds := result["outbounds"].([]any)
	ob := outbounds[0].(map[string]any)

	assert.Equal(t, "socks", ob["type"])
	assert.Equal(t, "user", ob["username"])
	assert.Equal(t, "pass", ob["password"])
}

func TestBuildOptionsJSONHTTPUsernamePassword(t *testing.T) {
	input := `{"type":"http","server":"example.com","server_port":8080,"username":"user","password":"pass"}`
	out, err := buildOptionsJSON([]byte(input))
	require.NoError(t, err)

	var result map[string]any
	require.NoError(t, common.Unmarshal(out, &result))
	outbounds := result["outbounds"].([]any)
	ob := outbounds[0].(map[string]any)

	assert.Equal(t, "http", ob["type"])
	assert.Equal(t, "user", ob["username"])
	assert.Equal(t, "pass", ob["password"])
}
