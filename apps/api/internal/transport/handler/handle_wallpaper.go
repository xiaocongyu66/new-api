package handler

import (
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/internal/service"
	"github.com/QuantumNous/new-api/internal/transport/contract"
)

// GetWallpaper 返回服务器端缓存的壁纸图片。
//
// 后端定时从 https://api.weirain.com/pc 抓取壁纸并缓存在内存中，
// 用户请求时直接返回缓存的图片，前端同源请求，无 CORS 问题。
// 客户端若发送 Accept-Encoding: gzip，则返回 gzip 压缩后的图片并设置
// Content-Encoding: gzip，减少传输体积与加载延迟。
//
// 注意：本路由注册在 /api 组下，该组已挂载 UseCompression() 中间件。
// handler 已自行按 Accept-Encoding 做 gzip 并设 Content-Encoding，
// 依赖 fasthttp CompressHandler 对已带 Content-Encoding 的响应跳过二次压缩。
// 若未来框架行为变更，可将本 handler 移出 UseCompression 覆盖范围，
// 或改由中间件统一压缩、handler 只返回原始数据。
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
	c.Data(http.StatusOK, "image/jpeg", data)
}
