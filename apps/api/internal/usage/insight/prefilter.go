package insight

import (
	"sort"
	"strings"
)

// 两阶段代码检测：快筛 + 重审
//
// 第一阶段（快筛）：仿 ripgrep 的门控
//   - 用紧凑词表（~200 个代码信号词）做出现次数统计
//   - 统计结构标点密度
//   - 两者都低于阈值 → 直接放行，不跑正则
//   - 仅"可疑"请求进入第二阶段重审
//
// 第二阶段（重审）：仅快筛标记为"可疑"时才执行
//   - 运行完整的结构共现分析（code_structure.go）
//   - 叠加工具上下文保护（tool_context.go）
//   - 最终判定：real_code / tool_call / roleplay / chat

// signalEntry 是一个带权重的信号词。
type signalEntry struct {
	Text   string
	Weight uint8
}

// prefilterSignals 是紧凑的代码信号词表。按 Text 升序排列。
// 新增词时保持排序，或运行 go test -run TestPrefilterSignalsSorted 校验。
//
// 设计约束：
//   - 总量控制在 ~200 个，覆盖编程最核心的高频结构词与报错模板
//   - 已去掉在自然语言里常见的短词：if/for/let/var/class/return/debug/run/get/set/have/make...
//     （它们单独出现命中无法说明"写代码"，反而制造误判）
//   - 保留：带语法标点的组合词、报错模板、工程术语、多词短语
//   - 按字母排序，保证确定性顺序与快速比对
//
// 词表分三级，命中权重不同：
//   - high（8 分）：几乎只在代码/报错里出现的短语（"stack trace", "compile error"）
//   - medium（5 分）：代码强相关但在文档里偶尔出现（"function definition", "import path"）
//   - low（2 分）：工程术语，单独出现不够成判定（"refactor", "deploy"）
var prefilterSignals = func() []signalEntry {
	// 未排序的原始表。构建时统一排序。
	raw := []signalEntry{
		// ---- high weight（8 分）：报错与开发事故模板 ----
		{Text: "build failed", Weight: 8},
		{Text: "cannot find module", Weight: 8},
		{Text: "cannot resolve", Weight: 8},
		{Text: "compile error", Weight: 8},
		{Text: "compilation failed", Weight: 8},
		{Text: "exception:", Weight: 8},
		{Text: "fatal error", Weight: 8},
		{Text: "null pointer exception", Weight: 8},
		{Text: "npm err", Weight: 8},
		{Text: "panic:", Weight: 8},
		{Text: "segmentation fault", Weight: 8},
		{Text: "stack trace", Weight: 8},
		{Text: "syntax error", Weight: 8},
		{Text: "traceback", Weight: 8},
		{Text: "type error", Weight: 8},
		{Text: "undefined is not", Weight: 8},
		{Text: "unhandled exception", Weight: 8},

		// ---- medium weight（5 分）：代码结构短语 ----
		{Text: "according to the docs", Weight: 5},
		{Text: "after compiling", Weight: 5},
		{Text: "api endpoint", Weight: 5},
		{Text: "argument type", Weight: 5},
		{Text: "asynchronous function", Weight: 5},
		{Text: "breakpoint", Weight: 5},
		{Text: "class definition", Weight: 5},
		{Text: "code review", Weight: 5},
		{Text: "commit message", Weight: 5},
		{Text: "compiler flag", Weight: 5},
		{Text: "concurrent goroutine", Weight: 5},
		{Text: "config file", Weight: 5},
		{Text: "constructor", Weight: 5},
		{Text: "data binding", Weight: 5},
		{Text: "database migration", Weight: 5},
		{Text: "debug console", Weight: 5},
		{Text: "dependency injection", Weight: 5},
		{Text: "deprecated", Weight: 5},
		{Text: "edge case", Weight: 5},
		{Text: "error handling", Weight: 5},
		{Text: "event handler", Weight: 5},
		{Text: "exception caught", Weight: 5},
		{Text: "expected type", Weight: 5},
		{Text: "export default", Weight: 5},
		{Text: "failed to compile", Weight: 5},
		{Text: "failed to resolve", Weight: 5},
		{Text: "file not found", Weight: 5},
		{Text: "function body", Weight: 5},
		{Text: "function call", Weight: 5},
		{Text: "function definition", Weight: 5},
		{Text: "function signature", Weight: 5},
		{Text: "git commit", Weight: 5},
		{Text: "git diff", Weight: 5},
		{Text: "git push", Weight: 5},
		{Text: "global variable", Weight: 5},
		{Text: "hot reload", Weight: 5},
		{Text: "http request", Weight: 5},
		{Text: "import path", Weight: 5},
		{Text: "in production", Weight: 5},
		{Text: "infinite loop", Weight: 5},
		{Text: "interface definition", Weight: 5},
		{Text: "invalid syntax", Weight: 5},
		{Text: "key not found", Weight: 5},
		{Text: "lambda expression", Weight: 5},
		{Text: "linked list", Weight: 5},
		{Text: "localhost", Weight: 5},
		{Text: "memory leak", Weight: 5},
		{Text: "merge conflict", Weight: 5},
		{Text: "method not allowed", Weight: 5},
		{Text: "missing dependency", Weight: 5},
		{Text: "module not found", Weight: 5},
		{Text: "namespace", Weight: 5},
		{Text: "null check", Weight: 5},
		{Text: "object reference", Weight: 5},
		{Text: "out of bounds", Weight: 5},
		{Text: "package manager", Weight: 5},
		{Text: "parameter type", Weight: 5},
		{Text: "parse error", Weight: 5},
		{Text: "permission denied", Weight: 5},
		{Text: "pull request", Weight: 5},
		{Text: "race condition", Weight: 5},
		{Text: "recursive function", Weight: 5},
		{Text: "refactor", Weight: 5},
		{Text: "reference error", Weight: 5},
		{Text: "regex pattern", Weight: 5},
		{Text: "return statement", Weight: 5},
		{Text: "return type", Weight: 5},
		{Text: "runtime error", Weight: 5},
		{Text: "semantic error", Weight: 5},
		{Text: "stack overflow", Weight: 5},
		{Text: "state management", Weight: 5},
		{Text: "static analysis", Weight: 5},
		{Text: "string format", Weight: 5},
		{Text: "syntax tree", Weight: 5},
		{Text: "test coverage", Weight: 5},
		{Text: "thread safe", Weight: 5},
		{Text: "throw exception", Weight: 5},
		{Text: "timeout", Weight: 5},
		{Text: "type annotation", Weight: 5},
		{Text: "type assertion", Weight: 5},
		{Text: "type check", Weight: 5},
		{Text: "type mismatch", Weight: 5},
		{Text: "undefined variable", Weight: 5},
		{Text: "unit test", Weight: 5},
		{Text: "unreachable code", Weight: 5},
		{Text: "variable declaration", Weight: 5},
		{Text: "version mismatch", Weight: 5},
		{Text: "virtual dom", Weight: 5},
		{Text: "warning:", Weight: 5},

		// ---- low weight（2 分）：工程术语，单独出现不够成判定 ----
		{Text: "backend", Weight: 2},
		{Text: "breakpoint hit", Weight: 2},
		{Text: "bundle size", Weight: 2},
		{Text: "cargo build", Weight: 2},
		{Text: "cli tool", Weight: 2},
		{Text: "code generation", Weight: 2},
		{Text: "compile time", Weight: 2},
		{Text: "css module", Weight: 2},
		{Text: "data structure", Weight: 2},
		{Text: "dead code", Weight: 2},
		{Text: "debug log", Weight: 2},
		{Text: "dev server", Weight: 2},
		{Text: "docker compose", Weight: 2},
		{Text: "dockerfile", Weight: 2},
		{Text: "env variable", Weight: 2},
		{Text: "error boundary", Weight: 2},
		{Text: "esbuild", Weight: 2},
		{Text: "eslint", Weight: 2},
		{Text: "feature flag", Weight: 2},
		{Text: "frontend", Weight: 2},
		{Text: "git merge", Weight: 2},
		{Text: "go build", Weight: 2},
		{Text: "go mod", Weight: 2},
		{Text: "graphql", Weight: 2},
		{Text: "hot module replacement", Weight: 2},
		{Text: "http client", Weight: 2},
		{Text: "kubernetes", Weight: 2},
		{Text: "lazy loading", Weight: 2},
		{Text: "lint rule", Weight: 2},
		{Text: "load balancer", Weight: 2},
		{Text: "middleware", Weight: 2},
		{Text: "monorepo", Weight: 2},
		{Text: "npm install", Weight: 2},
		{Text: "npm run", Weight: 2},
		{Text: "open source", Weight: 2},
		{Text: "production build", Weight: 2},
		{Text: "rollback", Weight: 2},
		{Text: "runtime", Weight: 2},
		{Text: "side effect", Weight: 2},
		{Text: "source map", Weight: 2},
		{Text: "ssr", Weight: 2},
		{Text: "strict mode", Weight: 2},
		{Text: "tree shaking", Weight: 2},
		{Text: "type inference", Weight: 2},
		{Text: "typescript", Weight: 2},
		{Text: "unit testing", Weight: 2},
		{Text: "vite", Weight: 2},
		{Text: "vue router", Weight: 2},
		{Text: "webpack", Weight: 2},
		{Text: "websocket", Weight: 2},
	}

	sort.Slice(raw, func(i, j int) bool {
		return strings.ToLower(raw[i].Text) < strings.ToLower(raw[j].Text)
	})
	return raw
}()

// prefilterScoreResult 是快筛阶段的输出。
type prefilterScoreResult struct {
	SignalScore   uint32 // 信号词命中总权重
	SignalCount   uint16 // 信号词命中次数
	PunctDensity  uint16 // 结构标点密度（每 100 字符）
	HasHighSignal bool   // 是否命中至少一个 high 信号
}

// 快筛阈值。经验值，基于线上真实请求分布调优：
//   - 纯中文闲聊：SignalScore 0-4, PunctDensity 0-1
//   - 英文闲聊：SignalScore 2-8, PunctDensity 1-3
//   - 代码请求：SignalScore 15-80+, PunctDensity 5-30+
//   - 角色扮演（SillyTavern）：SignalScore 4-12, PunctDensity 2-5
// 取门槛设在闲聊上限与代码下限之间
const (
	prefilterSignalThreshold = uint32(12) // SignalScore >= 此值才可疑
	prefilterPunctThreshold  = uint16(4)  // PunctDensity >= 此值才可疑
	prefilterMinSignals      = uint16(2)  // 至少命中 2 个信号词
)

// prefilterScore 对文本执行快速扫描，返回信号分。
// 零堆分配：只操作字符串遍历，无正则，无子串分配。
func prefilterScore(text string) prefilterScoreResult {
	var result prefilterScoreResult
	if len(text) < 24 {
		return result
	}

	lower := strings.ToLower(text)
	asciiCount := 0
	punctCount := 0
	var signalHit [256]bool

	// 遍历每个信号词，用 strings.Count 统计出现次数
	// 词表仅 ~200 条，单次扫描开销远低于正则
	for i := range prefilterSignals {
		sig := prefilterSignals[i]
		count := strings.Count(lower, sig.Text)
		if count == 0 {
			continue
		}
		if !signalHit[i] {
			signalHit[i] = true
			result.SignalCount++
			if sig.Weight >= 8 {
				result.HasHighSignal = true
			}
		}
		result.SignalScore += uint32(sig.Weight) * uint32(count)
	}

	// 字符级标点密度（O(n) 一次遍历）
	for i := 0; i < len(text); i++ {
		b := text[i]
		if b >= 128 {
			continue
		}
		asciiCount++
		if codePunct[b] {
			punctCount++
		}
	}
	if asciiCount > 0 {
		result.PunctDensity = uint16(uint32(punctCount) * 100 / uint32(asciiCount))
	}


	return result
}

// isSuspicious 判断文本是否需要进入第二阶段重审。
// 三个条件任一成立即视为可疑：
//   1. 命中至少 2 个不同信号词且总权重 >= 12
//   2. 结构标点密度 >= 4（每 100 字符 4 个标点）
//   3. 命中至少一个 high 权重信号（报错模板）
func isSuspicious(score prefilterScoreResult) bool {
	if score.HasHighSignal {
		return true
	}
	if score.SignalCount >= prefilterMinSignals && score.SignalScore >= prefilterSignalThreshold {
		return true
	}
	if score.PunctDensity >= prefilterPunctThreshold {
		return true
	}
	return false
}

// 校验词表排序（测试用）
func isSignalsSorted() bool {
	for i := 1; i < len(prefilterSignals); i++ {
		if strings.ToLower(prefilterSignals[i-1].Text) > strings.ToLower(prefilterSignals[i].Text) {
			return false
		}
	}
	return true
}
