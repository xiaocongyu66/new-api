package setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFilterSensitiveWords(t *testing.T) {
	words := []string{
		"政府", "中央", "军火", "破甲", "越狱", "毒品",
		"出售全国政府官员数据", "gov.cn", "政府工作报告摘要",
		"", "  ",
	}
	got := FilterSensitiveWords(words)
	assert.ElementsMatch(t, []string{
		"破甲", "越狱", "毒品", "军火", // 攻击短词白名单保留
		"出售全国政府官员数据", "gov.cn", "政府工作报告摘要",
	}, got)
}

func TestSensitiveWordsFromStringFiltersShort(t *testing.T) {
	old := SensitiveWords
	t.Cleanup(func() { SensitiveWords = old })

	SensitiveWordsFromString("政府\n中央\n破甲\n越狱\n出售全国政府官员数据\n军火")
	assert.ElementsMatch(t, []string{"破甲", "越狱", "军火", "出售全国政府官员数据"}, SensitiveWords)
}