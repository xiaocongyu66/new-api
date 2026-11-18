package handler

import (
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/internal/common"

	"github.com/QuantumNous/new-api/internal/service"
	"github.com/QuantumNous/new-api/internal/transport/contract"
)

// GetWallpaper 返回服务器端缓存的壁纸图片。
//
// 后端定时从 hub.saintic.com 的 Wallhaven 代理接口抓取一张可爱卡通壁纸
// （关键词随机挑选，只取 sfw / anime），缓存在内存中；用户请求时直接返回缓存图片。
// 前端同源请求，无 CORS 问题。客户端若发送 Accept-Encoding: gzip，
// 则返回 gzip 压缩后的图片并设置 Content-Encoding，减少传输体积与加载延迟。
//
// 注意：本路由注册在 /api 组下，该组已挂载 UseCompression() 中间件。
// handler 已自行按 Accept-Encoding 做 gzip 并设 Content-Encoding，
// 依赖 fasthttp CompressHandler 对已带 Content-Encoding 的响应跳过二次压缩。
func GetWallpaper(c contract.Context) {
	acceptEncoding := c.Header("Accept-Encoding")
	wantGzip := strings.Contains(acceptEncoding, "gzip")

	var (
		data       []byte
		contentEnc string
		ok         bool
	)
	if wantGzip {
		data, ok = service.GetWallpaperDataGzip()
		if ok {
			contentEnc = "gzip"
		}
	}
	if !ok {
		data, ok = service.GetWallpaperData()
	}
	if !ok {
		c.String(http.StatusServiceUnavailable, "wallpaper unavailable")
		return
	}

	c.SetHeader("Cache-Control", "public, max-age=3600, immutable")
	if contentEnc != "" {
		c.SetHeader("Content-Encoding", contentEnc)
	}
	// Content-Type 来自上游响应头：gzip 字节不能用 MIME 嗅探，否则会变成 octet-stream。
	c.Data(http.StatusOK, service.GetWallpaperContentType(), data)
}

// RefreshWallpaper 供后台在管理员权限下强制刷新一次壁纸缓存。
func RefreshWallpaper(c contract.Context) {
	service.RefreshWallpaper()
	_ = c.JSON(http.StatusOK, common.H{"success": true, "message": ""})
}
