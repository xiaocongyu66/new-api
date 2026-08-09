package service

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// assertSocks5Proxied verifies the client carries a SOCKS5-configured
// transport. configureProxyTransport sets transport.Proxy to nil and installs
// the SOCKS dialer for socks5/socks5h, as opposed to a direct transport whose
// Proxy field is non-nil (http.ProxyFromEnvironment).
func assertSocks5Proxied(t *testing.T, client *http.Client) {
	t.Helper()
	transport, ok := client.Transport.(*http.Transport)
	require.True(t, ok, "expected a single *http.Transport for the default policy")
	assert.Nil(t, transport.Proxy, "socks5 proxy client must not use the environment proxy")
	assert.NotNil(t, transport.DialContext, "socks5 proxy client must install the SOCKS dialer")
}

func TestGetHttpClientWithProxySettings(t *testing.T) {
	const globalProxyURL = "socks5://127.0.0.1:1080"
	const channelProxyURL = "socks5://proxy.example:1080"

	initDefaultHTTPClientFixture(t)

	t.Run("bypass proxy ignores channel and global proxy and connects directly", func(t *testing.T) {
		db := useProxyConfigTestDB(t)
		saveProxyConfigOption(t, db, ProxyConfig{GlobalProxyURL: globalProxyURL, Enabled: true})

		client, err := GetHttpClientWithProxySettings(channelProxyURL, dto.ChannelSettings{BypassProxy: true})
		require.NoError(t, err)
		assert.Same(t, GetHttpClient(), client, "BypassProxy must ignore the channel and global proxy and share the direct pool")
	})

	t.Run("empty proxy falls back to the enabled global proxy", func(t *testing.T) {
		db := useProxyConfigTestDB(t)
		saveProxyConfigOption(t, db, ProxyConfig{GlobalProxyURL: globalProxyURL, Enabled: true})

		client, err := GetHttpClientWithProxySettings("", dto.ChannelSettings{})
		require.NoError(t, err)
		assert.NotSame(t, GetHttpClient(), client, "empty channel proxy with global enabled must return the proxied client")
		assertSocks5Proxied(t, client)
	})

	t.Run("empty proxy with disabled global proxy connects directly", func(t *testing.T) {
		db := useProxyConfigTestDB(t)
		saveProxyConfigOption(t, db, ProxyConfig{GlobalProxyURL: globalProxyURL, Enabled: false})

		client, err := GetHttpClientWithProxySettings("", dto.ChannelSettings{})
		require.NoError(t, err)
		assert.Same(t, GetHttpClient(), client, "disabled global proxy must fall back to the direct pool")
	})

	t.Run("empty proxy without global proxy configured connects directly", func(t *testing.T) {
		useProxyConfigTestDB(t) // no proxy_config row

		client, err := GetHttpClientWithProxySettings("", dto.ChannelSettings{})
		require.NoError(t, err)
		assert.Same(t, GetHttpClient(), client, "unset global proxy must fall back to the direct pool")
	})

	t.Run("non-empty channel proxy uses the channel proxy", func(t *testing.T) {
		useProxyConfigTestDB(t) // no global proxy

		client, err := GetHttpClientWithProxySettings(channelProxyURL, dto.ChannelSettings{})
		require.NoError(t, err)
		assert.NotSame(t, GetHttpClient(), client, "non-empty channel proxy must not return the direct client")
		assertSocks5Proxied(t, client)
	})

	t.Run("invalid proxy URL returns an error", func(t *testing.T) {
		useProxyConfigTestDB(t) // no global proxy

		client, err := GetHttpClientWithProxySettings("ftp://proxy:1080", dto.ChannelSettings{})
		require.Error(t, err)
		assert.Nil(t, client)
	})

	t.Run("http proxy uses the http proxy client", func(t *testing.T) {
		useProxyConfigTestDB(t) // no global proxy

		client, err := GetHttpClientWithProxySettings("http://proxy.example:3128", dto.ChannelSettings{})
		require.NoError(t, err)
		assert.NotSame(t, GetHttpClient(), client, "http channel proxy must not return the direct client")
		transport, ok := client.Transport.(*http.Transport)
		require.True(t, ok, "expected a single *http.Transport for the default policy")
		assert.NotNil(t, transport.Proxy, "http proxy client must use the http.Proxy function")
	})
}
