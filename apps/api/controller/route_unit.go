package controller

import (
	"errors"
	"math"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type updateRouteUnitRequest struct {
	StaticWeight *float64 `json:"static_weight"`
	Enabled      *bool    `json:"enabled"`
}

// GetRouteUnitViews returns all route units for a given public model alias.
func GetRouteUnitViews(c *gin.Context) {
	alias := c.Query("alias")
	if alias == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "alias参数不能为空",
		})
		return
	}

	views, err := model.GetRouteUnitViewsByAlias(alias)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	// After F1, expected_share is per-group (static weight within the route's
	// own group). The cross-group aggregate totalWeight was misleading (a channel
	// in both default and vip inflated the denominator). Drop it; the frontend
	// uses unit.expected_share directly.
	common.ApiSuccess(c, gin.H{
		"items": views,
	})
}

// ListRouteUnitAliases returns all distinct public model aliases with their route count and total weight.
func ListRouteUnitAliases(c *gin.Context) {
	summaries, err := model.ListRouteUnitAliases()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, summaries)
}

// UpdateRouteUnit updates a single route unit by ID.
func UpdateRouteUnit(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "无效的路由单元ID",
		})
		return
	}

	var req updateRouteUnitRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "请求参数格式错误",
		})
		return
	}

	var weight *int
	if req.StaticWeight != nil {
		v := *req.StaticWeight
		// Validate: finite, >= 0, integer value.
		if math.IsNaN(v) || math.IsInf(v, 0) {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"message": "static_weight 必须是有效数字",
			})
			return
		}
		if v < 0 {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"message": "static_weight 必须大于等于 0",
			})
			return
		}
		// Check integer (no fractional part)
		if v != math.Floor(v) {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"message": "static_weight 必须是整数",
			})
			return
		}
		// Upper bound: 1e9 (billing safety invariant)
		if v > 1000000000 {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"message": "static_weight 超出允许范围 (0-1000000000)",
			})
			return
		}
		w := int(v)
		weight = &w
	}

	if err := model.UpdateRouteUnitConfig(id, weight, req.Enabled); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"success": false,
				"message": "路由单元不存在",
			})
			return
		}
		common.ApiError(c, err)
		return
	}

	// Re-query the updated row by fetching its alias first, then the view.
	var route model.ChannelModelRoute
	if err := model.DB.First(&route, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// Concurrent deletion: construct minimal response from request params.
			updated := &model.RouteUnitView{
				Id:           id,
				StaticWeight: 0,
				Enabled:      false,
			}
			if weight != nil {
				updated.StaticWeight = *weight
			}
			if req.Enabled != nil {
				updated.Enabled = *req.Enabled
			}
			common.ApiSuccess(c, updated)
			return
		}
		common.ApiError(c, err)
		return
	}
	views, err := model.GetRouteUnitViewsByAlias(route.PublicModelAlias)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var updated *model.RouteUnitView
	for i := range views {
		if views[i].Id == id {
			updated = &views[i]
			break
		}
	}
	if updated == nil {
		// Concurrent deletion after alias lookup: construct minimal response.
		updated = &model.RouteUnitView{
			Id:           id,
			StaticWeight: 0,
			Enabled:      false,
		}
		if weight != nil {
			updated.StaticWeight = *weight
		}
		if req.Enabled != nil {
			updated.Enabled = *req.Enabled
		}
		common.ApiSuccess(c, updated)
		return
	}
	common.ApiSuccess(c, updated)
}
