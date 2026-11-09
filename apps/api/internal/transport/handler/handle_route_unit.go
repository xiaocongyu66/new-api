package handler

import (
	"errors"
	"math"
	"net/http"
	"strconv"

	channelpkg "github.com/QuantumNous/new-api/internal/catalog"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/transport/contract"

	"gorm.io/gorm"
)

type updateRouteUnitRequest struct {
	StaticWeight *float64 `json:"static_weight"`
	Enabled      *bool    `json:"enabled"`
}

// GetRouteUnitViews returns all route units for a given public model alias.
func GetRouteUnitViews(c contract.Context) {
	alias := c.Query("alias")
	if alias == "" {
		c.JSON(http.StatusBadRequest, common.H{
			"success": false,
			"message": "alias参数不能为空",
		})
		return
	}

	views, err := channelpkg.GetRouteUnitViewsByAlias(alias)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	// After F1, expected_share is per-group (static weight within the route's
	// own group). The cross-group aggregate totalWeight was misleading (a channel
	// in both default and vip inflated the denominator). Drop it; the frontend
	// uses unit.expected_share directly.
	common.ApiSuccess(c, common.H{
		"items": views,
	})
}

// ListRouteUnitAliases returns all distinct public model aliases with their route count and total weight.
func ListRouteUnitAliases(c contract.Context) {
	summaries, err := channelpkg.ListRouteUnitAliases()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, summaries)
}

// UpdateRouteUnit updates a single route unit by ID.
func UpdateRouteUnit(c contract.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, common.H{
			"success": false,
			"message": "无效的路由单元ID",
		})
		return
	}

	var req updateRouteUnitRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, common.H{
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
			c.JSON(http.StatusBadRequest, common.H{
				"success": false,
				"message": "static_weight 必须是有效数字",
			})
			return
		}
		if v < 0 {
			c.JSON(http.StatusBadRequest, common.H{
				"success": false,
				"message": "static_weight 必须大于等于 0",
			})
			return
		}
		// Check integer (no fractional part)
		if v != math.Floor(v) {
			c.JSON(http.StatusBadRequest, common.H{
				"success": false,
				"message": "static_weight 必须是整数",
			})
			return
		}
		// Upper bound: 1e9 (billing safety invariant)
		if v > 1000000000 {
			c.JSON(http.StatusBadRequest, common.H{
				"success": false,
				"message": "static_weight 超出允许范围 (0-1000000000)",
			})
			return
		}
		w := int(v)
		weight = &w
	}

	if err := channelpkg.UpdateRouteUnitConfig(id, weight, req.Enabled); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, common.H{
				"success": false,
				"message": "路由单元不存在",
			})
			return
		}
		common.ApiError(c, err)
		return
	}

	// Re-query the updated row by fetching its alias first, then the view.
	var route channelpkg.ChannelModelRoute
	if err := dbx.DB.First(&route, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// Concurrent deletion: construct minimal response from request params.
			updated := &channelpkg.RouteUnitView{
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
	views, err := channelpkg.GetRouteUnitViewsByAlias(route.PublicModelAlias)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var updated *channelpkg.RouteUnitView
	for i := range views {
		if views[i].Id == id {
			updated = &views[i]
			break
		}
	}
	if updated == nil {
		// Concurrent deletion after alias lookup: construct minimal response.
		updated = &channelpkg.RouteUnitView{
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

// GetModelAliasDispatchPolicy returns the dispatch policy for an alias.
// When no policy row exists, a default weighted_pool policy with no fallback is returned.
func GetModelAliasDispatchPolicy(c contract.Context) {
	alias := c.Query("alias")
	if alias == "" {
		c.JSON(http.StatusBadRequest, common.H{
			"success": false,
			"message": "alias参数不能为空",
		})
		return
	}

	policy, err := channelpkg.GetModelAliasDispatchPolicy(alias)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			common.ApiSuccess(c, channelpkg.ModelAliasDispatchPolicy{
				Alias: alias,
				Mode:  "weighted_pool",
			})
			return
		}
		common.ApiError(c, err)
		return
	}

	common.ApiSuccess(c, policy)
}

// updateModelAliasDispatchPolicyRequest is the request body for policy upsert.
type updateModelAliasDispatchPolicyRequest struct {
	Alias         string  `json:"alias"`
	Mode          string  `json:"mode"`
	FallbackAlias *string `json:"fallback_alias"`
}

// UpdateModelAliasDispatchPolicy upserts the dispatch policy for an alias.
func UpdateModelAliasDispatchPolicy(c contract.Context) {
	var req updateModelAliasDispatchPolicyRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, common.H{
			"success": false,
			"message": "请求参数格式错误",
		})
		return
	}

	if req.Alias == "" {
		c.JSON(http.StatusBadRequest, common.H{
			"success": false,
			"message": "alias参数不能为空",
		})
		return
	}

	switch req.Mode {
	case "weighted_pool", "context_range", "delegate":
	default:
		c.JSON(http.StatusBadRequest, common.H{
			"success": false,
			"message": "mode 必须为 weighted_pool、context_range 或 delegate",
		})
		return
	}

	if err := channelpkg.UpsertModelAliasDispatchPolicy(req.Alias, req.Mode, req.FallbackAlias); err != nil {
		common.ApiError(c, err)
		return
	}

	channelpkg.InitChannelCache()

	policy, err := channelpkg.GetModelAliasDispatchPolicy(req.Alias)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			common.ApiSuccess(c, channelpkg.ModelAliasDispatchPolicy{
				Alias:         req.Alias,
				Mode:          req.Mode,
				FallbackAlias: req.FallbackAlias,
			})
			return
		}
		common.ApiError(c, err)
		return
	}

	common.ApiSuccess(c, policy)
}

// ListModelAliasContextRoutes returns all context routes for an alias.
func ListModelAliasContextRoutes(c contract.Context) {
	alias := c.Query("alias")
	if alias == "" {
		c.JSON(http.StatusBadRequest, common.H{
			"success": false,
			"message": "alias参数不能为空",
		})
		return
	}

	routes, err := channelpkg.ListModelAliasContextRoutes(alias)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if routes == nil {
		routes = []channelpkg.ModelAliasContextRoute{}
	}

	common.ApiSuccess(c, common.H{
		"items": routes,
	})
}

// saveModelAliasDispatchRequest carries the whole alias dispatch config in one
// atomic request: policy fields plus the context-route rows.
type saveModelAliasDispatchRequest struct {
	Alias         string                              `json:"alias"`
	Mode          string                              `json:"mode"`
	FallbackAlias *string                             `json:"fallback_alias"`
	Items         []channelpkg.ModelAliasContextRoute `json:"items"`
}

// SaveModelAliasDispatch persists policy + context routes in one transaction.
// Validation mirrors UpdateModelAliasDispatchPolicy and
// ReplaceModelAliasContextRoutes: mode enum, interval bounds, weight bounds and
// no overlap among enabled intervals. The policy is never flipped without the
// rows landing, so a failed save cannot take the model offline.
func SaveModelAliasDispatch(c contract.Context) {
	var req saveModelAliasDispatchRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, common.H{
			"success": false,
			"message": "请求参数格式错误",
		})
		return
	}

	if req.Alias == "" {
		c.JSON(http.StatusBadRequest, common.H{
			"success": false,
			"message": "alias参数不能为空",
		})
		return
	}

	switch req.Mode {
	case "weighted_pool", "context_range", "delegate":
	default:
		c.JSON(http.StatusBadRequest, common.H{
			"success": false,
			"message": "mode 必须为 weighted_pool、context_range 或 delegate",
		})
		return
	}

	for _, item := range req.Items {
		if item.ContextMin < 0 {
			c.JSON(http.StatusBadRequest, common.H{
				"success": false,
				"message": "context_min 必须大于等于 0",
			})
			return
		}
		if item.ContextMax != 0 && item.ContextMax < item.ContextMin {
			c.JSON(http.StatusBadRequest, common.H{
				"success": false,
				"message": "context_max 必须为 0 或大于等于 context_min",
			})
			return
		}
		if item.StaticWeight < 0 || item.StaticWeight > 1000000000 {
			c.JSON(http.StatusBadRequest, common.H{
				"success": false,
				"message": "static_weight 超出允许范围 (0-1000000000)",
			})
			return
		}
	}

	enabled := make([]channelpkg.ModelAliasContextRoute, 0)
	for _, item := range req.Items {
		if item.Enabled {
			enabled = append(enabled, item)
		}
	}
	for i := 0; i < len(enabled); i++ {
		for j := i + 1; j < len(enabled); j++ {
			if intervalsOverlap(
				enabled[i].ContextMin, enabled[i].ContextMax,
				enabled[j].ContextMin, enabled[j].ContextMax,
			) {
				c.JSON(http.StatusBadRequest, common.H{
					"success": false,
					"message": "启用的上下文区间不能重叠",
				})
				return
			}
		}
	}

	routes := make([]channelpkg.ModelAliasContextRoute, len(req.Items))
	copy(routes, req.Items)
	for i := range routes {
		routes[i].Alias = req.Alias
		routes[i].Id = 0
	}

	if err := channelpkg.SaveModelAliasDispatch(req.Alias, req.Mode, req.FallbackAlias, routes); err != nil {
		common.ApiError(c, err)
		return
	}

	channelpkg.InitChannelCache()

	updated, err := channelpkg.ListModelAliasContextRoutes(req.Alias)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if updated == nil {
		updated = []channelpkg.ModelAliasContextRoute{}
	}

	common.ApiSuccess(c, common.H{
		"items": updated,
	})
}

// replaceModelAliasContextRoutesRequest is the request body for replacing context routes.
type replaceModelAliasContextRoutesRequest struct {
	Alias string                              `json:"alias"`
	Items []channelpkg.ModelAliasContextRoute `json:"items"`
}

// ReplaceModelAliasContextRoutes validates and replaces context routes for an alias.
func ReplaceModelAliasContextRoutes(c contract.Context) {
	var req replaceModelAliasContextRoutesRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, common.H{
			"success": false,
			"message": "请求参数格式错误",
		})
		return
	}

	if req.Alias == "" {
		c.JSON(http.StatusBadRequest, common.H{
			"success": false,
			"message": "alias参数不能为空",
		})
		return
	}

	for _, item := range req.Items {
		if item.ContextMin < 0 {
			c.JSON(http.StatusBadRequest, common.H{
				"success": false,
				"message": "context_min 必须大于等于 0",
			})
			return
		}
		if item.ContextMax != 0 && item.ContextMax < item.ContextMin {
			c.JSON(http.StatusBadRequest, common.H{
				"success": false,
				"message": "context_max 必须为 0 或大于等于 context_min",
			})
			return
		}
		if item.StaticWeight < 0 {
			c.JSON(http.StatusBadRequest, common.H{
				"success": false,
				"message": "static_weight 必须大于等于 0",
			})
			return
		}
	}

	// Validate no overlapping intervals among enabled items.
	enabled := make([]channelpkg.ModelAliasContextRoute, 0)
	for _, item := range req.Items {
		if item.Enabled {
			enabled = append(enabled, item)
		}
	}
	for i := 0; i < len(enabled); i++ {
		for j := i + 1; j < len(enabled); j++ {
			a := enabled[i]
			b := enabled[j]
			if intervalsOverlap(a.ContextMin, a.ContextMax, b.ContextMin, b.ContextMax) {
				c.JSON(http.StatusBadRequest, common.H{
					"success": false,
					"message": "启用的上下文区间不能重叠",
				})
				return
			}
		}
	}

	routes := make([]channelpkg.ModelAliasContextRoute, len(req.Items))
	copy(routes, req.Items)
	for i := range routes {
		routes[i].Alias = req.Alias
		routes[i].Id = 0
	}

	if err := channelpkg.ReplaceModelAliasContextRoutes(req.Alias, routes); err != nil {
		common.ApiError(c, err)
		return
	}

	channelpkg.InitChannelCache()

	updated, err := channelpkg.ListModelAliasContextRoutes(req.Alias)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if updated == nil {
		updated = []channelpkg.ModelAliasContextRoute{}
	}

	common.ApiSuccess(c, common.H{
		"items": updated,
	})
}

// intervalsOverlap reports whether two context intervals overlap.
// A ContextMax of 0 means unbounded (infinity).
func intervalsOverlap(minA, maxA, minB, maxB int) bool {
	aUnbounded := maxA == 0
	bUnbounded := maxB == 0
	// If neither contains the other start...
	if !aUnbounded && maxA < minB {
		return false
	}
	if !bUnbounded && maxB < minA {
		return false
	}
	return true
}
