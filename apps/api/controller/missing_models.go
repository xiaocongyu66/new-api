package controller

import (
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"net/http"

	"github.com/QuantumNous/new-api/model"
)

// GetMissingModels returns the list of model names that are referenced by channels
// but do not have corresponding records in the models meta table.
// This helps administrators quickly discover models that need configuration.
func GetMissingModels(c contract.Context) {
	missing, err := model.GetMissingModels()
	if err != nil {
		_ = c.JSON(http.StatusOK, common.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"data":    missing,
	})
}
