package insight

import (
	"regexp"
	"sort"
)

// 本文件用"语言无关的基础语法结构"判定这次请求是不是在写代码。
//
// 为什么不再用语言关键词/框架名：那些词在散文里也出现，只能说明话题相关。
// 为什么不再单靠单条语言语法（code_syntax.go）：单条命中太脆——线上把
// 群聊里的 "戳一戳 -> 3924002568" 判成代码，就是因为 "->" 单独算了分。
//
// 这里改成"结构共现"判定：任何编程语言的基础语法只由固定几类结构组成，
// 一段真实代码必然同时出现其中多类；散文即便偶然命中一类，也凑不出多类。
// 七类基础模块（与语言无关）：
//
//	declaration 变量/常量声明与赋值
//	datatype    数据类型：结构体/类/接口/枚举/类型别名
//	operator    复合运算符（==、&&、+=、=>、:: …）
//	control     逻辑控制：if/else/for/while/switch/case/break/continue
//	function    函数/方法定义
//	comment     注释
//	exception   异常捕获：try/catch/finally/except/rescue/panic
//
// 外加一类承载"代码排版"的辅助模块：
//
//	block       块结构：成对花括号、连续缩进块、def…end
//
// 判定不看单条命中，只看"命中了几类、其中有没有强结构"：
//   - 有强结构（函数定义 / 类型声明 / 块结构）且总共 ≥2 类 → 代码；
//   - 无强结构时要求 ≥3 类且其中 ≥2 类为中等强度 → 代码；
//   - 其余一律不算代码。
//
// 执行上仿 ripgrep 的两级过滤：先做字节级快筛（prefilterCodeShape），
// 没有代码形态的文本直接短路，不跑后面几十条正则；通过快筛的才逐条重审。
// 热路径上绝大多数请求（中文闲聊、角色扮演）在快筛就被挡掉。

// 基础语法模块标识。
const (
	SyntaxDeclaration = "declaration"
	SyntaxDataType    = "datatype"
	SyntaxOperator    = "operator"
	SyntaxControl     = "control"
	SyntaxFunction    = "function"
	SyntaxComment     = "comment"
	SyntaxException   = "exception"
	SyntaxBlock       = "block"
	SyntaxFence       = "fence"
	SyntaxDiff        = "diff"
)

// 模块强度。strong 单独一类就足以带动判定（配合另外任意一类），
// medium 需要成组出现，weak 只加分不参与门槛。
const (
	strengthWeak = iota
	strengthMedium
	strengthStrong
)

// moduleStrength 定义各模块的强度。
//
// function/datatype/block 是强结构：函数定义、类型定义、成对块排版
// 这三种形态在自然语言里不存在。control/declaration/exception 是中等——
// 单看一条可能是巧合（"if you..."、"let me..."），成组出现才可信。
// operator/comment 最弱：破折号、井号、双等号在散文与 Markdown 里都有。
var moduleStrength = map[string]int{
	SyntaxFunction:    strengthStrong,
	SyntaxDataType:    strengthStrong,
	SyntaxBlock:       strengthStrong,
	SyntaxControl:     strengthMedium,
	SyntaxDeclaration: strengthMedium,
	SyntaxException:   strengthMedium,
	SyntaxOperator:    strengthWeak,
	SyntaxComment:     strengthWeak,
	SyntaxFence:       strengthMedium,
	SyntaxDiff:        strengthStrong,
}

// structureRule 是一条结构特征。Desc 用于人工复核时展示"命中了什么结构"。
type structureRule struct {
	Module string
	Weight int
	Desc   string
	Re     *regexp.Regexp
}

// structureHit 是一条命中记录。
type structureHit struct {
	Module string
	Desc   string
	Weight int
}

// structureVerdict 是结构判定结论。
type structureVerdict struct {
	IsCode  bool
	Score   int
	Modules []string
	Hits    []structureHit
}

func mustStruct(expr string) *regexp.Regexp { return regexp.MustCompile(expr) }

// codeStructureRules 是语言无关的基础语法结构表。
//
// 设计约束（每一条都对应线上误判过的形态）：
//   - 尽量行首锚定 (?m)^\s*，因为代码是逐行写的，散文不是；
//   - 不收裸词：if/for/class/return 单独出现在英文散文里太常见，
//     必须带上语法标点（括号、冒号、花括号）才算；
//   - 不收单个符号：`->`、`==` 这类在群聊里也出现，只能作为 weak 模块，
//     且永远无法单独让判定通过。
var codeStructureRules = []structureRule{
	// ---- 1. 函数/方法定义（强）----
	{SyntaxFunction, 14, "函数定义关键字", mustStruct(`(?m)^\s*(?:@\w+[^\n]*\n\s*)?(?:public|private|protected|internal|static|final|export|default|async|pub|open|override|suspend|inline|virtual|abstract)?\s*(?:func|function|fn|def|sub|proc|method|fun)\s+[A-Za-z_$][\w$]*\s*[\(<]`)},
	{SyntaxFunction, 12, "带接收器/返回类型的函数签名", mustStruct(`\b(?:func|fn)\s*(?:\([^()\n]{1,60}\)\s*)?[A-Za-z_]\w*\s*\([^()\n]{0,120}\)\s*(?:->|:)?\s*[\w\[\]\*<>., ]{0,40}\{`)},
	{SyntaxFunction, 12, "类型化参数列表的函数头", mustStruct(`(?m)^\s*(?:public|private|protected|static|final|override)?\s*(?:[A-Za-z_][\w<>\[\]\.]*\s+)+[A-Za-z_]\w*\s*\([^()\n]{0,160}\)\s*(?:const|throws [\w, ]+)?\s*\{`)},
	// 下面两条不锚定行首：代码常被贴在一行文字后面（"帮我改下 func main(){}"），
	// 或请求体只读了前缀导致换行被切掉。关键词 + 标识符 + 括号这个组合在散文里
	// 不成立，因此不必依赖行结构。
	{SyntaxFunction, 12, "函数定义（行内）", mustStruct(`\b(?:function|func|fn|def|fun|sub|proc)\s+[A-Za-z_$][\w$]*\s*[\(<]`)},
	{SyntaxFunction, 10, "带修饰符的函数定义", mustStruct(`\b(?:export|async|pub|public|private|protected|static|final|override|suspend|inline)\s+(?:async\s+)?(?:function|func|fn|def|fun)\s+[A-Za-z_$][\w$]*`)},
	{SyntaxFunction, 10, "箭头函数赋值", mustStruct(`(?:const|let|var)\s+[A-Za-z_$][\w$]*\s*(?::\s*[^=\n]{1,60})?=\s*(?:async\s+)?(?:\([^()\n]{0,120}\)|[A-Za-z_$][\w$]*)\s*=>`)},
	{SyntaxFunction, 8, "方法调用链", mustStruct(`\b[A-Za-z_$][\w$]*(?:\.[A-Za-z_$][\w$]*){1,}\s*\([^\n]{0,80}\)\s*[;,)\n]`)},

	// ---- 2. 数据类型定义（强）----
	{SyntaxDataType, 14, "类/结构体/接口/枚举定义", mustStruct(`(?m)^\s*(?:@\w+[^\n]*\n\s*)?(?:public|private|protected|internal|export|abstract|final|sealed|open|data|case|static)?\s*(?:class|struct|interface|enum|trait|protocol|record|impl|type|typedef|union)\s+[A-Za-z_]\w*[\s\(<:{]`)},
	{SyntaxDataType, 12, "带修饰符的类型定义（行内）", mustStruct(`\b(?:export|public|private|protected|internal|abstract|final|sealed|open|data)\s+(?:abstract\s+|final\s+|data\s+|static\s+)?(?:class|struct|interface|enum|trait|protocol|record)\s+[A-Za-z_]\w*`)},
	{SyntaxDataType, 10, "类型别名/泛型声明", mustStruct(`(?m)^\s*(?:export\s+)?type\s+[A-Za-z_]\w*(?:<[^>\n]{1,60}>)?\s*=`)},
	{SyntaxDataType, 8, "字段类型标注块", mustStruct(`(?m)^\s{1,8}[A-Za-z_]\w*\??\s*:\s*(?:string|number|boolean|int|long|float|double|bool|str|any|unknown|void|list|dict|map|array|[A-Z]\w*)(?:\[\])?[,;]?\s*$`)},
	{SyntaxDataType, 8, "带类型的结构体字段", mustStruct("(?m)^\\s{1,8}[A-Z]\\w*\\s+(?:\\*|\\[\\])?[A-Za-z_][\\w\\.\\[\\]]*\\s+`[^`\\n]{1,80}`\\s*$")},

	// ---- 3. 逻辑控制（中）----
	{SyntaxControl, 10, "if 条件语句", mustStruct(`(?m)^\s*(?:\}\s*else\s+)?if\s*(?:\(|[\w"'\[!].{0,80}(?::|\{)\s*$)`)},
	{SyntaxControl, 10, "for/while 循环", mustStruct(`(?m)^\s*(?:for|while|foreach|loop)\s*(?:\(|[\w"'\[].{0,80}(?::|\{)\s*$|\{)`)},
	{SyntaxControl, 10, "switch/match 分支", mustStruct(`(?m)^\s*(?:switch|match|select)\s*(?:\([^\n]{0,80}\))?\s*\{|(?m)^\s*(?:case\s+[^\n]{1,60}:|default:)\s*$`)},
	{SyntaxControl, 8, "elif/else 分支", mustStruct(`(?m)^\s*(?:elif\b|else\s*(?:if\s*\(|:|\{)|\}\s*else\s*\{)`)},
	// 行内控制流：代码被贴在一句话后面（"重构这段代码 func Sum(a int) int { if a > 0 {"）
	// 或请求体前缀截断把换行切掉时，行首锚定的规则全部失效。
	// 三个约束共同排除散文与模板：条件里必须出现比较/逻辑运算符，
	// 左花括号后必须是空白（handlebars 的 {{#if a > b}}{{x}} 因 `{{` 相邻被排除）。
	{SyntaxControl, 8, "控制流（行内）", mustStruct(`\b(?:if|while|switch|for|foreach)\s*\(?[A-Za-z_$!\(][^\n{]{0,60}(?:==|!=|>=|<=|&&|\|\||<|>|:=)[^\n{]{0,60}\{(?:[ \t\r\n]|$)`)},
	{SyntaxControl, 6, "break/continue/return 语句", mustStruct(`(?m)^\s*(?:break|continue|return)\s*[\w"'\(\[\{&\*!-]?[^\n]{0,80}[;\n]`)},

	// ---- 4. 变量/常量声明（中）----
	{SyntaxDeclaration, 10, "声明关键字赋值", mustStruct(`(?m)^\s*(?:const|let|var|val|final|static|dim|my|local)\s+[A-Za-z_$][\w$]*\s*(?::\s*[\w<>\[\]\.\|]{1,40})?\s*(?:=|:=)`)},
	{SyntaxDeclaration, 10, "短变量声明/解构赋值", mustStruct(`(?m)^\s*(?:[A-Za-z_$][\w$]*|\[[^\]\n]{1,60}\]|\{[^}\n]{1,60}\})\s*(?::=|=)\s*(?:[A-Za-z_$][\w$\.]*\s*\(|new\s+[A-Z]|\[|\{|"|'|\d)`)},
	{SyntaxDeclaration, 8, "带类型的变量声明", mustStruct(`(?m)^\s*(?:public|private|protected|static|final)?\s*(?:int|long|short|byte|float|double|bool|boolean|char|string|str|void|auto|var)\s+[A-Za-z_]\w*\s*(?:=[^\n]{0,60})?;`)},
	{SyntaxDeclaration, 6, "常量组/枚举成员", mustStruct(`(?m)^\s*[A-Z][A-Z0-9_]{2,}\s*(?:=|:)\s*[^\n]{1,60}[,;]?\s*$`)},

	// ---- 5. 异常捕获（中）----
	{SyntaxException, 12, "try/catch/finally", mustStruct(`(?m)^\s*(?:try\s*(?:\{|:)|\}?\s*(?:catch|except|rescue)\s*(?:\(|\s+[\w\.]+|:)|\}?\s*finally\s*(?:\{|:))`)},
	{SyntaxException, 10, "抛出/包裹错误", mustStruct(`(?m)^\s*(?:throw\s+new\s+[A-Z]|raise\s+[A-Z]\w*\(|panic\(|throw\s+[a-z]\w*;)`)},
	{SyntaxException, 8, "错误返回判定", mustStruct(`(?m)^\s*if\s+err\s*(?:!=|==)\s*nil\s*\{|\?\s*;\s*$|\.unwrap\(\)|\.expect\(`)},

	// ---- 6. 注释（弱）----
	{SyntaxComment, 6, "行注释", mustStruct(`(?m)^\s*(?://|#(?:\s|!)|--\s|;\s|%\s)[^\n]{2,}$`)},
	{SyntaxComment, 6, "块注释/文档注释", mustStruct(`/\*[\s\S]{0,400}?\*/|(?m)^\s*(?:"""|'''|///|\*\s)`)},

	// ---- 7. 运算符（弱）----
	{SyntaxOperator, 6, "复合/逻辑运算符", mustStruct(`(?:==|!=|>=|<=|&&|\|\||\+=|-=|\*=|/=|%=|\+\+|--|=>|::|->|\?\?|\?\.|<<|>>|===|!==)`)},
	{SyntaxOperator, 4, "布尔字面量与空值", mustStruct(`\b(?:true|false|null|nil|None|undefined|NULL)\b\s*[,;\)\}\n]`)},

	// ---- 8. 块结构（强）----
	// 注意：RE2 的 {n,m} 上限是 1000，窗口不能再放大。
	// 这不影响判定——一段代码块的开合括号极少相隔千字符以上。
	{SyntaxBlock, 12, "花括号块", mustStruct(`(?m)\{\s*$[\s\S]{1,1000}?^\s*\}`)},
	{SyntaxBlock, 10, "缩进代码块", mustStruct(`(?m)^\s*(?:def|class|if|for|while|with|try|elif|else|switch|func)\b[^\n]{0,120}:\s*$\n(?:^[ \t]{2,}[^\s\n][^\n]*$\n?){2,}`)},
	{SyntaxBlock, 10, "语句块（连续分号结尾行）", mustStruct(`(?m)^[ \t]*[^\s\n][^\n]{2,};\s*$\n(?:^[ \t]*[^\s\n][^\n]{2,};\s*$\n?){1,}`)},
	{SyntaxBlock, 10, "do/begin…end 块", mustStruct(`(?m)^\s*(?:do|begin)\s*(?:\|[^|\n]*\|)?\s*$[\s\S]{1,1000}?^\s*end\b`)},

	// ---- 9. 代码围栏（中）----
	// 带编程语言标签的围栏是"用户明确在贴代码"的表态。
	// 数据类标签（json/yaml/log…）不在此列——它们在 toolcall.go 里已被整块剥离。
	{SyntaxFence, 10, "编程语言代码围栏", mustStruct("(?i)`{3,}[ \\t]*(?:go|golang|py|python|ts|tsx|typescript|js|jsx|javascript|java|kt|kotlin|swift|rs|rust|c|cc|cpp|c\\+\\+|cxx|h|hpp|cs|csharp|php|rb|ruby|sql|sh|bash|zsh|shell|ps1|powershell|lua|dart|scala|groovy|perl|r|m|mm|objc|vb|f#|fsharp|elixir|erlang|haskell|clojure|ocaml|zig|nim|julia|matlab|asm|html|css|scss|less|vue|svelte|graphql|proto|tf|hcl|dockerfile|makefile|cmake|gradle|xml)\\b")},

	// ---- 10. 代码改动（强）----
	// diff/patch 报头与冲突标记是"确实在改代码"的形态，且是真实内容而非工具协议。
	// 注意这里不收 apply_patch / str_replace / replace_in_file 这类工具名：
	// 它们既出现在 agent 的工具说明书里（线上 404 次误判的成因），
	// 也出现在工具调用外壳上，属于"调用工具"而不是"写代码"。
	{SyntaxDiff, 14, "git diff 报头", mustStruct(`(?m)^diff --git\s+a/`)},
	{SyntaxDiff, 14, "diff hunk 报头", mustStruct(`(?m)^@@ -\d+(?:,\d+)?\s\+\d+(?:,\d+)?\s@@`)},
	{SyntaxDiff, 12, "合并冲突标记", mustStruct(`(?m)^(?:<{7}\s*SEARCH|>{7}\s*REPLACE|={7})\s*$`)},
	{SyntaxDiff, 10, "统一 diff 文件头", mustStruct(`(?m)^(?:\+{3}|-{3})\s+[ab]?/?[\w./-]+\s*$`)},
}

// —— 第一级：字节快筛 ——
//
// 仿 ripgrep 的思路：先用极便宜的判据排掉绝大多数不可能是代码的文本，
// 只有通过快筛的才去跑上面那几十条回溯正则。
//
// 判据是"代码专用标点的密度"。代码里 {}()[];=<>/*|& 这些字符成片出现，
// 中文闲聊与角色扮演几乎不用（中文用的是全角标点，不在此列）。
// 阈值取得很宽松（命中即放行重审），只为砍掉纯自然语言，不做定性。

// codePunct 标记参与快筛的 ASCII 标点。
var codePunct = func() [128]bool {
	var table [128]bool
	for _, ch := range []byte("{}()[];=<>/*|&:#$_") {
		table[ch] = true
	}
	return table
}()

// prefilterMinPunct 是快筛所需的最小代码标点数。
// 取 4 是因为最短的一行代码（`func main(){}`）恰好是 4 个：( ) { }。
// 再高就会漏掉"中文提问里夹一行代码"这种最常见的贴法。
const prefilterMinPunct = 4

// prefilterDenseFloor 是"标点绝对量足够大就直接放行"的阈值。
// 长文里夹了成段代码时，全文平均密度会被自然语言稀释，
// 但标点总量本身已经说明值得重审。
const prefilterDenseFloor = 30

// prefilterCodeShape 判断文本是否值得进入完整结构审查。
//
// 快筛只负责砍掉纯自然语言以省下正则开销，不做定性：
// 放过一段散文只是多跑几十条正则，结论仍由结构共现决定；
// 但错杀一段代码会直接变成漏判，因此门槛偏宽松。
func prefilterCodeShape(text string) bool {
	if len(text) < 24 {
		return false
	}
	punct, ascii := 0, 0
	for i := 0; i < len(text); i++ {
		b := text[i]
		if b >= 128 {
			continue
		}
		ascii++
		if codePunct[b] {
			punct++
		}
	}
	if punct < prefilterMinPunct {
		return false
	}
	if punct >= prefilterDenseFloor {
		return true
	}
	if ascii == 0 {
		return false
	}
	// 密度按 ASCII 字符计：中文占多数时 ascii 很小，
	// 此时只要真有一行代码，密度就容易过线。
	return punct*100 >= ascii*2
}

// —— 第二级：完整结构审查 ——

// analyzeCodeStructure 跑完整结构表，返回按模块聚合的判定。
// text 应当已经过 stripToolCallSyntax 剥离，否则工具协议会被算成代码。
func analyzeCodeStructure(text string) structureVerdict {
	return analyzeCodeStructureWithToolContext(text, nil)
}

// analyzeCodeStructureWithToolContext 跑完整结构表，返回按模块聚合的判定。
// 当 toolSpans 非空时，落在工具调用参数区内的命中按如下规则处理：
//   - 弱模块（operator / comment）命中直接丢弃——工具参数内的标点不说明任何事；
//   - 强/中模块命中保留，但只作为"陪同"——仅在工具区外也有至少 1 个强/中模块命中时，
//     才计入最终判定。工具参数内的代码结构单独不构成"用户在写代码"的判定。
func analyzeCodeStructureWithToolContext(text string, toolSpans []ToolSpan) structureVerdict {
	verdict := structureVerdict{}
	if !prefilterCodeShape(text) {
		return verdict
	}
	// 记录每个模块在工具区内外的最高权重
	moduleScoreOutside := make(map[string]int, len(moduleStrength))
	moduleScoreInside := make(map[string]int, len(moduleStrength))
	for i := range codeStructureRules {
		rule := &codeStructureRules[i]
		if !rule.Re.MatchString(text) {
			continue
		}
		// 找到所有匹配位置（不只是第一个），逐个判断是否在工具区内
		locs := rule.Re.FindAllStringIndex(text, -1)
		for _, loc := range locs {
			hitStart, hitEnd := loc[0], loc[1]
			inTool := isInToolContext(hitStart, hitEnd, toolSpans)
			// 弱模块命中落在工具区内时直接丢弃：工具参数内的标点不说明任何事
			if inTool && moduleStrength[rule.Module] == strengthWeak {
				continue
			}
			// 同一模块内多条规则命中只取最高权重，避免"一段代码把同类刷满分"。
			if inTool {
				if rule.Weight > moduleScoreInside[rule.Module] {
					moduleScoreInside[rule.Module] = rule.Weight
				}
			} else {
				if rule.Weight > moduleScoreOutside[rule.Module] {
					moduleScoreOutside[rule.Module] = rule.Weight
				}
			}
			verdict.Hits = append(verdict.Hits, structureHit{
				Module: rule.Module, Desc: rule.Desc, Weight: rule.Weight,
			})
		}
	}
	if len(moduleScoreOutside) == 0 && len(moduleScoreInside) == 0 {
		return verdict
	}

	// 最终模块集合：工具区外命中 + 工具区内命中（仅保留非弱模块）
	// 工具区内的强/中模块作为"陪同"——只在工具区外也有至少 1 个强/中模块命中时计入
	strong, medium := 0, 0
	toolStrong, toolMedium := 0, 0
	verdict.Modules = make([]string, 0, len(moduleScoreOutside)+len(moduleScoreInside))
	for module := range moduleScoreOutside {
		verdict.Modules = append(verdict.Modules, module)
	}
	for module := range moduleScoreInside {
		if _, outside := moduleScoreOutside[module]; !outside {
			verdict.Modules = append(verdict.Modules, module)
		}
	}
	for module, score := range moduleScoreOutside {
		verdict.Score += score
		switch moduleStrength[module] {
		case strengthStrong:
			strong++
		case strengthMedium:
			medium++
		}
	}
	for module, score := range moduleScoreInside {
		verdict.Score += score
		switch moduleStrength[module] {
		case strengthStrong:
			toolStrong++
		case strengthMedium:
			toolMedium++
		}
	}
	sort.Strings(verdict.Modules)

	// 工具区外的命中直接参与判定
	// 工具区内的命中作为"陪同"——仅在工具区外已有至少 1 个强/中模块时计入
	if strong+medium > 0 {
		strong += toolStrong
		medium += toolMedium
	}

	// 判定门槛：结构类别的共现数决定结论，单条命中永远不够。
	//   - 两类强结构 → 代码；
	//   - 一类强结构 + 至少一类中等强度 → 代码；
	//   - 无强结构时要求 ≥3 类且其中 ≥2 类中等强度。
	//
	// 强结构不能只配弱模块：线上把角色扮演模板判成代码，唯一证据就是
	// "花括号块（强）+ HTML 注释 `<!-- -->` 命中运算符（弱）"。
	// 花括号块在 JSON 记忆体、状态栏模板、{{变量}} 里都会出现，
	// 必须有控制流/声明/异常这类真实语句结构陪同才算代码。
	total := len(verdict.Modules)
	switch {
	case strong >= 2:
		verdict.IsCode = true
	case strong == 1 && medium >= 1:
		verdict.IsCode = true
	case medium >= 2 && total >= 3:
		verdict.IsCode = true
	}
	return verdict
}

// —— 第三级：开发上下文旁证 ——
//
// 结构判定只看"这段文字长得像不像代码"，但有一类文本天然长得像代码却不是：
// 角色扮演预设。线上实证（用户 1251 / 1506，SillyTavern）：
//
//	class Ariadne(MethodActor, Biographer):
//	  def __init__(self):
//	    self.stance = "write from inside the character"
//	  # moment: capture what is here
//	SHOTSCALE := MediumToWide >> EstablishingShot
//
// 这是伪代码写的人设卡，函数定义、类型定义、注释、声明全都成立，
// 结构判定无从分辨。区分点不在语法，而在"有没有开发上下文"：
// 真实写代码的请求几乎必然带 import / 文件路径 / 构建命令 / 报错栈 /
// 带语言标签的围栏 / diff 之一；伪代码人设卡没有其中任何一项。
var devContextRules = []*regexp.Regexp{
	// 导入 / 依赖声明：伪代码人设卡不会 import 任何东西。
	mustStruct(`(?m)^\s*(?:import\s+[\w{*"'(\[]|from\s+[\w."'/-]+\s+import\b|#include\s*[<"]|using\s+[A-Z][\w.]*\s*;|package\s+[a-z][\w./]*\s*$|require\s*\(\s*['"]|use\s+[a-z][\w:]*\s*;)`),
	// 文件路径：带代码后缀的文件名。
	mustStruct(`(?i)\b[\w./\\-]{1,60}\.(?:go|ts|tsx|jsx?|mjs|py|rs|java|kt|swift|cpp|cc|hpp?|cs|rb|php|sql|sh|bash|zsh|vue|svelte|toml|mod|gradle|dockerfile)\b`),
	// 构建 / 版本控制 / 运维命令。
	mustStruct(`(?i)\b(?:git\s+(?:add|commit|status|diff|push|pull|rebase|checkout|log|clone)|npm\s+(?:run|install|ci)|pnpm\s+[\w-]+|yarn\s+[\w-]+|go\s+(?:build|test|run|mod|vet)|cargo\s+[\w-]+|mvn\s+[\w-]+|gradlew?\s+[\w-]+|docker\s+(?:build|run|compose|exec)|kubectl\s+[\w-]+|pip\s+install|make\s+[\w-]+|psql\s+-|systemctl\s+[\w-]+)\b`),
	// 报错 / 栈回溯：调试请求的标志。
	mustStruct(`(?i)(?:stack trace|traceback \(most recent|panic:|segmentation fault|npm err|cannot find module|nullpointerexception|syntaxerror|typeerror:|referenceerror|exception in thread|error\[E\d+\]|at [\w.$]+\([\w./]+:\d+\)|:\d+:\d+:\s+(?:error|warning))`),
}

// hasDevContext 判断文本是否带真实开发上下文旁证。
// 带语言标签的代码围栏与 diff 报头本身就是旁证，已由结构模块记录。
func hasDevContext(raw string, verdict structureVerdict) bool {
	for _, module := range verdict.Modules {
		if module == SyntaxFence || module == SyntaxDiff {
			return true
		}
	}
	for _, re := range devContextRules {
		if re.MatchString(raw) {
			return true
		}
	}
	return false
}

// structureModuleNames 返回命中的模块名，供证据与前端展示。
func structureModuleNames(v structureVerdict) []string { return v.Modules }
