package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/routestats"

	"github.com/gin-gonic/gin"
)

// GetRouteUnitAudit returns the attempt audit log and share window snapshots.
// Admin auth required.
func GetRouteUnitAudit(c *gin.Context) {
	attempts := routestats.SnapshotAttempts()
	shares := routestats.ShareSnapshot()

	payload := gin.H{
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