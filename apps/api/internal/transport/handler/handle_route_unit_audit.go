package handler

import (
	"net/http"

	"github.com/QuantumNous/new-api/internal/catalog/routestats"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/transport/contract"
)

// GetRouteUnitAudit returns the attempt audit log and share window snapshots.
// Admin auth required.
func GetRouteUnitAudit(c contract.Context) {
	attempts := routestats.SnapshotAttempts()
	shares := routestats.ShareSnapshot()

	payload := common.H{
		"attempts": attempts,
		"shares":   shares,
	}

	data, err := common.Marshal(payload)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.Data(http.StatusOK, "application/json", data)
}
