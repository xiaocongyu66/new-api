package service

import (
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/internal/common"
)

// wallpaperSourceKind 决定如何构造查询、以及从 JSON 里取哪个字段作为图片直链。
// 每种方言一个适配器，新增接口只需在配置里加一行 "<kind>|<URL>"，不改抓取主流程。
type wallpaperSourceKind string

const (
	wallpaperSourceWallhaven wallpaperSourceKind = "wallhaven" // {"data":[{"path":"..."}]}
	wallpaperSourceGelbooru  wallpaperSourceKind = "gelbooru"  // [{"file_url":"..."}] 或 directory+image
	wallpaperSourceE926      wallpaperSourceKind = "e926"      // [{"files":{"original":{"url":"..."}}}]
	wallpaperSourceDirect    wallpaperSourceKind = "direct"    // 直接返回图片字节
)

type wallpaperSource struct {
	kind     wallpaperSourceKind
	baseURL  string
	template string
	entry    string
}

// parseWallpaperSource 解析一条接口配置：
//
//	"wallhaven|https://...?q={keyword}"  显式方言
//	"direct:https://..."                 旧式直链前缀
//	无前缀的 URL                          兼容旧配置，按 wallhaven 方言处理
func parseWallpaperSource(entry string) wallpaperSource {
	src := wallpaperSource{entry: entry}
	rest := entry
	if strings.HasPrefix(entry, "direct:") {
		src.kind = wallpaperSourceDirect
		src.baseURL = strings.TrimPrefix(entry, "direct:")
		return src
	}
	if idx := strings.Index(entry, "|"); idx > 0 {
		kind := wallpaperSourceKind(strings.ToLower(strings.TrimSpace(entry[:idx])))
		switch kind {
		case wallpaperSourceWallhaven, wallpaperSourceGelbooru, wallpaperSourceE926, wallpaperSourceDirect:
			src.kind = kind
			rest = entry[idx+1:]
		}
	}
	if src.kind == "" {
		src.kind = wallpaperSourceWallhaven
	}
	if src.kind == wallpaperSourceDirect {
		src.baseURL = rest
		return src
	}
	src.baseURL, src.template = splitAPIEntry(rest)
	return src
}

// queryURL 按方言构造请求地址；配置文件里写了查询模板时只做占位符替换，
// 完全尊重管理员写的参数（分类、SFW 过滤、数量等）。
func (s wallpaperSource) queryURL(keyword string) (string, error) {
	parsed, err := url.Parse(s.baseURL)
	if err != nil {
		return "", err
	}
	if s.template != "" {
		parsed.RawQuery = substituteQuery(s.template, keyword)
		return parsed.String(), nil
	}
	query := parsed.Query()
	switch s.kind {
	case wallpaperSourceGelbooru:
		query.Set("page", "dapi")
		query.Set("s", "post")
		query.Set("q", "index")
		query.Set("json", "1")
		query.Set("tags", keyword+" rating:general")
		query.Set("limit", "5")
	case wallpaperSourceE926:
		query.Set("tags", keyword)
		query.Set("limit", "5")
	default:
		query.Set("q", keyword)
		query.Set("categories", "010")
		query.Set("purity", "100")
		query.Set("sorting", "random")
		query.Set("category", wallpaperOption("wallpaper.category", "anime"))
		query.Set("json", "")
	}
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

// extractFileURL 按方言从 JSON 响应里取出第一张图片直链。
func (s wallpaperSource) extractFileURL(body []byte, keyword string) (string, error) {
	switch s.kind {
	case wallpaperSourceGelbooru:
		var posts []struct {
			FileURL   string `json:"file_url"`
			Directory int    `json:"directory"`
			Image     string `json:"image"`
		}
		if err := common.Unmarshal(body, &posts); err != nil {
			return "", err
		}
		for _, post := range posts {
			if post.FileURL != "" {
				return validateWallpaperDirectURL(post.FileURL)
			}
			// tbib 的 DAPI 响应不带 file_url，按 Gelbooru 目录规则拼出原图地址。
			if post.Directory > 0 && post.Image != "" {
				parsed, err := url.Parse(s.baseURL)
				if err != nil {
					return "", err
				}
				return validateWallpaperDirectURL(
					fmt.Sprintf("%s://%s/images/%d/%s", parsed.Scheme, parsed.Host, post.Directory, post.Image))
			}
		}
	case wallpaperSourceE926:
		var posts []struct {
			Files struct {
				Original struct {
					URL string `json:"url"`
				} `json:"original"`
			} `json:"files"`
		}
		if err := common.Unmarshal(body, &posts); err != nil {
			return "", err
		}
		for _, post := range posts {
			if post.Files.Original.URL != "" {
				return validateWallpaperDirectURL(post.Files.Original.URL)
			}
		}
	default:
		var result wallpaperAPIResponse
		if err := common.Unmarshal(body, &result); err != nil {
			return "", err
		}
		if fileURL := result.pickFileURL(); fileURL != "" {
			return validateWallpaperDirectURL(fileURL)
		}
	}
	return "", &errNoWallpaperForKeyword{keyword: keyword}
}

func validateWallpaperDirectURL(fileURL string) (string, error) {
	if err := validateWallpaperFileURL(fileURL); err != nil {
		return "", err
	}
	return fileURL, nil
}

// 连续失败的接口进入短期冷却，避免反复打已经限流或失效的免费接口。
const (
	wallpaperSourceMaxFailures  = 3
	wallpaperSourceCooldownTime = 30 * time.Minute
)

var wallpaperSourceState = struct {
	sync.Mutex
	failures map[string]int
	cooldown map[string]time.Time
}{
	failures: make(map[string]int),
	cooldown: make(map[string]time.Time),
}

func wallpaperSourceAvailable(entry string) bool {
	wallpaperSourceState.Lock()
	defer wallpaperSourceState.Unlock()
	return time.Now().After(wallpaperSourceState.cooldown[entry])
}

func wallpaperSourceResult(entry string, ok bool) {
	wallpaperSourceState.Lock()
	defer wallpaperSourceState.Unlock()
	if ok {
		delete(wallpaperSourceState.failures, entry)
		delete(wallpaperSourceState.cooldown, entry)
		return
	}
	wallpaperSourceState.failures[entry]++
	if wallpaperSourceState.failures[entry] >= wallpaperSourceMaxFailures {
		wallpaperSourceState.cooldown[entry] = time.Now().Add(wallpaperSourceCooldownTime)
	}
}
