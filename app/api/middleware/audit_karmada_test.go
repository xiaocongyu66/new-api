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
