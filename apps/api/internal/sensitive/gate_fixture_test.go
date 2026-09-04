package sensitive

import "testing"

// 现有测试夹具 loadSensitiveTestData 的薄封装（避免改原签名）。
func loadSensitiveTestDataForTarget(t *testing.T) []string {
	_, _, words, _, _, _, _ := loadSensitiveTestData(t)
	return words
}
