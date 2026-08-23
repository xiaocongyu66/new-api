package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/service"
)

// GetRankings serves the usage ranking snapshot.
//
// It takes the framework-neutral transport contract rather than *gin.Context, so
// replacing the HTTP framework does not touch this handler.
func GetRankings(c contract.Context) {
	result, err := service.GetRankingsSnapshot(c.DefaultQuery("period", "week"))
	if err != nil {
		_ = c.JSON(http.StatusBadRequest, map[string]any{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	_ = c.JSON(http.StatusOK, map[string]any{
		"success": true,
		"data":    result,
	})
}
