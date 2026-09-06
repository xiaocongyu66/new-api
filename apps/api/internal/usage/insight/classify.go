package insight

import "strings"

// keywordSet 是一组带权重的关键词，命中一次累加一次权重（同一关键词只计一次）。
type keywordSet struct {
	Weight   int
	Keywords []string
}

// containsKeyword 判断关键词是否出现在文本中，并对纯英文短词施加词边界约束。
//
// 这是踩过坑之后加的：早期用裸 strings.Contains 匹配，导致 "orm" 命中
// "format"/"information"/"normal"，"less" 命中 "unless"/"regardless"，
// "dom" 命中 "random"/"domain"。任何一段英文提示词里出现 format 和 unless
// 就会凑出两个技术栈命中，把普通聊天推成"写代码"。
//
// 规则：关键词首/尾字符若是 ASCII 字母数字，则要求文本中对应侧的相邻字符
// 不是 ASCII 字母数字；中文关键词与以符号开头/结尾的关键词（如 ".ts"、
// "function "、"@@ -"）不受该侧约束，因为中文没有词间分隔符，
// 而符号本身已经起到了边界作用。
func containsKeyword(text, keyword string) bool {
	return indexKeyword(text, keyword) >= 0
}

// indexKeyword 返回满足词边界约束的首个命中下标，未命中返回 -1。
// 证据留存需要拿到这个下标去截原句，所以边界判定必须集中在这里实现，
// 否则打分用 containsKeyword、截句用 strings.Index，两者会指向不同的出现位置。
func indexKeyword(text, keyword string) int {
	if keyword == "" {
		return -1
	}
	needLeft := isASCIIAlnum(keyword[0])
	needRight := isASCIIAlnum(keyword[len(keyword)-1])
	if !needLeft && !needRight {
		return strings.Index(text, keyword)
	}
	// 逐个候选位置校验边界，避免第一处命中不合格就漏掉后面合格的命中。
	offset := 0
	for offset <= len(text)-len(keyword) {
		idx := strings.Index(text[offset:], keyword)
		if idx < 0 {
			return -1
		}
		start := offset + idx
		end := start + len(keyword)
		leftOK := !needLeft || start == 0 || !isASCIIAlnum(text[start-1])
		rightOK := !needRight || end == len(text) || !isASCIIAlnum(text[end])
		if leftOK && rightOK {
			return start
		}
		offset = start + 1
	}
	return -1
}

// isASCIIAlnum 只认 ASCII 字母数字。多字节字符（中文等）一律视为边界，
// 因为 UTF-8 续字节的高位为 1，不会落在这个区间内。
func isASCIIAlnum(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// 代码类信号：工程术语与报错文本。
//
// 本表只用于"证据展示"（evidence.go 把命中原句摘给管理员看），
// 不参与"是不是写代码"的判定——判定只认 code_structure.go 的基础语法结构。
//
// 已从本表移除的两类词，都因为在线上制造过误导性证据：
//   - 裸符号（"->"、"&&"、"();"、"```"）：群聊里的"戳一戳 -> 3924002568"
//     被摘成代码证据，1223 次请求全部如此；
//   - 工具名（str_replace / apply_patch / replace_in_file / apply_diff）：
//     它们出现在 agent 的工具说明书与工具调用外壳里，属于"模型在调用工具"，
//     不是用户在写代码（线上 404 次误判的直接成因）。
var codeSignals = []keywordSet{
	{Weight: 8, Keywords: []string{
		"stack trace", "traceback", "panic:", "compile error",
		"segmentation fault", "npm err", "cannot find module", "undefined is not",
		"nullpointerexception", "syntaxerror", "typeerror", "threw an exception",
		"unhandled exception", "exception:",
	}},
	{Weight: 6, Keywords: []string{
		"重构", "报错", "调试", "编译", "单元测试", "接口文档", "代码审查",
		"refactor", "unit test", "pull request", "code review", "lint",
		"debug", "build failed", "typecheck",
	}},
	{Weight: 6, Keywords: []string{
		".ts", ".tsx", ".js", ".jsx", ".go", ".py", ".rs", ".java", ".kt",
		".cpp", ".cs", ".rb", ".php", ".swift", ".sql", ".yaml", ".yml",
		"package.json", "go.mod", "cargo.toml", "requirements.txt", "dockerfile",
	}},
}

var roleplaySignals = []keywordSet{
	{Weight: 18, Keywords: []string{
		"{{char}}", "{{user}}", "<char>", "[start a new chat]",
		"write {{char}}'s next reply", "char's persona", "user's persona",
		"personality:", "scenario:", "example dialogue", "first message",
		"角色卡", "人设卡", "世界书", "预设",
	}},
	{Weight: 14, Keywords: []string{
		"roleplay", "role-play", "role play", "in character", "stay in character",
		"ooc:", "(ooc", "narrator", "immersive", "第一人称", "扮演", "角色扮演",
		"沉浸式", "旁白", "剧情", "对话续写",
	}},
	{Weight: 10, Keywords: []string{
		"你是我的", "你现在是", "从现在开始你是", "你将扮演", "请你扮演",
		"you are now", "you will play the role", "act as if you are",
		"pretend to be", "assume the role of", "you must never break character",
	}},
	{Weight: 8, Keywords: []string{
		"*smiles*", "*laughs*", "*blushes*", "*sighs*", "*giggles*",
		"（微笑）", "（脸红）", "（叹气）", "（笑）",
		"asterisk", "italics for actions", "用括号描述动作",
	}},
	{Weight: 8, Keywords: []string{
		"好感度", "亲密度", "状态栏", "属性面板", "剧情选项",
		"affection", "relationship level", "status window",
	}},
}

// 问答/通用助手信号。
var qaSignals = []keywordSet{
	{Weight: 10, Keywords: []string{
		"什么是", "为什么", "怎么办", "如何", "请解释", "帮我总结", "对比一下",
		"what is", "why does", "how do i", "explain", "summarize", "compare",
		"give me a list", "step by step", "推荐", "建议",
	}},
	{Weight: 8, Keywords: []string{
		"你是一个专业的", "you are a helpful assistant", "作为一名专家",
		"帮我写一篇", "写一封", "润色", "改写", "起个名字",
	}},
}

// 翻译类信号，单独归类避免污染问答比例。
//
// 这里刻意不收裸的 "原文" / "译文"：线上实证它们是全站最大的误判源
// （translate 占 1231/3096 = 40%，是第一大类别）。角色扮演的角色卡里
// 普遍出现多语言输出格式约定，例如
//
//	对话必须用母语+「中文翻译」格式
//	Ghost/Price/Soap → 英语 "原文" 「中文」
//
// 一个 20 分的关键词一击命中，就把整段角色扮演判成翻译请求。
// 现在只保留结构化的翻译指令（动词 + 宾语形态），这类写法不会出现在
// "请在回复里附上原文"这种排版要求里。
var translateSignals = []keywordSet{
	{Weight: 20, Keywords: []string{
		"translate the following", "translate this", "translate to",
		"translate into", "machine translation engine",
		"翻译成", "翻译为", "翻译下面", "翻译以下", "请翻译", "帮我翻译",
		"译成中文", "译成英文", "中译英", "英译中", "日译中", "中译日",
	}},
	// 弱信号：只在已有强信号时才有意义，单独出现（角色卡的排版约定）不足以定性。
	// 权重压到 6，配合 classifyUsage 的 15 分门槛，单独命中不会成为主类别。
	{Weight: 6, Keywords: []string{
		"保持原文格式", "keep the original format", "逐句翻译", "对照翻译",
	}},
}

// 前端技术栈关键词。
// 注意：这里刻意不收 "less"、"grid"、"props" 这类与英文常用词同形的短词，
// 它们即便加了词边界仍会在普通英文对话里大量误命中（"less than"、"grid" 等）。
var frontendKeywords = []string{
	"react", "vue", "svelte", "angular", "next.js", "nextjs", "nuxt",
	"tailwind", "css", "scss", "sass", "html", "jsx", "tsx",
	"vite", "webpack", "rsbuild", "eslint", "flexbox", "grid layout",
	"组件", "样式", "前端", "useeffect",
	"usestate", "react hooks", "styled-components", "antd", "element-ui",
	"shadcn", "响应式", "canvas 绘制", "svg", "wasm",
}

// 后端技术栈关键词。
// 同样剔除了 "orm"（命中 format/information/normal 的残留风险）、
// "事务"、"索引"、"缓存" 这类在非技术语境也常见的中文词单独出现的情况——
// 保留更具体的组合词。
var backendKeywords = []string{
	"golang", "gin框架", "gorm", "spring", "springboot", "django", "flask",
	"fastapi", "nestjs", "nest.js", "laravel", "rails", "mysql", "postgres",
	"expressjs", "express.js", "node.js", "nodejs",
	"postgresql", "redis", "mongodb", "clickhouse", "sqlite", "grpc",
	"restful", "api 设计", "微服务", "消息队列", "kafka", "rabbitmq",
	"数据库事务", "数据库索引", "分库分表", "jwt", "oauth", "鉴权", "后端",
	"数据库", "并发", "goroutine", "线程池", "缓存穿透", "限流", "熔断",
}

// 基础设施 / 运维关键词。
var infraKeywords = []string{
	"docker", "kubernetes", "k8s", "helm", "nginx", "caddy", "traefik",
	"terraform", "ansible", "ci/cd", "github actions", "gitlab ci",
	"prometheus", "grafana", "systemd", "linux", "shell", "bash",
	"部署", "运维", "监控", "日志采集", "证书", "ssl", "反向代理",
}

// 移动端关键词。
var mobileKeywords = []string{
	"android", "ios", "swiftui", "uikit", "kotlin", "jetpack compose",
	"flutter", "react native", "小程序", "uniapp", "apk", "gradle", "xcode",
}

// 数据 / 算法关键词。
// "微调"、"rag"、"embedding" 单独出现在闲聊里也不罕见（"微调价格"、
// 英文里的 rag 一词），因此这里只保留更具体的表述。
var dataKeywords = []string{
	"pandas", "numpy", "pytorch", "tensorflow", "sklearn", "spark", "hive",
	"flink", "etl", "数据清洗", "特征工程", "模型训练", "模型微调", "lora 训练",
	"向量检索", "检索增强", "数据分析",
}

// markupOnlyLangs 是"只有它命中时不足以说明在写代码"的标记语言。
//
// 线上实证（用户 557，1744 次请求中 409 次被判 code）：
// languages_json = {"html":407,"css":1,"shell":2}，看板把他画成前端工程师。
// 实际命中来自角色扮演里的状态卡片排版：
//
//	<badge type="success" icon="star">超可爱模式</badge>
//	正在为主人调整可爱度喵~
//
// HTML/CSS 是内容排版语言，聊天机器人输出富文本、角色卡定义状态栏都会用。
// 判定已改由基础语法结构负责，这里只用于语言回填：若结构判定通过但语言
// 只认出 HTML/CSS，不写入 languages，避免看板凭一个状态栏把人标成前端。
var markupOnlyLangs = map[string]bool{
	"html": true,
	"css":  true,
}

// hasNonMarkupLang 判断语法命中里是否存在标记语言以外的真实编程语言。
func hasNonMarkupLang(hits []langHit) bool {
	for _, hit := range hits {
		if !markupOnlyLangs[hit.Lang] {
			return true
		}
	}
	return false
}

// roleplayContestScore 是"角色扮演信号已经足够明确"的分数线。
//
// 达到这个分数说明正文里出现了角色卡骨架（{{char}}、personality:、
// [start a new chat]）或成组的剧情/扮演词汇，此时若结构判定也说是代码，
// 两个结论必有一个错——伪代码人设卡就落在这个交叉点上。
// 取 30 是因为单条弱信号（8-18 分）不足以构成竞争，
// 需要一条强信号或两条中等信号同时成立。
const roleplayContestScore = 30

// classifyUsage 根据提示词判定用途类别与代码方向。
//
// text 为小写化文本，用于关键词匹配；raw 为原文（保留大小写），
// 用于代码结构与语法正则匹配——大小写信息能区分 Go 导出符号、System.out、
// Console.WriteLine 等语言特征。
func classifyUsage(text, raw string, hasTools bool, roleplayBoost int) (result usageResult) {
	// —— 唯一证据：基础语法结构 ——
	// "是否在写代码"只认语言无关的基础语法结构共现（code_structure.go），
	// 且判定前先剥离 tool call 协议与数据围栏（toolcall.go）。
	//
	// 三轮线上实证决定了这个设计：
	//  1. 技术栈关键词旁路：客户端注入的工具清单里满是 android/apk/linux/shell
	//     这类技术名词，把 NSFW 角色扮演用户判成"移动端程序员"；
	//  2. 单条语言语法：群聊里的"戳一戳 -> 3924002568"因 "->" 被判成代码，
	//     1223 次请求全部误判；
	//  3. 工具协议本身：<system-reminder> 注入块 13676 次、agent 工具手册里的
	//     ```ts interface ToolArgsMap 与 str_replace 示例 404 次，
	//     形态像代码但属于"模型在调用工具"，与用户是否写代码无关。
	//
	// 现在的判据是"多类基础语法模块共现"：函数定义、类型定义、控制流、
	// 声明、异常、块结构、注释、运算符。真实代码必然同时出现多类，
	// 散文或工具协议残留至多命中一类。
	stripped := stripToolCallSyntax(raw)

	// 第一阶段：快筛门控。仿 ripgrep 的两级过滤：
	//   - 用紧凑词表（~200 个代码信号词）做出现次数统计
	//   - 统计结构标点密度
	//   - 两者都低于阈值 → 直接放行，不跑正则
	//   - 仅"可疑"请求进入第二阶段重审（完整结构分析 + 工具上下文保护）
	pf := prefilterScore(stripped)
	if !isSuspicious(pf) {
		// 快筛放行：直接返回非代码分类，跑轻量级用途分类即可
		result.Roleplay = clampScore(scoreSignals(text, roleplaySignals) + roleplayBoost)
		result.QA = clampScore(scoreSignals(text, qaSignals))
		translate := clampScore(scoreSignals(text, translateSignals))
		result.Category = CategoryOther
		result.CategoryScore = 0
		for _, candidate := range []struct {
			name  string
			score int
		}{
			{CategoryRoleplay, result.Roleplay},
			{CategoryTranslate, translate},
			{CategoryQA, result.QA},
		} {
			if candidate.score > result.CategoryScore {
				result.Category = candidate.name
				result.CategoryScore = candidate.score
			}
		}
		if result.CategoryScore < 10 {
			result.Category = CategoryOther
			result.CategoryScore = 0
		}
		return result
	}


	// 第二阶段：重审。快筛标记为可疑，运行完整结构分析 + 工具上下文保护
	toolSpans := extractToolCallSpans(stripped)
	structure := analyzeCodeStructureWithToolContext(stripped, toolSpans)
	// 技术栈方向：本轮不做改动，沿用关键词 + 语言映射的既有实现，
	// 只在 code 已由结构确立后用于"细化方向"，绝不参与"是不是代码"的判定。
	// 方向判定的准确性由后续开发单独处理。
	dirScores, _, _ := classifyStack(text)
	syntaxTop, syntaxHits := detectCodeSyntax(stripped)

	result.Roleplay = clampScore(scoreSignals(text, roleplaySignals) + roleplayBoost)
	result.QA = clampScore(scoreSignals(text, qaSignals))
	translate := clampScore(scoreSignals(text, translateSignals))

	// 伪代码人设卡的裁决：结构成立但同时有明确角色扮演信号时，
	// 额外要求"开发上下文旁证"（import / 文件路径 / 构建命令 / 报错栈 /
	// 语言围栏 / diff）。线上实证（用户 1251、1506，SillyTavern）：
	// 预设正文是 `class Ariadne(MethodActor):` + `def __init__` + `# 注释`
	// 这种伪 Python 写的人设卡，纯语法判定无从分辨，但它不会 import 任何东西，
	// 也不会出现 main.go 或 go build。反之真实开发请求几乎必然带其中之一。
	// 没有角色扮演竞争时不加这道门槛——纯粹贴一段函数让改的请求也要认。
	if structure.IsCode && result.Roleplay >= roleplayContestScore &&
		!hasDevContext(stripped, structure) {
		structure.IsCode = false
		structure.Modules = nil
	}

	// —— 代码分：由结构共现驱动 ——
	// 结构分是各命中模块的权重之和（同模块内取最高，不重复累加），
	// 满分对应"函数 + 类型 + 控制流 + 块结构"这种完整代码形态。
	codeScore := 0
	if structure.IsCode {
		codeScore = structure.Score * 2
		if hasTools {
			// 工具定义只在"已经确认是代码"时才叠加：agent 编码工具通常带 tools，
			// 但单独的 tools 说明不了任何事（聊天客户端挂函数调用插件也有）。
			codeScore += 10
		}
		// 语言可辨认时略微加成——能认出具体语言意味着结构判定不是巧合。
		if syntaxTop > 0 {
			codeScore += 10
		}
	}
	result.Code = clampScore(codeScore)
	result.CodeModules = structure.Modules

	if structure.IsCode {
		// 方向判定：以语法命中的方向为主，技术栈关键词作补充细化
		// （例如 TS 语法 + nestjs 关键词 → 后端）。
		nodeBackend := dirScores[StackBackend] > 0
		for _, hit := range syntaxHits {
			dir := hit.Stack
			if dir == StackFrontend && nodeBackend &&
				(hit.Lang == "typescript" || hit.Lang == "javascript") {
				dir = StackBackend
			}
			if dir != "" {
				dirScores[dir] += hit.Score
			}
		}
		stack, front, back := resolveStack(dirScores)
		result.Stack = stack
		result.StackFront = front
		result.StackBack = back
		// Languages 是编程语言（来自语法检测），不是框架名。
		// 只认出 HTML/CSS 时不回填：角色卡的状态栏排版会命中它们，
		// 而结构判定的依据往往来自别处（线上用户 571 被标成前端工程师）。
		if hasNonMarkupLang(syntaxHits) {
			result.Languages = languageNames(syntaxHits, 4)
		}
	} else {
		result.Code = 0
		result.Stack = StackUnknown
		result.StackFront = 0
		result.StackBack = 0
		result.Languages = nil
	}
	// 类别选择：平票时优先更具体的判定。roleplay 排在 code 之前——
	// 角色扮演有明确的人设/剧情结构，比"疑似在写代码"更具体；线上出现过
	// NSFW 角色扮演因脚手架关键词与 roleplay 同为 100 分、code 抢先占位的误判。
	result.Category = CategoryOther
	result.CategoryScore = 0
	for _, candidate := range []struct {
		name  string
		score int
	}{
		{CategoryRoleplay, result.Roleplay},
		{CategoryCode, result.Code},
		{CategoryTranslate, translate},
		{CategoryQA, result.QA},
	} {
		if candidate.score > result.CategoryScore {
			result.Category = candidate.name
			result.CategoryScore = candidate.score
		}
	}
	// 若最终类别不是 code，清掉技术方向结论——避免"角色扮演用户是移动端"。
	if result.Category != CategoryCode {
		result.Stack = StackUnknown
		result.StackFront = 0
		result.StackBack = 0
		result.Languages = nil
	}
	if result.CategoryScore < 15 {
		// 一般类别要求 15 分门槛，但 qa 是"通用问答"这个语义最弱的类别：
		// 它的最强信号（"什么是"/"如何"/"帮我总结"）只值 10 分，永远够不到 15，
		// 结果线上 qa_requests 全站为 0，所有普通提问都被塞进 other。
		// 因此对 qa 单独放宽到 10 分——代价是 other 会少一些、qa 稍宽松，
		// 但这比"看板上 90% 都是其他"更接近真实分布。
		// 注意仅当 qa 就是当前最高分时才生效，不会去抢 code/roleplay 的判定。
		if result.Category == CategoryQA && result.CategoryScore >= 10 {
			return result
		}
		result.Category = CategoryOther
		result.CategoryScore = 0
	}
	return result
}

type usageResult struct {
	Category      string
	CategoryScore int
	Code          int
	Roleplay      int
	QA            int
	Stack         string
	StackFront    int
	StackBack     int
	Languages     []string
	// CodeModules 是命中的基础语法模块名（function/datatype/control…），
	// 供证据面板展示"判定为代码的依据是哪几类结构"。
	CodeModules []string
}

// classifyStack 统计各技术栈方向的关键词命中，返回：
//   - dirScores: 各方向的原始得分表（frontend/backend/infra/mobile/data），
//     供 classifyUsage 与语法命中的方向权重合并后再判定；
//   - languages: 命中的框架/技术名标签（仅用于内部参考，不再写入画像的
//     languages 字段——那里现在存的是编程语言）；
//   - totalHits: 命中的关键词总数，用于给"这是编程请求"提供独立证据。
func classifyStack(text string) (dirScores map[string]int, languages []string, totalHits int) {
	frontCount, frontHits := countKeywords(text, frontendKeywords)
	backCount, backHits := countKeywords(text, backendKeywords)
	infra, infraHits := countKeywords(text, infraKeywords)
	mobile, mobileHits := countKeywords(text, mobileKeywords)
	data, dataHits := countKeywords(text, dataKeywords)

	languages = appendLimited(languages, frontHits, 4)
	languages = appendLimited(languages, backHits, 4)
	languages = appendLimited(languages, infraHits, 3)
	languages = appendLimited(languages, mobileHits, 2)
	languages = appendLimited(languages, dataHits, 2)

	totalHits = frontCount + backCount + infra + mobile + data
	dirScores = map[string]int{
		StackFrontend: frontCount * 10,
		StackBackend:  backCount * 10,
		StackInfra:    infra * 10,
		StackMobile:   mobile * 10,
		StackData:     data * 10,
	}
	return dirScores, languages, totalHits
}

// resolveStack 从方向得分表判定主方向，并返回前后端得分。
// 移动端/数据方向只要有明确信号且不弱于前后端就优先（它们是更具体的判断）；
// 前后端接近时判全栈。
func resolveStack(dirScores map[string]int) (stack string, front, back int) {
	front = clampScore(dirScores[StackFrontend])
	back = clampScore(dirScores[StackBackend])
	infra := dirScores[StackInfra]
	mobile := dirScores[StackMobile]
	data := dirScores[StackData]
	rawFront := dirScores[StackFrontend]
	rawBack := dirScores[StackBackend]

	switch {
	case rawFront == 0 && rawBack == 0 && infra == 0 && mobile == 0 && data == 0:
		return StackUnknown, front, back
	case mobile > 0 && mobile >= rawFront && mobile >= rawBack:
		return StackMobile, front, back
	case data > 0 && data >= rawFront && data >= rawBack:
		return StackData, front, back
	case infra > rawFront && infra > rawBack:
		return StackInfra, front, back
	case rawFront > 0 && rawBack > 0 && absDiff(rawFront, rawBack) <= 20:
		return StackFull, front, back
	case rawFront > rawBack:
		return StackFrontend, front, back
	default:
		return StackBackend, front, back
	}
}

func scoreSignals(text string, sets []keywordSet) int {
	total := 0
	for _, set := range sets {
		for _, keyword := range set.Keywords {
			if containsKeyword(text, keyword) {
				total += set.Weight
				break
			}
		}
	}
	return total
}

func countKeywords(text string, keywords []string) (int, []string) {
	count := 0
	var hits []string
	for _, keyword := range keywords {
		if containsKeyword(text, keyword) {
			count++
			hits = append(hits, keyword)
		}
	}
	return count, hits
}

func appendLimited(dst []string, src []string, limit int) []string {
	for i, item := range src {
		if i >= limit {
			break
		}
		dst = append(dst, item)
	}
	return dst
}

func clampScore(v int) int {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

func absDiff(a, b int) int {
	if a > b {
		return a
	}
	return b - a
}
