package controller

import (
	"net/http"


	"github.com/gin-gonic/gin"
	catalogmodel "github.com/QuantumNous/new-api/internal/catalog/model"
)

// GetMissingModels returns the list of model names that are referenced by channels
// but do not have corresponding records in the models meta table.
// This helps administrators quickly discover models that need configuration.
func GetMissingModels(c *gin.Context) {
	missing, err := catalogmodel.GetMissingModels()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    missing,
	})
}
