package ops

import (
	"net/http"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/geoip"
	"github.com/QuantumNous/new-api/internal/transport/contract"
)

// GetGeoIPDatabase reports the on-disk state of the GeoIP database so the
// dashboard can show the last update time and staleness.
func GetGeoIPDatabase(c contract.Context) {
	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"message": "",
		"data":    geoip.GetDatabaseStatus(),
	})
}

// UpdateGeoIPDatabase downloads a fresh database on demand (the dashboard's
// update button). The download runs synchronously with the request context,
// so closing the page cancels it.
func UpdateGeoIPDatabase(c contract.Context) {
	status, err := geoip.UpdateDatabase(c.Context())
	if err != nil {
		common.CtxApiErrorMsg(c, "更新 GeoIP 数据库失败: "+err.Error())
		return
	}
	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"message": "",
		"data":    status,
	})
}
