package middleware

import (
	"fmt"
	"github.com/QuantumNous/new-api/internal/security"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

type headerNavAccess struct {
	Enabled     bool
	RequireAuth bool
}

func getHeaderNavAccess(module string) headerNavAccess {
	fallback := headerNavAccess{
		Enabled:     true,
		RequireAuth: false,
	}

	common.OptionMapRWMutex.RLock()
	raw := common.OptionMap["HeaderNavModules"]
	common.OptionMapRWMutex.RUnlock()

	if strings.TrimSpace(raw) == "" {
		return fallback
	}

	var parsed map[string]any
	if err := common.Unmarshal([]byte(raw), &parsed); err != nil {
		return fallback
	}

	return parseHeaderNavAccess(parsed[module], fallback)
}

func parseHeaderNavAccess(raw any, fallback headerNavAccess) headerNavAccess {
	switch value := raw.(type) {
	case bool:
		return headerNavAccess{
			Enabled:     value,
			RequireAuth: fallback.RequireAuth,
		}
	case string:
		return headerNavAccess{
			Enabled:     parseHeaderNavBool(value, fallback.Enabled),
			RequireAuth: fallback.RequireAuth,
		}
	case float64:
		return headerNavAccess{
			Enabled:     parseHeaderNavBool(value, fallback.Enabled),
			RequireAuth: fallback.RequireAuth,
		}
	case map[string]any:
		access := fallback
		if enabled, ok := value["enabled"]; ok {
			access.Enabled = parseHeaderNavBool(enabled, fallback.Enabled)
		}
		if requireAuth, ok := value["requireAuth"]; ok {
			access.RequireAuth = parseHeaderNavBool(requireAuth, fallback.RequireAuth)
		}
		return access
	default:
		return fallback
	}
}

func parseHeaderNavBool(value any, fallback bool) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true", "1":
			return true
		case "false", "0":
			return false
		default:
			return fallback
		}
	case float64:
		if v == 1 {
			return true
		}
		if v == 0 {
			return false
		}
		return fallback
	case int:
		if v == 1 {
			return true
		}
		if v == 0 {
			return false
		}
		return fallback
	default:
		return fallback
	}
}

func HeaderNavModuleAuth(module string) contract.Middleware {
	return func(c contract.Context) {
		access := getHeaderNavAccess(module)
		if !access.Enabled {
			_ = c.JSON(http.StatusForbidden, common.H{
				"success": false,
				"message": fmt.Sprintf("%s is disabled", module),
			})
			c.Abort()
			return
		}

		if access.RequireAuth {
			security.UserAuth()(c)
			return
		}

		security.TryUserAuth()(c)
	}
}

func HeaderNavModulePublicOrUserAuth(module string) contract.Middleware {
	return func(c contract.Context) {
		access := getHeaderNavAccess(module)
		if !access.Enabled || access.RequireAuth {
			security.UserAuth()(c)
			return
		}

		security.TryUserAuth()(c)
	}
}
