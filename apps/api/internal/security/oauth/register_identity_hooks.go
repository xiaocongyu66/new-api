package oauth

import "github.com/QuantumNous/new-api/internal/identity"

// customOAuthRegistrar adapts this package's registry to the interface identity
// declares. identity owns the CustomOAuthProvider record and the admin handlers,
// but cannot import this package (this package imports identity), so the
// registry is injected as a hook.
//
// Without this registration identity.oauthRegistrar stays nil and every custom
// OAuth provider admin route — create, update, delete — panics on a nil
// interface call.
type customOAuthRegistrar struct{}

func (customOAuthRegistrar) IsProviderRegistered(slug string) bool { return IsProviderRegistered(slug) }

func (customOAuthRegistrar) IsCustomProvider(slug string) bool { return IsCustomProvider(slug) }

func (customOAuthRegistrar) RegisterOrUpdateCustomProvider(config *identity.CustomOAuthProvider) {
	RegisterOrUpdateCustomProvider(config)
}

func (customOAuthRegistrar) UnregisterCustomProvider(slug string) { UnregisterCustomProvider(slug) }

func init() {
	identity.RegisterCustomOAuthRegistrar(customOAuthRegistrar{})
}
