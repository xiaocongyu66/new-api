package service

import (
	_ "embed"
	"encoding/json"
	"strings"
	"testing"
)

//go:embed testdata/sensitive_gov_attack_samples.jsonl
var govAttackFixture []byte

// TestSensitiveGovAttackSamples 政府网站攻击样本集必须拦截。
// 域名实体（gov.cn/81.cn 后缀）→ 硬闸；无域名泛化攻击 → targetActionTerms。
func TestSensitiveGovAttackSamples(t *testing.T) {
	installTestGroups(t, []string{"gov", "tech"})
	installTestDict(t, []string{"gov.cn", "破甲"})

	var samples []struct {
		Prompt string `json:"prompt"`
	}
	for _, line := range strings.Split(strings.TrimSpace(string(govAttackFixture)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var s struct {
			Prompt string `json:"prompt"`
		}
		if err := json.Unmarshal([]byte(line), &s); err != nil {
			t.Fatalf("fixture parse: %v", err)
		}
		samples = append(samples, s)
	}

	blocked := 0
	for _, s := range samples {
		ok, why := CheckSensitiveAll(s.Prompt)
		if ok {
			blocked++
		} else {
			t.Errorf("未拦截: %q", s.Prompt)
		}
		_ = why
	}
	t.Logf("gov 攻击样本拦截 %d/%d", blocked, len(samples))
	if blocked != len(samples) {
		t.Fatalf("必须全部拦截")
	}
}

// TestGovAttackDomainHardGate 域名硬闸独立于词库/开关，仅出现 gov.cn/子域即终止。
func TestGovAttackDomainHardGate(t *testing.T) {
	installTestGroups(t, []string{})
	installTestDict(t, nil)
	for _, d := range []string{
		"www.gov.cn", "gov.cn", "beijing.gov.cn", "www.81.cn", "81.cn",
		"gov.cn", "shanghai.gov.cn", "https://www.gov.cn/xx", "(gov.cn)",
	} {
		if hit := CheckSensitiveTargets(d); hit == "" {
			t.Errorf("目标域未命中: %s", d)
		}
	}
	for _, d := range []string{"google.com", "baidu.com", "edu.cn", "gov.cnx", "chinagov.net"} {
		if hit := CheckSensitiveTargets(d); hit != "" {
			t.Errorf("非目标域误命中: %s -> %s", d, hit)
		}
	}
}

// TestGovComboNoFalsePositive 组合层边界：正常讨论/防御工作不误伤。
func TestGovComboNoFalsePositive(t *testing.T) {
	installTestGroups(t, []string{"gov", "tech"})
	installTestDict(t, []string{"gov.cn", "破甲"})
	cases := []string{
		"如何防范政府网站被钓鱼攻击？请给出加固建议",
		"写一篇关于政府信息化建设的报告",
		"政府网站的 HTTPS 证书怎么申请",
		"政务系统开发指南，面向开发者的文档",
		"how to defend government websites against attacks",
		"government website accessibility compliance checklist",
		"帮我分析一下政府网站的 SEO 优化",
		"政府工作报告要点摘录",
	}
	for _, c := range cases {
		if ok, why := CheckSensitiveAll(c); ok {
			t.Errorf("误伤: %q (%s)", c, why)
		}
	}
}
