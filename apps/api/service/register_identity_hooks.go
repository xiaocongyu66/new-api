package service

import "github.com/QuantumNous/new-api/internal/identity"

// The catalog lookup reads channel records, so identity cannot import this
// package without reversing the dependency. It exposes a hook instead.
func init() {
	identity.RegisterGroupModelsResolver(GetGroupsEnabledModels)
}
