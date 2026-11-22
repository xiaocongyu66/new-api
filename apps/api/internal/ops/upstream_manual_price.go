package ops

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	billing "github.com/QuantumNous/new-api/internal/billing"
	channel "github.com/QuantumNous/new-api/internal/catalog"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/transport/contract"
)

// ManualUpstreamPrice is an operator-entered upstream price for one
// (channel, model) pair. It exists for upstreams that expose no /api/pricing
// endpoint: without it those channels can never take part in the profit
// comparison. A snapshot of the same channel always wins over a manual entry.
type ManualUpstreamPrice struct {
	ID        int    `json:"id" gorm:"primaryKey"`
	ChannelID int    `json:"channel_id" gorm:"uniqueIndex:uk_manual_channel_model,priority:1"`
	ModelName string `json:"model_name" gorm:"size:255;uniqueIndex:uk_manual_channel_model,priority:2"`
	// Mode is "per_call" (PriceIn = USD per request) or "per_token"
	// (PriceIn/PriceOut = USD per 1M input/output tokens).
	Mode      string  `json:"mode" gorm:"size:32"`
	PriceIn   float64 `json:"price_in"`
	PriceOut  float64 `json:"price_out"`
	UpdatedAt int64   `json:"updated_at"`
	UpdatedBy int     `json:"updated_by"`
}

// TableName pins the snake_case table name across all supported dialects.
func (ManualUpstreamPrice) TableName() string { return "manual_upstream_prices" }

func init() {
	dbx.RegisterMigrations(dbx.Migration{Model: &ManualUpstreamPrice{}, Name: "ManualUpstreamPrice"})
}

const (
	manualModePerCall  = "per_call"
	manualModePerToken = "per_token"
)

// manualPriceFields converts a stored manual price into the field map the
// pricing expression builder understands: a fixed per-request price, or a
// two-coefficient USD/1M expression used as-is (coefficients are already USD).
func manualPriceFields(p *ManualUpstreamPrice) map[string]any {
	if p.Mode == manualModePerToken {
		return map[string]any{
			billing.BillingModeField: billing.BillingModeTieredExpr,
			billing.BillingExprField: fmt.Sprintf("tier(\"manual\", p * %g + c * %g)", p.PriceIn, p.PriceOut),
		}
	}
	return map[string]any{"model_price": p.PriceIn}
}

// loadManualUpstreamFields indexes every manual price by (channel, model) for
// the analysis. Best effort: a query failure only means no manual fallback.
func loadManualUpstreamFields() map[[2]any]map[string]any {
	out := make(map[[2]any]map[string]any)
	var rows []ManualUpstreamPrice
	if err := dbx.DB.Find(&rows).Error; err != nil {
		common.SysError("failed to load manual upstream prices: " + err.Error())
		return out
	}
	for i := range rows {
		out[[2]any{int64(rows[i].ChannelID), rows[i].ModelName}] = manualPriceFields(&rows[i])
	}
	return out
}

func manualPriceKey(channelID int, modelName string) [2]any {
	return [2]any{int64(channelID), modelName}
}

// ListManualUpstreamPrices returns the manual prices of one channel.
func ListManualUpstreamPrices(c contract.Context) {
	channelID, _ := strconv.Atoi(strings.TrimSpace(c.Query("channel_id")))
	if channelID <= 0 {
		_ = c.JSON(http.StatusBadRequest, common.H{"success": false, "message": "channel_id 必填"})
		return
	}
	var rows []ManualUpstreamPrice
	if err := dbx.DB.Where("channel_id = ?", channelID).Order("model_name").Find(&rows).Error; err != nil {
		_ = c.JSON(http.StatusInternalServerError, common.H{"success": false, "message": err.Error()})
		return
	}
	_ = c.JSON(http.StatusOK, common.H{"success": true, "message": "", "data": rows})
}

// UpsertManualUpstreamPrice creates or replaces one manual price entry.
func UpsertManualUpstreamPrice(c contract.Context) {
	var req struct {
		ChannelID int     `json:"channel_id" binding:"required"`
		ModelName string  `json:"model_name" binding:"required"`
		Mode      string  `json:"mode" binding:"required"`
		PriceIn   float64 `json:"price_in"`
		PriceOut  float64 `json:"price_out"`
	}
	if err := c.BindJSON(&req); err != nil {
		_ = c.JSON(http.StatusBadRequest, common.H{"success": false, "message": "请求参数格式错误"})
		return
	}
	req.ModelName = strings.TrimSpace(req.ModelName)
	if req.ChannelID <= 0 || req.ModelName == "" {
		_ = c.JSON(http.StatusBadRequest, common.H{"success": false, "message": "channel_id 与 model_name 必填"})
		return
	}
	if req.Mode != manualModePerCall && req.Mode != manualModePerToken {
		_ = c.JSON(http.StatusBadRequest, common.H{"success": false, "message": "mode 只能是 per_call 或 per_token"})
		return
	}
	if req.PriceIn < 0 || req.PriceOut < 0 {
		_ = c.JSON(http.StatusBadRequest, common.H{"success": false, "message": "价格不能为负"})
		return
	}
	if _, err := channel.GetChannelById(req.ChannelID, false); err != nil {
		_ = c.JSON(http.StatusBadRequest, common.H{"success": false, "message": "渠道不存在"})
		return
	}

	entry := ManualUpstreamPrice{
		ChannelID: req.ChannelID,
		ModelName: req.ModelName,
		Mode:      req.Mode,
		PriceIn:   req.PriceIn,
		PriceOut:  req.PriceOut,
		UpdatedAt: common.GetTimestamp(),
	}
	if uid, ok := c.Get("id"); ok {
		if id, ok := uid.(int); ok {
			entry.UpdatedBy = id
		}
	}
	if err := dbx.DB.Where("channel_id = ? AND model_name = ?", req.ChannelID, req.ModelName).
		Assign(map[string]any{
			"mode":       entry.Mode,
			"price_in":   entry.PriceIn,
			"price_out":  entry.PriceOut,
			"updated_at": entry.UpdatedAt,
			"updated_by": entry.UpdatedBy,
		}).
		FirstOrCreate(&entry).Error; err != nil {
		_ = c.JSON(http.StatusInternalServerError, common.H{"success": false, "message": err.Error()})
		return
	}
	_ = c.JSON(http.StatusOK, common.H{"success": true, "message": "", "data": entry})
}

// DeleteManualUpstreamPrice removes one manual price entry.
func DeleteManualUpstreamPrice(c contract.Context) {
	channelID, _ := strconv.Atoi(strings.TrimSpace(c.Query("channel_id")))
	modelName := strings.TrimSpace(c.DefaultQuery("model_name", ""))
	if channelID <= 0 || modelName == "" {
		_ = c.JSON(http.StatusBadRequest, common.H{"success": false, "message": "channel_id 与 model_name 必填"})
		return
	}
	if err := dbx.DB.Where("channel_id = ? AND model_name = ?", channelID, modelName).
		Delete(&ManualUpstreamPrice{}).Error; err != nil {
		_ = c.JSON(http.StatusInternalServerError, common.H{"success": false, "message": err.Error()})
		return
	}
	_ = c.JSON(http.StatusOK, common.H{"success": true, "message": ""})
}

// UpstreamPriceSyncStatus remembers the last /fetch outcome per channel so the
// channel picker can flag upstreams that have no pricing endpoint instead of
// letting the operator rediscover it on every sync.
type UpstreamPriceSyncStatus struct {
	ChannelID   int    `json:"channel_id" gorm:"primaryKey;autoIncrement:false"`
	ChannelName string `json:"channel_name" gorm:"size:255"`
	Status      string `json:"status" gorm:"size:16"`
	Error       string `json:"error" gorm:"type:text"`
	SyncedAt    int64  `json:"synced_at"`
}

// TableName pins the snake_case table name across all supported dialects.
func (UpstreamPriceSyncStatus) TableName() string { return "upstream_price_sync_status" }

func init() {
	dbx.RegisterMigrations(dbx.Migration{Model: &UpstreamPriceSyncStatus{}, Name: "UpstreamPriceSyncStatus"})
}

// recordUpstreamSyncStatus stores the outcome of one channel fetch. Best
// effort: a failure is logged and never breaks the fetch response.
func recordUpstreamSyncStatus(channelID int, name, status, errMsg string, syncedAt int64) {
	if channelID <= 0 {
		return
	}
	row := UpstreamPriceSyncStatus{
		ChannelID:   channelID,
		ChannelName: name,
		Status:      status,
		Error:       errMsg,
		SyncedAt:    syncedAt,
	}
	if err := dbx.DB.Where("channel_id = ?", channelID).Assign(map[string]any{
		"channel_name": name,
		"status":       status,
		"error":        errMsg,
		"synced_at":    syncedAt,
	}).FirstOrCreate(&row).Error; err != nil {
		common.SysError("failed to record upstream sync status: " + err.Error())
	}
}

// loadUpstreamSyncStatuses indexes the last fetch outcome by channel id.
func loadUpstreamSyncStatuses() map[int]UpstreamPriceSyncStatus {
	out := make(map[int]UpstreamPriceSyncStatus)
	var rows []UpstreamPriceSyncStatus
	if err := dbx.DB.Find(&rows).Error; err != nil {
		common.SysError("failed to load upstream sync statuses: " + err.Error())
		return out
	}
	for _, row := range rows {
		out[row.ChannelID] = row
	}
	return out
}
