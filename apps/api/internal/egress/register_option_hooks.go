package egress

import (
	"github.com/QuantumNous/new-api/internal/identity"
	"github.com/QuantumNous/new-api/internal/settings"
)

// ServerAddress belongs to this domain; settings keeps only the generic option
// mechanism and reaches it through these nil-safe hooks (same convention as
// catalog/resolve_group.go).
func applyEgressOption(key, value string) error {
	if key == "ServerAddress" {
		ServerAddress = value
	}
	return nil
}

func init() {
	settings.OnApplyEgressOption = applyEgressOption

	// identity builds absolute links from ServerAddress but cannot import this
	// package, so it reads the value through this hook. Registered here rather
	// than from main so every binary linking egress gets the real value.
	identity.OnResolveServerAddress = func() string { return ServerAddress }
}
