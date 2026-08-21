package service

// 全新网页抓取样本评测（与旧 1405/3000 池零重叠，2026-08-21 抓取）：
//   - attack 10：DAN / DAN6.0 / STAN / DUDE / Mongo Tom 原文（coolaj86 gist）、
//     ZORG ×2（trinib/ZORG）、伪装攻击 3（heist 剧本 / 毒药教科书 / 军事通信 RP）
//   - normal 10：learnprompt.org everyday prompts
//   - rp 8：tavernsprite 角色卡模板（浪漫/导师/对手/神秘 + 填充变体）
//
// 断言：normal 与 rp 样本在任何组配置下均不得误伤（验收锚点）；
// 攻击召回率仅记录（对旧池行为有硬断言，见 sensitive_engine_test.go）。
// 样本源：gist coolaj86/6f4f7b30129b0251f61fa7baaa881516、trinib/ZORG、
// learnprompt.org、tavernsprite.com（SillyTavern archetype 模板）。

import (
	"embed"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//go:embed testdata/sensitive_eval_fresh.json
var freshEvalFixture []byte

type freshEvalCase struct {
	ID   string `json:"id"`
	Cat  string `json:"cat"`
	Name string `json:"name"`
	Text string `json:"text"`
}

func loadFreshEvalCases(t testing.TB) []freshEvalCase {
	t.Helper()
	var payload struct {
		Cases []freshEvalCase `json:"cases"`
	}
	require.NoError(t, json.Unmarshal(freshEvalFixture, &payload))
	require.Len(t, payload.Cases, 28, "fresh eval pool size")
	return payload.Cases
}

func evalCatCount(cases []freshEvalCase, cat string) int {
	n := 0
	for _, c := range cases {
		if c.Cat == cat {
			n++
		}
	}
	return n
}

// runEvalMatrix 跑完整矩阵：断言正常/RP 零误伤，返回攻击被拦的 id 列表。
func runEvalMatrix(t *testing.T, cases []freshEvalCase, groups []string) []string {
	t.Helper()
	installTestGroups(t, groups)
	var attackHits []string
	for _, c := range cases {
		got, words := CheckSensitiveText(c.Text)
		switch c.Cat {
		case "attack":
			if got {
				attackHits = append(attackHits, c.ID)
				t.Logf("[%v] BLOCK %s (%s) hit=%v", groups, c.ID, c.Name, words)
			} else {
				t.Logf("[%v] leak  %s (%s)", groups, c.ID, c.Name)
			}
		case "normal", "rp":
			if got {
				t.Errorf("误伤 %s %s (%s): %v", c.Cat, c.ID, c.Name, words)
			}
		}
	}
	return attackHits
}

// TestSensitiveEvalFreshDefault 默认组（gov,tech）跑全样本，零误伤硬断言 +
// 攻击召回锚点记录（不做硬断言：模板库只覆盖已知载荷，召回提升属后续范略）。
func TestSensitiveEvalFreshDefault(t *testing.T) {
	cases := loadFreshEvalCases(t)
	require.Equal(t, 10, evalCatCount(cases, "attack"))
	require.Equal(t, 10, evalCatCount(cases, "normal"))
	require.Equal(t, 8, evalCatCount(cases, "rp"))

	_, _, words, _, _, _, _ := loadSensitiveTestData(t)
	installTestDict(t, words)

	hits := runEvalMatrix(t, cases, []string{"gov", "tech"})
	t.Logf("default(gov,tech): attack blocked %d/10: %v", len(hits), hits)
}

// TestSensitiveEvalFreshAllGroups 全开组（gov,tech,rp）对比。验证 rp 组开启
// 不新增误伤（真实 RP/正常样本仍全放行）；攻击差异记录。
func TestSensitiveEvalFreshAllGroups(t *testing.T) {
	cases := loadFreshEvalCases(t)
	_, _, words, _, _, _, _ := loadSensitiveTestData(t)
	installTestDict(t, words)

	hits := runEvalMatrix(t, cases, []string{"gov", "tech", "rp"})
	t.Logf("all(gov,tech,rp): attack blocked %d/10: %v", len(hits), hits)
}

// TestSensitiveEvalFreshZeroFalsePositive 零误伤专项：默认组与全开组下
// 全部 18 条正常/RP 样本均不得被拦。
func TestSensitiveEvalFreshZeroFalsePositive(t *testing.T) {
	cases := loadFreshEvalCases(t)
	_, _, words, _, _, _, _ := loadSensitiveTestData(t)

	for _, groups := range [][]string{{"gov", "tech"}, {"gov", "tech", "rp"}} {
		installTestGroups(t, groups)
		installTestDict(t, words)
		for _, c := range cases {
			if c.Cat == "attack" {
				continue
			}
			got, _ := CheckSensitiveText(c.Text)
			require.Falsef(t, got, "%v 误伤 %s %s (%s)", groups, c.Cat, c.ID, c.Name)
		}
	}
}
