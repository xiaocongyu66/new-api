package middleware

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestKarmadaResourceWriteActionsAreRegisteredForAudit(t *testing.T) {
	assert.Equal(t, "karmada.resource_scale",
		auditRouteActions["PUT /api/karmada/resources/:kind/:namespace/:name/scale"])
	assert.Equal(t, "karmada.resource_delete",
		auditRouteActions["DELETE /api/karmada/resources/:kind/:namespace/:name"])
	assert.Equal(t, "karmada.resource_delete",
		auditRouteActions["DELETE /api/karmada/resources/:kind/:namespace"])
}

func TestKarmadaPolicyWriteActionsAreRegisteredForAudit(t *testing.T) {
	assert.Equal(t, "karmada.policy_create", auditRouteActions["POST /api/karmada/policies"])
	assert.Equal(t, "karmada.policy_update", auditRouteActions["PUT /api/karmada/policies/:type/:name"])
	assert.Equal(t, "karmada.policy_update", auditRouteActions["PUT /api/karmada/policies/:type/namespaces/:namespace/:name"])
	assert.Equal(t, "karmada.policy_delete", auditRouteActions["DELETE /api/karmada/policies/:type/:name"])
	assert.Equal(t, "karmada.policy_delete", auditRouteActions["DELETE /api/karmada/policies/:type/namespaces/:namespace/:name"])
}
