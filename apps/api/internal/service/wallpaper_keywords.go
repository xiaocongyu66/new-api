package service

import (
	"math/rand/v2"
	"strings"

	"github.com/QuantumNous/new-api/internal/common"
)

// cuteWallpaperKeywords 是内置的可爱卡通壁纸关键词。
// 与抓取逻辑解耦：新增/删减关键词只改这里；管理员也可以在后台
// 「站点与品牌 → 背景设置」里用自定义关键词覆盖这份内置清单。
// 角色名而非泛词（如 "cute"）能显著降低命中真人/动漫人物图的概率。
var cuteWallpaperKeywords = []string{
	"chiikawa",
	"sanrio",
	"cinnamoroll",
	"pompompurin",
	"kuromi",
	"my melody",
	"pochacco",
	"keroppi",
	"tuxedosam",
	"chococat",
	"gudetama",
	"pikachu",
	"eevee",
	"jigglypuff",
	"rilakkuma",
	"kirby",
	"totoro",
	"doraemon",
	"miffy",
	"hello kitty",
	"pokemon",
}

// selectWallpaperKeyword 随机返回一个关键词：优先使用后台配置的关键词，
// 配置为空时回退到内置清单。
func selectWallpaperKeyword() string {
	common.OptionMapRWMutex.RLock()
	configured := common.OptionMap["wallpaper.keywords"]
	common.OptionMapRWMutex.RUnlock()

	var keywords []string
	for _, part := range strings.FieldsFunc(configured, func(r rune) bool {
		return r == ',' || r == '\n' || r == ';'
	}) {
		if keyword := strings.TrimSpace(part); keyword != "" {
			keywords = append(keywords, keyword)
		}
	}
	if len(keywords) == 0 {
		keywords = cuteWallpaperKeywords
	}
	return keywords[rand.IntN(len(keywords))]
}
