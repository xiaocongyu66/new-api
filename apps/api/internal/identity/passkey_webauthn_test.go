package identity

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/internal/transport/ginadapter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pinPasskeySettings installs a private settings instance and a fixed
// ServerAddress for one test. Both are package-level hooks that internal/security
// and internal/egress overwrite in their init(), and any test in this package
// that links those packages leaves the hooks installed. Reading the ambient
// settings instead would let security's GetPasskeySettings backfill RPID and
// Origins from egress.ServerAddress, which silently replaces the input this
// table is asserting on.
func pinPasskeySettings(t *testing.T, settings PasskeySettings, serverAddress string) {
	t.Helper()
	previousSettings, previousAddress := OnGetPasskeySettings, OnResolveServerAddress
	pinned := settings
	if serverAddress == "" {
		serverAddress = "http://fallback.example.com:7000"
	}
	OnGetPasskeySettings = func() *PasskeySettings { return &pinned }
	OnResolveServerAddress = func() string { return serverAddress }
	t.Cleanup(func() {
		OnGetPasskeySettings, OnResolveServerAddress = previousSettings, previousAddress
	})
}

// passkeyRequestContext builds a contract context whose Host() and IsTLS() carry
// the case's transport facts. httptest.NewRequest attaches a dummy TLS state when
// the target names https, so an absolute https target is a genuine TLS request;
// forceTLS covers a TLS request whose target is in origin form, which is what a
// real server sees.
func passkeyRequestContext(target, host string, emptyHost, forceTLS bool, headers map[string]string) contract.Context {
	request := httptest.NewRequest(http.MethodGet, target, nil)
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	if emptyHost {
		request.Host = ""
	} else if host != "" {
		request.Host = host
	}
	if forceTLS && request.TLS == nil {
		request.TLS = &tls.ConnectionState{}
	}
	context, _ := ginadapter.NewSyntheticContext(request)
	return context
}

// TestBuildWebAuthnResolvesRPFromTransportHostAndTLS is the equivalence table for
// the migration of BuildWebAuthn off *http.Request onto contract Host()/IsTLS().
// Every row states the RP-ID and origins explicitly, because RP-ID and origin
// decide which domain a passkey credential is bound to: a drift here does not
// raise an error, it makes a credential usable from another origin.
func TestBuildWebAuthnResolvesRPFromTransportHostAndTLS(t *testing.T) {
	tests := []struct {
		name       string
		target     string
		host       string
		forceTLS   bool
		headers    map[string]string
		settings   PasskeySettings
		serverAddr string

		wantRPID    string
		wantOrigins []string
		wantErr     string
	}{
		{
			name:        "plain HTTP with a bare host and insecure origins allowed",
			target:      "http://example.com/api/user/passkey/register/begin",
			settings:    PasskeySettings{Enabled: true, AllowInsecureOrigin: true},
			wantRPID:    "example.com",
			wantOrigins: []string{"http://example.com"},
		},
		{
			name:     "plain HTTP with a bare host is refused unless insecure origins are allowed",
			target:   "http://example.com/api/user/passkey/register/begin",
			settings: PasskeySettings{Enabled: true},
			wantErr:  "Passkey 仅支持 HTTPS，当前访问: http://example.com，请在 Passkey 设置中允许不安全 Origin 或配置 HTTPS",
		},
		{
			name:        "HTTPS keeps the port in the origin and drops it from the RP-ID",
			target:      "https://example.com:8443/api/user/passkey/register/begin",
			settings:    PasskeySettings{Enabled: true},
			wantRPID:    "example.com",
			wantOrigins: []string{"https://example.com:8443"},
		},
		{
			name:        "TLS on an origin-form target still resolves as HTTPS",
			target:      "/api/user/passkey/register/begin",
			forceTLS:    true,
			settings:    PasskeySettings{Enabled: true},
			wantRPID:    "example.com",
			wantOrigins: []string{"https://example.com"},
		},

		// The two security rows. IsTLS and Host reflect the transport only, so a
		// client that sets these headers cannot move the credential binding.
		{
			name:     "X-Forwarded-Proto https over a plaintext transport stays non-TLS",
			target:   "/api/user/passkey/register/begin",
			headers:  map[string]string{"X-Forwarded-Proto": "https"},
			settings: PasskeySettings{Enabled: true},
			// Non-TLS with insecure origins disallowed, so this is the HTTPS-only
			// refusal, never an https:// origin minted from the header.
			wantErr: "Passkey 仅支持 HTTPS，当前访问: http://example.com，请在 Passkey 设置中允许不安全 Origin 或配置 HTTPS",
		},
		{
			name:        "X-Forwarded-Proto https over a plaintext transport yields an http origin",
			target:      "/api/user/passkey/register/begin",
			headers:     map[string]string{"X-Forwarded-Proto": "https"},
			settings:    PasskeySettings{Enabled: true, AllowInsecureOrigin: true},
			wantRPID:    "example.com",
			wantOrigins: []string{"http://example.com"},
		},
		{
			name:        "X-Forwarded-Host evil.com never displaces the real host",
			target:      "https://panel.example.com:8443/api/user/passkey/register/begin",
			headers:     map[string]string{"X-Forwarded-Host": "evil.com"},
			settings:    PasskeySettings{Enabled: true},
			wantRPID:    "panel.example.com",
			wantOrigins: []string{"https://panel.example.com:8443"},
		},
		{
			name:     "a forwarded proto and host pair cannot upgrade or relocate the origin",
			target:   "/api/user/passkey/register/begin",
			headers:  map[string]string{"X-Forwarded-Proto": "https", "X-Forwarded-Host": "evil.com"},
			settings: PasskeySettings{Enabled: true},
			wantErr:  "Passkey 仅支持 HTTPS，当前访问: http://example.com，请在 Passkey 设置中允许不安全 Origin 或配置 HTTPS",
		},
		{
			name:        "X-Forwarded-Proto http cannot downgrade a TLS transport",
			target:      "/api/user/passkey/register/begin",
			forceTLS:    true,
			headers:     map[string]string{"X-Forwarded-Proto": "http"},
			settings:    PasskeySettings{Enabled: true},
			wantRPID:    "example.com",
			wantOrigins: []string{"https://example.com"},
		},
		{
			name:        "X-Forwarded-Protocol https over a plaintext transport stays non-TLS",
			target:      "/api/user/passkey/register/begin",
			headers:     map[string]string{"X-Forwarded-Protocol": "https"},
			settings:    PasskeySettings{Enabled: true, AllowInsecureOrigin: true},
			wantRPID:    "example.com",
			wantOrigins: []string{"http://example.com"},
		},

		{
			name:        "configured RP-ID and origins win over the request",
			target:      "https://panel.example.com/api/user/passkey/register/begin",
			settings:    PasskeySettings{Enabled: true, RPID: "example.com:8443", Origins: "https://a.example.com, https://b.example.com"},
			wantRPID:    "example.com",
			wantOrigins: []string{"https://a.example.com", "https://b.example.com"},
		},
		{
			name:        "configured origins derive the RP-ID from the first origin",
			target:      "http://ignored.example.com/api/user/passkey/register/begin",
			settings:    PasskeySettings{Enabled: true, Origins: "https://a.example.com:9443"},
			wantRPID:    "a.example.com",
			wantOrigins: []string{"https://a.example.com:9443"},
		},
		{
			name:     "a configured http origin is refused unless insecure origins are allowed",
			target:   "https://panel.example.com/api/user/passkey/register/begin",
			settings: PasskeySettings{Enabled: true, Origins: "http://a.example.com"},
			wantErr:  "Passkey 不允许使用不安全的 Origin: http://a.example.com",
		},
		{
			name:        "configured origins that collapse to empty fall back to the request",
			target:      "https://panel.example.com:8443/api/user/passkey/register/begin",
			settings:    PasskeySettings{Enabled: true, Origins: " , "},
			wantRPID:    "panel.example.com",
			wantOrigins: []string{"https://panel.example.com:8443"},
		},
		{
			name:        "localhost is exempt from the HTTPS requirement",
			target:      "http://localhost:3000/api/user/passkey/register/begin",
			settings:    PasskeySettings{Enabled: true},
			wantRPID:    "localhost",
			wantOrigins: []string{"http://localhost:3000"},
		},
		{
			name:        "the loopback IP is exempt from the HTTPS requirement",
			target:      "http://127.0.0.1:3000/api/user/passkey/register/begin",
			settings:    PasskeySettings{Enabled: true},
			wantRPID:    "127.0.0.1",
			wantOrigins: []string{"http://127.0.0.1:3000"},
		},
		{
			name:        "an IPv6 host keeps its brackets in the origin",
			target:      "https://[2001:db8::1]:8443/api/user/passkey/register/begin",
			settings:    PasskeySettings{Enabled: true, RPID: "example.com"},
			wantRPID:    "example.com",
			wantOrigins: []string{"https://[2001:db8::1]:8443"},
		},
		{
			// hostWithoutPort strips the brackets, and go-webauthn then rejects the
			// bare IPv6 literal as an RP-ID. Recorded because it is pre-existing
			// behaviour the migration must not change, not because it is desirable.
			name:     "an IPv6 host without a configured RP-ID is rejected by go-webauthn",
			target:   "https://[2001:db8::1]:8443/api/user/passkey/register/begin",
			settings: PasskeySettings{Enabled: true},
			wantErr:  `error occurred validating the configuration: field 'RPID' is not a valid URI: parse "2001:db8::1": first path segment in URL cannot contain colon`,
		},
		{
			name:        "an empty host falls back to the configured ServerAddress",
			target:      "/api/user/passkey/register/begin",
			host:        emptyHostSentinel,
			serverAddr:  "http://fallback.example.com:7000",
			settings:    PasskeySettings{Enabled: true, AllowInsecureOrigin: true},
			wantRPID:    "fallback.example.com",
			wantOrigins: []string{"http://fallback.example.com:7000"},
		},
		{
			// The scheme comes from the transport, so an https ServerAddress does not
			// make a plaintext request's origin https.
			name:        "the ServerAddress supplies the host but never the scheme",
			target:      "/api/user/passkey/register/begin",
			host:        emptyHostSentinel,
			serverAddr:  "https://fallback.example.com",
			settings:    PasskeySettings{Enabled: true, AllowInsecureOrigin: true},
			wantRPID:    "fallback.example.com",
			wantOrigins: []string{"http://fallback.example.com"},
		},
		{
			name:        "a display name and user verification preference do not affect the RP",
			target:      "https://panel.example.com/api/user/passkey/register/begin",
			settings:    PasskeySettings{Enabled: true, RPDisplayName: "Panel", UserVerification: "required"},
			wantRPID:    "panel.example.com",
			wantOrigins: []string{"https://panel.example.com"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pinPasskeySettings(t, test.settings, test.serverAddr)

			emptyHost := test.host == emptyHostSentinel
			context := passkeyRequestContext(test.target, test.host, emptyHost, test.forceTLS, test.headers)
			if emptyHost {
				require.Empty(t, context.Host(), "the case needs an empty transport host")
			}

			wa, err := BuildWebAuthn(context)
			if test.wantErr != "" {
				require.Error(t, err)
				assert.Equal(t, test.wantErr, err.Error())
				return
			}
			require.NoError(t, err)
			assert.Equal(t, test.wantRPID, wa.Config.RPID)
			assert.Equal(t, test.wantOrigins, wa.Config.RPOrigins)
		})
	}
}

// emptyHostSentinel marks a case that needs a request with no Host at all; the
// zero value already means "leave the target's host alone".
const emptyHostSentinel = "\x00empty"

// TestBuildWebAuthnIgnoresForwardedHeadersEntirely is the mutation guard for the
// two security rows above. It asserts the outcome is byte-identical with and
// without the forwarded headers, so reintroducing any X-Forwarded-* read into the
// passkey RP derivation fails here even if someone also updates the table rows.
func TestBuildWebAuthnIgnoresForwardedHeadersEntirely(t *testing.T) {
	spoofs := []map[string]string{
		{"X-Forwarded-Proto": "https"},
		{"X-Forwarded-Proto": "https, http"},
		{"X-Forwarded-Protocol": "https"},
		{"X-Forwarded-Ssl": "on"},
		{"Forwarded": "proto=https;host=evil.com"},
		{"X-Forwarded-Host": "evil.com"},
		{"X-Forwarded-Proto": "https", "X-Forwarded-Host": "evil.com"},
	}

	for _, tlsTransport := range []bool{false, true} {
		settings := PasskeySettings{Enabled: true, AllowInsecureOrigin: true}

		pinPasskeySettings(t, settings, "")
		baseline := passkeyRequestContext("/api/user/passkey/register/begin", "", false, tlsTransport, nil)
		baselineWA, err := BuildWebAuthn(baseline)
		require.NoError(t, err)

		for _, headers := range spoofs {
			pinPasskeySettings(t, settings, "")
			spoofed := passkeyRequestContext("/api/user/passkey/register/begin", "", false, tlsTransport, headers)
			spoofedWA, err := BuildWebAuthn(spoofed)
			require.NoError(t, err)

			assert.Equal(t, baselineWA.Config.RPID, spoofedWA.Config.RPID,
				"forwarded headers %v must not change the RP-ID (tls=%v)", headers, tlsTransport)
			assert.Equal(t, baselineWA.Config.RPOrigins, spoofedWA.Config.RPOrigins,
				"forwarded headers %v must not change the origins (tls=%v)", headers, tlsTransport)
		}
	}
}

// TestBuildWebAuthnRejectsTheHistoricalForwardedProtoBypass records the exact
// pre-migration boundary: detectScheme read X-Forwarded-Proto before r.TLS, so
// this plaintext request previously minted https://example.com. The contract
// reports the transport instead, and BuildWebAuthn therefore enforces HTTPS.
func TestBuildWebAuthnRejectsTheHistoricalForwardedProtoBypass(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/user/passkey/register/begin", nil)
	request.Header.Set("X-Forwarded-Proto", "https")
	assert.Equal(t, "https", historicalDetectScheme(request), "the removed request-based logic accepted the spoofed header")

	pinPasskeySettings(t, PasskeySettings{Enabled: true}, "")
	context, _ := ginadapter.NewSyntheticContext(request)
	assert.False(t, context.IsTLS(), "the request arrived over plaintext")
	_, err := BuildWebAuthn(context)
	require.EqualError(t, err, "Passkey 仅支持 HTTPS，当前访问: http://example.com，请在 Passkey 设置中允许不安全 Origin 或配置 HTTPS")
}

// historicalDetectScheme is the narrow pre-migration rule relevant to the
// regression above. It intentionally lives only in the test as evidence of the
// removed unsafe boundary, not as a production fallback.
func historicalDetectScheme(request *http.Request) string {
	if proto := request.Header.Get("X-Forwarded-Proto"); proto != "" {
		return strings.ToLower(strings.TrimSpace(strings.Split(proto, ",")[0]))
	}
	if request.TLS != nil {
		return "https"
	}
	return "http"
}
