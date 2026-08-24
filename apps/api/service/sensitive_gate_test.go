package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckSensitiveTargets(t *testing.T) {
	cases := []struct {
		text string
		want string // 命中域名（空 = 不命中）
	}{
		{"https://www.gov.cn/xx", "www.gov.cn"},
		{"gov.cn", "gov.cn"},
		{"访问 12345.gov.cn 查询投诉", "12345.gov.cn"},
		{"https://blog.gov.cn/p/1", "blog.gov.cn"},
		{"second.gov.cn/index.php", "second.gov.cn"},
		{"target=gov.cn&x=1", "gov.cn"},
		{"中国政务网 www.gov.cn 发布", "www.gov.cn"},
		{"abc.example.com 正常", ""},
		{"gov.com 不是政务域", ""},
		{"government.cn 不是政务域", ""},
		{"我写了一篇关于政府工作报告的总结", ""},
		{"目标网络 evil.cn 无关", ""},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, CheckSensitiveTargets(tc.text), "text=%q", tc.text)
	}
}

func TestSensitiveTargetsCyrillicHomoglyph(t *testing.T) {
	hits := []string{
		"gоv.cn",                 // 西里尔 о U+043E
		"gοv.cn",                 // 希腊 ο U+03BF
		"ｇov.cn",                 // 全角 ｇ U+FF47
		"https://www.gоv.cn/a/b", // URL 内西里尔同形
		"ｗｗｗ．８１．ｃｎ",              // 全整角 81.cn
		"攻击 gоv.cn 的方法",          // 混入句子
	}
	for _, s := range hits {
		require.NotEmpty(t, CheckSensitiveTargets(s), "同形变体必须命中硬闸: %q", s)
	}
	// 正常文本不受折叠影响
	assert.Equal(t, "www.gov.cn", CheckSensitiveTargets("https://www.gov.cn/xx"))
	assert.Equal(t, "", CheckSensitiveTargets("abc.example.com 正常"))
}

func TestIsTargetDomain(t *testing.T) {
	assert.True(t, IsTargetDomain("gov.cn"))
	assert.True(t, IsTargetDomain("www.gov.cn"))
	assert.True(t, IsTargetDomain("x.y.gov.cn"))
	assert.True(t, IsTargetDomain("WWW.GOV.CN"))
	assert.False(t, IsTargetDomain("gov.com"))
	assert.False(t, IsTargetDomain("gov"))
	assert.False(t, IsTargetDomain("notgov.cn"))
}
