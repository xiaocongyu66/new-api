package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/service/authz"
)

// GetPermissionCatalog returns the permission schema used by the client to
// render the permission editor: the registry of resources with their actions
// and display label keys, plus the roles with their baseline grant matrices.
// Defining it in the authz package keeps the schema in a single place.
//
// It takes the framework-neutral transport contract rather than *gin.Context, so
// replacing the HTTP framework does not touch this handler.
func GetPermissionCatalog(c contract.Context) {
	_ = c.JSON(http.StatusOK, map[string]any{
		"success": true,
		"message": "",
		"data": map[string]any{
			"resources": authz.Catalog(),
			"roles":     authz.Roles(),
		},
	})
}
