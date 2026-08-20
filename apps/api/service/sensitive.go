package service

import "github.com/QuantumNous/new-api/setting"

// CheckSensitiveText 分层敏感词引擎入口：
//
//	L1 词法（AC + 归一化/混淆处理）→ L2 指纹（8 类计分）→ L3 真实载荷模板特征子串。
//
// 引擎行为与 Python 验证基线（~/projects/sensitive-word-test/test_v8.py +
// 54 模板前缀特征）1:1 对齐：jailbreak 1405 池召回 21.1%（296/1405）、
// 正常 3000 池真误伤 2/3000（0.07%，两条均为词库 L1a 命中「政府」的良性文本）。
func CheckSensitiveText(text string) (bool, []string) {
	return sensitiveCheckHits(text, setting.SensitiveWords)
}
