package handler

import (
	usage "github.com/QuantumNous/new-api/internal/usage"
	"net/http"

	"github.com/QuantumNous/new-api/internal/transport/contract"
)

func GetRankings(c contract.Context) {
	result, err := usage.GetRankingsSnapshot(c.DefaultQuery("period", "week"))
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
