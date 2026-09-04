package identity_test

// identity declares two seams it cannot fill itself and calls both without a nil
// guard, so an unregistered seam is a panic on a live route rather than a
// degraded feature:
//
//   - redeemKey     -> billing.Redeem, used by POST /api/user/topup
//   - oauthRegistrar -> the oauth registry, used by every custom OAuth admin route
//
// Both are registered from the owning side's init(). This test lives in an
// external test package so it can import those packages (identity cannot: they
// import it) and therefore link exactly what the production binary links.

import (
	"testing"

	"github.com/QuantumNous/new-api/internal/billing"
	"github.com/QuantumNous/new-api/internal/identity"
	"github.com/QuantumNous/new-api/internal/security/oauth"
)

// Keep the imports load-bearing: referencing a symbol from each package is what
// forces its init() to run, which is the thing under test.
var (
	_ = billing.Redeem
	_ = oauth.IsProviderRegistered
)

func TestRedemptionRedeemerRegistered(t *testing.T) {
	// Redeem rejects an empty key before touching the database, so this reaches
	// the hook without needing one. A nil hook panics instead of returning.
	quota, err := identity.RedeemForTest("", 1)
	if err == nil {
		t.Fatal("expected an error for an empty redemption key")
	}
	if quota != 0 {
		t.Errorf("quota = %d, want 0 on a rejected redemption", quota)
	}
}

func TestCustomOAuthRegistrarRegistered(t *testing.T) {
	const slug = "registrar-wiring-probe"
	t.Cleanup(func() { oauth.Unregister(slug) })

	if identity.OAuthRegistrarForTest() == nil {
		t.Fatal("identity's custom OAuth registrar is nil: every custom OAuth admin route would panic")
	}

	registrar := identity.OAuthRegistrarForTest()
	if registrar.IsProviderRegistered(slug) {
		t.Fatalf("provider %q was already registered before the test ran", slug)
	}
	registrar.RegisterOrUpdateCustomProvider(&identity.CustomOAuthProvider{
		Slug:                  slug,
		Name:                  "Registrar Wiring Probe",
		ClientId:              "probe-client",
		AuthorizationEndpoint: "https://example.invalid/authorize",
		TokenEndpoint:         "https://example.invalid/token",
		UserInfoEndpoint:      "https://example.invalid/userinfo",
	})
	if !registrar.IsProviderRegistered(slug) {
		t.Fatal("registrar did not register the provider: identity and the oauth registry are not connected")
	}
	if !registrar.IsCustomProvider(slug) {
		t.Fatal("registrar did not mark the provider custom, so it could never be unregistered")
	}
	registrar.UnregisterCustomProvider(slug)
	if registrar.IsProviderRegistered(slug) {
		t.Fatal("registrar did not unregister the provider")
	}
}
