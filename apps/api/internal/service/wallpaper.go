package service

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/logger"
	"github.com/bytedance/gopkg/util/gopool"
)

const (
	// weirainWallpaperAPI 微雨壁纸接口：直接返回一张 JPEG 图片，免费无鉴权。
	// 同一 IP 每秒最多 20 次，偶尔超限会返回 429，下一秒恢复。
	weirainWallpaperAPI = "https://api.weirain.com/pc"

	// wallpaperFetchInterval 壁纸定时抓取间隔：每小时抓取一次，远低于微雨的每秒 20 次限制，
	// 基本不会触发 429，避免前端每次打开都跨域抓图导致的延迟和 CORS 问题。
	wallpaperFetchInterval = time.Hour

	// wallpaperFetchTimeout 抓取超时。
	wallpaperFetchTimeout = 15 * time.Second
)

var (
	wallpaperMu        sync.RWMutex
	wallpaperData      []byte
	wallpaperDataGzip  []byte
	wallpaperFetchedAt time.Time
	wallpaperOnce      sync.Once
)

// gzipData 对数据进行 gzip 压缩，用于 Accept-Encoding: gzip 的客户端。
func gzipData(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(data); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// fetchWallpaperFromUpstream 从微雨接口抓取一张壁纸。
// 遇 429（同一 IP 每秒最多 20 次，下一秒恢复）时等待 1 秒后重试一次。
func fetchWallpaperFromUpstream() ([]byte, error) {
	client := &http.Client{Timeout: wallpaperFetchTimeout}
	req, err := http.NewRequest(http.MethodGet, weirainWallpaperAPI, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "image/*")
	req.Header.Set("Cache-Control", "no-store")

	var resp *http.Response
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Second)
		}
		resp, err = client.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusTooManyRequests {
			break
		}
		resp.Body.Close()
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("weirain wallpaper API returned status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return data, nil
}

// refreshWallpaper 执行一次壁纸抓取并更新内存缓存（含 gzip 压缩版本）。
func refreshWallpaper() {
	data, err := fetchWallpaperFromUpstream()
	if err != nil {
		logger.LogError(context.Background(), fmt.Sprintf("failed to fetch wallpaper: %v", err))
		return
	}
	gz, err := gzipData(data)
	if err != nil {
		logger.LogError(context.Background(), fmt.Sprintf("failed to gzip wallpaper: %v", err))
		gz = nil
	}
	wallpaperMu.Lock()
	wallpaperData = data
	wallpaperDataGzip = gz
	wallpaperFetchedAt = time.Now()
	wallpaperMu.Unlock()
	logger.LogInfo(context.Background(), fmt.Sprintf("wallpaper refreshed: %d bytes (gzip: %d bytes)", len(data), len(gz)))
}

// StartWallpaperFetchTask 启动壁纸定时抓取任务（仅主节点）。
// 启动时立即抓取一次，之后每隔 wallpaperFetchInterval 抓取一次。
func StartWallpaperFetchTask() {
	wallpaperOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		gopool.Go(func() {
			logger.LogInfo(context.Background(), fmt.Sprintf("wallpaper fetch task started: interval=%s", wallpaperFetchInterval))
			refreshWallpaper()
			ticker := time.NewTicker(wallpaperFetchInterval)
			defer ticker.Stop()
			for range ticker.C {
				refreshWallpaper()
			}
		})
	})
}

// GetWallpaperData 返回当前缓存的壁纸原始数据。
// 若缓存为空（例如首次启动尚未抓取），同步抓取一次作为兜底，避免用户请求时等待。
func GetWallpaperData() ([]byte, bool) {
	wallpaperMu.RLock()
	data := wallpaperData
	wallpaperMu.RUnlock()
	if len(data) > 0 {
		return data, true
	}
	// 兜底同步抓取
	refreshWallpaper()
	wallpaperMu.RLock()
	data = wallpaperData
	wallpaperMu.RUnlock()
	return data, len(data) > 0
}

// GetWallpaperDataGzip 返回当前缓存的壁纸 gzip 压缩数据。
func GetWallpaperDataGzip() ([]byte, bool) {
	wallpaperMu.RLock()
	gz := wallpaperDataGzip
	wallpaperMu.RUnlock()
	if len(gz) > 0 {
		return gz, true
	}
	// 若 gzip 版本未就绪，返回原始数据的 gzip 现场压缩（兜底）
	data, ok := GetWallpaperData()
	if !ok {
		return nil, false
	}
	gz, err := gzipData(data)
	if err != nil {
		return nil, false
	}
	return gz, true
}
