package egress

import (
	"strconv"

	"github.com/QuantumNous/new-api/internal/identity"
	"github.com/QuantumNous/new-api/internal/settings"
)

// Server address and worker options belong to this domain; settings keeps only
// the generic option mechanism and reaches them through these nil-safe hooks
// (same convention as catalog/resolve_group.go).
func seedEgressOptions() map[string]string {
	return map[string]string{
		"WorkerUrl":                          WorkerUrl,
		"WorkerValidKey":                     WorkerValidKey,
		"WorkerAllowHttpImageRequestEnabled": strconv.FormatBool(WorkerAllowHttpImageRequestEnabled),
	}
}

func applyEgressOption(key, value string) error {
	switch key {
	case "ServerAddress":
		ServerAddress = value
	case "WorkerUrl":
		WorkerUrl = value
	case "WorkerValidKey":
		WorkerValidKey = value
	case "WorkerAllowHttpImageRequestEnabled":
		WorkerAllowHttpImageRequestEnabled = value == "true"
	}
	return nil
}

func init() {
	settings.OnSeedEgressOptions = seedEgressOptions
	settings.OnApplyEgressOption = applyEgressOption

	// identity builds absolute links from ServerAddress but cannot import this
	// package, so it reads the value through this hook. Registered here rather
	// than from main so every binary linking egress gets the real value.
	identity.OnResolveServerAddress = func() string { return ServerAddress }
}
