package oauth

import (
	"sync"

	"github.com/QuantumNous/new-api/modelapi"
)

var (
	providers = make(map[string]Provider)
	mu        sync.RWMutex
)

// Register registers an OAuth provider with the given name
func Register(name string, provider Provider) {}

// RegisterCustom registers a custom OAuth provider (can be unregistered later)
func RegisterCustom(name string, provider Provider) {}

// Unregister removes a provider from the registry
func Unregister(name string) {}

// GetProvider returns the OAuth provider for the given name
func GetProvider(name string) Provider { return nil }

// GetAllProviders returns all registered OAuth providers
func GetAllProviders() map[string]Provider { return nil }

// GetEnabledCustomProviders returns all enabled custom OAuth providers
func GetEnabledCustomProviders() []*GenericOAuthProvider { return nil }

// IsProviderRegistered checks if a provider is registered
func IsProviderRegistered(name string) bool { return false }

// IsCustomProvider checks if a provider is a custom provider
func IsCustomProvider(name string) bool { return false }

// LoadCustomProviders loads all custom OAuth providers from the database
func LoadCustomProviders() error { return nil }

// ReloadCustomProviders reloads all custom OAuth providers from the database
func ReloadCustomProviders() error { return nil }

// RegisterOrUpdateCustomProvider registers or updates a single custom provider
func RegisterOrUpdateCustomProvider(config *modelapi.CustomOAuthProvider) {}

// UnregisterCustomProvider unregisters a custom provider by slug
func UnregisterCustomProvider(slug string) {}