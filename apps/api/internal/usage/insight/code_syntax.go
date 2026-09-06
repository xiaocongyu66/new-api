package insight

import (
	"regexp"
	"sort"
)

// 本文件用"代码语法结构"判定编程语言，而不是靠关键词命中。
//
// 关键词匹配的根本问题：react / docker / 数据库 这些词在自然语言里也会出现，
// 只能说明"话题相关"，不能说明"用户在写代码"，更无法区分是什么语言。
// 语法特征则不同——`func (r *T) Foo(`、`fn main()`、`<?php`、`err != nil`
// 这类形态几乎不可能出现在散文里，命中即可高置信度判定"这是 X 语言的代码"，
// 顺带就把技术栈方向定了（Go/Rust→后端，Swift/Kotlin→移动端，SQL→数据……）。

// syntaxRule 是一条语言级语法特征。Desc 用于人工复核时展示"命中了什么形态"。
type syntaxRule struct {
	Lang   string
	Weight int
	Desc   string
	Re     *regexp.Regexp
}

func mustSyntax(expr string) *regexp.Regexp { return regexp.MustCompile(expr) }

// codeSyntaxRules 覆盖站内实际会出现的主流语言。
// 注意：扫描在“原文”（保留大小写）上进行，因此可以利用大小写信息
// （如 Go 导出符号、System.out、Console.WriteLine）。
// 大小写不敏感的语言（SQL）单独用 (?i)。行首锚定 (?m)^ 用来压低散文误报。
var codeSyntaxRules = []syntaxRule{
	// ---- Go ----
	{"go", 12, "package 声明", mustSyntax(`(?m)^\s*package\s+[a-z][a-z0-9_]*\s*$`)},
	{"go", 10, "func 定义", mustSyntax(`\bfunc\s+(\([^)]{1,40}\)\s+)?[A-Za-z_]\w*\s*\(`)},
	{"go", 8, "err != nil", mustSyntax(`\berr\s*!=\s*nil\b`)},
	{"go", 6, "短变量声明 :=", mustSyntax(`\w+\s*:=\s*`)},
	{"go", 6, "import 分组", mustSyntax(`(?m)^\s*import\s+\(`)},
	{"go", 5, "fmt 包调用", mustSyntax(`\bfmt\.[A-Z]\w+\(`)},
	{"go", 5, "[]byte", mustSyntax(`\[\]byte\b`)},

	// ---- TypeScript（先于 JS，命中即优先判 TS）----
	{"typescript", 10, "interface 声明", mustSyntax(`(?m)^\s*(export\s+)?interface\s+[A-Za-z_]\w*`)},
	{"typescript", 8, "type 别名", mustSyntax(`\btype\s+[A-Za-z_]\w*\s*=\s*`)},
	{"typescript", 6, "类型注解", mustSyntax(`[\w)]\s*:\s*(string|number|boolean|void|any|unknown|never)\b`)},
	{"typescript", 6, "泛型参数", mustSyntax(`\b[A-Za-z_]\w*<[A-Za-z_][\w, <>]*>\s*[({=]`)},
	{"typescript", 5, "as 断言", mustSyntax(`\bas\s+(const|[A-Z]\w*)\b`)},

	// ---- JavaScript ----
	{"javascript", 6, "const 赋值", mustSyntax(`\bconst\s+[A-Za-z_$]\w*\s*=`)},
	{"javascript", 6, "箭头函数", mustSyntax(`=>\s*[{(]`)},
	{"javascript", 6, "function 定义", mustSyntax(`\bfunction\s*\*?\s*[A-Za-z_$]*\s*\(`)},
	{"javascript", 8, "console 调用", mustSyntax(`\bconsole\.(log|error|warn|info)\s*\(`)},
	{"javascript", 8, "require 导入", mustSyntax(`\brequire\s*\(\s*['"]`)},
	{"javascript", 8, "ES import", mustSyntax(`(?m)^\s*import\s+[\w{}*,\s]+\s+from\s+['"]`)},

	// ---- Python ----
	{"python", 10, "def 定义", mustSyntax(`(?m)^\s*def\s+[A-Za-z_]\w*\s*\(`)},
	{"python", 8, "from import", mustSyntax(`(?m)^\s*from\s+[\w.]+\s+import\s+`)},
	{"python", 8, "class 定义", mustSyntax(`(?m)^\s*class\s+[A-Za-z_]\w*\s*[\(:]`)},
	{"python", 6, "self 引用", mustSyntax(`\bself\.[A-Za-z_]\w*`)},
	{"python", 6, "elif 分支", mustSyntax(`(?m)^\s*elif\b`)},
	{"python", 5, "dunder", mustSyntax(`__(init|name|main|str|repr)__`)},
	{"python", 4, "import 语句", mustSyntax(`(?m)^\s*import\s+[a-z_][\w.]*\s*$`)},

	// ---- Rust ----
	{"rust", 10, "fn 定义", mustSyntax(`(?m)^\s*(pub\s+)?(async\s+)?fn\s+[A-Za-z_]\w*`)},
	{"rust", 8, "let mut", mustSyntax(`\blet\s+mut\b`)},
	{"rust", 8, "impl 块", mustSyntax(`(?m)^\s*impl(\s*<[^>]+>)?\s+[A-Za-z_]`)},
	{"rust", 8, "derive 宏", mustSyntax(`#\[derive\(`)},
	{"rust", 8, "宏调用!", mustSyntax(`\b(println|print|vec|format|panic|write)!\s*[\(\[]`)},
	{"rust", 6, "use 路径", mustSyntax(`\buse\s+[a-z_][\w:]*::`)},

	// ---- Java ----
	{"java", 10, "java 包导入", mustSyntax(`(?m)^\s*import\s+java(x)?\.`)},
	{"java", 10, "main 方法", mustSyntax(`public\s+static\s+void\s+main\s*\(`)},
	{"java", 8, "System.out", mustSyntax(`\bSystem\.(out|err)\.print`)},
	{"java", 6, "访问修饰声明", mustSyntax(`\b(public|private|protected)\s+(static\s+)?(final\s+)?(class|void|int|String|boolean)\b`)},
	{"java", 6, "注解", mustSyntax(`@(Override|Autowired|Service|Component|RestController|Entity)\b`)},

	// ---- Kotlin ----
	{"kotlin", 10, "androidx 导入", mustSyntax(`(?m)^\s*import\s+androidx\.`)},
	{"kotlin", 8, "fun 定义", mustSyntax(`\bfun\s+[A-Za-z_]\w*\s*\(`)},
	{"kotlin", 6, "val/var 声明", mustSyntax(`\b(val|var)\s+[A-Za-z_]\w*\s*[:=]`)},
	{"kotlin", 8, "companion object", mustSyntax(`\bcompanion\s+object\b`)},
	{"kotlin", 6, "data class", mustSyntax(`\bdata\s+class\b`)},

	// ---- Swift ----
	{"swift", 10, "Apple 框架导入", mustSyntax(`(?m)^\s*import\s+(UIKit|SwiftUI|Foundation|Combine)\b`)},
	{"swift", 8, "guard let", mustSyntax(`\bguard\s+let\b`)},
	{"swift", 8, "属性包装器", mustSyntax(`@(State|Published|Binding|ObservedObject|IBOutlet|IBAction|objc)\b`)},
	{"swift", 6, "func 定义", mustSyntax(`\bfunc\s+[A-Za-z_]\w*\s*\(`)},
	{"swift", 5, "类型标注声明", mustSyntax(`\b(let|var)\s+[A-Za-z_]\w*\s*:\s*[A-Z]\w*`)},

	// ---- C / C++ ----
	{"cpp", 10, "std:: 用法", mustSyntax(`\bstd::[a-z_]\w*`)},
	{"cpp", 8, "cout/cin", mustSyntax(`\bc(out|in)\s*(<<|>>)`)},
	{"cpp", 8, "template", mustSyntax(`\btemplate\s*<`)},
	{"c", 10, "include 头文件", mustSyntax(`(?m)^\s*#include\s*[<"][\w./]+[>"]`)},
	{"c", 8, "main 函数", mustSyntax(`\bint\s+main\s*\([^)]*\)\s*\{`)},
	{"c", 6, "printf/scanf", mustSyntax(`\b(printf|scanf|malloc|free|sizeof)\s*\(`)},

	// ---- C# ----
	{"csharp", 10, "using System", mustSyntax(`(?m)^\s*using\s+System(\.[\w.]+)?\s*;`)},
	{"csharp", 8, "namespace", mustSyntax(`\bnamespace\s+[A-Za-z_][\w.]*`)},
	{"csharp", 8, "Console.WriteLine", mustSyntax(`\bConsole\.(WriteLine|Write|ReadLine)\s*\(`)},
	{"csharp", 5, "public class", mustSyntax(`\bpublic\s+(sealed\s+|abstract\s+)?class\b`)},

	// ---- PHP ----
	{"php", 12, "PHP 起始标签", mustSyntax(`<\?php`)},
	{"php", 6, "变量 $", mustSyntax(`\$[a-zA-Z_]\w*\s*=`)},
	{"php", 6, "对象调用 ->", mustSyntax(`\$[a-zA-Z_]\w*->[a-zA-Z_]\w*`)},
	{"php", 6, "namespace 分号", mustSyntax(`(?m)^\s*namespace\s+[\w\\]+\s*;`)},

	// ---- Ruby ----
	{"ruby", 8, "def...end", mustSyntax(`(?m)^\s*def\s+[a-z_]\w*[!?]?`)},
	{"ruby", 8, "块 do", mustSyntax(`\.\w+\s+do\s*(\|[^|]*\|)?`)},
	{"ruby", 8, "attr_accessor", mustSyntax(`\battr_(accessor|reader|writer)\b`)},
	{"ruby", 6, "require", mustSyntax(`(?m)^\s*require(_relative)?\s+['"]`)},
	{"ruby", 5, "实例变量 @", mustSyntax(`@[a-z_]\w*\s*=`)},

	// ---- SQL（大小写不敏感）----
	{"sql", 10, "CREATE TABLE", mustSyntax(`(?i)\bcreate\s+(table|index|view)\b`)},
	{"sql", 8, "SELECT...FROM", mustSyntax(`(?is)\bselect\b.{1,200}\bfrom\b`)},
	{"sql", 8, "INSERT INTO", mustSyntax(`(?i)\binsert\s+into\b`)},
	{"sql", 8, "UPDATE...SET", mustSyntax(`(?is)\bupdate\b.{1,80}\bset\b`)},
	{"sql", 6, "JOIN", mustSyntax(`(?i)\b(inner|left|right|outer)\s+join\b`)},

	// ---- Shell ----
	{"shell", 12, "shebang", mustSyntax(`(?m)^#!\s*/(usr/)?bin/(env\s+)?(ba|z)?sh`)},
	{"shell", 8, "管道到工具", mustSyntax(`\|\s*(grep|awk|sed|xargs|cut|sort|uniq|head|tail)\b`)},
	{"shell", 6, "命令替换 $()", mustSyntax(`\$\([a-zA-Z_][\w ]*\)`)},
	{"shell", 5, "变量展开 ${}", mustSyntax(`\$\{[A-Za-z_]\w*\}`)},

	// ---- HTML ----
	{"html", 8, "闭合标签", mustSyntax(`(?i)</(div|span|body|html|head|section|button|form|table|ul|li)>`)},
	{"html", 6, "自定义/常见标签", mustSyntax(`(?i)<(div|span|button|input|img|a|form)\b[^>]*>`)},

	// ---- CSS ----
	{"css", 8, "选择器规则块", mustSyntax(`(?m)[.#]?[\w-]+\s*\{[^}]{0,200}[\w-]+\s*:\s*[^;}]+;`)},
	{"css", 6, "媒体查询/属性", mustSyntax(`@media\b|\b(margin|padding|display|flex|grid-template|background-color)\s*:`)},

	// ---- 带语言标签的代码围栏 ----
	// Markdown ```lang 围栏是"用户明确贴了代码"的强信号，且标签本身就点明了语言。
	// 放在最后，作为各语言语法规则之外的补充（例如围栏里只有一两行、
	// 未命中具体语法形态时，围栏标签仍能定性）。
	{"go", 10, "go 代码围栏", mustSyntax("(?i)```go\\b")},
	{"python", 10, "python 代码围栏", mustSyntax("(?i)```(python|py)\\b")},
	{"typescript", 10, "ts 代码围栏", mustSyntax("(?i)```(typescript|tsx?)\\b")},
	{"javascript", 10, "js 代码围栏", mustSyntax("(?i)```(javascript|jsx?)\\b")},
	{"rust", 10, "rust 代码围栏", mustSyntax("(?i)```(rust|rs)\\b")},
	{"java", 10, "java 代码围栏", mustSyntax("(?i)```java\\b")},
	{"kotlin", 10, "kotlin 代码围栏", mustSyntax("(?i)```(kotlin|kt)\\b")},
	{"swift", 10, "swift 代码围栏", mustSyntax("(?i)```swift\\b")},
	{"cpp", 10, "cpp 代码围栏", mustSyntax("(?i)```(cpp|c\\+\\+|cxx)\\b")},
	{"c", 9, "c 代码围栏", mustSyntax("(?i)```c\\b")},
	{"csharp", 10, "c# 代码围栏", mustSyntax("(?i)```(csharp|c#|cs)\\b")},
	{"php", 10, "php 代码围栏", mustSyntax("(?i)```php\\b")},
	{"ruby", 10, "ruby 代码围栏", mustSyntax("(?i)```(ruby|rb)\\b")},
	{"sql", 10, "sql 代码围栏", mustSyntax("(?i)```sql\\b")},
	{"shell", 10, "shell 代码围栏", mustSyntax("(?i)```(bash|sh|shell|zsh)\\b")},
	{"html", 9, "html 代码围栏", mustSyntax("(?i)```html\\b")},
	{"css", 9, "css 代码围栏", mustSyntax("(?i)```(css|scss|less)\\b")},
}

// langStack 把语言映射到技术栈方向，用于把语言识别结果并入方向判定。
// JS/TS/HTML/CSS 归前端（Node 后端由框架关键词修正），移动端语言独立成档。
var langStack = map[string]string{
	"go":         StackBackend,
	"python":     StackBackend,
	"rust":       StackBackend,
	"java":       StackBackend,
	"csharp":     StackBackend,
	"php":        StackBackend,
	"ruby":       StackBackend,
	"cpp":        StackBackend,
	"c":          StackBackend,
	"typescript": StackFrontend,
	"javascript": StackFrontend,
	"html":       StackFrontend,
	"css":        StackFrontend,
	"kotlin":     StackMobile,
	"swift":      StackMobile,
	"sql":        StackData,
	"shell":      StackInfra,
}

// langHit 是一门语言的语法命中汇总。
type langHit struct {
	Lang  string
	Score int
	Stack string
}

// detectCodeSyntax 在原文上跑所有语法规则，按语言聚合得分。
// 返回最高语言分（用作"这是代码"的强证据）与按分降序的语言列表。
// 语言列表顺序稳定（分数相同按名称），以保证证据指纹与测试可复现。
func detectCodeSyntax(raw string) (topScore int, hits []langHit) {
	if raw == "" {
		return 0, nil
	}
	scores := make(map[string]int, 8)
	for i := range codeSyntaxRules {
		rule := &codeSyntaxRules[i]
		if rule.Re.MatchString(raw) {
			scores[rule.Lang] += rule.Weight
		}
	}
	if len(scores) == 0 {
		return 0, nil
	}
	hits = make([]langHit, 0, len(scores))
	for lang, score := range scores {
		hits = append(hits, langHit{Lang: lang, Score: score, Stack: langStack[lang]})
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Score != hits[j].Score {
			return hits[i].Score > hits[j].Score
		}
		return hits[i].Lang < hits[j].Lang
	})
	return hits[0].Score, hits
}

// languageNames 取前 n 门语言的名称，供 languages 展示字段使用。
func languageNames(hits []langHit, n int) []string {
	if len(hits) == 0 {
		return nil
	}
	if n > len(hits) {
		n = len(hits)
	}
	names := make([]string, 0, n)
	for i := 0; i < n; i++ {
		names = append(names, hits[i].Lang)
	}
	return names
}
