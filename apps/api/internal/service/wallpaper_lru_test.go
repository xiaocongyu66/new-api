package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWallpaperLRUKeepsLastThree(t *testing.T) {
	l := &wallpaperLRU{limit: 3}
	for i := 0; i < 5; i++ {
		l.add(localWallpaper{data: []byte{byte(i)}, contentType: "image/jpeg"})
	}
	require.Equal(t, 3, l.count(), "LRU 只保留最近 3 张")
	// 最旧的 0、1 应被淘汰；20 次随机取样应覆盖最近三张全部（2/3/4）。
	seen := map[byte]bool{}
	for range 20 {
		item, ok := l.random()
		require.True(t, ok)
		seen[item.data[0]] = true
	}
	require.Equal(t, map[byte]bool{2: true, 3: true, 4: true}, seen)
}

func TestWallpaperLRUDedup(t *testing.T) {
	l := &wallpaperLRU{limit: 3}
	l.add(localWallpaper{data: []byte("a"), contentType: "image/jpeg"})
	l.add(localWallpaper{data: []byte("b"), contentType: "image/jpeg"})
	l.add(localWallpaper{data: []byte("a"), contentType: "image/jpeg"}) // 重复
	assert.Equal(t, 2, l.count(), "重复图不重复占位")
}

func TestLocalWallpapersEmbedded(t *testing.T) {
	require.Len(t, localWallpapers, 3, "三张内置卡通备用图必须 embed 成功")
	for _, w := range localWallpapers {
		assert.True(t, len(w.data) > 10000, "备用图数据非空")
		assert.Contains(t, []string{"image/jpeg", "image/png"}, w.contentType)
	}
}
