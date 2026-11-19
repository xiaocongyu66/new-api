package service

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
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
	wallpaperDefaultRefreshMinutes = 60
	wallpaperMinRefreshMinutes     = 1
	wallpaperMaxRefreshMinutes     = 1440
	wallpaperCheckInterval         = time.Minute
	wallpaperAPIClientTimeout      = 20 * time.Second
	wallpaperImageTimeout          = 30 * time.Second
)

// 默认接口条目：<方言>|<URL>。方言决定 JSON 解析方式，URL 上的查询模板
// 由管理员直接编辑，{keyword}/{category} 占位符由 substituteQuery 替换。
// 前四个支持关键词查询（动漫/SFW，Wallhaven 用 categories=010 排除 people），
// 最后两个是直接返回图片字节的兜底源。
const (
	safeBooruAPI = "gelbooru|https://safebooru.org/index.php?page=dapi&s=post&q=index&json=1&tags={keyword}+rating:general&limit=5"
	e926API      = "e926|https://e926.net/posts.json?tags={keyword}&limit=5"
	tbibAPI      = "gelbooru|https://tbib.org/index.php?page=dapi&s=post&q=index&json=1&tags={keyword}+rating:general&limit=5"
	wallhavenAPI = "wallhaven|https://wallhaven.clbug.com/api/v1/search?q={keyword}&categories=010&purity=100&sorting=random"
	alcyAPI      = "direct|https://t.alcy.cc/ycy/"
	xjhAPI       = "direct|https://img.xjh.me/random_img.php?type=json&return=302"
)

var defaultWallpaperAPIs = []string{safeBooruAPI, e926API, tbibAPI, wallhavenAPI, alcyAPI, xjhAPI}
var (
	wallpaperMu            sync.RWMutex
	wallpaperRefreshMu     sync.Mutex
	wallpaperData          []byte
	wallpaperDataGzip      []byte
	wallpaperContentType   string
	wallpaperFetchedAt     time.Time
	wallpaperLastSignature string
	wallpaperOnce          sync.Once
	// wallpaperClient 拒绝跳转到非 http(s) 或私有/回环地址，防止 302 跳转绕过 SSRF 校验。
	wallpaperClient = &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("wallpaper redirect limit exceeded")
			}
			return validateWallpaperFileURL(req.URL.String())
		},
	}
)

// wallpaperAPIResponse 兼容两种上游方言：
//   - 代理方言（saintic）：{"success":true,"data":{"file_url":"..."}}
//   - Wallhaven 方言：{"data":[{"path":"https://.../file.jpg"}, ...]}
type wallpaperAPIResponse struct {
	Success bool `json:"success"`
	Proxy   struct {
		FileURL  string `json:"file_url"`
		Purity   string `json:"purity"`
		Category string `json:"category"`
	} `json:"-"`
	Data json.RawMessage `json:"data"`
}

// pickFileURL 从两种方言里取出第一张图片直链。
func (r *wallpaperAPIResponse) pickFileURL() string {
	// 代理方言：data 是对象。
	var proxyShape struct {
		FileURL string `json:"file_url"`
	}
	if err := common.Unmarshal(r.Data, &proxyShape); err == nil && proxyShape.FileURL != "" {
		return proxyShape.FileURL
	}
	// Wallhaven 方言：data 是数组，元素带 path。
	var list []struct {
		Path string `json:"path"`
	}
	if err := common.Unmarshal(r.Data, &list); err == nil && len(list) > 0 {
		return list[0].Path
	}
	return ""
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

// splitAPIEntry 将一条接口配置拆成「基础 URL」和「查询模板」。
// 形如 https://host/search?q={keyword}&categories=010 ——模板部分随条目配置，
// 由 UI 直接编辑，后端不做接口方言假设。未写模板时返回空串，走默认参数。
func splitAPIEntry(entry string) (string, string) {
	idx := strings.Index(entry, "?")
	if idx < 0 {
		return entry, ""
	}
	return entry[:idx], entry[idx+1:]
}

// substituteQuery 替换模板里的占位符：{keyword} / {category}。
func substituteQuery(template string, keyword string) string {
	category := wallpaperOption("wallpaper.category", "anime")
	template = strings.ReplaceAll(template, "{keyword}", url.QueryEscape(keyword))
	template = strings.ReplaceAll(template, "{category}", url.QueryEscape(category))
	return template
}

// errNoWallpaperForKeyword 表示接口可达但该关键词没搜到图：
// 应换关键词重试，而不是跳过整个接口。
type errNoWallpaperForKeyword struct{ keyword string }

func (e *errNoWallpaperForKeyword) Error() string {
	return "no wallpaper for keyword " + e.keyword
}

// fetchWallpaperFileURL 用关键词查询指定的壁纸接口，取回图片直链。
// 保留为独立入口供测试与旧配置使用；方言细节由 wallpaperSource 处理。
func fetchWallpaperFileURL(keyword string, apiEntry string) (string, error) {
	source := parseWallpaperSource(apiEntry)
	queryURL, err := source.queryURL(keyword)
	if err != nil {
		return "", err
	}
	body, _, err := fetchWithTimeout(queryURL, "application/json", wallpaperAPIClientTimeout)
	if err != nil {
		return "", err
	}
	return source.extractFileURL(body, keyword)
}

// wallpaperAPIURLs 解析管理员配置的壁纸「查询接口」列表。
// 每个 URL 都会带上关键词等参数发起查询（不是图片直链），按顺序回退。
// 支持每行一个（推荐）或逗号分隔；保持与旧版单 URL 配置兼容。
func wallpaperAPIURLs() []string {
	raw := wallpaperOption("wallpaper.image_url", "")
	if strings.TrimSpace(raw) == "" {
		// 兼容旧版配置：管理员单独配置的「随机图片接口 URL」仍然生效。
		if legacy := strings.TrimSpace(wallpaperOption("wallpaper.api_url", "")); legacy != "" {
			return []string{legacy}
		}
		return nil
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == '\n' || r == '\r' || r == ',' || r == ';'
	})
	urls := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			urls = append(urls, trimmed)
		}
	}
	return urls
}

// looksLikeImage 校验响应确实是图片字节。
// 只信任 Content-Type 不够：镜像站在限流/出错时会返回 HTML 页面（200），
// 这类响应必须判失败并继续尝试下一个来源。这里以魔数嗅探为准。
func looksLikeImage(data []byte, contentType string) bool {
	if len(data) < 12 {
		return false
	}
	// JPEG / PNG / GIF / WebP / AVIF / BMP
	if data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF {
		return true
	}
	if string(data[1:4]) == "PNG" {
		return true
	}
	if string(data[0:3]) == "GIF" {
		return true
	}
	if string(data[0:4]) == "RIFF" && string(data[8:12]) == "WEBP" {
		return true
	}
	if string(data[4:8]) == "ftyp" {
		return true // AVIF/HEIF 容器
	}
	if data[0] == 'B' && data[1] == 'M' {
		return true
	}
	return strings.HasPrefix(strings.ToLower(contentType), "image/")
}

// fetchWallpaperFromUpstream 遍历管理员配置的壁纸接口列表，
// 每个接口用随机关键词查询（无结果换词，接口故障换源），
// 第一个返回真实图片的生效；连续失败的源进入短期冷却。
func fetchWallpaperFromUpstream() ([]byte, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	var lastErr error
	apiURLs := wallpaperAPIURLs()
	if len(apiURLs) == 0 {
		apiURLs = defaultWallpaperAPIs
	}

	for _, apiEntry := range apiURLs {
		if ctx.Err() != nil {
			return nil, "", ctx.Err()
		}
		source := parseWallpaperSource(apiEntry)
		if !wallpaperSourceAvailable(source.entry) {
			continue
		}
		if source.kind == wallpaperSourceDirect {
			if err := validateWallpaperFileURL(source.baseURL); err != nil {
				lastErr = err
				wallpaperSourceResult(source.entry, false)
				logger.LogInfo(context.Background(), "wallpaper direct API rejected: "+source.baseURL+": "+err.Error())
				continue
			}
			data, contentType, err := fetchWithTimeout(source.baseURL, "image/*", wallpaperImageTimeout)
			if err == nil && !looksLikeImage(data, contentType) {
				err = fmt.Errorf("direct API returned non-image (content-type %q)", contentType)
			}
			if err != nil {
				lastErr = err
				wallpaperSourceResult(source.entry, false)
				logger.LogInfo(context.Background(), "wallpaper direct API skipped: "+source.baseURL+": "+err.Error())
				continue
			}
			wallpaperSourceResult(source.entry, true)
			return data, contentType, nil
		}

		urlFailed := false
		for range 3 {
			if ctx.Err() != nil {
				return nil, "", ctx.Err()
			}
			keyword := selectWallpaperKeyword()
			fileURL, err := fetchWallpaperFileURL(keyword, apiEntry)
			if err != nil {
				lastErr = err
				var noResult *errNoWallpaperForKeyword
				if errors.As(err, &noResult) {
					continue
				}
				logger.LogInfo(context.Background(),
					"wallpaper API unreachable, skipping keyword retries: "+source.baseURL+": "+err.Error())
				urlFailed = true
				break
			}
			data, contentType, err := fetchWithTimeout(fileURL, "image/*", wallpaperImageTimeout)
			if err != nil || !looksLikeImage(data, contentType) {
				if err == nil {
					err = fmt.Errorf("query returned no image bytes")
				}
				lastErr = err
				continue
			}
			wallpaperSourceResult(source.entry, true)
			return data, contentType, nil
		}
		wallpaperSourceResult(source.entry, false)
		if !urlFailed {
			logger.LogInfo(context.Background(),
				"wallpaper API skipped after keyword retries: "+source.baseURL)
		}
	}
	return nil, "", lastErr
}

// refreshWallpaper 执行一次壁纸抓取并更新内存缓存（含 gzip 压缩版本）。
// 来源优先级：local 模式直接用本地 LRU/内置图；url 模式先试接口，
// 全部失败时回退到本地 LRU/内置图随机一张（永不空白）。
func refreshWallpaper() {
	ensureLocalWallpapersSeeded()

	var data []byte
	var contentType string
	var err error

	if wallpaperSourcePriority() == "local" {
		if item, ok := resolveLocalWallpaper(); ok {
			data, contentType = item.data, item.contentType
		}
	} else {
		data, contentType, err = fetchWallpaperFromUpstream()
		if err != nil {
			logger.LogInfo(context.Background(), "wallpaper upstream failed, falling back to local cache: "+err.Error())
			if item, ok := resolveLocalWallpaper(); ok {
				data, contentType = item.data, item.contentType
			}
		} else {
			// 抓取成功：加入 LRU（保留最近三张），供后续 fallback 使用。
			recentWallpapers.add(localWallpaper{data: data, contentType: contentType})
		}
	}

	if len(data) == 0 {
		logger.LogError(context.Background(), "failed to fetch wallpaper: no upstream image and no local cache")
		return
	}
	gz, gzipErr := gzipData(data)
	if gzipErr != nil {
		logger.LogError(context.Background(), fmt.Sprintf("failed to gzip wallpaper: %v", gzipErr))
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
		wallpaperOption("wallpaper.api_url", ""),
		wallpaperOption("wallpaper.keywords", ""),
		wallpaperOption("wallpaper.category", "anime"),
		wallpaperSourcePriority(),
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
