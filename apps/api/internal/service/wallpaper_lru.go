package service

import (
	"bytes"
	"embed"
	"math/rand/v2"
	"strings"
	"sync"
)

// 内置三张卡通备用壁纸（chiikawa / sanrio / pikachu），随二进制 embed，
// 不依赖任何外部接口或前端构建产物，永远是最后兜底。
//
//go:embed static/wallpapers/*
var localWallpaperFS embed.FS

var localWallpaperFiles = []string{
	"static/wallpapers/chiikawa.jpg",
	"static/wallpapers/sanrio.jpg",
	"static/wallpapers/pikachu.png",
}

type localWallpaper struct {
	data        []byte
	contentType string
}

var localWallpapers = loadLocalWallpapers()

func loadLocalWallpapers() []localWallpaper {
	out := make([]localWallpaper, 0, len(localWallpaperFiles))
	for _, name := range localWallpaperFiles {
		data, err := localWallpaperFS.ReadFile(name)
		if err != nil || len(data) == 0 {
			continue
		}
		contentType := "image/jpeg"
		if strings.HasSuffix(strings.ToLower(name), ".png") {
			contentType = "image/png"
		}
		out = append(out, localWallpaper{data: data, contentType: contentType})
	}
	return out
}

// wallpaperLRU 保存最近成功抓取的 3 张图片（MRU 在前），
// URL 全部失败时随机取一张返回；同时作为「本地缓存优先」模式的数据源。
type wallpaperLRU struct {
	mu    sync.RWMutex
	items []localWallpaper
	limit int
}

var recentWallpapers = &wallpaperLRU{limit: 3}

// add 将图片加入 LRU 头部，超出上限淘汰最旧的一张。
func (l *wallpaperLRU) add(item localWallpaper) {
	l.mu.Lock()
	defer l.mu.Unlock()
	// 同字节序列已在缓存里时提到头部（内置图反复命中不重复占位）。
	for i, existing := range l.items {
		if bytes.Equal(existing.data, item.data) {
			l.items = append(l.items[:i], l.items[i+1:]...)
			break
		}
	}
	l.items = append([]localWallpaper{item}, l.items...)
	if len(l.items) > l.limit {
		l.items = l.items[:l.limit]
	}
}

// random 从 LRU 里等概率取一张。
func (l *wallpaperLRU) random() (localWallpaper, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if len(l.items) == 0 {
		return localWallpaper{}, false
	}
	return l.items[rand.IntN(len(l.items))], true
}

// count 返回当前 LRU 条目数，供诊断使用。
func (l *wallpaperLRU) count() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return len(l.items)
}

// seedLocalWallpapers 用内置图填满 LRU，保证任何时刻都有可用兜底。
var seedOnce sync.Once

func ensureLocalWallpapersSeeded() {
	seedOnce.Do(func() {
		for _, item := range localWallpapers {
			recentWallpapers.add(item)
		}
	})
}

// wallpaperSourcePriority 控制背景来源优先级：
//   - "url"   ：先试 URL 接口（默认），失败回退 LRU/内置图；
//   - "local" ：直接使用 LRU/内置图，不打网络。
func wallpaperSourcePriority() string {
	if wallpaperOption("wallpaper.source_priority", "url") == "local" {
		return "local"
	}
	return "url"
}

// randomLocalWallpaper 从内置图里随机取一张（最后兜底）。
func randomLocalWallpaper() (localWallpaper, bool) {
	if len(localWallpapers) == 0 {
		return localWallpaper{}, false
	}
	return localWallpapers[rand.IntN(len(localWallpapers))], true
}

// resolveLocalWallpaper 按 LRU → 内置图顺序随机返回一张本地壁纸。
func resolveLocalWallpaper() (localWallpaper, bool) {
	ensureLocalWallpapersSeeded()
	if item, ok := recentWallpapers.random(); ok {
		return item, true
	}
	return randomLocalWallpaper()
}
