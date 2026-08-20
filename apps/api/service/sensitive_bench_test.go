package service

import (
	"encoding/json"
	"strings"
	"testing"
)

// benchRows 混合语料：正常英文 + 正常中文 + 越狱载荷 + 绕过手法，
// 模拟真实 relay 流量分布。
func benchRows(tb testing.TB) []string {
	tb.Helper()
	var jail []struct {
		Prompt string `json:"prompt"`
	}
	for _, line := range strings.Split(string(jailbreakFixture), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var r struct {
			Prompt string `json:"prompt"`
		}
		_ = json.Unmarshal([]byte(line), &r)
		jail = append(jail, r)
	}
	var norm []struct {
		Prompt string `json:"prompt"`
	}
	for _, line := range strings.Split(string(normalFixture), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var r struct {
			Prompt string `json:"prompt"`
		}
		_ = json.Unmarshal([]byte(line), &r)
		norm = append(norm, r)
	}

	// 越狱池 1405 + 正常池 1500 → 2905 条（重现实流量：绝大多数是正常请求）
	rows := make([]string, 0, len(jail)+1500)
	rows = append(rows, "What is the capital of France? Give me a summary of today's tech news in three points.", "帮我总结一下最近的技术新闻，并给出三个例子。")
	for _, r := range jail {
		rows = append(rows, r.Prompt)
	}
	for i := 0; i < 1500; i++ {
		rows = append(rows, norm[i].Prompt)
	}
	return rows
}

// BenchmarkSensitiveEngine 分层引擎全路径（含词库 AC + 指纹 + 模板）。
func BenchmarkSensitiveEngine(b *testing.B) {
	_, _, words, _, _ := loadSensitiveTestData(b)
	installTestDict(b, words)
	rows := benchRows(b)
	b.ResetTimer()
	total := 0
	for i := 0; i < b.N; i++ {
		for _, r := range rows {
			if blocked, _ := CheckSensitiveText(r); blocked {
				total++
			}
		}
	}
	b.StopTimer()
	_ = total
}

// BenchmarkSensitiveLegacyToLowerAc baseline：strings.ToLower + AC 词库（旧实现路径）。
func BenchmarkSensitiveLegacyToLowerAC(b *testing.B) {
	_, _, words, _, _ := loadSensitiveTestData(b)
	installTestDict(b, words)
	rows := benchRows(b)
	b.ResetTimer()
	total := 0
	for i := 0; i < b.N; i++ {
		for _, r := range rows {
			if ok, _ := AcSearchLegacy(strings.ToLower(r), words, true); ok {
				total++
			}
		}
	}
	b.StopTimer()
	_ = total
}

// BenchmarkSensitiveNormal 纯正常池（Issue 验收口径：正常请求 <=2.5x 现状）。
func BenchmarkSensitiveNormal(b *testing.B) {
	_, _, words, _, _ := loadSensitiveTestData(b)
	installTestDict(b, words)
	var rows []string
	for _, line := range strings.Split(string(normalFixture), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var r struct {
			Prompt string `json:"prompt"`
		}
		_ = json.Unmarshal([]byte(line), &r)
		rows = append(rows, r.Prompt)
	}
	b.Logf("rows=%d avgLen=%d", len(rows), totalBytes(rows)/len(rows))
	total := 0
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, r := range rows {
			if blocked, _ := CheckSensitiveText(r); blocked {
				total++
			}
		}
	}
	b.StopTimer()
	_ = total
}

// BenchmarkSensitiveLegacyNormal 正常池 × 旧实现（ToLower + AC）。
func BenchmarkSensitiveLegacyNormal(b *testing.B) {
	_, _, words, _, _ := loadSensitiveTestData(b)
	installTestDict(b, words)
	var rows []string
	for _, line := range strings.Split(string(normalFixture), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var r struct {
			Prompt string `json:"prompt"`
		}
		_ = json.Unmarshal([]byte(line), &r)
		rows = append(rows, r.Prompt)
	}
	b.Logf("rows=%d avgLen=%d", len(rows), totalBytes(rows)/len(rows))
	total := 0
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, r := range rows {
			if ok, _ := AcSearchLegacy(strings.ToLower(r), words, true); ok {
				total++
			}
		}
	}
	b.StopTimer()
	_ = total
}

func totalBytes(ss []string) int {
	n := 0
	for _, s := range ss {
		n += len(s)
	}
	return n
}
