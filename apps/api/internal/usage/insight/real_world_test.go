package insight_test

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/internal/usage/insight"
	"github.com/stretchr/testify/assert"
)

// TestRealWorldScenarios 用线上真实请求模式验证检测效果
func TestRealWorldScenarios(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		body     []byte
		path     string
		expected string // 期望的 category
		minCode  int    // code score 最低期望（0 表示不期望是代码）
	}{
		// ========== 真写代码 ==========
		{
			name: "real_code_python_function",
			body: []byte(`{"model":"claude-sonnet-4-20250514","messages":[{"role":"user","content":"请帮我写一个 Python 函数，计算斐波那契数列：\ndef fibonacci(n):\n    if n <= 1:\n        return n\n    return fibonacci(n-1) + fibonacci(n-2)\n\nprint(fibonacci(10))"}]}`),
			path:     "/v1/chat/completions",
			expected: insight.CategoryCode,
			minCode:  20,
		},
		{
			name: "real_code_react_component",
			body: []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"写一个 React 组件：\nimport React, { useState } from 'react';\n\nexport function Counter() {\n  const [count, setCount] = useState(0);\n  return (\n    <div>\n      <p>{count}</p>\n      <button onClick={() => setCount(count + 1)}>+</button>\n    </div>\n  );\n}"}]}`),
			path:     "/v1/chat/completions",
			expected: insight.CategoryCode,
			minCode:  20,
		},
		{
			name: "real_code_error_debug",
			body: []byte(`{"model":"claude-sonnet-4-20250514","messages":[{"role":"user","content":"报错：\nTraceback (most recent call last):\n  File \"main.py\", line 10, in <module>\n    result = divide(10, 0)\nZeroDivisionError: division by zero\n\n代码是：\ndef divide(a, b):\n    return a / b\n\n请帮我修复"}]}`),
			path:     "/v1/chat/completions",
			expected: insight.CategoryCode,
			minCode:  15,
		},
		{
			name: "tool_call_code_interpreter",
			body: []byte(`{"model":"claude-sonnet-4-20250514","messages":[{"role":"user","content":"Run this code"}],"tools":[{"type":"function","function":{"name":"code_interpreter"}}],"tool_choice":{"type":"tool","name":"code_interpreter"}}}`),
			path:     "/v1/chat/completions",
			expected: insight.CategoryOther,
			minCode:  0,
		},
		{
			name: "tool_call_str_replace",
			body: []byte(`{"model":"claude-sonnet-4-20250514","messages":[{"role":"assistant","content":"","tool_calls":[{"function":{"name":"str_replace","arguments":"def old():\n  return 1"}}]}]}`),
			path:     "/v1/chat/completions",
			expected: insight.CategoryOther,
			minCode:  0,
		},
		{
			name: "tool_call_deepseek_r1",
			body: []byte(`{"model":"deepseek-r1","messages":[{"role":"assistant","content":"","tool_calls":[{"function":{"name":"code_interpreter","arguments":"def solve(n):\n    return n * 2\n\nprint(solve(5))"}}]}]}`),
			path:     "/v1/chat/completions",
			expected: insight.CategoryOther,
			minCode:  0,
		},
		{
			name: "tool_call_with_system_reminder",
			body: []byte(`{"model":"claude-sonnet-4-20250514","system":[{"type":"text","text":"<system-reminder>The user has changed their message...</system-reminder>"}],"messages":[{"role":"user","content":"Run the code"}]}`),
			path:     "/v1/chat/completions",
			expected: insight.CategoryOther,
			minCode:  0,
		},

		// ========== 角色扮演（不应判为代码） ==========
		{
			name: "roleplay_sillytavern",
			body: []byte(`{"model":"claude-sonnet-4-20250514","system":[{"type":"text","text":"{{char}} is a wizard. Write {{char}}'s next reply in a fictional roleplay between {{char}} and {{user}}. Stay in character. Use asterisks for actions."}],"messages":[{"role":"user","content":"Hello there!"}]}`),
			path:     "/v1/chat/completions",
			expected: insight.CategoryRoleplay,
			minCode:  0,
		},
		{
			name: "roleplay_preset_class",
			body: []byte(`{"model":"claude-sonnet-4-20250514","system":[{"type":"text","text":"[Start a new chat]\nYou are a writer.\nclass Ariadne(MethodActor, Biographer):\n  def __init__(self):\n    self.stance = \"write from inside the character\"\n    # moment: capture what is here\n旁白不得总结剧情，扮演时保持角色一致。"}],"messages":[{"role":"user","content":"Start the scene"}]}`),
			path:     "/v1/chat/completions",
			expected: insight.CategoryRoleplay,
			minCode:  0,
		},

		// ========== 普通问答（不应判为代码） ==========
		{
			name: "qa_what_is",
			body: []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"什么是布隆过滤器？请解释一下它的原理"}]}`),
			path:     "/v1/chat/completions",
			expected: insight.CategoryQA,
			minCode:  0,
		},
		{
			name: "qa_chat",
			body: []byte(`{"model":"claude-sonnet-4-20250514","messages":[{"role":"user","content":"你好，今天天气怎么样？帮我推荐一部电影"}]}`),
			path:     "/v1/chat/completions",
			expected: insight.CategoryQA,
			minCode:  0,
		},

		// ========== 翻译（不应判为代码） ==========
		{
			name: "translate_request",
			body: []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"translate the following to Chinese: Hello world, this is a test."}]}`),
			path:     "/v1/chat/completions",
			expected: insight.CategoryTranslate,
			minCode:  0,
		},

		// ========== 混合场景 ==========
		{
			name: "mixed_user_code_with_tool_calls",
			body: []byte(`{"model":"claude-sonnet-4-20250514","messages":[{"role":"user","content":"帮我修复这段代码：\ndef hello():\n    print('hello')\n    # TODO: add return"},{"role":"assistant","content":"Here is the fix:","tool_calls":[{"function":{"name":"str_replace","arguments":"def hello():\n    print('hello')\n    return True"}}]}]}`),
			path:     "/v1/chat/completions",
			expected: insight.CategoryCode,
			minCode:  15,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := insight.Analyze(http.Header{}, tc.body, tc.path, insight.Options{})
			assert.Equal(t, tc.expected, result.Category,
				"Category mismatch: got %s (code=%d, roleplay=%d, qa=%d)",
				result.Category, result.CodeScore, result.RoleplayScore, result.QAScore)
			if tc.minCode > 0 {
				assert.GreaterOrEqual(t, result.CodeScore, tc.minCode,
					"Code score too low: got %d, want >= %d", result.CodeScore, tc.minCode)
			}
		})
	}
}
