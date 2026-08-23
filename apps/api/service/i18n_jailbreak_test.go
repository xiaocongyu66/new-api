package service

import (
	_ "embed"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

//go:embed testdata/sensitive_i18n_samples.jsonl
var i18nSamplesFixture []byte

// TestSensitiveI18nSamples 多语言（日/韩/法/德/西/俄）破甲样本必须拦，
// 正常多语言文本不得误伤。样本来自 testdata/sensitive_i18n_samples.jsonl。
func TestSensitiveI18nSamples(t *testing.T) {
	installTestGroups(t, []string{"gov", "tech", "rp"})
	installTestDict(t, []string{"gov.cn", "破甲"})

	var samples []struct {
		Lang   string `json:"lang"`
		Expect string `json:"expect"` // block | pass
		Prompt string `json:"prompt"`
	}
	for _, line := range strings.Split(strings.TrimSpace(string(i18nSamplesFixture)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var s struct {
			Lang   string `json:"lang"`
			Expect string `json:"expect"`
			Prompt string `json:"prompt"`
		}
		require.NoError(t, json.Unmarshal([]byte(line), &s), "fixture parse: %s", line)
		samples = append(samples, s)
	}
	require.NotEmpty(t, samples)

	blocked, passed := 0, 0
	for _, s := range samples {
		ok, why := CheckSensitiveAll(s.Prompt)
		switch s.Expect {
		case "block":
			require.True(t, ok, "[%s] 应拦截: %s", s.Lang, s.Prompt)
			blocked++
		case "pass":
			require.False(t, ok, "[%s] 不应拦截: %s -> %s", s.Lang, s.Prompt, why)
			passed++
		default:
			t.Fatalf("fixture expect 字段非法: %q", s.Expect)
		}
	}
	t.Logf("i18n 样本: 拦截 %d / 放行 %d", blocked, passed)
}
