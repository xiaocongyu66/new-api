package insight

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractToolCallSpans(t *testing.T) {
	t.Parallel()

	// OpenAI style arguments
	raw1 := `{"tool_calls":[{"function":{"name":"code_interpreter","arguments":"def f():\n  return 1"}}]}`
	spans := extractToolCallSpans(raw1)
	assert.NotEmpty(t, spans, "OpenAI style tool call arguments must be detected")
	assert.Contains(t, raw1[spans[0].Start:spans[0].End], "def f()")

	// Anthropic style parameter
	raw2 := `<parameter name="code">class Foo:\n  pass</parameter>`
	spans = extractToolCallSpans(raw2)
	assert.NotEmpty(t, spans, "Anthropic style parameter must be detected")

	// No tool context
	raw3 := "just a normal chat message"
	spans = extractToolCallSpans(raw3)
	assert.Empty(t, spans, "Normal message should have no tool spans")
}

func TestAnalyzeCodeStructureWithToolContext(t *testing.T) {
	t.Parallel()

	// Scenario 1: Code ONLY in tool call arguments -> should NOT be classified as code
	toolOnlyCode := `{"tool_calls":[{"function":{"name":"code_interpreter","arguments":"def hello_world():\n    print(\"hello\")\n    return True"}}]}`
	spans := extractToolCallSpans(toolOnlyCode)
	v := analyzeCodeStructureWithToolContext(toolOnlyCode, spans)
	assert.False(t, v.IsCode, "Code inside tool arguments alone should not be classified as user writing code")

	// Scenario 2: Code OUTSIDE tool call arguments -> should be classified as code
	codeOutside := "please write a Python script:\ndef hello_world():\n    print(\"hello\")\n    return True"
	v = analyzeCodeStructureWithToolContext(codeOutside, nil)
	assert.True(t, v.IsCode, "Code outside tool context should be classified as code")

	// Scenario 3: Code in BOTH tool args and outside -> should be classified as code
	mixed := "please help me fix this function:\ndef hello():\n    pass\nAlso here is the existing code: {\"tool_calls\":[{\"function\":{\"name\":\"str_replace\",\"arguments\":\"def old():\n  return 1\"}}]}"
	spans = extractToolCallSpans(mixed)
	v = analyzeCodeStructureWithToolContext(mixed, spans)
	assert.True(t, v.IsCode, "Code outside tool context should still trigger classification even with tool code present")

	// Scenario 4: Natural language (no code) -> should NOT be classified as code
	natural := "Hello, how are you today? I would like to ask about machine learning."
	v = analyzeCodeStructureWithToolContext(natural, nil)
	assert.False(t, v.IsCode, "Natural language should not be classified as code")

	// Scenario 5: Tool call with markdown fence inside arguments
	fenceInTool := "{\"tool_calls\":[{\"function\":{\"name\":\"write_file\",\"arguments\":\"```python\\ndef f():\\n  pass\\n```\"}}]}"
	spans = extractToolCallSpans(fenceInTool)
	v = analyzeCodeStructureWithToolContext(fenceInTool, spans)
	assert.False(t, v.IsCode, "Markdown fence inside tool arguments should not be classified as code")

	// Scenario 6: DeepSeek R1 style with tool_calls containing code arguments
	deepSeekTool := "{\"tool_calls\":[{\"function\":{\"name\":\"code_interpreter\",\"arguments\":\"def solve(n):\\n    return n * 2\\n\\nprint(solve(5))\"}}]}"
	spans = extractToolCallSpans(deepSeekTool)
	v = analyzeCodeStructureWithToolContext(deepSeekTool, spans)
	assert.False(t, v.IsCode, "DeepSeek tool call arguments should not trigger code classification")

	// Scenario 7: Real user code WITH tool calls mixed - user message has code
	mixedReal := "Please fix this bug in my code:\n```python\ndef calculate(a, b):\n    return a + b  # should be a * b\n```\nAlso check this tool output: {\"tool_calls\":[{\"function\":{\"name\":\"str_replace\",\"arguments\":\"old_code\"}}]}"
	spans = extractToolCallSpans(mixedReal)
	v = analyzeCodeStructureWithToolContext(mixedReal, spans)
	assert.True(t, v.IsCode, "User code outside tool context should still be classified as code even with tool calls present")

	// Scenario 8: Multiple tool calls with different code in each
	multiTool := "{\"tool_calls\":[{\"function\":{\"name\":\"code_interpreter\",\"arguments\":\"def a():\\n  return 1\"}},{\"function\":{\"name\":\"write_file\",\"arguments\":\"def b():\\n  return 2\"}},{\"function\":{\"name\":\"str_replace\",\"arguments\":\"def c():\\n  return 3\"}}]}"
	spans = extractToolCallSpans(multiTool)
	v = analyzeCodeStructureWithToolContext(multiTool, spans)
	assert.False(t, v.IsCode, "Multiple tool calls with code in arguments should not be classified as code")
}

func TestToolContextEdgeCases(t *testing.T) {
	t.Parallel()

	// Empty tool arguments
	emptyArgs := `{"tool_calls":[{"function":{"name":"code_interpreter","arguments":""}}]}`
	spans := extractToolCallSpans(emptyArgs)
	v := analyzeCodeStructureWithToolContext(emptyArgs, spans)
	assert.False(t, v.IsCode, "Empty tool arguments should not be classified as code")

	// Nested quotes in arguments (escaped)
	nestedQuotes := `{"tool_calls":[{"function":{"name":"echo","arguments":"He said \"hello\" to me"}}]}`
	spans = extractToolCallSpans(nestedQuotes)
	// This should not panic
	_ = analyzeCodeStructureWithToolContext(nestedQuotes, spans)

	// Very long tool arguments (simulating a large file)
	longArg := `{"tool_calls":[{"function":{"name":"write_file","arguments":"` + strings.Repeat("def func():\n    pass\n", 100) + `"}}]}`
	spans = extractToolCallSpans(longArg)
	v = analyzeCodeStructureWithToolContext(longArg, spans)
	assert.False(t, v.IsCode, "Long tool arguments should not cause performance issues or false positives")
}
