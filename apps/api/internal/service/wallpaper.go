package service

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/logger"
	"github.com/bytedance/gopkg/util/gopool"
)

const (
	// wallhavenPicAPI 是 hub.saintic.com 的 Wallhaven 随机壁纸代理接口。
	// 只取 sfw、anime 分类；具体角色由 cuteWallpaperKeywords 随机挑选。
	wallhavenPicAPI = "https://hub.saintic.com/openservice/wallhavenpic"

	wallpaperDefaultRefreshMinutes = 60
	wallpaperMinRefreshMinutes     = 1
	wallpaperMaxRefreshMinutes     = 1440
	wallpaperCheckInterval         = time.Minute
	wallpaperAPIClientTimeout      = 20 * time.Second
	wallpaperImageTimeout          = 15 * time.Second
)

var (
	wallpaperMu            sync.RWMutex
	wallpaperRefreshMu     sync.Mutex
	wallpaperData          []byte
	wallpaperDataGzip      []byte
	wallpaperContentType   string
	wallpaperFetchedAt     time.Time
	wallpaperLastSignature string
	wallpaperOnce          sync.Once
	wallpaperClient        = &http.Client{}
)

type wallpaperAPIResponse struct {
	Success bool `json:"success"`
	Data    struct {
		FileURL  string `json:"file_url"`
		Purity   string `json:"purity"`
		Category string `json:"category"`
	} `json:"data"`
}

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

// fetchWithTimeout 发起一次带独立超时的 GET 请求，返回响应体和 Content-Type。
func fetchWithTimeout(rawURL string, accept string, timeout time.Duration) ([]byte, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", err
	}
	// hub 接口对空 UA 和爬虫 UA 返回 403。
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 Chrome/120 Safari/537.36")
	req.Header.Set("Accept", accept)
	req.Header.Set("Cache-Control", "no-store")

	resp, err := wallpaperClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("wallpaper request returned status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	return body, resp.Header.Get("Content-Type"), nil
}

// validateWallpaperFileURL 校验上游返回的图片直链，阻断非 HTTP 协议和内网地址。
func validateWallpaperFileURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("unsupported wallpaper URL scheme %q", parsed.Scheme)
	}
	host := parsed.Hostname()
	if strings.EqualFold(host, "localhost") {
		return fmt.Errorf("private wallpaper host is not allowed")
	}
	if ip := net.ParseIP(host); ip != nil &&
		(ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast()) {
		return fmt.Errorf("private wallpaper address is not allowed")
	}
	return nil
}

// wallpaperOption 读取后台可配置选项；空值回退到内置默认值。
func wallpaperOption(name string, fallback string) string {
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	if value := strings.TrimSpace(common.OptionMap[name]); value != "" {
		return value
	}
	return fallback
}

// fetchWallpaperFileURL 用一个随机关键词查询代理接口，取回图片直链。
func fetchWallpaperFileURL(keyword string) (string, error) {
	apiURL := wallpaperOption("wallpaper.api_url", wallhavenPicAPI)
	parsed, err := url.Parse(apiURL)
	if err != nil {
		return "", err
	}
	query := parsed.Query()
	query.Set("category", wallpaperOption("wallpaper.category", "anime"))
	query.Set("q", keyword)
	query.Set("json", "")
	parsed.RawQuery = query.Encode()

	body, _, err := fetchWithTimeout(parsed.String(), "application/json", wallpaperAPIClientTimeout)
	if err != nil {
		return "", err
	}
	var result wallpaperAPIResponse
	if err := common.Unmarshal(body, &result); err != nil {
		return "", err
	}
	if !result.Success || result.Data.FileURL == "" {
		return "", fmt.Errorf("no wallpaper for keyword %q", keyword)
	}
	if err := validateWallpaperFileURL(result.Data.FileURL); err != nil {
		return "", err
	}
	return result.Data.FileURL, nil
}

// fetchWallpaperFromUpstream 随机挑选关键词抓取一张壁纸；单个关键词失败时换下一个。
// 若管理员配置了自定义图片直链，则直接取该图片，不走随机接口。
func fetchWallpaperFromUpstream() ([]byte, string, error) {
	if customURL := wallpaperOption("wallpaper.image_url", ""); customURL != "" {
		data, contentType, err := fetchWithTimeout(customURL, "image/*", wallpaperImageTimeout)
		return data, contentType, err
	}
	var lastErr error
	for range 3 {
		keyword := selectWallpaperKeyword()
		fileURL, err := fetchWallpaperFileURL(keyword)
		if err != nil {
			lastErr = err
			continue
		}
		data, contentType, err := fetchWithTimeout(fileURL, "image/*", wallpaperImageTimeout)
		if err != nil {
			lastErr = err
			continue
		}
		return data, contentType, nil
	}
	return nil, "", lastErr
}

// refreshWallpaper 执行一次壁纸抓取并更新内存缓存（含 gzip 压缩版本）。
func refreshWallpaper() {
	data, contentType, err := fetchWallpaperFromUpstream()
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
	wallpaperContentType = contentType
	wallpaperFetchedAt = time.Now()
	wallpaperLastSignature = wallpaperConfigSignature()
	wallpaperMu.Unlock()
	logger.LogInfo(context.Background(), fmt.Sprintf("wallpaper refreshed: %d bytes (gzip: %d bytes)", len(data), len(gz)))
}

// GetWallpaperContentType 返回上游图片的 Content-Type，避免对 gzip 字节做 MIME 嗅探。
func GetWallpaperContentType() string {
	wallpaperMu.RLock()
	defer wallpaperMu.RUnlock()
	if wallpaperContentType == "" {
		return "image/jpeg"
	}
	return wallpaperContentType
}

// RefreshWallpaper 强制重新抓取一次壁纸，供后台「刷新背景」按钮调用。
func RefreshWallpaper() {
	refreshWallpaper()
}

// wallpaperRefreshMinutes 读取后台配置的自动切换间隔（分钟），非法值回退到默认值。
func wallpaperRefreshMinutes() int {
	minutes, err := strconv.Atoi(strings.TrimSpace(wallpaperOption("wallpaper.refresh_interval", "")))
	if err != nil || minutes < wallpaperMinRefreshMinutes || minutes > wallpaperMaxRefreshMinutes {
		return wallpaperDefaultRefreshMinutes
	}
	return minutes
}

// StartWallpaperFetchTask 启动壁纸定时抓取任务（仅主节点）。
// 启动时立即抓取一次；之后每分钟检查一次，距上次抓取达到配置间隔时再抓取，
// 这样后台修改「自动切换间隔」无需重启即可生效。
func StartWallpaperFetchTask() {
	wallpaperOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		gopool.Go(func() {
			refreshWallpaper()
			logger.LogInfo(context.Background(), fmt.Sprintf("wallpaper fetch task started: refresh_interval=%dm", wallpaperRefreshMinutes()))
			ticker := time.NewTicker(wallpaperCheckInterval)
			defer ticker.Stop()
			for range ticker.C {
				wallpaperMu.RLock()
				last := wallpaperFetchedAt
				wallpaperMu.RUnlock()
				if time.Since(last) < time.Duration(wallpaperRefreshMinutes())*time.Minute {
					continue
				}
				refreshWallpaper()
			}
		})
	})
}

// wallpaperConfigSignature 反映当前壁纸相关配置，配置变化时让缓存立即失效。
func wallpaperConfigSignature() string {
	return strings.Join([]string{
		wallpaperOption("wallpaper.image_url", ""),
		wallpaperOption("wallpaper.api_url", wallhavenPicAPI),
		wallpaperOption("wallpaper.keywords", ""),
		wallpaperOption("wallpaper.category", "anime"),
	}, "|")
}

func wallpaperCacheCurrent() bool {
	signature := wallpaperConfigSignature()
	wallpaperMu.RLock()
	defer wallpaperMu.RUnlock()
	return len(wallpaperData) > 0 && wallpaperLastSignature == signature
}

// ensureWallpaperFresh 在缓存缺失或配置变化时回源；并发请求只回源一次。
func ensureWallpaperFresh() {
	if wallpaperCacheCurrent() {
		return
	}
	// 并发缓存未命中时只让一个请求回源，避免惊群。
	wallpaperRefreshMu.Lock()
	defer wallpaperRefreshMu.Unlock()
	if wallpaperCacheCurrent() {
		return
	}
	refreshWallpaper()
}

// GetWallpaperData 返回当前缓存的壁纸原始数据。
// 缓存为空或后台配置刚变更时同步抓取一次，保证设置保存后立即生效。
func GetWallpaperData() ([]byte, bool) {
	ensureWallpaperFresh()
	wallpaperMu.RLock()
	data := wallpaperData
	wallpaperMu.RUnlock()
	return data, len(data) > 0
}

// GetWallpaperDataGzip 返回当前缓存的壁纸 gzip 压缩数据。
func GetWallpaperDataGzip() ([]byte, bool) {
	ensureWallpaperFresh()
	wallpaperMu.RLock()
	gz := wallpaperDataGzip
	data := wallpaperData
	wallpaperMu.RUnlock()
	if len(gz) > 0 {
		return gz, true
	}
	// 若 gzip 版本未就绪，用缓存中的原始数据现场压缩，不再触发第二次回源。
	if len(data) == 0 {
		return nil, false
	}
	gz, err := gzipData(data)
	if err != nil {
		return nil, false
	}
	return gz, true
}
