package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func withWallpaperOptions(t *testing.T, values map[string]string) {
	t.Helper()
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}
	previous := make(map[string]string, len(values))
	for key, value := range values {
		previous[key] = common.OptionMap[key]
		common.OptionMap[key] = value
	}
	common.OptionMapRWMutex.Unlock()

	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		for key, value := range previous {
			common.OptionMap[key] = value
		}
		common.OptionMapRWMutex.Unlock()
	})
}

func TestWallpaperRefreshMinutes(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  int
	}{
		{name: "default when unset", value: "", want: wallpaperDefaultRefreshMinutes},
		{name: "default when invalid", value: "abc", want: wallpaperDefaultRefreshMinutes},
		{name: "default when too small", value: "0", want: wallpaperDefaultRefreshMinutes},
		{name: "default when too large", value: "1441", want: wallpaperDefaultRefreshMinutes},
		{name: "boundary minimum", value: "1", want: 1},
		{name: "boundary maximum", value: "1440", want: 1440},
		{name: "valid value", value: "15", want: 15},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withWallpaperOptions(t, map[string]string{"wallpaper.refresh_interval": tt.value})
			assert.Equal(t, tt.want, wallpaperRefreshMinutes())
		})
	}
}

func TestValidateWallpaperFileURL(t *testing.T) {
	require.NoError(t, validateWallpaperFileURL("https://w.wallhaven.cc/full/vp/wallhaven-vp5653.jpg"))
	require.Error(t, validateWallpaperFileURL("file:///etc/passwd"))
	require.Error(t, validateWallpaperFileURL("http://127.0.0.1:8099/image.jpg"))
	require.Error(t, validateWallpaperFileURL("http://localhost/image.jpg"))
	require.Error(t, validateWallpaperFileURL("http://192.168.1.10/image.jpg"))
}

func TestFetchWallpaperFileURLMergesExistingQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "1", r.URL.Query().Get("existing"))
		assert.Equal(t, "anime", r.URL.Query().Get("category"))
		assert.Equal(t, "chiikawa", r.URL.Query().Get("q"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"file_url":"https://example.com/a.jpg"}}`))
	}))
	defer server.Close()

	withWallpaperOptions(t, map[string]string{
		"wallpaper.api_url":  server.URL + "/?existing=1",
		"wallpaper.category": "anime",
	})
	// 用带占位符的查询模板调用：existing=1 保留，q/category 由占位符替换注入。
	fileURL, err := fetchWallpaperFileURL("chiikawa",
		server.URL+"/search?existing=1&q={keyword}&category={category}")
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/a.jpg", fileURL)
}

func TestParseWallpaperSource(t *testing.T) {
	tests := []struct {
		name     string
		entry    string
		kind     wallpaperSourceKind
		baseURL  string
		template string
	}{
		{name: "legacy url", entry: "https://example.com/search?q=chiikawa", kind: wallpaperSourceWallhaven, baseURL: "https://example.com/search", template: "q=chiikawa"},
		{name: "direct prefix", entry: "direct:https://example.com/a.jpg", kind: wallpaperSourceDirect, baseURL: "https://example.com/a.jpg"},
		{name: "explicit direct", entry: "direct|https://example.com/a.jpg", kind: wallpaperSourceDirect, baseURL: "https://example.com/a.jpg"},
		{name: "wallhaven", entry: "wallhaven|https://example.com/api?q={keyword}", kind: wallpaperSourceWallhaven, baseURL: "https://example.com/api", template: "q={keyword}"},
		{name: "gelbooru", entry: "gelbooru|https://example.com/index.php?page=dapi&tags={keyword}", kind: wallpaperSourceGelbooru, baseURL: "https://example.com/index.php", template: "page=dapi&tags={keyword}"},
		{name: "e926", entry: "e926|https://example.com/posts.json?tags={keyword}", kind: wallpaperSourceE926, baseURL: "https://example.com/posts.json", template: "tags={keyword}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseWallpaperSource(tt.entry)
			assert.Equal(t, tt.kind, got.kind)
			assert.Equal(t, tt.baseURL, got.baseURL)
			assert.Equal(t, tt.template, got.template)
		})
	}
}

func TestWallpaperSourceExtractFileURL(t *testing.T) {
	tests := []struct {
		name  string
		entry string
		body  string
		want  string
	}{
		{
			name:  "wallhaven path",
			entry: "wallhaven|https://example.com/api",
			body:  `{"data":[{"path":"https://example.com/a.jpg"}]}`,
			want:  "https://example.com/a.jpg",
		},
		{
			name:  "gelbooru file_url",
			entry: "gelbooru|https://example.com/index.php",
			body:  `[{"file_url":"https://example.com/b.jpg"}]`,
			want:  "https://example.com/b.jpg",
		},
		{
			name:  "gelbooru directory fallback",
			entry: "gelbooru|https://example.com/index.php",
			body:  `[{"directory":42,"image":"c.jpg"}]`,
			want:  "https://example.com/images/42/c.jpg",
		},
		{
			name:  "e926 original",
			entry: "e926|https://example.com/posts.json",
			body:  `[{"files":{"original":{"url":"https://example.com/d.jpg"}}}]`,
			want:  "https://example.com/d.jpg",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fileURL, err := parseWallpaperSource(tt.entry).extractFileURL([]byte(tt.body), "chiikawa")
			require.NoError(t, err)
			assert.Equal(t, tt.want, fileURL)
		})
	}
}

func TestWallpaperSourceCooldown(t *testing.T) {
	entry := "cooldown-test|https://example.com/api"
	reset := func() {
		wallpaperSourceState.Lock()
		delete(wallpaperSourceState.failures, entry)
		delete(wallpaperSourceState.cooldown, entry)
		wallpaperSourceState.Unlock()
	}
	reset()
	t.Cleanup(reset)

	assert.True(t, wallpaperSourceAvailable(entry))
	wallpaperSourceResult(entry, false)
	wallpaperSourceResult(entry, false)
	assert.True(t, wallpaperSourceAvailable(entry))
	wallpaperSourceResult(entry, false)
	assert.False(t, wallpaperSourceAvailable(entry))
	wallpaperSourceResult(entry, true)
	assert.True(t, wallpaperSourceAvailable(entry))
}
