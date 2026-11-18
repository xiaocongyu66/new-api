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
