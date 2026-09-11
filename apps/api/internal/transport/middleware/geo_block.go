package middleware

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/geoip"
	"github.com/QuantumNous/new-api/internal/logger"
	"github.com/QuantumNous/new-api/internal/transport/contract"
)

// GeoBlock 按请求 IP 的国家执行地理封禁：总开关
// geo_block_setting.enabled 打开且 IP 归属国在 blocked_countries 列表里时直接拒绝。
// 挂在引擎层（所有路由之前），对 Web、API、静态资源全部生效。
//
// 拒绝策略是"隐形"的：不告诉访问者"你被地区封禁了"，而是让请求看起来
// 就像访问了一个不存在的页面 —— Web 端 404 状态码 + 302 到前端 404 路由，
// API 端直接 404 JSON。这样被禁区域既无法使用网站，也拿不到任何功能线索。
//
// 判定依赖本机 MMDB 数据库（GEOIP_DB_PATH 环境变量指定文件路径），
// 数据库不可用（路径未配置或文件损坏）时本中间件对每个请求都直接放行
//（fail-open）：缺库导致全站 404 是不可接受的，而"没拦住"只是损失封禁
// 效果。因此部署方在打开开关前应确认 GEOIP_DB_PATH 指向有效文件。
func GeoBlock() contract.Middleware {
	return func(c contract.Context) {
		if !geoip.GetGeoBlockSetting().Enabled {
			c.Next()
			return
		}
		clientIP := c.ClientIP()
		if !geoip.IsBlocked(clientIP) {
			c.Next()
			return
		}
		rejectGeoBlocked(c, clientIP)
	}
}

// rejectGeoBlocked 以 404 拒绝命中地理封禁的请求并留审计日志。
// 审计日志照实记录 "geo block"，管理员可查；访问者看到的只是一次"页面不存在"。
func rejectGeoBlocked(c contract.Context, clientIP string) {
	req := c.HTTPRequest()
	path := req.URL.Path
	method := req.Method

	logger.LogWarn(c.Context(), fmt.Sprintf(
		"geo block rejected: client_ip=%s path=%s method=%s",
		clientIP, path, method,
	))

	// /api、/mj、/pg 开头是 API 请求：直接 404 JSON，不泄露站点结构。
	if strings.HasPrefix(path, "/api") || strings.HasPrefix(path, "/mj") || strings.HasPrefix(path, "/pg") {
		c.AbortWithStatusJSON(http.StatusNotFound, common.H{
			"success": false,
			"message": "page not found",
		})
		return
	}

	// Web 页面：404 状态 + 302 Location: /404，浏览器跳到 SPA 的 404 路由。
	c.Redirect(http.StatusNotFound, "/404")
	c.Abort()
}
